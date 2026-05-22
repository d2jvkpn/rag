package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"backend/internal/config"
	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/service"
)

type Handler struct {
	cfg             config.Config
	authService     *service.AuthService
	documentService *service.DocumentService
}

func NewHandler(cfg config.Config, authService *service.AuthService, documentService *service.DocumentService) *Handler {
	return &Handler{
		cfg:             cfg,
		authService:     authService,
		documentService: documentService,
	}
}

func (h *Handler) Routes() http.Handler {
	router := gin.New()
	router.Use(gin.Recovery())

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
	c.SetCookie(h.cfg.SessionCookie, token, 3600*24, "/", "", false, true)
	writeData(c, 200, map[string]any{"user": sanitizeUser(user)})
}

func (h *Handler) handleLogout(c *gin.Context) {
	cookie, err := c.Request.Cookie(h.cfg.SessionCookie)
	if err == nil {
		_ = h.authService.Logout(cookie.Value)
	}
	c.SetCookie(h.cfg.SessionCookie, "", -1, "/", "", false, true)
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

	document, err := h.documentService.CreateDocument(
		file,
		header,
		c.Request.FormValue("knowledge_base_id"),
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
	documents := h.documentService.ListDocuments()
	items := make([]any, 0, len(documents))
	for _, document := range documents {
		if kb := strings.TrimSpace(c.Query("knowledge_base_id")); kb != "" && document.KnowledgeBaseID != kb {
			continue
		}
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
	writeData(c, 200, map[string]any{"items": chunks})
}

func (h *Handler) handleRechunk(c *gin.Context) {
	documentID := c.Param("document_id")
	if err := h.documentService.RechunkDocument(documentID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 202, map[string]any{"accepted": true})
}

func (h *Handler) withAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Request.Cookie(h.cfg.SessionCookie)
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
