package repository

import "backend/internal/model"

type Store interface {
	FindUserByUsername(username string) (model.User, error)
	GetUser(userID string) (model.User, error)
	UpdateUser(user model.User) error

	CreateDocument(document model.Document) error
	UpdateDocument(document model.Document) error
	GetDocument(documentID string) (model.Document, error)
	ListDocuments() []model.Document
	DeleteDocument(documentID string) (model.Document, []model.DocumentChunk, error)

	ReplaceChunks(documentID string, chunks []model.DocumentChunk) error
	GetChunks(documentID string) ([]model.DocumentChunk, error)
}
