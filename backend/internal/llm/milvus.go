package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Milvus implements VectorStore using the Milvus RESTful API v2 (Milvus 2.4+).
// Each knowledge base maps to a separate collection.
type Milvus struct {
	baseURL     string
	db          string
	collections map[string]int // collection name → vector dim
	client      *http.Client
}

func NewMilvus(addr, db string, cols []CollectionConfig) (*Milvus, error) {
	base := addr
	if !strings.HasPrefix(base, "http") {
		base = "http://" + base
	}
	m := &Milvus{
		baseURL:     strings.TrimRight(base, "/"),
		db:          db,
		collections: make(map[string]int, len(cols)),
		client:      &http.Client{Timeout: 30 * time.Second},
	}
	for _, c := range cols {
		m.collections[c.Name] = c.Dim
	}
	ctx := context.Background()
	if err := m.ensureDatabase(ctx); err != nil {
		return nil, err
	}
	for _, c := range cols {
		if err := m.ensureCollection(ctx, c.Name, c.Dim); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Milvus) ValidateKnowledgeBase(kbID string) error {
	if _, ok := m.collections[kbID]; !ok {
		return fmt.Errorf("knowledge base %q is not configured in milvus.collections", kbID)
	}
	return nil
}

func (m *Milvus) ListKnowledgeBases() []string {
	names := make([]string, 0, len(m.collections))
	for name := range m.collections {
		names = append(names, name)
	}
	return names
}

func (m *Milvus) Upsert(ctx context.Context, records []VectorRecord) error {
	if len(records) == 0 {
		return nil
	}
	collection := records[0].KnowledgeBaseID
	if err := m.ValidateKnowledgeBase(collection); err != nil {
		return err
	}

	data := make([]map[string]any, len(records))
	for i, r := range records {
		data[i] = map[string]any{
			"id":                r.ID,
			"knowledge_base_id": r.KnowledgeBaseID,
			"document_id":       r.DocumentID,
			"chunk_id":          r.ChunkID,
			"filename":          r.Filename,
			"source_type":       r.SourceType,
			"section_title":     r.SectionTitle,
			"page_start":        r.PageStart,
			"page_end":          r.PageEnd,
			"chunk_index":       r.ChunkIndex,
			"text":              r.Text,
			"embedding":         r.Embedding,
		}
	}

	return m.post(ctx, "/v2/vectordb/entities/upsert", map[string]any{
		"collectionName": collection,
		"data":           data,
	}, nil)
}

func (m *Milvus) DeleteByDocument(ctx context.Context, knowledgeBaseID, documentID string) error {
	if err := m.ValidateKnowledgeBase(knowledgeBaseID); err != nil {
		return err
	}
	return m.post(ctx, "/v2/vectordb/entities/delete", map[string]any{
		"collectionName": knowledgeBaseID,
		"filter":         fmt.Sprintf(`document_id == "%s"`, documentID),
	}, nil)
}

func (m *Milvus) Search(ctx context.Context, knowledgeBaseID string, embedding []float32, topK int) ([]SearchResult, error) {
	if err := m.ValidateKnowledgeBase(knowledgeBaseID); err != nil {
		return nil, err
	}

	req := map[string]any{
		"collectionName": knowledgeBaseID,
		"data":           [][]float32{embedding},
		"annsField":      "embedding",
		"limit":          topK,
		"outputFields": []string{
			"id", "document_id", "chunk_id", "knowledge_base_id",
			"filename", "source_type", "section_title",
			"page_start", "page_end", "chunk_index", "text",
		},
	}

	var resp struct {
		Code int `json:"code"`
		Data []struct {
			ID              string  `json:"id"`
			DocumentID      string  `json:"document_id"`
			ChunkID         string  `json:"chunk_id"`
			KnowledgeBaseID string  `json:"knowledge_base_id"`
			Filename        string  `json:"filename"`
			SourceType      string  `json:"source_type"`
			SectionTitle    string  `json:"section_title"`
			PageStart       int     `json:"page_start"`
			PageEnd         int     `json:"page_end"`
			ChunkIndex      int     `json:"chunk_index"`
			Text            string  `json:"text"`
			Distance        float32 `json:"distance"`
		} `json:"data"`
	}

	if err := m.post(ctx, "/v2/vectordb/entities/search", req, &resp); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(resp.Data))
	for _, d := range resp.Data {
		results = append(results, SearchResult{
			ChunkID:         d.ChunkID,
			DocumentID:      d.DocumentID,
			KnowledgeBaseID: d.KnowledgeBaseID,
			Filename:        d.Filename,
			SourceType:      d.SourceType,
			SectionTitle:    d.SectionTitle,
			PageStart:       d.PageStart,
			PageEnd:         d.PageEnd,
			ChunkIndex:      d.ChunkIndex,
			Text:            d.Text,
			Score:           1 - d.Distance,
		})
	}
	return results, nil
}

func (m *Milvus) ensureDatabase(ctx context.Context) error {
	if m.db == "" {
		return nil
	}
	raw, err := json.Marshal(map[string]any{"dbName": m.db})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/v2/vectordb/databases/create", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var apiResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("milvus create_database: %w", err)
	}
	// code 0 = created; ignore "already exists" errors
	if apiResp.Code != 0 && !strings.Contains(strings.ToLower(apiResp.Message), "exist") {
		return fmt.Errorf("milvus create_database %q: code %d: %s", m.db, apiResp.Code, apiResp.Message)
	}
	return nil
}

func (m *Milvus) ensureCollection(ctx context.Context, collection string, dim int) error {
	var hasResp struct {
		Code int `json:"code"`
		Data struct {
			Has bool `json:"has"`
		} `json:"data"`
	}
	if err := m.post(ctx, "/v2/vectordb/collections/has", map[string]any{
		"collectionName": collection,
	}, &hasResp); err != nil {
		return fmt.Errorf("milvus has_collection %q: %w", collection, err)
	}

	if hasResp.Data.Has {
		return m.post(ctx, "/v2/vectordb/collections/load", map[string]any{
			"collectionName": collection,
		}, nil)
	}

	schema := map[string]any{
		"autoId": false,
		"fields": []map[string]any{
			{"fieldName": "id", "dataType": "VarChar", "isPrimary": true, "elementTypeParams": map[string]any{"max_length": "64"}},
			{"fieldName": "knowledge_base_id", "dataType": "VarChar", "elementTypeParams": map[string]any{"max_length": "64"}},
			{"fieldName": "document_id", "dataType": "VarChar", "elementTypeParams": map[string]any{"max_length": "64"}},
			{"fieldName": "chunk_id", "dataType": "VarChar", "elementTypeParams": map[string]any{"max_length": "64"}},
			{"fieldName": "filename", "dataType": "VarChar", "elementTypeParams": map[string]any{"max_length": "512"}},
			{"fieldName": "source_type", "dataType": "VarChar", "elementTypeParams": map[string]any{"max_length": "32"}},
			{"fieldName": "section_title", "dataType": "VarChar", "elementTypeParams": map[string]any{"max_length": "512"}},
			{"fieldName": "page_start", "dataType": "Int32"},
			{"fieldName": "page_end", "dataType": "Int32"},
			{"fieldName": "chunk_index", "dataType": "Int32"},
			{"fieldName": "text", "dataType": "VarChar", "elementTypeParams": map[string]any{"max_length": "65535"}},
			{"fieldName": "embedding", "dataType": "FloatVector", "elementTypeParams": map[string]any{"dim": fmt.Sprintf("%d", dim)}},
		},
	}
	if err := m.post(ctx, "/v2/vectordb/collections/create", map[string]any{
		"collectionName": collection,
		"schema":         schema,
	}, nil); err != nil {
		return fmt.Errorf("milvus create_collection %q: %w", collection, err)
	}

	if err := m.post(ctx, "/v2/vectordb/indexes/create", map[string]any{
		"collectionName": collection,
		"indexParams": []map[string]any{{
			"fieldName":  "embedding",
			"metricType": "L2",
			"indexType":  "HNSW",
			"params":     map[string]any{"M": 16, "efConstruction": 64},
		}},
	}, nil); err != nil {
		return fmt.Errorf("milvus create_index %q: %w", collection, err)
	}

	return m.post(ctx, "/v2/vectordb/collections/load", map[string]any{
		"collectionName": collection,
	}, nil)
}

func (m *Milvus) post(ctx context.Context, path string, body map[string]any, out any) error {
	if m.db != "" {
		body["dbName"] = m.db
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("milvus %s: HTTP %d: %s", path, resp.StatusCode, respBody)
	}

	var apiResp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return err
	}
	if apiResp.Code != 0 {
		return fmt.Errorf("milvus %s: code %d: %s", path, apiResp.Code, apiResp.Message)
	}

	if out != nil && len(apiResp.Data) > 0 {
		return json.Unmarshal(respBody, out)
	}
	return nil
}
