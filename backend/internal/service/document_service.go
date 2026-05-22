package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/config"
	"backend/internal/model"
	"backend/internal/parser"
	"backend/internal/repository"
	"backend/internal/uuid"
)

type DocumentService struct {
	cfg   config.Config
	store repository.Store
	queue chan processTask
	wg    sync.WaitGroup
}

type processTask struct {
	documentID string
	rechunk    bool
}

func NewDocumentService(cfg config.Config, store repository.Store) (*DocumentService, error) {
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "documents"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "chunks"), 0o755); err != nil {
		return nil, err
	}
	service := &DocumentService{
		cfg:   cfg,
		store: store,
		queue: make(chan processTask, 32),
	}
	service.wg.Add(1)
	go service.run()
	return service, nil
}

func (s *DocumentService) Close() {
	close(s.queue)
	s.wg.Wait()
}

func (s *DocumentService) CreateDocument(file multipart.File, header *multipart.FileHeader, knowledgeBaseID, title string, tags []string) (model.Document, error) {
	if knowledgeBaseID == "" {
		return model.Document{}, errors.New("knowledge_base_id is required")
	}
	if header == nil || header.Filename == "" {
		return model.Document{}, errors.New("file is required")
	}

	sourceType, err := detectSourceType(header.Filename)
	if err != nil {
		return model.Document{}, err
	}

	content, err := io.ReadAll(file)
	if err != nil {
		return model.Document{}, err
	}
	if len(content) == 0 {
		return model.Document{}, errors.New("file is empty")
	}

	documentID := uuid.NewV7()
	dir := filepath.Join(s.cfg.DataDir, "documents", knowledgeBaseID, documentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return model.Document{}, err
	}

	filename := sanitizeFilename(header.Filename)
	storagePath := filepath.Join(dir, filename)
	if err := os.WriteFile(storagePath, content, 0o644); err != nil {
		return model.Document{}, err
	}

	sum := sha256.Sum256(content)
	now := time.Now().UTC()
	document := model.Document{
		DocumentID:      documentID,
		KnowledgeBaseID: knowledgeBaseID,
		Filename:        filename,
		Title:           strings.TrimSpace(title),
		Tags:            cleanTags(tags),
		SourceType:      sourceType,
		StoragePath:     storagePath,
		SHA256:          hex.EncodeToString(sum[:]),
		Status:          "uploaded",
		Stage:           "upload",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.store.CreateDocument(document); err != nil {
		_ = os.RemoveAll(dir)
		return model.Document{}, err
	}

	s.queue <- processTask{documentID: document.DocumentID}
	return document, nil
}

func (s *DocumentService) ListDocuments() []model.Document {
	return s.store.ListDocuments()
}

func (s *DocumentService) GetDocument(documentID string) (model.Document, error) {
	return s.store.GetDocument(documentID)
}

func (s *DocumentService) GetChunks(documentID string) ([]model.DocumentChunk, error) {
	return s.store.GetChunks(documentID)
}

func (s *DocumentService) DeleteDocument(documentID string) error {
	document, _, err := s.store.DeleteDocument(documentID)
	if err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Dir(document.StoragePath))
	if document.ChunkSnapshotPath != "" {
		_ = os.RemoveAll(filepath.Dir(document.ChunkSnapshotPath))
	}
	return nil
}

func (s *DocumentService) RechunkDocument(documentID string) error {
	document, err := s.store.GetDocument(documentID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	document.Status = "processing"
	document.Stage = "chunk"
	document.ErrorMessage = ""
	document.FinishedAt = nil
	document.UpdatedAt = now
	if err := s.store.UpdateDocument(document); err != nil {
		return err
	}

	s.queue <- processTask{documentID: documentID, rechunk: true}
	return nil
}

func (s *DocumentService) run() {
	defer s.wg.Done()
	for task := range s.queue {
		s.process(task)
	}
}

func (s *DocumentService) process(task processTask) {
	document, err := s.store.GetDocument(task.documentID)
	if err != nil {
		return
	}

	now := time.Now().UTC()
	document.Status = "processing"
	if task.rechunk {
		document.Stage = "chunk"
	} else {
		document.Stage = "parse"
	}
	document.StartedAt = &now
	document.UpdatedAt = now
	document.ErrorMessage = ""
	_ = s.store.UpdateDocument(document)

	parsed, err := parser.Parse(document.StoragePath, document.SourceType)
	if err != nil {
		s.failDocument(document, "parse", err)
		return
	}

	cleaned := parser.CleanText(parsed.Text)
	if cleaned == "" {
		s.failDocument(document, "chunk", errors.New("document content is empty after cleanup"))
		return
	}

	document.Stage = "chunk"
	document.UpdatedAt = time.Now().UTC()
	_ = s.store.UpdateDocument(document)

	chunkVersion := 1
	if task.rechunk && document.ChunkVersion > 0 {
		chunkVersion = document.ChunkVersion + 1
	}

	chunks := BuildChunks(document.DocumentID, document.Filename, cleaned, chunkVersion)
	if len(chunks) == 0 {
		s.failDocument(document, "chunk", errors.New("chunk result is empty"))
		return
	}

	snapshotPath, err := s.writeSnapshot(document, chunks, chunkVersion)
	if err != nil {
		s.failDocument(document, "chunk", err)
		return
	}

	if err := s.store.ReplaceChunks(document.DocumentID, chunks); err != nil {
		s.failDocument(document, "chunk", err)
		return
	}

	done := time.Now().UTC()
	document.Status = "review_pending"
	document.Stage = "done"
	document.PageCount = parsed.PageCount
	document.ChunkCount = len(chunks)
	document.ChunkVersion = chunkVersion
	document.ChunkConfigHash = chunkConfigHash()
	document.ChunkSnapshotPath = snapshotPath
	document.FinishedAt = &done
	document.UpdatedAt = done
	_ = s.store.UpdateDocument(document)
}

func (s *DocumentService) writeSnapshot(document model.Document, chunks []model.DocumentChunk, chunkVersion int) (string, error) {
	dir := filepath.Join(s.cfg.DataDir, "chunks", document.KnowledgeBaseID, document.DocumentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "chunks-v"+strconv.Itoa(chunkVersion)+".json")
	snapshot := model.ChunkSnapshot{
		DocumentID:      document.DocumentID,
		KnowledgeBaseID: document.KnowledgeBaseID,
		ChunkVersion:    chunkVersion,
		SourceSHA256:    document.SHA256,
		ChunkConfigHash: chunkConfigHash(),
		CreatedAt:       time.Now().UTC(),
		Chunks:          chunks,
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, raw, 0o644)
}

func (s *DocumentService) failDocument(document model.Document, stage string, reason error) {
	now := time.Now().UTC()
	document.Status = "failed"
	document.Stage = stage
	document.ErrorMessage = reason.Error()
	document.FinishedAt = &now
	document.UpdatedAt = now
	_ = s.store.UpdateDocument(document)
}

func detectSourceType(filename string) (string, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown":
		return "markdown", nil
	case ".docx":
		return "docx", nil
	case ".pptx":
		return "pptx", nil
	case ".pdf":
		return "pdf", nil
	default:
		return "", errors.New("unsupported file type")
	}
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, " ", "_")
	if name == "." || name == "" {
		return "document.bin"
	}
	return name
}

func cleanTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			out = append(out, tag)
		}
	}
	return out
}
