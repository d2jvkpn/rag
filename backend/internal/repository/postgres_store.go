package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"backend/internal/model"
	"backend/internal/uuid"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(dsn, adminUsername, adminPassword string) (*PostgresStore, error) {
	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}

	store := &PostgresStore{db: db}
	if err := store.ensureAdmin(adminUsername, adminPassword); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) ensureAdmin(username, password string) error {
	var count int64
	if err := s.db.Model(&userRow{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	now := time.Now().UTC()
	row := userRow{
		UserID:       uuid.NewV7(),
		Username:     username,
		PasswordHash: hashPassword(password),
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	return s.db.Create(&row).Error
}

func (s *PostgresStore) FindUserByUsername(username string) (model.User, error) {
	var row userRow
	if err := s.db.Where("username = ?", username).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}
	return userFromRow(row), nil
}

func (s *PostgresStore) GetUser(userID string) (model.User, error) {
	var row userRow
	if err := s.db.First(&row, "user_id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}
	return userFromRow(row), nil
}

func (s *PostgresStore) UpdateUser(user model.User) error {
	return s.db.Save(userToRow(user)).Error
}

func (s *PostgresStore) CreateDocument(document model.Document) error {
	var count int64
	s.db.Model(&documentRow{}).
		Where("knowledge_base_id = ? AND sha256 = ?", document.KnowledgeBaseID, document.SHA256).
		Count(&count)
	if count > 0 {
		return errors.New("document already exists in knowledge base")
	}
	return s.db.Create(documentToRow(document)).Error
}

func (s *PostgresStore) UpdateDocument(document model.Document) error {
	return s.db.Save(documentToRow(document)).Error
}

func (s *PostgresStore) GetDocument(documentID string) (model.Document, error) {
	var row documentRow
	if err := s.db.First(&row, "document_id = ?", documentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Document{}, ErrNotFound
		}
		return model.Document{}, err
	}
	return documentFromRow(row), nil
}

func (s *PostgresStore) ListDocuments() []model.Document {
	var rows []documentRow
	s.db.Order("created_at desc").Find(&rows)
	docs := make([]model.Document, len(rows))
	for i, r := range rows {
		docs[i] = documentFromRow(r)
	}
	return docs
}

func (s *PostgresStore) DeleteDocument(documentID string) (model.Document, []model.DocumentChunk, error) {
	var doc model.Document
	var chunks []model.DocumentChunk

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row documentRow
		if err := tx.First(&row, "document_id = ?", documentID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		doc = documentFromRow(row)

		var chunkRows []chunkRow
		if err := tx.Where("document_id = ?", documentID).Find(&chunkRows).Error; err != nil {
			return err
		}
		chunks = make([]model.DocumentChunk, len(chunkRows))
		for i, r := range chunkRows {
			chunks[i] = chunkFromRow(r)
		}

		if err := tx.Delete(&chunkRow{}, "document_id = ?", documentID).Error; err != nil {
			return err
		}
		return tx.Delete(&documentRow{}, "document_id = ?", documentID).Error
	})

	if err != nil {
		return model.Document{}, nil, err
	}
	return doc, chunks, nil
}

func (s *PostgresStore) ReplaceChunks(documentID string, chunks []model.DocumentChunk) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&chunkRow{}, "document_id = ?", documentID).Error; err != nil {
			return err
		}
		if len(chunks) == 0 {
			return nil
		}
		rows := make([]chunkRow, len(chunks))
		for i, c := range chunks {
			rows[i] = chunkToRow(c)
		}
		return tx.Create(&rows).Error
	})
}

func (s *PostgresStore) GetChunks(documentID string) ([]model.DocumentChunk, error) {
	var rows []chunkRow
	if err := s.db.Where("document_id = ?", documentID).Order("chunk_index asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	chunks := make([]model.DocumentChunk, len(rows))
	for i, r := range rows {
		chunks[i] = chunkFromRow(r)
	}
	return chunks, nil
}

// ---- row types ----

type userRow struct {
	UserID       string     `gorm:"column:user_id;primaryKey"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	Username     string     `gorm:"column:username"`
	PasswordHash string     `gorm:"column:password_hash"`
	Status       string     `gorm:"column:status"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"`
}

func (userRow) TableName() string { return "users" }

type documentRow struct {
	DocumentID        string         `gorm:"column:document_id;primaryKey"`
	CreatedAt         time.Time      `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt         time.Time      `gorm:"column:updated_at;autoUpdateTime:false"`
	KnowledgeBaseID   string         `gorm:"column:knowledge_base_id"`
	Filename          string         `gorm:"column:filename"`
	Title             string         `gorm:"column:title"`
	Tags              pq.StringArray `gorm:"column:tags;type:text[]"`
	SourceType        string         `gorm:"column:source_type"`
	StoragePath       string         `gorm:"column:storage_path"`
	ChunkSnapshotPath string         `gorm:"column:chunk_snapshot_path"`
	SHA256            string         `gorm:"column:sha256"`
	Status            string         `gorm:"column:status"`
	Stage             string         `gorm:"column:stage"`
	ErrorMessage      string         `gorm:"column:error_message"`
	RetryCount        int            `gorm:"column:retry_count"`
	PageCount         int            `gorm:"column:page_count"`
	ChunkCount        int            `gorm:"column:chunk_count"`
	ChunkVersion      int            `gorm:"column:chunk_version"`
	ChunkConfigHash   string         `gorm:"column:chunk_config_hash"`
	StartedAt         *time.Time     `gorm:"column:started_at"`
	FinishedAt        *time.Time     `gorm:"column:finished_at"`
}

func (documentRow) TableName() string { return "documents" }

type chunkRow struct {
	ChunkID        string          `gorm:"column:chunk_id;primaryKey"`
	CreatedAt      time.Time       `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time       `gorm:"column:updated_at;autoUpdateTime:false"`
	DocumentID     string          `gorm:"column:document_id"`
	ChunkIndex     int             `gorm:"column:chunk_index"`
	SectionTitle   string          `gorm:"column:section_title"`
	PageStart      int             `gorm:"column:page_start"`
	PageEnd        int             `gorm:"column:page_end"`
	Text           string          `gorm:"column:text"`
	NormalizedText string          `gorm:"column:normalized_text"`
	Status         string          `gorm:"column:status"`
	ChunkVersion   int             `gorm:"column:chunk_version"`
	Source         string          `gorm:"column:source"`
	IsCurrent      bool            `gorm:"column:is_current"`
	ReviewComment  string          `gorm:"column:review_comment"`
	Filename       string          `gorm:"column:filename"`
	EmbeddingModel string          `gorm:"column:embedding_model"`
	ResourceRefs   json.RawMessage `gorm:"column:resource_refs;type:jsonb"`
}

func (chunkRow) TableName() string { return "document_chunks" }

// ---- conversions ----

func userFromRow(r userRow) model.User {
	return model.User{
		UserID:       r.UserID,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		Username:     r.Username,
		PasswordHash: r.PasswordHash,
		Status:       r.Status,
		LastLoginAt:  r.LastLoginAt,
	}
}

func userToRow(u model.User) userRow {
	return userRow{
		UserID:       u.UserID,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
		Username:     u.Username,
		PasswordHash: u.PasswordHash,
		Status:       u.Status,
		LastLoginAt:  u.LastLoginAt,
	}
}

func documentFromRow(r documentRow) model.Document {
	tags := []string(r.Tags)
	if tags == nil {
		tags = []string{}
	}
	return model.Document{
		DocumentID:        r.DocumentID,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
		KnowledgeBaseID:   r.KnowledgeBaseID,
		Filename:          r.Filename,
		Title:             r.Title,
		Tags:              tags,
		SourceType:        r.SourceType,
		StoragePath:       r.StoragePath,
		ChunkSnapshotPath: r.ChunkSnapshotPath,
		SHA256:            r.SHA256,
		Status:            r.Status,
		Stage:             r.Stage,
		ErrorMessage:      r.ErrorMessage,
		RetryCount:        r.RetryCount,
		PageCount:         r.PageCount,
		ChunkCount:        r.ChunkCount,
		ChunkVersion:      r.ChunkVersion,
		ChunkConfigHash:   r.ChunkConfigHash,
		StartedAt:         r.StartedAt,
		FinishedAt:        r.FinishedAt,
	}
}

func documentToRow(d model.Document) documentRow {
	tags := pq.StringArray(d.Tags)
	if tags == nil {
		tags = pq.StringArray{}
	}
	return documentRow{
		DocumentID:        d.DocumentID,
		CreatedAt:         d.CreatedAt,
		UpdatedAt:         d.UpdatedAt,
		KnowledgeBaseID:   d.KnowledgeBaseID,
		Filename:          d.Filename,
		Title:             d.Title,
		Tags:              tags,
		SourceType:        d.SourceType,
		StoragePath:       d.StoragePath,
		ChunkSnapshotPath: d.ChunkSnapshotPath,
		SHA256:            d.SHA256,
		Status:            d.Status,
		Stage:             d.Stage,
		ErrorMessage:      d.ErrorMessage,
		RetryCount:        d.RetryCount,
		PageCount:         d.PageCount,
		ChunkCount:        d.ChunkCount,
		ChunkVersion:      d.ChunkVersion,
		ChunkConfigHash:   d.ChunkConfigHash,
		StartedAt:         d.StartedAt,
		FinishedAt:        d.FinishedAt,
	}
}

func chunkFromRow(r chunkRow) model.DocumentChunk {
	var refs []model.ResourceRef
	if len(r.ResourceRefs) > 0 {
		_ = json.Unmarshal(r.ResourceRefs, &refs)
	}
	if refs == nil {
		refs = []model.ResourceRef{}
	}
	return model.DocumentChunk{
		ChunkID:        r.ChunkID,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
		DocumentID:     r.DocumentID,
		ChunkIndex:     r.ChunkIndex,
		SectionTitle:   r.SectionTitle,
		PageStart:      r.PageStart,
		PageEnd:        r.PageEnd,
		Text:           r.Text,
		NormalizedText: r.NormalizedText,
		Status:         r.Status,
		ChunkVersion:   r.ChunkVersion,
		Source:         r.Source,
		IsCurrent:      r.IsCurrent,
		ReviewComment:  r.ReviewComment,
		Filename:       r.Filename,
		EmbeddingModel: r.EmbeddingModel,
		ResourceRefs:   refs,
	}
}

func chunkToRow(c model.DocumentChunk) chunkRow {
	refs, _ := json.Marshal(c.ResourceRefs)
	if refs == nil {
		refs = json.RawMessage("[]")
	}
	return chunkRow{
		ChunkID:        c.ChunkID,
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
		DocumentID:     c.DocumentID,
		ChunkIndex:     c.ChunkIndex,
		SectionTitle:   c.SectionTitle,
		PageStart:      c.PageStart,
		PageEnd:        c.PageEnd,
		Text:           c.Text,
		NormalizedText: c.NormalizedText,
		Status:         c.Status,
		ChunkVersion:   c.ChunkVersion,
		Source:         c.Source,
		IsCurrent:      c.IsCurrent,
		ReviewComment:  c.ReviewComment,
		Filename:       c.Filename,
		EmbeddingModel: c.EmbeddingModel,
		ResourceRefs:   refs,
	}
}
