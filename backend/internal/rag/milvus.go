package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	milvusindex "github.com/milvus-io/milvus/client/v2/index"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

var searchOutputFields = []string{
	"document_id",
	"chunk_id",
	"knowledge_base_id",
	"filename",
	"source_type",
	"section_title",
	"page_start",
	"page_end",
	"chunk_index",
	"text",
}

// Milvus implements VectorStore using the official Milvus gRPC client v2.
// Requires Milvus 2.5+ for BM25 full-text search support.
type Milvus struct {
	client *milvusclient.Client
}

func NewMilvus(addr, db string) (*Milvus, error) {
	cfg := &milvusclient.ClientConfig{
		Address: addr,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := milvusclient.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("milvus connect: %w", err)
	}

	m := &Milvus{client: c}

	if db != "" {
		if err := m.ensureDatabase(ctx, db); err != nil {
			_ = c.Close(ctx)
			return nil, err
		}
		if err := c.UseDatabase(ctx, milvusclient.NewUseDatabaseOption(db)); err != nil {
			_ = c.Close(ctx)
			return nil, fmt.Errorf("milvus use database %q: %w", db, err)
		}
	}
	return m, nil
}

func (m *Milvus) Close(ctx context.Context) error {
	return m.client.Close(ctx)
}

func (m *Milvus) ValidateKnowledgeBase(kbID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	has, err := m.client.HasCollection(ctx, milvusclient.NewHasCollectionOption(kbID))
	if err != nil {
		return fmt.Errorf("milvus has_collection %q: %w", kbID, err)
	}
	if !has {
		return fmt.Errorf("knowledge base %q is not configured", kbID)
	}

	return nil
}

func (m *Milvus) CreateKnowledgeBase(ctx context.Context, cfg CollectionConfig) error {
	return m.ensureCollection(ctx, cfg)
}

func (m *Milvus) DeleteKnowledgeBase(ctx context.Context, kbID string) error {
	err := m.client.DropCollection(ctx, milvusclient.NewDropCollectionOption(kbID))
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not exist") {
		return fmt.Errorf("milvus drop_collection %q: %w", kbID, err)
	}
	return nil
}

func (m *Milvus) Upsert(ctx context.Context, records []VectorRecord) error {
	if len(records) == 0 {
		return nil
	}
	collection := records[0].KnowledgeBaseID
	if err := m.ValidateKnowledgeBase(collection); err != nil {
		return err
	}
	dim := len(records[0].Embedding)

	ids := make([]string, len(records))
	kbIDs := make([]string, len(records))
	docIDs := make([]string, len(records))
	chunkIDs := make([]string, len(records))
	filenames := make([]string, len(records))
	sourceTypes := make([]string, len(records))
	sectionTitles := make([]string, len(records))
	pageStarts := make([]int32, len(records))
	pageEnds := make([]int32, len(records))
	chunkIdxs := make([]int32, len(records))
	texts := make([]string, len(records))
	embeddings := make([][]float32, len(records))

	for i, r := range records {
		ids[i] = r.ID
		kbIDs[i] = r.KnowledgeBaseID
		docIDs[i] = r.DocumentID
		chunkIDs[i] = r.ChunkID
		filenames[i] = r.Filename
		sourceTypes[i] = r.SourceType
		sectionTitles[i] = r.SectionTitle
		pageStarts[i] = int32(r.PageStart)
		pageEnds[i] = int32(r.PageEnd)
		chunkIdxs[i] = int32(r.ChunkIndex)
		texts[i] = r.Text
		embeddings[i] = r.Embedding
	}

	_, err := m.client.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(collection,
		column.NewColumnVarChar("id", ids),
		column.NewColumnVarChar("knowledge_base_id", kbIDs),
		column.NewColumnVarChar("document_id", docIDs),
		column.NewColumnVarChar("chunk_id", chunkIDs),
		column.NewColumnVarChar("filename", filenames),
		column.NewColumnVarChar("source_type", sourceTypes),
		column.NewColumnVarChar("section_title", sectionTitles),
		column.NewColumnInt32("page_start", pageStarts),
		column.NewColumnInt32("page_end", pageEnds),
		column.NewColumnInt32("chunk_index", chunkIdxs),
		column.NewColumnVarChar("text", texts),
		column.NewColumnFloatVector("embedding", dim, embeddings),
	))
	return err
}

func (m *Milvus) DeleteByDocument(ctx context.Context, knowledgeBaseID, documentID string) error {
	if err := m.ValidateKnowledgeBase(knowledgeBaseID); err != nil {
		return err
	}
	_, err := m.client.Delete(ctx, milvusclient.NewDeleteOption(knowledgeBaseID).
		WithExpr(fmt.Sprintf(`document_id == "%s"`, documentID)))
	return err
}

func (m *Milvus) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := m.ValidateKnowledgeBase(req.KnowledgeBaseID); err != nil {
		return nil, err
	}
	filter := buildDocFilter(req.DocumentIDs)

	var annReqs []*milvusclient.AnnRequest
	switch req.Mode {
	case SearchModeBM25:
		annReqs = []*milvusclient.AnnRequest{
			bm25AnnReq(req.Query, req.TopK, req.DropRatio, filter),
		}
	case SearchModeHybrid:
		annReqs = []*milvusclient.AnnRequest{
			denseAnnReq(req.Embedding, req.TopK, req.EF, filter),
			bm25AnnReq(req.Query, req.TopK, req.DropRatio, filter),
		}
	default:
		annReqs = []*milvusclient.AnnRequest{
			denseAnnReq(req.Embedding, req.TopK, req.EF, filter),
		}
	}

	opt := milvusclient.NewHybridSearchOption(req.KnowledgeBaseID, req.TopK, annReqs...).
		WithOutputFields(searchOutputFields...)
	if req.Mode == SearchModeHybrid {
		rrf := milvusclient.NewRRFReranker()
		if req.RRFK > 0 {
			rrf = rrf.WithK(float64(req.RRFK))
		}
		opt = opt.WithReranker(rrf)
	}

	resultSets, err := m.client.HybridSearch(ctx, opt)
	if err != nil {
		return nil, err
	}
	if len(resultSets) == 0 {
		return []SearchResult{}, nil
	}

	return parseResults(resultSets[0]), nil
}

func denseAnnReq(embedding []float32, topK, ef int, filter string) *milvusclient.AnnRequest {
	r := milvusclient.NewAnnRequest("embedding", topK, entity.FloatVector(embedding))
	if ef > 0 {
		r = r.WithSearchParam("ef", fmt.Sprintf("%d", ef))
	}
	if filter != "" {
		r = r.WithFilter(filter)
	}
	return r
}

func bm25AnnReq(query string, topK int, dropRatio float64, filter string) *milvusclient.AnnRequest {
	r := milvusclient.NewAnnRequest("sparse", topK, entity.Text(query))
	if dropRatio > 0 {
		r = r.WithSearchParam("drop_ratio_search", fmt.Sprintf("%g", dropRatio))
	}
	if filter != "" {
		r = r.WithFilter(filter)
	}
	return r
}

func buildDocFilter(documentIDs []string) string {
	if len(documentIDs) == 0 {
		return ""
	}
	if len(documentIDs) == 1 {
		return fmt.Sprintf(`document_id == "%s"`, documentIDs[0])
	}
	quoted := make([]string, len(documentIDs))
	for i, id := range documentIDs {
		quoted[i] = fmt.Sprintf(`"%s"`, id)
	}
	return fmt.Sprintf("document_id in [%s]", strings.Join(quoted, ", "))
}

func parseResults(rs milvusclient.ResultSet) []SearchResult {
	results := make([]SearchResult, 0, rs.ResultCount)
	for i := range rs.ResultCount {
		score := float32(0)
		if i < len(rs.Scores) {
			score = rs.Scores[i]
		}
		results = append(results, SearchResult{
			ChunkID:         colStr(rs, "chunk_id", i),
			DocumentID:      colStr(rs, "document_id", i),
			KnowledgeBaseID: colStr(rs, "knowledge_base_id", i),
			Filename:        colStr(rs, "filename", i),
			SourceType:      colStr(rs, "source_type", i),
			SectionTitle:    colStr(rs, "section_title", i),
			PageStart:       colInt(rs, "page_start", i),
			PageEnd:         colInt(rs, "page_end", i),
			ChunkIndex:      colInt(rs, "chunk_index", i),
			Text:            colStr(rs, "text", i),
			Score:           score,
		})
	}
	return results
}

func colStr(rs milvusclient.ResultSet, field string, i int) string {
	col := rs.GetColumn(field)
	if col == nil {
		return ""
	}
	v, _ := col.GetAsString(i)
	return v
}

func colInt(rs milvusclient.ResultSet, field string, i int) int {
	col := rs.GetColumn(field)
	if col == nil {
		return 0
	}
	v, _ := col.GetAsInt64(i)
	return int(v)
}

func (m *Milvus) ensureDatabase(ctx context.Context, db string) error {
	err := m.client.CreateDatabase(ctx, milvusclient.NewCreateDatabaseOption(db))
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "exist") {
		return fmt.Errorf("milvus create database %q: %w", db, err)
	}
	return nil
}

func (m *Milvus) ensureCollection(ctx context.Context, cfg CollectionConfig) error {
	name := cfg.Name
	analyzer := cfg.Analyzer
	if analyzer == "" {
		analyzer = "chinese"
	}

	has, err := m.client.HasCollection(ctx, milvusclient.NewHasCollectionOption(name))
	if err != nil {
		return fmt.Errorf("milvus has_collection %q: %w", name, err)
	}
	if has {
		col, err := m.client.DescribeCollection(ctx, milvusclient.NewDescribeCollectionOption(name))
		if err != nil {
			return fmt.Errorf("milvus describe_collection %q: %w", name, err)
		}
		hasSparse := false
		existingAnalyzer := ""
		for _, f := range col.Schema.Fields {
			if f.Name == "sparse" {
				hasSparse = true
			}
			if f.Name == "text" {
				if p, ok := f.TypeParams["analyzer_params"]; ok {
					existingAnalyzer = p
				}
			}
		}
		schemaOK := hasSparse && (existingAnalyzer == "" || analyzerType(existingAnalyzer) == analyzer)
		if schemaOK {
			task, err := m.client.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(name))
			if err != nil {
				return fmt.Errorf("milvus load_collection %q: %w", name, err)
			}
			return task.Await(ctx)
		}
		// Schema mismatch (missing sparse or different analyzer) — drop and recreate
		if err := m.client.DropCollection(ctx, milvusclient.NewDropCollectionOption(name)); err != nil {
			return fmt.Errorf("milvus drop_collection %q: %w", name, err)
		}
	}

	textField := entity.NewField().
		WithName("text").
		WithDataType(entity.FieldTypeVarChar).
		WithMaxLength(65535).
		WithEnableAnalyzer(true).
		WithAnalyzerParams(map[string]any{"type": analyzer})

	schema := entity.NewSchema().
		WithName(name).
		WithField(entity.NewField().WithName("id").
			WithDataType(entity.FieldTypeVarChar).
			WithIsPrimaryKey(true).
			WithMaxLength(64)).
		WithField(entity.NewField().WithName("knowledge_base_id").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(64)).
		WithField(entity.NewField().WithName("document_id").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(64)).
		WithField(entity.NewField().WithName("chunk_id").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(64)).
		WithField(entity.NewField().WithName("filename").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(512)).
		WithField(entity.NewField().WithName("source_type").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(32)).
		WithField(entity.NewField().WithName("section_title").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(512)).
		WithField(entity.NewField().WithName("page_start").WithDataType(entity.FieldTypeInt32)).
		WithField(entity.NewField().WithName("page_end").WithDataType(entity.FieldTypeInt32)).
		WithField(entity.NewField().WithName("chunk_index").WithDataType(entity.FieldTypeInt32)).
		WithField(textField).
		WithField(entity.NewField().WithName("embedding").
			WithDataType(entity.FieldTypeFloatVector).
			WithDim(int64(cfg.Dim))).
		WithField(entity.NewField().WithName("sparse").WithDataType(entity.FieldTypeSparseVector)).
		WithFunction(entity.NewFunction().
			WithName("bm25").
			WithType(entity.FunctionTypeBM25).
			WithInputFields("text").
			WithOutputFields("sparse"))

	if err := m.client.CreateCollection(
		ctx, milvusclient.NewCreateCollectionOption(name, schema),
	); err != nil {
		return fmt.Errorf("milvus create_collection %q: %w", name, err)
	}

	denseTask, err := m.client.CreateIndex(ctx, milvusclient.NewCreateIndexOption(
		name, "embedding", milvusindex.NewHNSWIndex(entity.L2, 16, 64),
	))
	if err != nil {
		return fmt.Errorf("milvus create_index embedding %q: %w", name, err)
	}
	if err := denseTask.Await(ctx); err != nil {
		return fmt.Errorf("milvus index embedding await %q: %w", name, err)
	}

	sparseTask, err := m.client.CreateIndex(ctx, milvusclient.NewCreateIndexOption(
		name, "sparse", milvusindex.NewSparseInvertedIndex(entity.BM25, 0.0),
	))
	if err != nil {
		return fmt.Errorf("milvus create_index sparse %q: %w", name, err)
	}
	if err := sparseTask.Await(ctx); err != nil {
		return fmt.Errorf("milvus index sparse await %q: %w", name, err)
	}

	loadTask, err := m.client.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(name))
	if err != nil {
		return fmt.Errorf("milvus load_collection %q: %w", name, err)
	}
	return loadTask.Await(ctx)
}

// analyzerType extracts the "type" field from a Milvus analyzer_params JSON string.
// Falls back to the raw string when the value is not valid JSON, so a plain type
// name (e.g. "chinese") also compares correctly.
func analyzerType(raw string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return raw
	}
	if t, ok := m["type"].(string); ok {
		return t
	}
	return raw
}
