package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"backend/internal/migrations"
	"backend/internal/model"
)

type PostgresStore struct {
	db *gorm.DB
}

func (s *PostgresStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func NewPostgresStore(dsn string, accounts []AccountSeed) (*PostgresStore, error) {
	sqlDB, err := sql.Open("postgres", postgresDSNWithConnectTimeout(dsn))
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	if err := runMigrations(sqlDB); err != nil {
		return nil, err
	}

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DefaultContextTimeout: 10 * time.Second,
		Logger:                gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}

	store := &PostgresStore{db: db}
	if err := store.ensureAccounts(accounts); err != nil {
		return nil, err
	}
	return store, nil
}

func postgresDSNWithConnectTimeout(dsn string) string {
	if strings.Contains(dsn, "connect_timeout") {
		return dsn
	}
	u, err := url.Parse(dsn)
	if err == nil && u.Scheme != "" {
		q := u.Query()
		q.Set("connect_timeout", "5")
		u.RawQuery = q.Encode()
		return u.String()
	}
	if strings.TrimSpace(dsn) == "" {
		return dsn
	}
	return strings.TrimSpace(dsn) + " connect_timeout=5"
}

func runMigrations(sqlDB *sql.DB) error {
	src, err := iofs.New(migrations.FS, "sql")
	if err != nil {
		return err
	}
	driver, err := migratepg.WithInstance(sqlDB, &migratepg.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

func (s *PostgresStore) ensureAccounts(accounts []AccountSeed) error {
	for _, acc := range accounts {
		var count int64
		if err := s.db.Model(&userRow{}).
			Where("username = ?", acc.Username).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		now := time.Now().UTC()
		row := userRow{
			UserID:       uuid.Must(uuid.NewV7()).String(),
			Username:     acc.Username,
			PasswordHash: resolvePasswordHash(acc.Password),
			Status:       "active",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := s.db.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
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

func (s *PostgresStore) ListUsers() []model.User {
	var rows []userRow
	s.db.Order("created_at asc").Find(&rows)
	users := make([]model.User, len(rows))
	for i, r := range rows {
		users[i] = userFromRow(r)
	}
	return users
}

func (s *PostgresStore) CreateKnowledgeBase(kb model.KnowledgeBase) error {
	return s.db.Create(knowledgeBaseToRow(kb)).Error
}

func (s *PostgresStore) GetKnowledgeBase(knowledgeBaseID string) (model.KnowledgeBase, error) {
	var row knowledgeBaseRow
	if err := s.db.First(&row, "knowledge_base_id = ?", knowledgeBaseID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.KnowledgeBase{}, ErrNotFound
		}
		return model.KnowledgeBase{}, err
	}
	return knowledgeBaseFromRow(row), nil
}

func (s *PostgresStore) ListKnowledgeBases() []model.KnowledgeBase {
	type resultRow struct {
		KnowledgeBaseID string    `gorm:"column:knowledge_base_id"`
		CreatedAt       time.Time `gorm:"column:created_at"`
		UpdatedAt       time.Time `gorm:"column:updated_at"`
		CreatedBy       string    `gorm:"column:created_by"`
		Dim             int       `gorm:"column:dim"`
		Model           string    `gorm:"column:model"`
		Analyzer        string    `gorm:"column:analyzer"`
		ChunkSize       int       `gorm:"column:chunk_size"`
		ChunkOverlap    int       `gorm:"column:chunk_overlap"`
		MinChunks       int       `gorm:"column:min_chunks"`
		DocumentCount   int       `gorm:"column:document_count"`
	}
	var rows []resultRow
	sql := `
		SELECT kb.*, count(d.document_id)::int AS document_count
		FROM knowledge_bases kb
		LEFT JOIN documents d ON d.knowledge_base_id = kb.knowledge_base_id
		GROUP BY kb.knowledge_base_id
		ORDER BY kb.created_at ASC
	`
	s.db.Raw(sql).Scan(&rows)
	items := make([]model.KnowledgeBase, len(rows))
	for i, r := range rows {
		items[i] = model.KnowledgeBase{
			KnowledgeBaseID: r.KnowledgeBaseID,
			CreatedAt:       r.CreatedAt,
			UpdatedAt:       r.UpdatedAt,
			CreatedBy:       r.CreatedBy,
			Dim:             r.Dim,
			Model:           r.Model,
			Analyzer:        r.Analyzer,
			ChunkSize:       r.ChunkSize,
			ChunkOverlap:    r.ChunkOverlap,
			MinChunks:       r.MinChunks,
			DocumentCount:   r.DocumentCount,
		}
	}
	return items
}

func (s *PostgresStore) EnsureKnowledgeBasesFromDocuments(dim int, embedderModel string) error {
	sql := `
		INSERT INTO knowledge_bases (
			knowledge_base_id, created_at, updated_at, created_by,
			dim, model, analyzer, chunk_size, chunk_overlap, min_chunks
		)
		SELECT
			knowledge_base_id, min(created_at), max(updated_at), '',
			?, ?, 'chinese', 1000, 150, 2
		FROM documents
		WHERE knowledge_base_id <> ''
		GROUP BY knowledge_base_id
		ON CONFLICT (knowledge_base_id) DO NOTHING
	`
	return s.db.Exec(sql, dim, embedderModel).Error
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

func (s *PostgresStore) ListDocuments(knowledgeBaseID, tag string) ([]model.Document, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	q := s.documentsQuery(s.db.WithContext(ctx), knowledgeBaseID, tag, "").Order("created_at desc")
	var rows []documentRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	docs := make([]model.Document, len(rows))
	for i, r := range rows {
		docs[i] = documentFromRow(r)
	}
	return docs, nil
}

func (s *PostgresStore) ListDocumentsPage(
	knowledgeBaseID, tag, status string,
	page, pageSize int,
) (model.DocumentPage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	q := s.documentsQuery(s.db.WithContext(ctx).Model(&documentRow{}), knowledgeBaseID, tag, status)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return model.DocumentPage{}, err
	}
	totalPages := 0
	if total > 0 {
		totalPages = (int(total) + pageSize - 1) / pageSize
	}
	if totalPages > 0 && page > totalPages {
		page = totalPages
	}

	var rows []documentRow
	offset := (page - 1) * pageSize
	if err := q.Order("created_at desc").Limit(pageSize).Offset(offset).Find(&rows).Error; err != nil {
		return model.DocumentPage{}, err
	}
	docs := make([]model.Document, len(rows))
	for i, r := range rows {
		docs[i] = documentFromRow(r)
	}
	return model.DocumentPage{
		Items:      docs,
		Page:       page,
		PageSize:   pageSize,
		Total:      int(total),
		TotalPages: totalPages,
		HasNext:    totalPages > 0 && page < totalPages,
		HasPrev:    totalPages > 0 && page > 1,
	}, nil
}

func (s *PostgresStore) documentsQuery(q *gorm.DB, knowledgeBaseID, tag, status string) *gorm.DB {
	if knowledgeBaseID != "" {
		q = q.Where("knowledge_base_id = ?", knowledgeBaseID)
	}
	if tag != "" {
		q = q.Where("tags @> ?", pq.Array([]string{tag}))
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	return q
}

func (s *PostgresStore) ListDocumentTags(knowledgeBaseID string) []model.DocumentTagCount {
	type row struct {
		Tag   string `gorm:"column:tag"`
		Count int    `gorm:"column:count"`
	}

	var rows []row
	sql := `
		SELECT tag, count(*) AS count
		FROM documents, unnest(tags) AS tag
		WHERE ($1 = '' OR knowledge_base_id = $1) AND tag <> ''
		GROUP BY tag
		ORDER BY count DESC, tag ASC
	`
	s.db.Raw(sql, knowledgeBaseID).Scan(&rows)

	items := make([]model.DocumentTagCount, len(rows))
	for i, r := range rows {
		items[i] = model.DocumentTagCount{Tag: r.Tag, Count: r.Count}
	}
	return items
}

func (s *PostgresStore) DeleteDocument(
	documentID string,
) (model.Document, []model.DocumentChunk, error) {
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

func (s *PostgresStore) GetChunk(chunkID string) (model.DocumentChunk, error) {
	var row chunkRow
	if err := s.db.First(&row, "chunk_id = ?", chunkID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.DocumentChunk{}, ErrNotFound
		}
		return model.DocumentChunk{}, err
	}
	return chunkFromRow(row), nil
}

func (s *PostgresStore) UpdateChunk(chunk model.DocumentChunk) error {
	return s.db.Save(chunkToRow(chunk)).Error
}

func (s *PostgresStore) BulkUpdateChunks(chunks []model.DocumentChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, c := range chunks {
			if err := tx.Save(chunkToRow(c)).Error; err != nil {
				return err
			}
		}
		return nil
	})
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
	if err := s.db.Where("document_id = ?", documentID).
		Order("chunk_index asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	chunks := make([]model.DocumentChunk, len(rows))
	for i, r := range rows {
		chunks[i] = chunkFromRow(r)
	}
	return chunks, nil
}

func (s *PostgresStore) ListChunksPage(documentID string, page, pageSize int) (model.DocumentChunkPage, error) {
	var total int64
	if err := s.db.Model(&chunkRow{}).Where("document_id = ?", documentID).Count(&total).Error; err != nil {
		return model.DocumentChunkPage{}, err
	}
	totalPages := 0
	if total > 0 {
		totalPages = (int(total) + pageSize - 1) / pageSize
	}
	if totalPages > 0 && page > totalPages {
		page = totalPages
	}

	var rows []chunkRow
	offset := (page - 1) * pageSize
	if err := s.db.Where("document_id = ?", documentID).
		Order("chunk_index asc").
		Limit(pageSize).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return model.DocumentChunkPage{}, err
	}
	chunks := make([]model.DocumentChunk, len(rows))
	for i, r := range rows {
		chunks[i] = chunkFromRow(r)
	}
	return model.DocumentChunkPage{
		Items:      chunks,
		Page:       page,
		PageSize:   pageSize,
		Total:      int(total),
		TotalPages: totalPages,
		HasNext:    totalPages > 0 && page < totalPages,
		HasPrev:    totalPages > 0 && page > 1,
	}, nil
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
	TOTPSecret   string     `gorm:"column:totp_secret"`
	TOTPEnabled  bool       `gorm:"column:totp_enabled"`
}

func (userRow) TableName() string { return "users" }

type knowledgeBaseRow struct {
	KnowledgeBaseID string    `gorm:"column:knowledge_base_id;primaryKey"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
	CreatedBy       string    `gorm:"column:created_by"`
	Dim             int       `gorm:"column:dim"`
	Model           string    `gorm:"column:model"`
	Analyzer        string    `gorm:"column:analyzer"`
	ChunkSize       int       `gorm:"column:chunk_size"`
	ChunkOverlap    int       `gorm:"column:chunk_overlap"`
	MinChunks       int       `gorm:"column:min_chunks"`
}

func (knowledgeBaseRow) TableName() string { return "knowledge_bases" }

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
	HumanReview       bool           `gorm:"column:human_review"`
	UploaderID        string         `gorm:"column:uploader_id"`
	UploaderName      string         `gorm:"column:uploader_name"`
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
		TOTPSecret:   r.TOTPSecret,
		TOTPEnabled:  r.TOTPEnabled,
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
		TOTPSecret:   u.TOTPSecret,
		TOTPEnabled:  u.TOTPEnabled,
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
		HumanReview:       r.HumanReview,
		UploaderID:        r.UploaderID,
		UploaderName:      r.UploaderName,
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
		HumanReview:       d.HumanReview,
		UploaderID:        d.UploaderID,
		UploaderName:      d.UploaderName,
	}
}

func knowledgeBaseFromRow(r knowledgeBaseRow) model.KnowledgeBase {
	return model.KnowledgeBase{
		KnowledgeBaseID: r.KnowledgeBaseID,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
		CreatedBy:       r.CreatedBy,
		Dim:             r.Dim,
		Model:           r.Model,
		Analyzer:        r.Analyzer,
		ChunkSize:       r.ChunkSize,
		ChunkOverlap:    r.ChunkOverlap,
		MinChunks:       r.MinChunks,
	}
}

func knowledgeBaseToRow(kb model.KnowledgeBase) knowledgeBaseRow {
	return knowledgeBaseRow{
		KnowledgeBaseID: kb.KnowledgeBaseID,
		CreatedAt:       kb.CreatedAt,
		UpdatedAt:       kb.UpdatedAt,
		CreatedBy:       kb.CreatedBy,
		Dim:             kb.Dim,
		Model:           kb.Model,
		Analyzer:        kb.Analyzer,
		ChunkSize:       kb.ChunkSize,
		ChunkOverlap:    kb.ChunkOverlap,
		MinChunks:       kb.MinChunks,
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
