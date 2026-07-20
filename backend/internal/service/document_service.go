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

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/d2jvkpn/rag/backend/internal/infra"
	"github.com/d2jvkpn/rag/backend/internal/model"
	"github.com/d2jvkpn/rag/backend/internal/queue"
	"github.com/d2jvkpn/rag/backend/internal/repository"
	"github.com/d2jvkpn/rag/backend/pkg/rag"
	"github.com/d2jvkpn/rag/backend/pkg/rag/parser"
)

// docStore is the subset of repository.Store used by DocumentService.
type docStore interface {
	repository.KnowledgeBaseStore
	repository.DocumentStore
	repository.ChunkStore
}

type DocumentService struct {
	cfg         *viper.Viper
	store       docStore
	embedder    rag.Embedder
	vectorStore rag.VectorStore
	taskQueue   queue.TaskQueue
	wg          sync.WaitGroup
}

func NewDocumentService(
	cfg *viper.Viper,
	store docStore,
	opts ...func(*DocumentService),
) (*DocumentService, error) {
	dataDir := cfg.GetString("app.data_dir")
	if err := os.MkdirAll(filepath.Join(dataDir, "documents"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "static"), 0o755); err != nil {
		return nil, err
	}
	svc := &DocumentService{
		cfg:         cfg,
		store:       store,
		embedder:    rag.NoopEmbedder{},
		vectorStore: rag.NoopVectorStore{},
	}
	for _, opt := range opts {
		opt(svc)
	}
	if svc.DefaultKnowledgeBaseDim() <= 0 {
		return nil, errors.New("embedder.dim is required and must be positive")
	}
	err := store.EnsureKnowledgeBasesFromDocuments(
		svc.DefaultKnowledgeBaseDim(), svc.DefaultKnowledgeBaseModel(),
	)
	if err != nil {
		return nil, err
	}
	for _, kb := range store.ListKnowledgeBases() {
		if err := svc.vectorStore.CreateKnowledgeBase(
			context.Background(),
			collectionConfigFromKnowledgeBase(kb),
		); err != nil {
			return nil, err
		}
	}
	if svc.taskQueue == nil {
		if redisDSN := cfg.GetString("redis.dsn"); redisDSN != "" {
			infra.L.Info("task queue: asynq", zap.String("redis", infra.RedactDSN(redisDSN)))
			tq, err := queue.NewAsynqQueue(redisDSN, 2, svc.processDocument)
			if err != nil {
				return nil, err
			}
			svc.taskQueue = tq
		} else {
			infra.L.Info("task queue: goroutine")
			svc.taskQueue = queue.NewGoroutineQueue(1, svc.processDocument)
		}
	}
	return svc, nil
}

func WithEmbedder(e rag.Embedder) func(*DocumentService) {
	return func(s *DocumentService) { s.embedder = e }
}

func WithVectorStore(vs rag.VectorStore) func(*DocumentService) {
	return func(s *DocumentService) { s.vectorStore = vs }
}

func WithTaskQueue(tq queue.TaskQueue) func(*DocumentService) {
	return func(s *DocumentService) { s.taskQueue = tq }
}

func (s *DocumentService) VectorStore() rag.VectorStore {
	return s.vectorStore
}

func (s *DocumentService) collectionCfg(kbID string) rag.CollectionConfig {
	kb, err := s.store.GetKnowledgeBase(kbID)
	if err != nil {
		return defaultCollectionConfig(kbID, s.cfg.GetInt("embedder.dim"))
	}
	return collectionConfigFromKnowledgeBase(kb)
}

func defaultCollectionConfig(kbID string, dim int) rag.CollectionConfig {
	return rag.CollectionConfig{
		Name:         kbID,
		Dim:          dim,
		Analyzer:     "chinese",
		ChunkSize:    DefaultChunkSize,
		ChunkOverlap: DefaultChunkOverlap,
		MinChunks:    DefaultMinChunks,
	}
}

func collectionConfigFromKnowledgeBase(kb model.KnowledgeBase) rag.CollectionConfig {
	return rag.CollectionConfig{
		Name:         kb.KnowledgeBaseID,
		Dim:          kb.Dim,
		Analyzer:     kb.Analyzer,
		ChunkSize:    kb.ChunkSize,
		ChunkOverlap: kb.ChunkOverlap,
		MinChunks:    kb.MinChunks,
	}
}

func (s *DocumentService) Close() {
	s.taskQueue.Shutdown()
	s.wg.Wait()
}

func (s *DocumentService) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.Close()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *DocumentService) CreateDocument(
	file multipart.File,
	header *multipart.FileHeader,
	knowledgeBaseID, title string,
	tags []string,
	humanReview bool,
	uploaderID, uploaderName string,
) (model.Document, error) {
	if knowledgeBaseID == "" {
		return model.Document{}, errors.New("knowledge_base_id is required")
	}
	if _, err := s.store.GetKnowledgeBase(knowledgeBaseID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return model.Document{}, fmt.Errorf("knowledge base %q does not exist", knowledgeBaseID)
		}
		return model.Document{}, err
	}
	if header == nil || header.Filename == "" {
		return model.Document{}, errors.New("file is required")
	}

	fileType, err := detectFileType(header.Filename)
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

	now := time.Now().UTC()
	documentID := uuid.Must(uuid.NewV7()).String()
	dir := documentStorageDir(s.cfg.GetString("app.data_dir"), documentID, now)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return model.Document{}, err
	}

	filename := sanitizeFilename(header.Filename)
	storagePath := filepath.Join(dir, filename)
	if err := os.WriteFile(storagePath, content, 0o644); err != nil {
		return model.Document{}, err
	}

	sum := sha256.Sum256(content)
	cleanedTags, err := cleanTags(tags)
	if err != nil {
		_ = os.RemoveAll(dir)
		return model.Document{}, err
	}

	document := model.Document{
		DocumentID:      documentID,
		KnowledgeBaseID: knowledgeBaseID,
		Filename:        filename,
		Title:           strings.TrimSpace(title),
		Tags:            cleanedTags,
		FileType:        fileType,
		StoragePath:     storagePath,
		SHA256:          hex.EncodeToString(sum[:]),
		Status:          "uploaded",
		Stage:           "upload",
		HumanReview:     humanReview,
		UploaderID:      uploaderID,
		UploaderName:    uploaderName,
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

func (s *DocumentService) CreateKnowledgeBase(
	kbID, analyzer string,
	chunkSize, chunkOverlap, minChunks int,
	createdBy string,
) (model.KnowledgeBase, error) {
	var err error

	kbID = strings.TrimSpace(kbID)
	analyzer = strings.TrimSpace(analyzer)
	if analyzer == "" {
		analyzer = "chinese"
	}
	err = validateKnowledgeBaseInput(kbID, analyzer, chunkSize, chunkOverlap, minChunks)
	if err != nil {
		return model.KnowledgeBase{}, err
	}
	if _, err := s.store.GetKnowledgeBase(kbID); err == nil {
		return model.KnowledgeBase{}, ErrKnowledgeBaseExists
	} else if !errors.Is(err, repository.ErrNotFound) {
		return model.KnowledgeBase{}, err
	}

	now := time.Now().UTC()
	kb := model.KnowledgeBase{
		KnowledgeBaseID: kbID,
		CreatedAt:       now,
		UpdatedAt:       now,
		CreatedBy:       createdBy,
		Dim:             s.cfg.GetInt("embedder.dim"),
		Model:           s.DefaultKnowledgeBaseModel(),
		Analyzer:        analyzer,
		ChunkSize:       chunkSize,
		ChunkOverlap:    chunkOverlap,
		MinChunks:       minChunks,
	}
	if kb.Dim <= 0 {
		return model.KnowledgeBase{}, errors.New("embedder.dim is required and must be positive")
	}
	err = s.vectorStore.CreateKnowledgeBase(
		context.Background(), collectionConfigFromKnowledgeBase(kb),
	)
	if err != nil {
		return model.KnowledgeBase{}, err
	}
	if err := s.store.CreateKnowledgeBase(kb); err != nil {
		_ = s.vectorStore.DeleteKnowledgeBase(context.Background(), kb.KnowledgeBaseID)
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") ||
			strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return model.KnowledgeBase{}, ErrKnowledgeBaseExists
		}
		return model.KnowledgeBase{}, err
	}
	return kb, nil
}

func (s *DocumentService) DefaultKnowledgeBaseDim() int {
	return s.cfg.GetInt("embedder.dim")
}

func (s *DocumentService) DefaultKnowledgeBaseModel() string {
	return s.cfg.GetString("embedder.model")
}

func (s *DocumentService) ListKnowledgeBases() []model.KnowledgeBase {
	return s.store.ListKnowledgeBases()
}

func (s *DocumentService) ListAvailableKnowledgeBases() []model.KnowledgeBase {
	return s.ListKnowledgeBases()
}

var ErrKnowledgeBaseExists = errors.New("knowledge base already exists")
var ErrKnowledgeBaseNotEmpty = errors.New(
	"knowledge base still has documents; delete all documents before deleting the knowledge base",
)

func (s *DocumentService) DeleteKnowledgeBase(kbID string) error {
	if _, err := s.store.GetKnowledgeBase(kbID); err != nil {
		return err
	}
	docs, err := s.store.ListDocuments(kbID, "")
	if err != nil {
		return err
	}
	if len(docs) > 0 {
		return ErrKnowledgeBaseNotEmpty
	}
	if err := s.vectorStore.DeleteKnowledgeBase(context.Background(), kbID); err != nil {
		return err
	}
	return s.store.DeleteKnowledgeBase(kbID)
}

func validateKnowledgeBaseInput(kbID, analyzer string, chunkSize, chunkOverlap, minChunks int) error {
	if kbID == "" {
		return errors.New("knowledge_base_id is required")
	}
	if len(kbID) > 63 {
		return errors.New("knowledge_base_id must be at most 63 characters")
	}
	for _, r := range kbID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return errors.New("knowledge_base_id may only contain letters, numbers, underscore, and hyphen")
	}
	switch analyzer {
	case "chinese", "english", "standard":
	default:
		return errors.New("analyzer must be one of chinese, english, standard")
	}
	if chunkSize <= 0 {
		return errors.New("chunk_size must be positive")
	}
	if chunkOverlap < 0 {
		return errors.New("chunk_overlap must be zero or positive")
	}
	if chunkOverlap >= chunkSize {
		return errors.New("chunk_overlap must be less than chunk_size")
	}
	if minChunks <= 0 {
		return errors.New("min_chunks must be positive")
	}
	return nil
}

func (s *DocumentService) ListDocuments(knowledgeBaseID, tag string) ([]model.Document, error) {
	return s.store.ListDocuments(knowledgeBaseID, tag)
}

func (s *DocumentService) ListDocumentsPage(
	knowledgeBaseID, tag, status string,
	page, pageSize int,
) (model.DocumentPage, error) {
	return s.store.ListDocumentsPage(knowledgeBaseID, tag, status, page, pageSize)
}

func (s *DocumentService) ListDocumentTags(knowledgeBaseID string) ([]model.DocumentTagCount, error) {
	return s.store.ListDocumentTags(knowledgeBaseID)
}

func (s *DocumentService) GetDocument(documentID string) (model.Document, error) {
	return s.store.GetDocument(documentID)
}

func (s *DocumentService) GetChunks(documentID string) ([]model.DocumentChunk, error) {
	return s.store.GetChunks(documentID)
}

func (s *DocumentService) ListChunksPage(documentID string, page, pageSize int) (
	model.DocumentChunkPage, error) {
	return s.store.ListChunksPage(documentID, page, pageSize)
}

func (s *DocumentService) DeleteDocument(documentID string) error {
	document, _, err := s.store.DeleteDocument(documentID)
	if err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Dir(document.StoragePath))
	if document.ChunkSnapshotPath != "" &&
		filepath.Clean(
			filepath.Dir(document.ChunkSnapshotPath),
		) != filepath.Clean(
			filepath.Dir(document.StoragePath),
		) {
		_ = os.RemoveAll(filepath.Dir(document.ChunkSnapshotPath))
	}
	_ = os.RemoveAll(
		staticStorageDir(s.cfg.GetString("app.data_dir"), document.DocumentID, document.CreatedAt),
	)
	_ = os.RemoveAll(filepath.Join(s.cfg.GetString("app.data_dir"), "static", document.DocumentID))
	_ = s.vectorStore.DeleteByDocument(
		context.Background(),
		document.KnowledgeBaseID,
		document.DocumentID,
	)
	return nil
}

func (s *DocumentService) ApproveChunks(documentID string) error {
	document, err := s.store.GetDocument(documentID)
	if err != nil {
		return err
	}
	if document.Status != "review_pending" {
		return fmt.Errorf("document is not in review_pending state (current: %s)", document.Status)
	}
	chunks, err := s.store.GetChunks(documentID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var toApprove []model.DocumentChunk
	alreadyApproved := 0
	for i := range chunks {
		if chunks[i].Status == "draft" {
			chunks[i].Status = "approved"
			chunks[i].UpdatedAt = now
			toApprove = append(toApprove, chunks[i])
		} else if chunks[i].Status == "approved" {
			alreadyApproved++
		}
	}
	if len(toApprove) == 0 && alreadyApproved == 0 {
		return errors.New("no chunks to approve: all chunks have been rejected")
	}
	if err := s.store.BulkUpdateChunks(toApprove); err != nil {
		return err
	}
	document.Status = "approved"
	document.UpdatedAt = now
	if err := s.store.UpdateDocument(document); err != nil {
		return err
	}
	return s.IndexDocument(document.DocumentID)
}

func (s *DocumentService) EditChunk(documentID, chunkID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("chunk text cannot be empty")
	}
	document, err := s.store.GetDocument(documentID)
	if err != nil {
		return err
	}
	if document.Status == "indexed" {
		return errors.New("cannot edit chunks of an indexed document")
	}
	chunk, err := s.store.GetChunk(chunkID)
	if err != nil {
		return err
	}
	if chunk.DocumentID != documentID {
		return repository.ErrNotFound
	}
	if chunk.Status == "rejected" {
		return errors.New("cannot edit a rejected chunk")
	}
	now := time.Now().UTC()
	chunk.Text = text
	chunk.NormalizedText = text
	chunk.Source = "manual"
	chunk.UpdatedAt = now
	return s.store.UpdateChunk(chunk)
}

func (s *DocumentService) RejectChunk(documentID, chunkID string) error {
	document, err := s.store.GetDocument(documentID)
	if err != nil {
		return err
	}
	if document.Status == "indexed" {
		return errors.New("cannot reject chunks of an indexed document")
	}
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

func (s *DocumentService) RestoreChunk(documentID, chunkID string) error {
	document, err := s.store.GetDocument(documentID)
	if err != nil {
		return err
	}
	if document.Status == "indexed" {
		return errors.New("cannot restore chunks of an indexed document")
	}
	chunk, err := s.store.GetChunk(chunkID)
	if err != nil {
		return err
	}
	if chunk.DocumentID != documentID {
		return repository.ErrNotFound
	}
	if chunk.Status != "rejected" {
		return errors.New("chunk is not rejected")
	}
	now := time.Now().UTC()
	chunk.Status = "draft"
	chunk.IsCurrent = true
	chunk.UpdatedAt = now
	return s.store.UpdateChunk(chunk)
}

func (s *DocumentService) MergeChunks(documentID string, chunkIDs []string) error {
	if len(chunkIDs) < 2 {
		return errors.New("merge requires at least 2 chunk IDs")
	}

	document, err := s.store.GetDocument(documentID)
	if err != nil {
		return err
	}
	if document.Status == "indexed" {
		return errors.New("cannot merge chunks of an indexed document")
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

	// create merged chunk at the first chunk's index position
	newChunk := model.DocumentChunk{
		ChunkID:        uuid.Must(uuid.NewV7()).String(),
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
	if document.Status != "approved" && document.Status != "failed" {
		return errors.New("document must be in approved or failed state to trigger indexing")
	}
	if document.Status == "failed" {
		chunks, err := s.store.GetChunks(documentID)
		if err != nil {
			return err
		}
		hasApproved := false
		for _, c := range chunks {
			if c.Status == "approved" && c.IsCurrent {
				hasApproved = true
				break
			}
		}
		if !hasApproved {
			return errors.New("document has no approved chunks; re-process before indexing")
		}
	}

	now := time.Now().UTC()
	document.Status = "processing"
	document.Stage = "embed"
	document.UpdatedAt = now
	document.ErrorMessage = ""
	if err := s.store.UpdateDocument(document); err != nil {
		return err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runIndex(document)
	}()
	return nil
}

func (s *DocumentService) runIndex(document model.Document) {
	infra.L.Info("indexing document", zap.String("document_id", document.DocumentID))

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
		approved[i].Embedding = embeddings[i]
		approved[i].UpdatedAt = now
	}
	if err := s.store.BulkUpdateChunks(approved); err != nil {
		s.failDocument(document, "embed", err)
		return
	}

	snapshotPath, err := s.writeSnapshot(
		document,
		approved,
		document.ChunkVersion,
		document.ChunkConfigHash,
	)
	if err != nil {
		infra.L.Warn(
			"write snapshot failed",
			zap.String("document_id", document.DocumentID),
			zap.Error(err),
		)
	} else {
		document.ChunkSnapshotPath = snapshotPath
	}

	document.Stage = "index"
	document.UpdatedAt = now
	if err := s.store.UpdateDocument(document); err != nil {
		infra.L.Warn(
			"failed to persist index stage",
			zap.String("document_id", document.DocumentID),
			zap.Error(err),
		)
	}

	records := rag.BuildRecords(document, approved, embeddings)
	if err := s.vectorStore.Upsert(context.Background(), records); err != nil {
		s.failDocument(document, "index", err)
		return
	}

	done := time.Now().UTC()
	document.Status = "indexed"
	document.Stage = "done"
	document.FinishedAt = &done
	document.UpdatedAt = done
	if err := s.store.UpdateDocument(document); err != nil {
		infra.L.Warn(
			"failed to persist indexed status",
			zap.String("document_id", document.DocumentID),
			zap.Error(err),
		)
	}

	infra.L.Info("document indexed",
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
	if document.Status == "indexed" {
		return errors.New("document is already indexed; delete and re-upload to reprocess")
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
	if err := s.store.UpdateDocument(document); err != nil {
		infra.L.Warn(
			"failed to persist processing status",
			zap.String("document_id", document.DocumentID),
			zap.Error(err),
		)
	}

	infra.L.Info("processing document",
		zap.String("document_id", document.DocumentID),
		zap.String("file_type", document.FileType),
		zap.Bool("rechunk", rechunk),
	)

	mediaDir := staticStorageDir(
		s.cfg.GetString("app.data_dir"),
		document.DocumentID,
		document.CreatedAt,
	)
	parsed, err := parser.Parse(document.StoragePath, document.FileType, mediaDir)
	if err != nil {
		infra.L.Warn("parse failed",
			zap.String("document_id", document.DocumentID),
			zap.String("filename", document.Filename),
			zap.String("storage_path", document.StoragePath),
			zap.String("file_type", document.FileType),
			zap.Error(err),
		)
		s.failDocument(document, "parse", err)
		return
	}

	var blocks []parser.ParseBlock
	if len(parsed.Blocks) > 0 {
		for i := range parsed.Blocks {
			parsed.Blocks[i].Text = parser.CleanText(parsed.Blocks[i].Text)
		}
		for _, b := range parsed.Blocks {
			if strings.TrimSpace(b.Text) != "" {
				blocks = append(blocks, b)
			}
		}
	} else {
		cleaned := parser.CleanText(parsed.Text)
		if cleaned != "" {
			blocks = []parser.ParseBlock{{Text: cleaned}}
		}
	}
	if len(blocks) == 0 {
		s.failDocument(document, "chunk", errors.New("document content is empty after cleanup"))
		return
	}

	document.Stage = "chunk"
	document.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateDocument(document); err != nil {
		infra.L.Warn(
			"failed to persist chunk stage",
			zap.String("document_id", document.DocumentID),
			zap.Error(err),
		)
	}

	chunkVersion := 1
	if rechunk && document.ChunkVersion > 0 {
		chunkVersion = document.ChunkVersion + 1
	}

	colCfg := s.collectionCfg(document.KnowledgeBaseID)
	cfgHash := chunkConfigHash(colCfg.ChunkSize, colCfg.ChunkOverlap, colCfg.MinChunks)
	chunks := BuildChunks(
		document.DocumentID,
		blocks,
		chunkVersion,
		colCfg.ChunkSize,
		colCfg.ChunkOverlap,
		colCfg.MinChunks,
		!document.HumanReview,
	)
	if len(chunks) == 0 {
		s.failDocument(document, "chunk", errors.New("chunk result is empty"))
		return
	}

	if err := s.store.ReplaceChunks(document.DocumentID, chunks); err != nil {
		s.failDocument(document, "chunk", err)
		return
	}

	infra.L.Info("document processed",
		zap.String("document_id", document.DocumentID),
		zap.Int("chunks", len(chunks)),
		zap.Int("chunk_version", chunkVersion),
	)

	done := time.Now().UTC()
	document.Status = "review_pending"
	if !document.HumanReview {
		document.Status = "approved"
	}
	document.Stage = "done"
	document.PageCount = parsed.PageCount
	document.ChunkCount = len(chunks)
	document.ChunkVersion = chunkVersion
	document.ChunkConfigHash = cfgHash
	document.ChunkSnapshotPath = ""
	document.FinishedAt = &done
	document.UpdatedAt = done
	if err := s.store.UpdateDocument(document); err != nil {
		infra.L.Warn(
			"failed to persist processed status",
			zap.String("document_id", document.DocumentID),
			zap.Error(err),
		)
	}

	if !document.HumanReview {
		if err := s.IndexDocument(document.DocumentID); err != nil {
			s.failDocument(document, "embed", err)
		}
	}
}

func (s *DocumentService) writeSnapshot(
	document model.Document,
	chunks []model.DocumentChunk,
	chunkVersion int,
	cfgHash string,
) (string, error) {
	dir := filepath.Dir(document.StoragePath)
	if dir == "." || dir == "" {
		dir = documentStorageDir(
			s.cfg.GetString("app.data_dir"),
			document.DocumentID,
			document.CreatedAt,
		)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "chunks-v"+strconv.Itoa(chunkVersion)+".json")
	snapshot := model.ChunkSnapshot{
		DocumentID:      document.DocumentID,
		KnowledgeBaseID: document.KnowledgeBaseID,
		ChunkVersion:    chunkVersion,
		Filename:        document.Filename,
		FileSHA256:      document.SHA256,
		ChunkConfigHash: cfgHash,
		CreatedAt:       time.Now().UTC(),
		Chunks:          chunks,
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, raw, 0o644)
}

func documentStorageDir(dataDir, documentID string, createdAt time.Time) string {
	parts := datedDocumentPathParts(documentID, createdAt)
	return filepath.Join(append([]string{dataDir, "documents"}, parts...)...)
}

func staticStorageDir(dataDir, documentID string, createdAt time.Time) string {
	parts := datedDocumentPathParts(documentID, createdAt)
	return filepath.Join(append([]string{dataDir, "static"}, parts...)...)
}

func datedDocumentPathParts(documentID string, createdAt time.Time) []string {
	t := createdAt.UTC()
	if t.IsZero() {
		t = time.Now().UTC()
	}
	date := t.Format("2006-01-02")
	return []string{
		t.Format("2006"),
		t.Format("01"),
		t.Format("02"),
		date + "_" + documentID,
	}
}

// QueryResult bundles semantic search hits.
type QueryResult struct {
	Items []rag.SearchResult
}

// QueryOptions controls search behaviour for a Query call.
type QueryOptions struct {
	DocumentIDs []string       // optional: restrict results to these documents
	SearchMode  rag.SearchMode // defaults to dense when empty
	EF          int            // HNSW ef; 0 = server default
	DropRatio   float64        // BM25 drop_ratio_search; 0 = server default
	RRFK        int            // RRF k (hybrid only); 0 = default (60)
}

func (s *DocumentService) Query(
	knowledgeBaseID, queryText string,
	topK int,
	opts QueryOptions,
) (QueryResult, error) {
	if strings.TrimSpace(knowledgeBaseID) == "" {
		return QueryResult{}, errors.New("knowledge_base_id is required")
	}
	if strings.TrimSpace(queryText) == "" {
		return QueryResult{}, errors.New("query is required")
	}

	items, _, err := rag.Query(context.Background(), s.embedder, s.vectorStore, rag.QueryParams{
		KnowledgeBaseID: knowledgeBaseID,
		Query:           queryText,
		TopK:            topK,
		Mode:            opts.SearchMode,
		DocumentIDs:     opts.DocumentIDs,
		EF:              opts.EF,
		DropRatio:       opts.DropRatio,
		RRFK:            opts.RRFK,
	})
	if err != nil {
		return QueryResult{}, err
	}

	return QueryResult{Items: items}, nil
}

func (s *DocumentService) failDocument(document model.Document, stage string, reason error) {
	infra.L.Warn("document failed",
		zap.String("document_id", document.DocumentID),
		zap.String("filename", document.Filename),
		zap.String("storage_path", document.StoragePath),
		zap.String("file_type", document.FileType),
		zap.String("stage", stage),
		zap.Error(reason),
	)

	now := time.Now().UTC()
	document.Status = "failed"
	document.Stage = stage
	document.ErrorMessage = reason.Error()
	document.FinishedAt = &now
	document.UpdatedAt = now
	if err := s.store.UpdateDocument(document); err != nil {
		infra.L.Warn(
			"failed to persist failed status",
			zap.String("document_id", document.DocumentID),
			zap.Error(err),
		)
	}
}

func detectFileType(filename string) (string, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown":
		return "md", nil
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

func cleanTags(tags []string) ([]string, error) {
	const maxTagRunes = 64
	const maxTagCount = 20
	seen := make(map[string]struct{})
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > maxTagRunes {
			return nil, fmt.Errorf("tag %q exceeds maximum length of %d characters", tag, maxTagRunes)
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		if len(out) >= maxTagCount {
			return nil, fmt.Errorf("too many tags: maximum is %d", maxTagCount)
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out, nil
}
