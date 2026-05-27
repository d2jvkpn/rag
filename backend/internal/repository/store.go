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

	"backend/internal/model"
)

// AccountSeed describes an account to ensure exists on startup.
type AccountSeed struct {
	Username    string
	Password    string   // plaintext or bcrypt hash (detected automatically)
	Permissions []string // config-only permissions; never persisted
}

var ErrNotFound = errors.New("not found")

type State struct {
	Users     map[string]model.User            `json:"users"`
	Documents map[string]model.Document        `json:"documents"`
	Chunks    map[string][]model.DocumentChunk `json:"chunks"`
}

type JSONStore struct {
	path string
	mu   sync.RWMutex
	data State
}

func (s *JSONStore) Close() error {
	return nil
}

func NewJSONStore(path string, accounts []AccountSeed) (*JSONStore, error) {
	store := &JSONStore{
		path: path,
		data: State{
			Users:     map[string]model.User{},
			Documents: map[string]model.Document{},
			Chunks:    map[string][]model.DocumentChunk{},
		},
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &store.data); err != nil {
			return nil, err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	changed := false
	for _, acc := range accounts {
		exists := false
		for _, u := range store.data.Users {
			if u.Username == acc.Username {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		now := time.Now().UTC()
		user := model.User{
			UserID:       uuid.Must(uuid.NewV7()).String(),
			Username:     acc.Username,
			PasswordHash: resolvePasswordHash(acc.Password),
			Status:       "active",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		store.data.Users[user.UserID] = user
		changed = true
	}
	if changed {
		if err := store.persistLocked(); err != nil {
			return nil, err
		}
	}

	return store, nil
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
		if existing.KnowledgeBaseID == document.KnowledgeBaseID && existing.SHA256 == document.SHA256 {
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

func (s *JSONStore) ListDocuments(knowledgeBaseID, tag string) []model.Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	documents := make([]model.Document, 0, len(s.data.Documents))
	for _, document := range s.data.Documents {
		if knowledgeBaseID != "" && document.KnowledgeBaseID != knowledgeBaseID {
			continue
		}
		if tag != "" && !containsTag(document.Tags, tag) {
			continue
		}
		documents = append(documents, document)
	}
	sort.Slice(documents, func(i, j int) bool {
		return documents[i].CreatedAt.After(documents[j].CreatedAt)
	})
	return documents
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

func (s *JSONStore) DeleteDocument(documentID string) (model.Document, []model.DocumentChunk, error) {
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

func hashPassword(password string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic("bcrypt: " + err.Error())
	}
	return string(hash)
}

func isBcryptHash(s string) bool {
	return strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") || strings.HasPrefix(s, "$2y$")
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
