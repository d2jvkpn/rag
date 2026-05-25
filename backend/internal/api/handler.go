package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	"backend/internal/infra"
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
	router.Use(gin.Recovery(), infra.RequestLogger())

	apiGroup := router.Group("/api")
	apiGroup.POST("/login", h.handleLogin)
	apiGroup.POST("/logout", h.withAuth(), h.handleLogout)
	apiGroup.GET("/me", h.withAuth(), h.handleMe)
	apiGroup.PUT("/me/password", h.withAuth(), h.handleChangePassword)
	apiGroup.POST("/me/totp/setup", h.withAuth(), h.handleTOTPSetup)
	apiGroup.POST("/me/totp/enable", h.withAuth(), h.handleTOTPEnable)
	apiGroup.POST("/me/totp/disable", h.withAuth(), h.handleTOTPDisable)
	apiGroup.POST("/documents", h.withAuth(), h.handleCreateDocument)
	apiGroup.GET("/documents", h.withAuth(), h.handleListDocuments)
	apiGroup.GET("/documents/:document_id", h.withAuth(), h.handleGetDocument)
	apiGroup.DELETE("/documents/:document_id", h.withAuth(), h.withDocumentOwner(), h.handleDeleteDocument)
	apiGroup.GET("/documents/:document_id/chunks", h.withAuth(), h.handleGetChunks)
	apiGroup.POST("/documents/:document_id/chunks/rechunk", h.withAuth(), h.withDocumentOwner(), h.handleRechunk)
	apiGroup.POST("/documents/:document_id/chunks/approve", h.withAuth(), h.withDocumentOwner(), h.handleApproveChunks)
	apiGroup.POST("/documents/:document_id/chunks/merge", h.withAuth(), h.withDocumentOwner(), h.handleMergeChunks)
	apiGroup.PUT("/documents/:document_id/chunks/:chunk_id", h.withAuth(), h.withDocumentOwner(), h.handleEditChunk)
	apiGroup.POST("/documents/:document_id/chunks/:chunk_id/reject", h.withAuth(), h.withDocumentOwner(), h.handleRejectChunk)
	apiGroup.POST("/documents/:document_id/chunks/:chunk_id/restore", h.withAuth(), h.withDocumentOwner(), h.handleRestoreChunk)
	apiGroup.POST("/documents/:document_id/index", h.withAuth(), h.withDocumentOwner(), h.handleIndexDocument)
	apiGroup.POST("/query", h.withAuth(), h.handleQuery)
	apiGroup.GET("/knowledge-bases", h.withAuth(), h.handleListKnowledgeBases)
	apiGroup.GET("/knowledge-bases/available", h.withAuth(), h.handleListAvailableKnowledgeBases)

	router.NoRoute(func(c *gin.Context) {
		writeError(c, 404, "not_found", "route not found", nil)
	})

	return router
}

func (h *Handler) handleLogin(c *gin.Context) {
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
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

	user, token, err := h.authService.Login(request.Username, request.Password, request.TOTPCode)
	if err != nil {
		if errors.Is(err, service.ErrTOTPRequired) {
			writeData(c, 200, map[string]any{"totp_required": true})
			return
		}
		writeError(c, 401, "unauthorized", err.Error(), nil)
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.cfg.GetString("http.session_cookie"), token, int(h.authService.TokenTTL().Seconds()), "/", "", gin.Mode() == gin.ReleaseMode, true)
	writeData(c, 200, sanitizeUser(user))
}

func (h *Handler) handleLogout(c *gin.Context) {
	cookie, err := c.Request.Cookie(h.cfg.GetString("http.session_cookie"))
	if err == nil {
		_ = h.authService.Logout(cookie.Value)
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.cfg.GetString("http.session_cookie"), "", -1, "/", "", gin.Mode() == gin.ReleaseMode, true)
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleMe(c *gin.Context) {
	user := c.MustGet("current_user").(model.User)
	writeData(c, 200, sanitizeUser(user))
}

func (h *Handler) handleChangePassword(c *gin.Context) {
	user := c.MustGet("current_user").(model.User)
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		writeError(c, 400, "validation_error", "invalid request body", nil)
		return
	}
	var errs []fieldError
	if strings.TrimSpace(body.OldPassword) == "" {
		errs = append(errs, fieldError{Field: "old_password", Reason: "required"})
	}
	if strings.TrimSpace(body.NewPassword) == "" {
		errs = append(errs, fieldError{Field: "new_password", Reason: "required"})
	}
	if len(errs) > 0 {
		writeError(c, 400, "validation_error", "missing required fields", errs)
		return
	}
	if err := h.authService.ChangePassword(user.UserID, body.OldPassword, body.NewPassword); err != nil {
		if err.Error() == "incorrect current password" {
			writeError(c, 400, "validation_error", err.Error(), []fieldError{{Field: "old_password", Reason: "incorrect"}})
			return
		}
		writeError(c, 500, "internal_error", err.Error(), nil)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
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

	humanReview := c.Request.FormValue("human_review") == "true"
	uploader := c.MustGet("current_user").(model.User)
	document, err := h.documentService.CreateDocument(
		file,
		header,
		knowledgeBaseID,
		c.Request.FormValue("title"),
		c.Request.MultipartForm.Value["tags"],
		humanReview,
		uploader.UserID,
		uploader.Username,
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
	chunks, err := h.documentService.GetChunks(documentID)
	if err != nil {
		h.writeStoreError(c, err)
		return
	}
	if chunks == nil {
		chunks = []model.DocumentChunk{}
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

func (h *Handler) handleEditChunk(c *gin.Context) {
	documentID := c.Param("document_id")
	chunkID := c.Param("chunk_id")
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		writeError(c, 400, "validation_error", "invalid request body", nil)
		return
	}
	if err := h.documentService.EditChunk(documentID, chunkID, body.Text); err != nil {
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

func (h *Handler) handleRestoreChunk(c *gin.Context) {
	documentID := c.Param("document_id")
	chunkID := c.Param("chunk_id")
	if err := h.documentService.RestoreChunk(documentID, chunkID); err != nil {
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

func (h *Handler) handleListAvailableKnowledgeBases(c *gin.Context) {
	cfgs := h.documentService.ListAvailableKnowledgeBases()
	items := make([]any, 0, len(cfgs))
	for _, cfg := range cfgs {
		items = append(items, map[string]any{
			"knowledge_base_id": cfg.Name,
			"dim":               cfg.Dim,
			"analyzer":          cfg.Analyzer,
			"chunk_size":        cfg.ChunkSize,
			"chunk_overlap":     cfg.ChunkOverlap,
		})
	}
	writeData(c, 200, map[string]any{"items": items})
}

func (h *Handler) handleQuery(c *gin.Context) {
	var body struct {
		KnowledgeBaseID string   `json:"knowledge_base_id"`
		Query           string   `json:"query"`
		TopK            int      `json:"top_k"`
		DocumentIDs     []string `json:"document_ids"`
		SearchMode      string   `json:"search_mode"`
		EF              int      `json:"ef"`
		DropRatio       float64  `json:"drop_ratio"`
		RRFK            int      `json:"rrf_k"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		writeError(c, 400, "validation_error", "invalid request body", nil)
		return
	}
	if strings.TrimSpace(body.KnowledgeBaseID) == "" {
		writeError(c, 400, "validation_error", "knowledge_base_id is required", []fieldError{{Field: "knowledge_base_id", Reason: "required"}})
		return
	}
	if strings.TrimSpace(body.Query) == "" {
		writeError(c, 400, "validation_error", "query is required", []fieldError{{Field: "query", Reason: "required"}})
		return
	}

	qr, err := h.documentService.Query(body.KnowledgeBaseID, body.Query, body.TopK, service.QueryOptions{
		DocumentIDs: body.DocumentIDs,
		SearchMode:  body.SearchMode,
		EF:          body.EF,
		DropRatio:   body.DropRatio,
		RRFK:        body.RRFK,
	})
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

func (h *Handler) withDocumentOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := c.MustGet("current_user").(model.User)
		doc, err := h.documentService.GetDocument(c.Param("document_id"))
		if err != nil {
			h.writeStoreError(c, err)
			c.Abort()
			return
		}
		if doc.UploaderID != "" && doc.UploaderID != user.UserID {
			writeError(c, 403, "forbidden", "you can only modify your own documents", nil)
			c.Abort()
			return
		}
		c.Next()
	}
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

func (h *Handler) handleTOTPSetup(c *gin.Context) {
	user := c.MustGet("current_user").(model.User)
	secret, qrURL, err := h.authService.SetupTOTP(user.UserID)
	if err != nil {
		writeError(c, 500, "internal_error", err.Error(), nil)
		return
	}
	writeData(c, 200, map[string]any{"secret": secret, "qr_url": qrURL})
}

func (h *Handler) handleTOTPEnable(c *gin.Context) {
	user := c.MustGet("current_user").(model.User)
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil || body.Code == "" {
		writeError(c, 400, "validation_error", "code is required", nil)
		return
	}
	if err := h.authService.EnableTOTP(user.UserID, body.Code); err != nil {
		writeError(c, 400, "validation_error", err.Error(), nil)
		return
	}
	writeData(c, 200, map[string]any{"enabled": true})
}

func (h *Handler) handleTOTPDisable(c *gin.Context) {
	user := c.MustGet("current_user").(model.User)
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil || body.Code == "" {
		writeError(c, 400, "validation_error", "code is required", nil)
		return
	}
	if err := h.authService.DisableTOTP(user.UserID, body.Code); err != nil {
		writeError(c, 400, "validation_error", err.Error(), nil)
		return
	}
	writeData(c, 200, map[string]any{"enabled": false})
}

func sanitizeUser(user model.User) map[string]any {
	return map[string]any{
		"user_id":       user.UserID,
		"username":      user.Username,
		"status":        user.Status,
		"last_login_at": user.LastLoginAt,
		"created_at":    user.CreatedAt,
		"updated_at":    user.UpdatedAt,
		"totp_enabled":  user.TOTPEnabled,
	}
}
