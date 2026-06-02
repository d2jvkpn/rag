package repository

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/d2jvkpn/rag/backend/internal/model"
)

// AccountSeed describes an account to ensure exists on startup.
type AccountSeed struct {
	Username    string   `mapstructure:"username"`
	Password    string   `mapstructure:"password"`    // plaintext or bcrypt hash (detected automatically)
	Permissions []string `mapstructure:"permissions"` // config-only permissions; never persisted
}

// AccountSyncResult reports what syncAccounts did during startup.
type AccountSyncResult struct {
	Existing []string // already active, no change
	Created  []string
	Enabled  []string // re-activated (were disabled, now back in accounts)
	Disabled []string
}

var ErrNotFound = errors.New("not found")

type State struct {
	Users          map[string]model.User            `json:"users"`
	KnowledgeBases map[string]model.KnowledgeBase   `json:"knowledge_bases"`
	Documents      map[string]model.Document        `json:"documents"`
	Chunks         map[string][]model.DocumentChunk `json:"chunks"`
}

type JSONStore struct {
	path string
	mu   sync.RWMutex
	data State
}

func (s *JSONStore) Close() error {
	return nil
}

func NewJSONStore(path string, accounts []AccountSeed) (*JSONStore, AccountSyncResult, error) {
	store := &JSONStore{
		path: path,
		data: State{
			Users:          map[string]model.User{},
			KnowledgeBases: map[string]model.KnowledgeBase{},
			Documents:      map[string]model.Document{},
			Chunks:         map[string][]model.DocumentChunk{},
		},
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, AccountSyncResult{}, err
	}

	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &store.data); err != nil {
			return nil, AccountSyncResult{}, err
		}
		if store.data.Users == nil {
			store.data.Users = map[string]model.User{}
		}
		if store.data.KnowledgeBases == nil {
			store.data.KnowledgeBases = map[string]model.KnowledgeBase{}
		}
		if store.data.Documents == nil {
			store.data.Documents = map[string]model.Document{}
		}
		if store.data.Chunks == nil {
			store.data.Chunks = map[string][]model.DocumentChunk{}
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, AccountSyncResult{}, err
	}

	result, err := store.syncAccounts(accounts)
	if err != nil {
		return nil, result, err
	}
	return store, result, nil
}

func (s *JSONStore) syncAccounts(accounts []AccountSeed) (AccountSyncResult, error) {
	var result AccountSyncResult

	accountSet := make(map[string]struct{}, len(accounts))
	for _, acc := range accounts {
		accountSet[acc.Username] = struct{}{}
	}

	now := time.Now().UTC()
	changed := false

	// Disable users absent from accounts config.
	for id, u := range s.data.Users {
		if _, ok := accountSet[u.Username]; ok {
			continue
		}
		if u.Status != "disabled" {
			u.Status = "disabled"
			u.UpdatedAt = now
			s.data.Users[id] = u
			changed = true
			result.Disabled = append(result.Disabled, u.Username)
		}
	}

	// Build username → user for quick lookup.
	byUsername := make(map[string]model.User, len(s.data.Users))
	for _, u := range s.data.Users {
		byUsername[u.Username] = u
	}

	// Create or re-enable accounts present in config.
	for _, acc := range accounts {
		existing, ok := byUsername[acc.Username]
		if !ok {
			user := model.User{
				UserID:       uuid.Must(uuid.NewV7()).String(),
				Username:     acc.Username,
				PasswordHash: resolvePasswordHash(acc.Password),
				Status:       "active",
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			s.data.Users[user.UserID] = user
			changed = true
			result.Created = append(result.Created, acc.Username)
		} else if existing.Status == "disabled" {
			existing.Status = "active"
			existing.UpdatedAt = now
			s.data.Users[existing.UserID] = existing
			changed = true
			result.Enabled = append(result.Enabled, acc.Username)
		} else {
			result.Existing = append(result.Existing, acc.Username)
		}
	}

	if changed {
		if err := s.persistLocked(); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *JSONStore) EnsureKnowledgeBasesFromDocuments(dim int, embedderModel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, document := range s.data.Documents {
		if document.KnowledgeBaseID == "" {
			continue
		}
		if _, ok := s.data.KnowledgeBases[document.KnowledgeBaseID]; ok {
			continue
		}
		createdAt := document.CreatedAt
		updatedAt := document.UpdatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}
		s.data.KnowledgeBases[document.KnowledgeBaseID] = model.KnowledgeBase{
			KnowledgeBaseID: document.KnowledgeBaseID,
			CreatedAt:       createdAt,
			UpdatedAt:       updatedAt,
			Dim:             dim,
			Model:           embedderModel,
			Analyzer:        "chinese",
			ChunkSize:       800,
			ChunkOverlap:    100,
			MinChunks:       2,
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return s.persistLocked()
}

func (s *JSONStore) CreateKnowledgeBase(kb model.KnowledgeBase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data.KnowledgeBases[kb.KnowledgeBaseID]; exists {
		return errors.New("knowledge base already exists")
	}
	s.data.KnowledgeBases[kb.KnowledgeBaseID] = kb
	return s.persistLocked()
}

func (s *JSONStore) GetKnowledgeBase(knowledgeBaseID string) (model.KnowledgeBase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	kb, ok := s.data.KnowledgeBases[knowledgeBaseID]
	if !ok {
		return model.KnowledgeBase{}, ErrNotFound
	}
	return kb, nil
}

func (s *JSONStore) ListKnowledgeBases() []model.KnowledgeBase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]model.KnowledgeBase, 0, len(s.data.KnowledgeBases))
	counts := make(map[string]int)
	for _, document := range s.data.Documents {
		counts[document.KnowledgeBaseID]++
	}
	for _, kb := range s.data.KnowledgeBases {
		kb.DocumentCount = counts[kb.KnowledgeBaseID]
		items = append(items, kb)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (s *JSONStore) DeleteKnowledgeBase(knowledgeBaseID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data.KnowledgeBases[knowledgeBaseID]; !ok {
		return ErrNotFound
	}
	delete(s.data.KnowledgeBases, knowledgeBaseID)
	return s.persistLocked()
}

func (s *JSONStore) FindUserByUsername(username string) (model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.data.Users {
		if user.Username == username {
			return user, nil
		}
	}
	return model.User{}, ErrNotFound
}

func (s *JSONStore) UpdateUser(user model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Users[user.UserID] = user
	return s.persistLocked()
}

func (s *JSONStore) GetUser(userID string) (model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.data.Users[userID]
	if !ok {
		return model.User{}, ErrNotFound
	}
	return user, nil
}

func (s *JSONStore) ListUsers() []model.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]model.User, 0, len(s.data.Users))
	for _, u := range s.data.Users {
		users = append(users, u)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].CreatedAt.Before(users[j].CreatedAt)
	})
	return users
}

func (s *JSONStore) CreateDocument(document model.Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.data.Documents {
		if existing.KnowledgeBaseID == document.KnowledgeBaseID &&
			existing.SHA256 == document.SHA256 {
			return errors.New("document already exists in knowledge base")
		}
	}
	s.data.Documents[document.DocumentID] = document
	return s.persistLocked()
}

func (s *JSONStore) UpdateDocument(document model.Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Documents[document.DocumentID] = document
	return s.persistLocked()
}

func (s *JSONStore) GetDocument(documentID string) (model.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	document, ok := s.data.Documents[documentID]
	if !ok {
		return model.Document{}, ErrNotFound
	}
	return document, nil
}

func (s *JSONStore) ListDocuments(knowledgeBaseID, tag string) ([]model.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	documents := s.filterDocumentsLocked(knowledgeBaseID, tag, "")
	sortDocumentsByCreatedDesc(documents)
	return documents, nil
}

func (s *JSONStore) ListDocumentsPage(
	knowledgeBaseID, tag, status string,
	page, pageSize int,
) (model.DocumentPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	documents := s.filterDocumentsLocked(knowledgeBaseID, tag, status)
	sortDocumentsByCreatedDesc(documents)
	return paginateDocuments(documents, page, pageSize), nil
}

func (s *JSONStore) filterDocumentsLocked(knowledgeBaseID, tag, status string) []model.Document {
	documents := make([]model.Document, 0, len(s.data.Documents))
	for _, document := range s.data.Documents {
		if knowledgeBaseID != "" && document.KnowledgeBaseID != knowledgeBaseID {
			continue
		}
		if tag != "" && !containsTag(document.Tags, tag) {
			continue
		}
		if status != "" && document.Status != status {
			continue
		}
		documents = append(documents, document)
	}
	return documents
}

func sortDocumentsByCreatedDesc(documents []model.Document) {
	sort.Slice(documents, func(i, j int) bool {
		return documents[i].CreatedAt.After(documents[j].CreatedAt)
	})
}

func (s *JSONStore) ListDocumentTags(knowledgeBaseID string) []model.DocumentTagCount {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[string]int)
	for _, document := range s.data.Documents {
		if knowledgeBaseID != "" && document.KnowledgeBaseID != knowledgeBaseID {
			continue
		}
		for _, tag := range document.Tags {
			if tag == "" {
				continue
			}
			counts[tag]++
		}
	}

	items := make([]model.DocumentTagCount, 0, len(counts))
	for tag, count := range counts {
		items = append(items, model.DocumentTagCount{Tag: tag, Count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Tag < items[j].Tag
		}
		return items[i].Count > items[j].Count
	})
	return items
}

func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

func (s *JSONStore) DeleteDocument(
	documentID string,
) (model.Document, []model.DocumentChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	document, ok := s.data.Documents[documentID]
	if !ok {
		return model.Document{}, nil, ErrNotFound
	}
	chunks := s.data.Chunks[documentID]
	delete(s.data.Documents, documentID)
	delete(s.data.Chunks, documentID)
	if err := s.persistLocked(); err != nil {
		return model.Document{}, nil, err
	}
	return document, chunks, nil
}

func (s *JSONStore) GetChunk(chunkID string) (model.DocumentChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, chunks := range s.data.Chunks {
		for _, c := range chunks {
			if c.ChunkID == chunkID {
				return c, nil
			}
		}
	}
	return model.DocumentChunk{}, ErrNotFound
}

func (s *JSONStore) UpdateChunk(chunk model.DocumentChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, ok := s.data.Chunks[chunk.DocumentID]
	if !ok {
		return ErrNotFound
	}
	for i, c := range list {
		if c.ChunkID == chunk.ChunkID {
			list[i] = chunk
			s.data.Chunks[chunk.DocumentID] = list
			return s.persistLocked()
		}
	}
	return ErrNotFound
}

func (s *JSONStore) BulkUpdateChunks(chunks []model.DocumentChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// build per-document update maps to scan each list only once
	byDoc := make(map[string]map[string]model.DocumentChunk, 1)
	for _, c := range chunks {
		if byDoc[c.DocumentID] == nil {
			byDoc[c.DocumentID] = make(map[string]model.DocumentChunk)
		}
		byDoc[c.DocumentID][c.ChunkID] = c
	}
	for docID, updates := range byDoc {
		list := s.data.Chunks[docID]
		for i, c := range list {
			if updated, ok := updates[c.ChunkID]; ok {
				list[i] = updated
			}
		}
		s.data.Chunks[docID] = list
	}
	return s.persistLocked()
}

func (s *JSONStore) ReplaceChunks(documentID string, chunks []model.DocumentChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Chunks[documentID] = append([]model.DocumentChunk(nil), chunks...)
	return s.persistLocked()
}

func (s *JSONStore) GetChunks(documentID string) ([]model.DocumentChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chunks, ok := s.data.Chunks[documentID]
	if !ok {
		return []model.DocumentChunk{}, nil
	}
	out := append([]model.DocumentChunk(nil), chunks...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ChunkIndex < out[j].ChunkIndex
	})
	return out, nil
}

func (s *JSONStore) ListChunksPage(documentID string, page, pageSize int) (model.DocumentChunkPage, error) {
	chunks, err := s.GetChunks(documentID)
	if err != nil {
		return model.DocumentChunkPage{}, err
	}
	return paginateChunks(chunks, page, pageSize), nil
}

func paginateChunks(chunks []model.DocumentChunk, page, pageSize int) model.DocumentChunkPage {
	window := paginate(len(chunks), page, pageSize)
	items := []model.DocumentChunk{}
	if window.start < window.end {
		items = append(items, chunks[window.start:window.end]...)
	}
	return model.DocumentChunkPage{
		Items:      items,
		Page:       window.page,
		PageSize:   pageSize,
		Total:      window.total,
		TotalPages: window.totalPages,
		HasNext:    window.hasNext,
		HasPrev:    window.hasPrev,
	}
}

func paginateDocuments(documents []model.Document, page, pageSize int) model.DocumentPage {
	window := paginate(len(documents), page, pageSize)
	items := []model.Document{}
	if window.start < window.end {
		items = append(items, documents[window.start:window.end]...)
	}
	return model.DocumentPage{
		Items:      items,
		Page:       window.page,
		PageSize:   pageSize,
		Total:      window.total,
		TotalPages: window.totalPages,
		HasNext:    window.hasNext,
		HasPrev:    window.hasPrev,
	}
}

type pageWindow struct {
	total      int
	totalPages int
	page       int
	start      int
	end        int
	hasNext    bool
	hasPrev    bool
}

func paginate(total, page, pageSize int) pageWindow {
	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	if totalPages > 0 && page > totalPages {
		page = totalPages
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return pageWindow{
		total:      total,
		totalPages: totalPages,
		page:       page,
		start:      start,
		end:        end,
		hasNext:    totalPages > 0 && page < totalPages,
		hasPrev:    totalPages > 0 && page > 1,
	}
}

func hashPassword(password string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic("bcrypt: " + err.Error())
	}
	return string(hash)
}

func isBcryptHash(s string) bool {
	return strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") ||
		strings.HasPrefix(s, "$2y$")
}

func resolvePasswordHash(password string) string {
	if isBcryptHash(password) {
		return password
	}
	return hashPassword(password)
}

func (s *JSONStore) persistLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}
