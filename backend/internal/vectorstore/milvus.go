package vectorstore

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
type Milvus struct {
	baseURL    string
	collection string
	dim        int
	client     *http.Client
}

func NewMilvus(addr, collection string, dim int) (*Milvus, error) {
	base := addr
	if !strings.HasPrefix(base, "http") {
		base = "http://" + base
	}
	m := &Milvus{
		baseURL:    strings.TrimRight(base, "/"),
		collection: collection,
		dim:        dim,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
	if err := m.ensureCollection(context.Background()); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Milvus) Upsert(ctx context.Context, records []VectorRecord) error {
	if len(records) == 0 {
		return nil
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
		"collectionName": m.collection,
		"data":           data,
	}, nil)
}

func (m *Milvus) DeleteByDocument(ctx context.Context, knowledgeBaseID, documentID string) error {
	return m.post(ctx, "/v2/vectordb/entities/delete", map[string]any{
		"collectionName": m.collection,
		"filter":         fmt.Sprintf(`document_id == "%s"`, documentID),
	}, nil)
}

func (m *Milvus) Search(ctx context.Context, knowledgeBaseID string, embedding []float32, topK int) ([]SearchResult, error) {
	filter := ""
	if knowledgeBaseID != "" {
		filter = fmt.Sprintf(`knowledge_base_id == "%s"`, knowledgeBaseID)
	}

	req := map[string]any{
		"collectionName": m.collection,
		"data":           [][]float32{embedding},
		"annsField":      "embedding",
		"limit":          topK,
		"outputFields": []string{
			"id", "document_id", "chunk_id", "knowledge_base_id",
			"filename", "source_type", "section_title",
			"page_start", "page_end", "chunk_index", "text",
		},
	}
	if filter != "" {
		req["filter"] = filter
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
			Score:           1 - d.Distance, // L2: smaller distance = higher score
		})
	}
	return results, nil
}

func (m *Milvus) ensureCollection(ctx context.Context) error {
	var hasResp struct {
		Code int  `json:"code"`
		Data bool `json:"data"`
	}
	if err := m.post(ctx, "/v2/vectordb/collections/has", map[string]any{
		"collectionName": m.collection,
	}, &hasResp); err != nil {
		return fmt.Errorf("milvus has_collection: %w", err)
	}

	if hasResp.Data {
		return m.post(ctx, "/v2/vectordb/collections/load", map[string]any{
			"collectionName": m.collection,
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
			{"fieldName": "embedding", "dataType": "FloatVector", "elementTypeParams": map[string]any{"dim": fmt.Sprintf("%d", m.dim)}},
		},
	}
	if err := m.post(ctx, "/v2/vectordb/collections/create", map[string]any{
		"collectionName": m.collection,
		"schema":         schema,
	}, nil); err != nil {
		return fmt.Errorf("milvus create_collection: %w", err)
	}

	if err := m.post(ctx, "/v2/vectordb/indexes/create", map[string]any{
		"collectionName": m.collection,
		"indexParams": []map[string]any{{
			"fieldName":  "embedding",
			"metricType": "L2",
			"indexType":  "HNSW",
			"params":     map[string]any{"M": 16, "efConstruction": 64},
		}},
	}, nil); err != nil {
		return fmt.Errorf("milvus create_index: %w", err)
	}

	return m.post(ctx, "/v2/vectordb/collections/load", map[string]any{
		"collectionName": m.collection,
	}, nil)
}

func (m *Milvus) post(ctx context.Context, path string, body any, out any) error {
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
		// out is the full response struct, not just .data
		return json.Unmarshal(respBody, out)
	}
	return nil
}
