package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"

	"backend/internal/embedder"
	"backend/internal/llm"
	"backend/internal/logger"
	"backend/internal/model"
	"backend/internal/parser"
	"backend/internal/queue"
	"backend/internal/repository"
	"backend/internal/uuid"
	"backend/internal/vectorstore"
)

type DocumentService struct {
	cfg         *viper.Viper
	store       repository.Store
	embedder    embedder.Embedder
	vectorStore vectorstore.VectorStore
	llm         llm.LLM
	taskQueue   queue.TaskQueue
	indexWg     sync.WaitGroup
}

func NewDocumentService(cfg *viper.Viper, store repository.Store, opts ...func(*DocumentService)) (*DocumentService, error) {
	dataDir := cfg.GetString("app.data_dir")
	if err := os.MkdirAll(filepath.Join(dataDir, "documents"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "chunks"), 0o755); err != nil {
		return nil, err
	}
	svc := &DocumentService{
		cfg:         cfg,
		store:       store,
		embedder:    embedder.Noop{},
		vectorStore: vectorstore.Noop{},
		llm:         llm.Noop{},
	}
	for _, opt := range opts {
		opt(svc)
	}
	if svc.taskQueue == nil {
		if redisDSN := cfg.GetString("redis.dsn"); redisDSN != "" {
			logger.L.Info("task queue: asynq", zap.String("redis", redisDSN))
			tq, err := queue.NewAsynqQueue(redisDSN, 2, svc.processDocument)
			if err != nil {
				return nil, err
			}
			svc.taskQueue = tq
		} else {
			logger.L.Info("task queue: goroutine")
			svc.taskQueue = queue.NewGoroutineQueue(1, svc.processDocument)
		}
	}
	return svc, nil
}

func WithEmbedder(e embedder.Embedder) func(*DocumentService) {
	return func(s *DocumentService) { s.embedder = e }
}

func WithVectorStore(vs vectorstore.VectorStore) func(*DocumentService) {
	return func(s *DocumentService) { s.vectorStore = vs }
}

func WithTaskQueue(tq queue.TaskQueue) func(*DocumentService) {
	return func(s *DocumentService) { s.taskQueue = tq }
}

func WithLLM(l llm.LLM) func(*DocumentService) {
	return func(s *DocumentService) { s.llm = l }
}

func (s *DocumentService) Close() {
	s.taskQueue.Shutdown()
	s.indexWg.Wait()
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
	dir := filepath.Join(s.cfg.GetString("app.data_dir"), "documents", knowledgeBaseID, documentID)
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

	_ = s.taskQueue.Enqueue(document.DocumentID, false)
	return document, nil
}

func (s *DocumentService) ListDocuments(knowledgeBaseID string) []model.Document {
	return s.store.ListDocuments(knowledgeBaseID)
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
	if document.Status == "indexed" {
		_ = s.vectorStore.DeleteByDocument(context.Background(), document.KnowledgeBaseID, document.DocumentID)
	}
	return nil
}

func (s *DocumentService) ApproveChunks(documentID string) error {
	document, err := s.store.GetDocument(documentID)
	if err != nil {
		return err
	}
	chunks, err := s.store.GetChunks(documentID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for i := range chunks {
		if chunks[i].Status == "draft" {
			chunks[i].Status = "approved"
			chunks[i].UpdatedAt = now
			if err := s.store.UpdateChunk(chunks[i]); err != nil {
				return err
			}
		}
	}
	document.Status = "approved"
	document.UpdatedAt = now
	return s.store.UpdateDocument(document)
}

func (s *DocumentService) RejectChunk(documentID, chunkID string) error {
	chunk, err := s.store.GetChunk(chunkID)
	if err != nil {
		return err
	}
	if chunk.DocumentID != documentID {
		return repository.ErrNotFound
	}
	now := time.Now().UTC()
	chunk.Status = "rejected"
	chunk.IsCurrent = false
	chunk.UpdatedAt = now
	return s.store.UpdateChunk(chunk)
}

func (s *DocumentService) MergeChunks(documentID string, chunkIDs []string) error {
	if len(chunkIDs) < 2 {
		return errors.New("merge requires at least 2 chunk IDs")
	}

	chunks, err := s.store.GetChunks(documentID)
	if err != nil {
		return err
	}

	idSet := make(map[string]bool, len(chunkIDs))
	for _, id := range chunkIDs {
		idSet[id] = true
	}

	var selected []model.DocumentChunk
	for _, c := range chunks {
		if idSet[c.ChunkID] {
			selected = append(selected, c)
		}
	}
	if len(selected) != len(chunkIDs) {
		return errors.New("one or more chunk IDs not found")
	}

	// verify consecutive chunk_index
	for i := 1; i < len(selected); i++ {
		if selected[i].ChunkIndex != selected[i-1].ChunkIndex+1 {
			return errors.New("chunks must be adjacent (consecutive chunk_index)")
		}
	}

	var texts []string
	for _, c := range selected {
		texts = append(texts, c.Text)
	}
	merged := strings.Join(texts, "\n\n")

	now := time.Now().UTC()

	// mark old chunks not current
	for _, c := range selected {
		c.IsCurrent = false
		c.UpdatedAt = now
		if err := s.store.UpdateChunk(c); err != nil {
			return err
		}
	}

	// create merged chunk at the first chunk's index position
	newChunk := model.DocumentChunk{
		ChunkID:        uuid.NewV7(),
		CreatedAt:      now,
		UpdatedAt:      now,
		DocumentID:     documentID,
		ChunkIndex:     selected[0].ChunkIndex,
		SectionTitle:   selected[0].SectionTitle,
		PageStart:      selected[0].PageStart,
		PageEnd:        selected[len(selected)-1].PageEnd,
		Text:           merged,
		NormalizedText: merged,
		Status:         "draft",
		ChunkVersion:   selected[0].ChunkVersion,
		Source:         "manual",
		IsCurrent:      true,
		Filename:       selected[0].Filename,
		ResourceRefs:   []model.ResourceRef{},
	}

	// persist: build new full chunk list for this document
	var updated []model.DocumentChunk
	inserted := false
	for _, c := range chunks {
		if !idSet[c.ChunkID] {
			updated = append(updated, c)
		} else if !inserted {
			updated = append(updated, newChunk)
			inserted = true
		}
	}

	return s.store.ReplaceChunks(documentID, updated)
}

func (s *DocumentService) IndexDocument(documentID string) error {
	document, err := s.store.GetDocument(documentID)
	if err != nil {
		return err
	}
	if document.Status != "approved" {
		return errors.New("document must be approved before indexing")
	}

	now := time.Now().UTC()
	document.Status = "processing"
	document.Stage = "embed"
	document.UpdatedAt = now
	document.ErrorMessage = ""
	if err := s.store.UpdateDocument(document); err != nil {
		return err
	}

	s.indexWg.Add(1)
	go func() {
		defer s.indexWg.Done()
		s.runIndex(document)
	}()
	return nil
}

func (s *DocumentService) runIndex(document model.Document) {
	logger.L.Info("indexing document", zap.String("document_id", document.DocumentID))

	chunks, err := s.store.GetChunks(document.DocumentID)
	if err != nil {
		s.failDocument(document, "embed", err)
		return
	}

	var approved []model.DocumentChunk
	for _, c := range chunks {
		if c.Status == "approved" && c.IsCurrent {
			approved = append(approved, c)
		}
	}
	if len(approved) == 0 {
		s.failDocument(document, "embed", errors.New("no approved chunks to index"))
		return
	}

	texts := make([]string, len(approved))
	for i, c := range approved {
		texts[i] = c.Text
	}

	embeddings, err := s.embedder.Embed(context.Background(), texts)
	if err != nil {
		s.failDocument(document, "embed", err)
		return
	}

	now := time.Now().UTC()
	for i := range approved {
		approved[i].EmbeddingModel = s.embedder.Model()
		approved[i].UpdatedAt = now
		if err := s.store.UpdateChunk(approved[i]); err != nil {
			s.failDocument(document, "embed", err)
			return
		}
	}

	document.Stage = "index"
	document.UpdatedAt = now
	_ = s.store.UpdateDocument(document)

	records := vectorstore.BuildRecords(document, approved, embeddings)
	if err := s.vectorStore.Upsert(context.Background(), records); err != nil {
		s.failDocument(document, "index", err)
		return
	}

	done := time.Now().UTC()
	document.Status = "indexed"
	document.Stage = "done"
	document.FinishedAt = &done
	document.UpdatedAt = done
	_ = s.store.UpdateDocument(document)

	logger.L.Info("document indexed",
		zap.String("document_id", document.DocumentID),
		zap.Int("vectors", len(records)),
		zap.String("model", s.embedder.Model()),
	)
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

	_ = s.taskQueue.Enqueue(documentID, true)
	return nil
}

func (s *DocumentService) processDocument(documentID string, rechunk bool) {
	document, err := s.store.GetDocument(documentID)
	if err != nil {
		return
	}

	now := time.Now().UTC()
	document.Status = "processing"
	if rechunk {
		document.Stage = "chunk"
	} else {
		document.Stage = "parse"
	}
	document.StartedAt = &now
	document.UpdatedAt = now
	document.ErrorMessage = ""
	_ = s.store.UpdateDocument(document)

	logger.L.Info("processing document",
		zap.String("document_id", document.DocumentID),
		zap.String("source_type", document.SourceType),
		zap.Bool("rechunk", rechunk),
	)

	parsed, err := parser.Parse(document.StoragePath, document.SourceType)
	if err != nil {
		logger.L.Warn("parse failed", zap.String("document_id", document.DocumentID), zap.Error(err))
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
	if rechunk && document.ChunkVersion > 0 {
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

	logger.L.Info("document processed",
		zap.String("document_id", document.DocumentID),
		zap.Int("chunks", len(chunks)),
		zap.Int("chunk_version", chunkVersion),
	)

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
	dir := filepath.Join(s.cfg.GetString("app.data_dir"), "chunks", document.KnowledgeBaseID, document.DocumentID)
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

// QueryResult bundles semantic search hits with an optional LLM-generated answer.
type QueryResult struct {
	Items  []vectorstore.SearchResult
	Answer string
}

func (s *DocumentService) Query(knowledgeBaseID, queryText string, topK int) (QueryResult, error) {
	if strings.TrimSpace(queryText) == "" {
		return QueryResult{}, errors.New("query is required")
	}
	if topK <= 0 {
		topK = 5
	}
	if topK > 50 {
		topK = 50
	}

	embeddings, err := s.embedder.Embed(context.Background(), []string{queryText})
	if err != nil {
		return QueryResult{}, fmt.Errorf("embed query: %w", err)
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return QueryResult{}, errors.New("embedder returned no vector for query; configure embedder.base_url and embedder.api_key")
	}

	hits, err := s.vectorStore.Search(context.Background(), knowledgeBaseID, embeddings[0], topK)
	if err != nil {
		return QueryResult{}, fmt.Errorf("vector search: %w", err)
	}

	answer, err := s.generateAnswer(queryText, hits)
	if err != nil {
		logger.L.Warn("llm answer generation failed", zap.Error(err))
		// non-fatal: return hits without answer
	}

	return QueryResult{Items: hits, Answer: answer}, nil
}

const ragSystemPrompt = `You are a helpful assistant. Answer the user's question based only on the provided context.
If the context does not contain enough information to answer the question, say so clearly.
Be concise and accurate.`

func (s *DocumentService) generateAnswer(query string, hits []vectorstore.SearchResult) (string, error) {
	if len(hits) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("Context:\n")
	for i, h := range hits {
		sb.WriteString(fmt.Sprintf("---\n[%d] %s", i+1, h.Text))
		if h.SectionTitle != "" {
			sb.WriteString(fmt.Sprintf(" (Section: %s)", h.SectionTitle))
		}
		if h.PageStart > 0 {
			sb.WriteString(fmt.Sprintf(" (Page %d)", h.PageStart))
		}
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("---\n\nQuestion: %s", query))

	return s.llm.Complete(context.Background(), ragSystemPrompt, sb.String())
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
