package repository

import "backend/internal/model"

type Store interface {
	FindUserByUsername(username string) (model.User, error)
	GetUser(userID string) (model.User, error)
	UpdateUser(user model.User) error
	ListUsers() []model.User

	CreateKnowledgeBase(kb model.KnowledgeBase) error
	GetKnowledgeBase(knowledgeBaseID string) (model.KnowledgeBase, error)
	ListKnowledgeBases() []model.KnowledgeBase
	EnsureKnowledgeBasesFromDocuments(dim int) error

	CreateDocument(document model.Document) error
	UpdateDocument(document model.Document) error
	GetDocument(documentID string) (model.Document, error)
	ListDocuments(knowledgeBaseID, tag string) ([]model.Document, error)
	ListDocumentsPage(knowledgeBaseID, tag, status string, page, pageSize int) (model.DocumentPage, error)
	ListDocumentTags(knowledgeBaseID string) []model.DocumentTagCount
	DeleteDocument(documentID string) (model.Document, []model.DocumentChunk, error)

	ReplaceChunks(documentID string, chunks []model.DocumentChunk) error
	GetChunks(documentID string) ([]model.DocumentChunk, error)
	ListChunksPage(documentID string, page, pageSize int) (model.DocumentChunkPage, error)
	GetChunk(chunkID string) (model.DocumentChunk, error)
	UpdateChunk(chunk model.DocumentChunk) error
	BulkUpdateChunks(chunks []model.DocumentChunk) error
}
