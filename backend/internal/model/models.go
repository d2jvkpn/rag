package model

import "time"

type User struct {
	UserID       string     `json:"user_id"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"password_hash"`
	Status       string     `json:"status"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
}

type Document struct {
	DocumentID        string     `json:"document_id"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	KnowledgeBaseID   string     `json:"knowledge_base_id"`
	Filename          string     `json:"filename"`
	Title             string     `json:"title,omitempty"`
	Tags              []string   `json:"tags,omitempty"`
	SourceType        string     `json:"source_type"`
	StoragePath       string     `json:"storage_path"`
	ChunkSnapshotPath string     `json:"chunk_snapshot_path,omitempty"`
	SHA256            string     `json:"sha256"`
	Status            string     `json:"status"`
	Stage             string     `json:"stage"`
	ErrorMessage      string     `json:"error_message,omitempty"`
	RetryCount        int        `json:"retry_count"`
	PageCount         int        `json:"page_count"`
	ChunkCount        int        `json:"chunk_count"`
	ChunkVersion      int        `json:"chunk_version"`
	ChunkConfigHash   string     `json:"chunk_config_hash,omitempty"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
}

type ResourceRef struct {
	RefID       string `json:"ref_id"`
	RefType     string `json:"ref_type"`
	Label       string `json:"label,omitempty"`
	Caption     string `json:"caption,omitempty"`
	Page        int    `json:"page,omitempty"`
	AnchorText  string `json:"anchor_text,omitempty"`
	StoragePath string `json:"storage_path,omitempty"`
	URL         string `json:"url,omitempty"`
	IsExternal  bool   `json:"is_external,omitempty"`
}

type DocumentChunk struct {
	ChunkID        string        `json:"chunk_id"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	DocumentID     string        `json:"document_id"`
	ChunkIndex     int           `json:"chunk_index"`
	SectionTitle   string        `json:"section_title,omitempty"`
	PageStart      int           `json:"page_start,omitempty"`
	PageEnd        int           `json:"page_end,omitempty"`
	Text           string        `json:"text"`
	NormalizedText string        `json:"normalized_text"`
	Status         string        `json:"status"`
	ChunkVersion   int           `json:"chunk_version"`
	Source         string        `json:"source"`
	IsCurrent      bool          `json:"is_current"`
	ReviewComment  string        `json:"review_comment,omitempty"`
	Filename       string        `json:"filename"`
	EmbeddingModel string        `json:"embedding_model,omitempty"`
	ResourceRefs   []ResourceRef `json:"resource_refs"`
}

type ChunkSnapshot struct {
	DocumentID      string          `json:"document_id"`
	KnowledgeBaseID string          `json:"knowledge_base_id"`
	ChunkVersion    int             `json:"chunk_version"`
	SourceSHA256    string          `json:"source_sha256"`
	ChunkConfigHash string          `json:"chunk_config_hash"`
	CreatedAt       time.Time       `json:"created_at"`
	Chunks          []DocumentChunk `json:"chunks"`
}
