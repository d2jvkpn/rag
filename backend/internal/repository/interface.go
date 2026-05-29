package repository

import "backend/internal/model"

type UserStore interface {
	FindUserByUsername(username string) (model.User, error)
	GetUser(userID string) (model.User, error)
	UpdateUser(user model.User) error
	ListUsers() []model.User
}

type KnowledgeBaseStore interface {
	CreateKnowledgeBase(kb model.KnowledgeBase) error
	GetKnowledgeBase(knowledgeBaseID string) (model.KnowledgeBase, error)
	ListKnowledgeBases() []model.KnowledgeBase
	EnsureKnowledgeBasesFromDocuments(dim int, embedderModel string) error
}

type DocumentStore interface {
	CreateDocument(document model.Document) error
	UpdateDocument(document model.Document) error
	GetDocument(documentID string) (model.Document, error)
	ListDocuments(knowledgeBaseID, tag string) ([]model.Document, error)
	ListDocumentsPage(knowledgeBaseID, tag, status string, page, pageSize int) (model.DocumentPage, error)
	ListDocumentTags(knowledgeBaseID string) []model.DocumentTagCount
	DeleteDocument(documentID string) (model.Document, []model.DocumentChunk, error)
}

type ChunkStore interface {
	ReplaceChunks(documentID string, chunks []model.DocumentChunk) error
	GetChunks(documentID string) ([]model.DocumentChunk, error)
	ListChunksPage(documentID string, page, pageSize int) (model.DocumentChunkPage, error)
	GetChunk(chunkID string) (model.DocumentChunk, error)
	UpdateChunk(chunk model.DocumentChunk) error
	BulkUpdateChunks(chunks []model.DocumentChunk) error
}

// Store composes all sub-stores; satisfied by both JSONStore and PostgresStore.
type Store interface {
	UserStore
	KnowledgeBaseStore
	DocumentStore
	ChunkStore
}
