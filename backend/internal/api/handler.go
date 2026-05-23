package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/service"
)

type Handler struct {
	cfg             *viper.Viper
	authService     *service.AuthService
	documentService *service.DocumentService
}

func NewHandler(cfg *viper.Viper, authService *service.AuthService, documentService *service.DocumentService) *Handler {
	return &Handler{
		cfg:             cfg,
		authService:     authService,
		documentService: documentService,
	}
}

func (h *Handler) Routes() http.Handler {
	router := gin.New()
	router.Use(gin.Recovery(), RequestLogger())

	apiGroup := router.Group("/api")
	apiGroup.POST("/login", h.handleLogin)
	apiGroup.POST("/logout", h.withAuth(), h.handleLogout)
	apiGroup.GET("/me", h.withAuth(), h.handleMe)
	apiGroup.POST("/documents", h.withAuth(), h.handleCreateDocument)
	apiGroup.GET("/documents", h.withAuth(), h.handleListDocuments)
	apiGroup.GET("/documents/:document_id", h.withAuth(), h.handleGetDocument)
	apiGroup.DELETE("/documents/:document_id", h.withAuth(), h.handleDeleteDocument)
	apiGroup.GET("/documents/:document_id/chunks", h.withAuth(), h.handleGetChunks)
	apiGroup.POST("/documents/:document_id/chunks/rechunk", h.withAuth(), h.handleRechunk)
	apiGroup.POST("/documents/:document_id/chunks/approve", h.withAuth(), h.handleApproveChunks)
	apiGroup.POST("/documents/:document_id/chunks/merge", h.withAuth(), h.handleMergeChunks)
	apiGroup.POST("/documents/:document_id/chunks/:chunk_id/reject", h.withAuth(), h.handleRejectChunk)
	apiGroup.POST("/documents/:document_id/index", h.withAuth(), h.handleIndexDocument)
	apiGroup.POST("/query", h.withAuth(), h.handleQuery)
	apiGroup.GET("/knowledge-bases", h.withAuth(), h.handleListKnowledgeBases)

	return router
}

func (h *Handler) handleLogin(c *gin.Context) {
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&request); err != nil {
		writeError(c, 400, "validation_error", "invalid request body", nil)
		return
	}
	if strings.TrimSpace(request.Username) == "" || strings.TrimSpace(request.Password) == "" {
		writeError(c, 400, "validation_error", "username and password are required", []fieldError{
			{Field: "username", Reason: "required"},
			{Field: "password", Reason: "required"},
		})
		return
	}

	user, token, err := h.authService.Login(request.Username, request.Password)
	if err != nil {
		writeError(c, 401, "unauthorized", err.Error(), nil)
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.cfg.GetString("http.session_cookie"), token, 3600*24, "/", "", false, true)
	writeData(c, 200, sanitizeUser(user))
}

func (h *Handler) handleLogout(c *gin.Context) {
	cookie, err := c.Request.Cookie(h.cfg.GetString("http.session_cookie"))
	if err == nil {
		_ = h.authService.Logout(cookie.Value)
	}
	c.SetCookie(h.cfg.GetString("http.session_cookie"), "", -1, "/", "", false, true)
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleMe(c *gin.Context) {
	user := c.MustGet("current_user").(model.User)
	writeData(c, 200, sanitizeUser(user))
}

func (h *Handler) handleCreateDocument(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(64 << 20); err != nil {
		writeError(c, 400, "validation_error", "invalid multipart form", nil)
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		writeError(c, 400, "validation_error", "file is required", []fieldError{{Field: "file", Reason: "required"}})
		return
	}
	defer file.Close()

	knowledgeBaseID := strings.TrimSpace(c.Request.FormValue("knowledge_base_id"))
	if knowledgeBaseID == "" {
		writeError(c, 400, "validation_error", "knowledge_base_id is required", []fieldError{{Field: "knowledge_base_id", Reason: "required"}})
		return
	}

	document, err := h.documentService.CreateDocument(
		file,
		header,
		knowledgeBaseID,
		c.Request.FormValue("title"),
		c.Request.MultipartForm.Value["tags"],
	)
	if err != nil {
		status := 400
		code := "validation_error"
		if strings.Contains(err.Error(), "unsupported file type") {
			status = 415
			code = "unsupported_file_type"
		}
		if strings.Contains(err.Error(), "already exists") {
			status = 409
			code = "conflict"
		}
		writeError(c, status, code, err.Error(), nil)
		return
	}

	writeData(c, 202, document)
}

func (h *Handler) handleListDocuments(c *gin.Context) {
	kb := strings.TrimSpace(c.Query("knowledge_base_id"))
	documents := h.documentService.ListDocuments(kb)
	items := make([]any, 0, len(documents))
	for _, document := range documents {
		items = append(items, document)
	}
	writeData(c, 200, map[string]any{
		"items":     items,
		"page":      1,
		"page_size": len(items),
		"total":     len(items),
	})
}

func (h *Handler) handleGetDocument(c *gin.Context) {
	documentID := c.Param("document_id")
	document, err := h.documentService.GetDocument(documentID)
	if err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 200, document)
}

func (h *Handler) handleDeleteDocument(c *gin.Context) {
	documentID := c.Param("document_id")
	if err := h.documentService.DeleteDocument(documentID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	c.Status(204)
}

func (h *Handler) handleGetChunks(c *gin.Context) {
	documentID := c.Param("document_id")
	if _, err := h.documentService.GetDocument(documentID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	chunks, err := h.documentService.GetChunks(documentID)
	if err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 200, map[string]any{
		"items":     chunks,
		"page":      1,
		"page_size": len(chunks),
		"total":     len(chunks),
	})
}

func (h *Handler) handleRechunk(c *gin.Context) {
	documentID := c.Param("document_id")
	if err := h.documentService.RechunkDocument(documentID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 202, map[string]any{"accepted": true})
}

func (h *Handler) handleApproveChunks(c *gin.Context) {
	documentID := c.Param("document_id")
	if err := h.documentService.ApproveChunks(documentID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleRejectChunk(c *gin.Context) {
	documentID := c.Param("document_id")
	chunkID := c.Param("chunk_id")
	if err := h.documentService.RejectChunk(documentID, chunkID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleMergeChunks(c *gin.Context) {
	documentID := c.Param("document_id")
	var body struct {
		ChunkIDs []string `json:"chunk_ids"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil || len(body.ChunkIDs) < 2 {
		writeError(c, 400, "validation_error", "chunk_ids must contain at least 2 IDs", nil)
		return
	}
	if err := h.documentService.MergeChunks(documentID, body.ChunkIDs); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleIndexDocument(c *gin.Context) {
	documentID := c.Param("document_id")
	if err := h.documentService.IndexDocument(documentID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 202, map[string]any{"accepted": true})
}

func (h *Handler) handleListKnowledgeBases(c *gin.Context) {
	all := h.documentService.ListDocuments("")
	counts := make(map[string]int)
	for _, d := range all {
		counts[d.KnowledgeBaseID]++
	}
	type kbItem struct {
		KnowledgeBaseID string `json:"knowledge_base_id"`
		DocumentCount   int    `json:"document_count"`
	}
	items := make([]any, 0, len(counts))
	for kb, n := range counts {
		items = append(items, kbItem{KnowledgeBaseID: kb, DocumentCount: n})
	}
	writeData(c, 200, map[string]any{"items": items})
}

func (h *Handler) handleQuery(c *gin.Context) {
	var body struct {
		KnowledgeBaseID string `json:"knowledge_base_id"`
		Query           string `json:"query"`
		TopK            int    `json:"top_k"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		writeError(c, 400, "validation_error", "invalid request body", nil)
		return
	}
	if strings.TrimSpace(body.Query) == "" {
		writeError(c, 400, "validation_error", "query is required", []fieldError{{Field: "query", Reason: "required"}})
		return
	}

	qr, err := h.documentService.Query(body.KnowledgeBaseID, body.Query, body.TopK)
	if err != nil {
		writeError(c, 500, "internal_error", err.Error(), nil)
		return
	}

	items := make([]any, 0, len(qr.Items))
	for _, r := range qr.Items {
		items = append(items, r)
	}
	writeData(c, 200, map[string]any{
		"items":             items,
		"answer":            qr.Answer,
		"query":             body.Query,
		"knowledge_base_id": body.KnowledgeBaseID,
	})
}

func (h *Handler) withAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Request.Cookie(h.cfg.GetString("http.session_cookie"))
		if err != nil || cookie.Value == "" {
			writeError(c, 401, "unauthorized", "login required", nil)
			c.Abort()
			return
		}
		user, err := h.authService.Me(cookie.Value)
		if err != nil {
			writeError(c, 401, "unauthorized", "invalid session", nil)
			c.Abort()
			return
		}
		c.Set("current_user", user)
		c.Next()
	}
}

func (h *Handler) writeStoreError(c *gin.Context, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		writeError(c, 404, "not_found", "resource not found", nil)
		return
	}
	writeError(c, 500, "internal_error", err.Error(), nil)
}

func sanitizeUser(user model.User) map[string]any {
	return map[string]any{
		"user_id":       user.UserID,
		"username":      user.Username,
		"status":        user.Status,
		"last_login_at": user.LastLoginAt,
		"created_at":    user.CreatedAt,
		"updated_at":    user.UpdatedAt,
	}
}
