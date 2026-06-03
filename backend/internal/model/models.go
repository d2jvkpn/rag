package model

import "time"

type User struct {
	UserID       string     `json:"user_id"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"password_hash"`        // bcrypt hash; never exposed in API responses
	Status       string     `json:"status"`               // active | disabled
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	TOTPSecret   string     `json:"totp_secret,omitempty"` // base32-encoded TOTP seed; omitted when TOTP is not enabled
	TOTPEnabled  bool       `json:"totp_enabled"`
}

type KnowledgeBase struct {
	KnowledgeBaseID string    `json:"knowledge_base_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	CreatedBy       string    `json:"created_by,omitempty"` // username of the creator
	Dim             int       `json:"dim"`                  // embedding vector dimension; must match the configured embedder
	Model           string    `json:"model"`                // embedding model name used for this collection
	Analyzer        string    `json:"analyzer"`             // BM25 text analyzer (e.g. "chinese")
	ChunkSize       int       `json:"chunk_size"`           // target token count per chunk
	ChunkOverlap    int       `json:"chunk_overlap"`        // overlap token count between adjacent chunks
	MinChunks       int       `json:"min_chunks"`           // minimum chunk count before merging the whole document into one chunk
	DocumentCount   int       `json:"document_count,omitempty"` // denormalized count; computed at query time, not stored
}

type Document struct {
	DocumentID        string     `json:"document_id"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	KnowledgeBaseID   string     `json:"knowledge_base_id"`             // owning knowledge base; immutable after creation
	Filename          string     `json:"filename"`                      // sanitized original filename
	Title             string     `json:"title,omitempty"`               // human-readable display title; defaults to Filename when empty
	Tags              []string   `json:"tags,omitempty"`                // user-supplied labels for filtering
	FileType          string     `json:"file_type"`                     // detected from extension: pdf | docx | pptx | markdown
	StoragePath       string     `json:"storage_path"`                  // absolute path to the uploaded file on disk
	ChunkSnapshotPath string     `json:"chunk_snapshot_path,omitempty"` // path to the post-index chunk JSON snapshot; empty until first index run
	SHA256            string     `json:"sha256"`                        // hex SHA-256 of the uploaded file; used for deduplication
	Status            string     `json:"status"`                        // uploaded | processing | review_pending | approved | indexed | failed
	Stage             string     `json:"stage"`                         // last completed pipeline stage: upload | parse | chunk | embed | index | done
	ErrorMessage      string     `json:"error_message,omitempty"`       // non-empty only when status = failed
	RetryCount        int        `json:"retry_count"`                   // number of times processing has been retried
	PageCount         int        `json:"page_count"`                    // page or slide count reported by the parser
	ChunkCount        int        `json:"chunk_count"`                   // number of chunks produced in the latest chunking run
	ChunkVersion      int        `json:"chunk_version"`                 // incremented on each rechunk; 0 before first processing
	ChunkConfigHash   string     `json:"chunk_config_hash,omitempty"`   // hash of (chunk_size, chunk_overlap, min_chunks) at the time of chunking
	StartedAt         *time.Time `json:"started_at,omitempty"`          // when the current processing run began; nil if not yet started
	FinishedAt        *time.Time `json:"finished_at,omitempty"`         // when the latest pipeline run completed; nil if still in progress
	HumanReview       bool       `json:"human_review"`                  // when true, chunks require manual approval before indexing
	UploaderID        string     `json:"uploader_id,omitempty"`         // user_id of the uploader
	UploaderName      string     `json:"uploader_name,omitempty"`       // username of the uploader; denormalized for display
}

type DocumentPage struct {
	Items      []Document `json:"items"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
	Total      int        `json:"total"`
	TotalPages int        `json:"total_pages"`
	HasNext    bool       `json:"has_next"`
	HasPrev    bool       `json:"has_prev"`
}

type ResourceRef struct {
	RefID       string `json:"ref_id"`                  // stable identifier within the document (e.g. "img-3")
	RefType     string `json:"ref_type"`                // image | table | link
	Label       string `json:"label,omitempty"`         // alt text or short description
	Caption     string `json:"caption,omitempty"`       // figure/table caption extracted from the source
	Page        int    `json:"page,omitempty"`          // 1-based page number where the resource appears
	AnchorText  string `json:"anchor_text,omitempty"`   // surrounding inline text that references this resource
	StoragePath string `json:"storage_path,omitempty"` // relative path under the static directory for local assets
	URL         string `json:"url,omitempty"`           // original URL for external resources
	IsExternal  bool   `json:"is_external,omitempty"`   // true when the resource is hosted externally
}

type DocumentChunk struct {
	ChunkID        string        `json:"chunk_id"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	DocumentID     string        `json:"document_id"`
	ChunkIndex     int           `json:"chunk_index"`            // 0-based position within the document's current chunk list
	SectionTitle   string        `json:"section_title,omitempty"` // nearest heading above this chunk, if any
	PageStart      int           `json:"page_start,omitempty"`   // first page covered by this chunk (1-based)
	PageEnd        int           `json:"page_end,omitempty"`     // last page covered by this chunk (1-based)
	Text           string        `json:"text"`                   // raw chunk text as stored and indexed
	NormalizedText string        `json:"normalized_text"`        // lightly cleaned text used for display during review
	Status         string        `json:"status"`                 // draft | approved | rejected
	ChunkVersion   int           `json:"chunk_version"`          // matches Document.ChunkVersion; stale chunks have a lower value
	Source         string        `json:"source"`                 // auto | manual — manual when the chunk was edited or created by a reviewer
	IsCurrent      bool          `json:"is_current"`             // false for rejected chunks and chunks superseded by a rechunk
	EmbeddingModel string        `json:"embedding_model,omitempty"` // model name written after embedding; empty until indexed
	Embedding      []float32     `json:"embedding,omitempty"`    // vector; present in snapshots, omitted from API responses
	ResourceRefs   []ResourceRef `json:"resource_refs"`          // structured references to images, tables, and links within this chunk
}

type DocumentChunkPage struct {
	Items      []DocumentChunk `json:"items"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	Total      int             `json:"total"`
	TotalPages int             `json:"total_pages"`
	HasNext    bool            `json:"has_next"`
	HasPrev    bool            `json:"has_prev"`
}

type ChunkSnapshot struct {
	DocumentID      string          `json:"document_id"`
	KnowledgeBaseID string          `json:"knowledge_base_id"`
	ChunkVersion    int             `json:"chunk_version"`    // version of the chunk set captured in this snapshot
	Filename        string          `json:"filename"`         // original filename of the source document
	FileSHA256      string          `json:"file_sha256"`      // SHA-256 of the source file; verifies which file version produced these chunks
	ChunkConfigHash string          `json:"chunk_config_hash"` // hash of chunking parameters; detects config drift between snapshot and current settings
	CreatedAt       time.Time       `json:"created_at"`
	Chunks          []DocumentChunk `json:"chunks"` // full chunk list including embeddings; self-contained for offline audit
}

type DocumentTagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}
