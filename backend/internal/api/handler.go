package api

import (
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/d2jvkpn/rag/backend/internal/infra"
	"github.com/d2jvkpn/rag/backend/internal/model"
	"github.com/d2jvkpn/rag/backend/internal/repository"
	"github.com/d2jvkpn/rag/backend/internal/service"
	pkginfra "github.com/d2jvkpn/rag/backend/pkg/infra"
)

type Handler struct {
	cfg             *viper.Viper
	authService     *service.AuthService
	documentService *service.DocumentService
}

func NewHandler(
	cfg *viper.Viper,
	authService *service.AuthService,
	documentService *service.DocumentService,
) *Handler {
	return &Handler{
		cfg:             cfg,
		authService:     authService,
		documentService: documentService,
	}
}

func (h *Handler) Routes() http.Handler {
	var (
		router        *gin.Engine
		basePath      string
		spas          []pkginfra.SPA
		hasDefaultUI  bool
		err           error
		root          *gin.RouterGroup
		staticDir     string
		apiGroup      *gin.RouterGroup
		documentOwner *gin.RouterGroup
		manageUsers   *gin.RouterGroup
	)

	router = gin.New()

	basePath = h.cfg.GetString("http.base_path")
	router.Use(gin.Recovery(), infra.RequestLogger(basePath, "/healthz", "/version", "/static/"), h.cors())

	spas, err = pkginfra.ScanSPAs(basePath)
	if err != nil {
		infra.L.Warn("scan SPAs", zap.Error(err))
	}
	hasDefaultUI = false
	for _, spa := range spas {
		if spa.Path == path.Join("/", basePath, "ui") {
			hasDefaultUI = true
			break
		}
	}

	if hasDefaultUI {
		router.GET(basePath, func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, path.Join("/", basePath, "ui/index.html"))
		})
		router.GET(basePath+"/index.html", func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, path.Join("/", basePath, "ui/index.html"))
		})
	}

	router.NoRoute(func(c *gin.Context) {
		for _, spa := range spas {
			if pkginfra.GinSPA(c, spa.Path, spa.WebDir) {
				return
			}
		}
		writeError(c, 404, "route_not_found", "route not found", nil)
	})

	root = router.Group(basePath)

	root.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	root.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"git_branch":  pkginfra.GitBranch,
			"git_commit":  pkginfra.GitCommit,
			"commit_time": pkginfra.CommitTime,
		})
	})

	staticDir = filepath.Join(h.cfg.GetString("app.data_dir"), "static")
	root.Static("/static", staticDir)

	root.POST("/api/login", h.handleLogin)

	apiGroup = root.Group("/api", h.withAuth())
	apiGroup.POST("/logout", h.handleLogout)

	apiGroup.GET("/me", h.handleMe)
	apiGroup.PUT("/me/password", h.handleChangePassword)
	apiGroup.POST("/me/totp/setup", h.handleTOTPSetup)
	apiGroup.POST("/me/totp/enable", h.handleTOTPEnable)
	apiGroup.POST("/me/totp/disable", h.handleTOTPDisable)

	apiGroup.POST("/documents", h.handleCreateDocument)
	apiGroup.GET("/documents", h.handleListDocuments)
	apiGroup.GET("/document-tags", h.handleListDocumentTags)
	apiGroup.GET("/documents/:document_id", h.handleGetDocument)

	apiGroup.DELETE(
		"/documents/:document_id",
		h.withDocumentOwnerOrPermission("manage_documents"),
		h.handleDeleteDocument,
	)
	apiGroup.GET("/documents/:document_id/chunks", h.handleGetChunks)

	documentOwner = apiGroup.Group("/documents", h.withDocumentOwner())
	documentOwner.POST("/:document_id/chunks/rechunk", h.handleRechunk)
	documentOwner.POST("/:document_id/chunks/approve", h.handleApproveChunks)
	documentOwner.POST("/:document_id/chunks/merge", h.handleMergeChunks)
	documentOwner.PUT("/:document_id/chunks/:chunk_id", h.handleEditChunk)
	documentOwner.POST("/:document_id/chunks/:chunk_id/reject", h.handleRejectChunk)
	documentOwner.POST("/:document_id/chunks/:chunk_id/restore", h.handleRestoreChunk)
	documentOwner.POST("/:document_id/index", h.handleIndexDocument)

	manageUsers = apiGroup.Group("/users", h.withPermission("manage_users"))
	manageUsers.GET("", h.handleListUsers)
	manageUsers.POST("", h.handleCreateUser)
	manageUsers.POST("/:user_id/disable", h.handleDisableUser)
	manageUsers.POST("/:user_id/enable", h.handleEnableUser)
	manageUsers.PUT("/:user_id/permissions", h.handleSetUserPermissions)
	manageUsers.POST("/:user_id/reset-password", h.handleResetUserPassword)

	apiGroup.GET("/knowledge-bases", h.handleListKnowledgeBases)
	apiGroup.POST(
		"/knowledge-bases",
		h.withPermission("manage_knowledge_bases"),
		h.handleCreateKnowledgeBase,
	)
	apiGroup.DELETE(
		"/knowledge-bases/:knowledge_base_id",
		h.withPermission("manage_knowledge_bases"),
		h.handleDeleteKnowledgeBase,
	)
	apiGroup.GET("/knowledge-bases/available", h.handleListAvailableKnowledgeBases)
	apiGroup.POST("/knowledge-bases/query", h.handleQuery)

	return router
}

func (h *Handler) handleLogin(c *gin.Context) {
	var (
		request struct {
			Username string `json:"username"`
			Password string `json:"password"`
			TOTPCode string `json:"totp_code"`
		}
		user  model.User
		token string
		err   error
	)

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

	user, token, err = h.authService.Login(request.Username, request.Password, request.TOTPCode)
	if err != nil {
		if errors.Is(err, service.ErrTOTPRequired) {
			writeData(c, 200, map[string]any{"totp_required": true})
			return
		}
		if errors.Is(err, service.ErrUserDisabled) {
			writeError(c, 403, "forbidden", "user is disabled", nil)
			return
		}
		writeError(c, 401, "unauthorized", err.Error(), nil)
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)

	c.SetCookie(
		h.cfg.GetString("http.session_cookie"),  // name
		token,                                   // value
		int(h.authService.TokenTTL().Seconds()), // maxAge
		h.cfg.GetString("http.base_path"),       // path
		"",                                      // domain
		gin.Mode() == gin.ReleaseMode,           // secure
		true,                                    // httpOnly
	)

	writeData(c, 200, h.sanitizeUser(user))
}

func (h *Handler) handleLogout(c *gin.Context) {
	var (
		cookie *http.Cookie
		err    error
	)

	cookie, err = c.Request.Cookie(h.cfg.GetString("http.session_cookie"))
	if err == nil {
		_ = h.authService.Logout(cookie.Value)
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		h.cfg.GetString("http.session_cookie"), // name
		"",                                     // token
		-1,                                     // maxAge
		h.cfg.GetString("http.base_path"),      // path
		"",                                     // domain
		gin.Mode() == gin.ReleaseMode,          // secure
		true,                                   // httpOnly
	)

	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleMe(c *gin.Context) {
	var user model.User

	user = c.MustGet("current_user").(model.User)
	writeData(c, 200, h.sanitizeUser(user))
}

func (h *Handler) handleListUsers(c *gin.Context) {
	var (
		users []model.User
		items []map[string]any
	)

	users = h.authService.ListUsers()
	items = make([]map[string]any, len(users))
	for i, u := range users {
		items[i] = h.sanitizeUser(u)
	}
	writeData(c, 200, map[string]any{"items": items, "total": len(items)})
}

func (h *Handler) handleDisableUser(c *gin.Context) {
	var actor model.User

	actor = c.MustGet("current_user").(model.User)

	if err := h.authService.SetUserStatus(actor, c.Param("user_id"), "disabled"); err != nil {
		h.writeActionError(c, err)
		return
	}

	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleEnableUser(c *gin.Context) {
	var actor model.User

	actor = c.MustGet("current_user").(model.User)
	if err := h.authService.SetUserStatus(actor, c.Param("user_id"), "active"); err != nil {
		h.writeActionError(c, err)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleChangePassword(c *gin.Context) {
	var (
		user model.User
		body struct {
			OldPassword string `json:"old_password"`
			NewPassword string `json:"new_password"`
		}
		errs []fieldError
		err  error
	)

	user = c.MustGet("current_user").(model.User)
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		writeError(c, 400, "validation_error", "invalid request body", nil)
		return
	}
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

	err = h.authService.ChangePassword(user.UserID, body.OldPassword, body.NewPassword)
	if err != nil {
		if errors.Is(err, service.ErrIncorrectPassword) {
			writeError(
				c,
				400,
				"validation_error",
				err.Error(),
				[]fieldError{{Field: "old_password", Reason: "incorrect"}},
			)
			return
		}
		writeError(c, 500, "internal_error", err.Error(), nil)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleCreateDocument(c *gin.Context) {
	var (
		file            multipart.File
		header          *multipart.FileHeader
		err             error
		knowledgeBaseID string
		humanReview     bool
		uploader        model.User
		document        model.Document
	)

	if err := c.Request.ParseMultipartForm(64 << 20); err != nil {
		writeError(c, 400, "validation_error", "invalid multipart form: "+err.Error(), nil)
		return
	}

	file, header, err = c.Request.FormFile("file")
	if err != nil {
		writeError(
			c,
			400,
			"validation_error",
			"file is required",
			[]fieldError{{Field: "file", Reason: "required"}},
		)
		return
	}
	defer file.Close()

	knowledgeBaseID = strings.TrimSpace(c.Request.FormValue("knowledge_base_id"))
	if knowledgeBaseID == "" {
		writeError(
			c,
			400,
			"validation_error",
			"knowledge_base_id is required",
			[]fieldError{{Field: "knowledge_base_id", Reason: "required"}},
		)
		return
	}

	humanReview = c.Request.FormValue("human_review") == "true"
	uploader = c.MustGet("current_user").(model.User)
	document, err = h.documentService.CreateDocument(
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
		switch {
		case errors.Is(err, service.ErrUnsupportedFileType):
			writeError(c, 415, "unsupported_file_type", err.Error(), nil)
		case errors.Is(err, repository.ErrDocumentExists):
			writeError(c, 409, "conflict", err.Error(), nil)
		default:
			writeError(c, 400, "validation_error", err.Error(), nil)
		}
		return
	}

	writeData(c, 202, document)
}

func (h *Handler) handleListDocuments(c *gin.Context) {
	var (
		kb           string
		tag          string
		status       string
		page         int
		pageSize     int
		documentPage model.DocumentPage
		err          error
	)

	kb = strings.TrimSpace(c.Query("knowledge_base_id"))
	tag = strings.TrimSpace(c.Query("tag"))
	status = strings.TrimSpace(c.Query("status"))
	page, pageSize = parsePageQueryWithDefault(c, 20, 200)

	documentPage, err = h.documentService.ListDocumentsPage(kb, tag, status, page, pageSize)
	if err != nil {
		h.writeStoreError(c, err)
		return
	}
	if documentPage.Items == nil {
		documentPage.Items = []model.Document{}
	}

	writeData(c, 200, documentPage)
}

func (h *Handler) handleGetDocument(c *gin.Context) {
	var (
		documentID string
		document   model.Document
		err        error
	)

	documentID = c.Param("document_id")
	document, err = h.documentService.GetDocument(documentID)
	if err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 200, document)
}

func (h *Handler) handleListDocumentTags(c *gin.Context) {
	var (
		kb    string
		items []model.DocumentTagCount
		err   error
	)

	kb = strings.TrimSpace(c.Query("knowledge_base_id"))
	items, err = h.documentService.ListDocumentTags(kb)
	if err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 200, map[string]any{"items": items, "total": len(items)})
}

func (h *Handler) handleDeleteDocument(c *gin.Context) {
	var documentID string

	documentID = c.Param("document_id")
	if err := h.documentService.DeleteDocument(documentID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	c.Status(204)
}

func (h *Handler) handleGetChunks(c *gin.Context) {
	var (
		documentID string
		page       int
		pageSize   int
		chunkPage  model.DocumentChunkPage
		err        error
	)

	documentID = c.Param("document_id")
	page, pageSize = parsePageQuery(c)
	chunkPage, err = h.documentService.ListChunksPage(documentID, page, pageSize)
	if err != nil {
		h.writeStoreError(c, err)
		return
	}
	if chunkPage.Items == nil {
		chunkPage.Items = []model.DocumentChunk{}
	}
	writeData(c, 200, chunkPage)
}

func parsePageQuery(c *gin.Context) (int, int) {
	return parsePageQueryWithDefault(c, 50, 200)
}

func parsePageQueryWithDefault(c *gin.Context, defaultPageSize, maxPageSize int) (int, int) {
	var (
		page     int
		pageSize int
	)

	page = 1
	pageSize = defaultPageSize
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func (h *Handler) handleRechunk(c *gin.Context) {
	var documentID string

	documentID = c.Param("document_id")
	if err := h.documentService.RechunkDocument(documentID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 202, map[string]any{"accepted": true})
}

func (h *Handler) handleApproveChunks(c *gin.Context) {
	var documentID string

	documentID = c.Param("document_id")
	if err := h.documentService.ApproveChunks(documentID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleEditChunk(c *gin.Context) {
	var (
		documentID string
		chunkID    string
		body       struct {
			Text string `json:"text"`
		}
	)

	documentID = c.Param("document_id")
	chunkID = c.Param("chunk_id")
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
	var (
		documentID string
		chunkID    string
	)

	documentID = c.Param("document_id")
	chunkID = c.Param("chunk_id")
	if err := h.documentService.RejectChunk(documentID, chunkID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleRestoreChunk(c *gin.Context) {
	var (
		documentID string
		chunkID    string
	)

	documentID = c.Param("document_id")
	chunkID = c.Param("chunk_id")
	if err := h.documentService.RestoreChunk(documentID, chunkID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleMergeChunks(c *gin.Context) {
	var (
		documentID string
		body       struct {
			ChunkIDs []string `json:"chunk_ids"`
		}
	)

	documentID = c.Param("document_id")
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
	var documentID string

	documentID = c.Param("document_id")
	if err := h.documentService.IndexDocument(documentID); err != nil {
		h.writeStoreError(c, err)
		return
	}
	writeData(c, 202, map[string]any{"accepted": true})
}

func (h *Handler) handleListKnowledgeBases(c *gin.Context) {
	var items []model.KnowledgeBase

	items = h.documentService.ListKnowledgeBases()
	writeData(c, 200, map[string]any{
		"items":         items,
		"total":         len(items),
		"default_dim":   h.documentService.DefaultKnowledgeBaseDim(),
		"default_model": h.documentService.DefaultKnowledgeBaseModel(),
	})
}

func (h *Handler) handleCreateKnowledgeBase(c *gin.Context) {
	var body struct {
		KnowledgeBaseID string `json:"knowledge_base_id"`
		Analyzer        string `json:"analyzer"`
		ChunkSize       int    `json:"chunk_size"`
		ChunkOverlap    int    `json:"chunk_overlap"`
		MinChunks       int    `json:"min_chunks"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		writeError(c, 400, "validation_error", "invalid request body", nil)
		return
	}
	if body.ChunkSize == 0 {
		body.ChunkSize = service.DefaultChunkSize
	}
	if body.ChunkOverlap == 0 {
		body.ChunkOverlap = service.DefaultChunkOverlap
	}
	if body.MinChunks == 0 {
		body.MinChunks = service.DefaultMinChunks
	}
	user := c.MustGet("current_user").(model.User)
	kb, err := h.documentService.CreateKnowledgeBase(
		body.KnowledgeBaseID,
		body.Analyzer,
		body.ChunkSize,
		body.ChunkOverlap,
		body.MinChunks,
		user.Username,
	)
	if err != nil {
		if errors.Is(err, service.ErrKnowledgeBaseExists) {
			writeError(c, 409, "conflict", err.Error(), nil)
			return
		}
		writeError(c, 400, "validation_error", err.Error(), nil)
		return
	}
	writeData(c, 201, kb)
}

func (h *Handler) handleDeleteKnowledgeBase(c *gin.Context) {
	kbID := c.Param("knowledge_base_id")
	if err := h.documentService.DeleteKnowledgeBase(kbID); err != nil {
		if errors.Is(err, service.ErrKnowledgeBaseNotEmpty) {
			writeError(c, 409, "conflict", err.Error(), nil)
			return
		}
		h.writeStoreError(c, err)
		return
	}
	c.Status(204)
}

func (h *Handler) handleListAvailableKnowledgeBases(c *gin.Context) {
	items := h.documentService.ListAvailableKnowledgeBases()
	writeData(c, 200, map[string]any{
		"items":         items,
		"total":         len(items),
		"default_dim":   h.documentService.DefaultKnowledgeBaseDim(),
		"default_model": h.documentService.DefaultKnowledgeBaseModel(),
	})
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
		writeError(
			c,
			400,
			"validation_error",
			"knowledge_base_id is required",
			[]fieldError{{Field: "knowledge_base_id", Reason: "required"}},
		)
		return
	}
	if strings.TrimSpace(body.Query) == "" {
		writeError(
			c,
			400,
			"validation_error",
			"query is required",
			[]fieldError{{Field: "query", Reason: "required"}},
		)
		return
	}

	qr, err := h.documentService.Query(
		body.KnowledgeBaseID,
		body.Query,
		body.TopK,
		service.QueryOptions{
			DocumentIDs: body.DocumentIDs,
			SearchMode:  body.SearchMode,
			EF:          body.EF,
			DropRatio:   body.DropRatio,
			RRFK:        body.RRFK,
		},
	)
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
		"query":             body.Query,
		"knowledge_base_id": body.KnowledgeBaseID,
	})
}

func (h *Handler) withDocumentOwner() gin.HandlerFunc {
	return h.withDocumentOwnerOrPermission("")
}

func (h *Handler) withDocumentOwnerOrPermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := c.MustGet("current_user").(model.User)
		doc, err := h.documentService.GetDocument(c.Param("document_id"))
		if err != nil {
			h.writeStoreError(c, err)
			c.Abort()
			return
		}
		if doc.UploaderID != "" && doc.UploaderID != user.UserID &&
			(permission == "" || !h.authService.HasPermission(user, permission)) {
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
			if errors.Is(err, service.ErrUserDisabled) {
				writeError(c, 403, "forbidden", "user is disabled", nil)
				c.Abort()
				return
			}
			writeError(c, 401, "unauthorized", "invalid session", nil)
			c.Abort()
			return
		}
		c.Set("current_user", user)
		c.Next()
	}
}

func (h *Handler) withPermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := c.MustGet("current_user").(model.User)
		if !h.authService.HasPermission(user, permission) {
			writeError(c, 403, "forbidden", "missing permission: "+permission, nil)
			c.Abort()
			return
		}
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

func (h *Handler) writeActionError(c *gin.Context, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		writeError(c, 404, "not_found", "resource not found", nil)
		return
	}
	if errors.Is(err, service.ErrCannotChangeOwnStatus) {
		writeError(c, 400, "validation_error", err.Error(), nil)
		return
	}
	writeError(c, 500, "internal_error", err.Error(), nil)
}

func (h *Handler) handleTOTPSetup(c *gin.Context) {
	user := c.MustGet("current_user").(model.User)
	origin := c.Request.Header.Get("Origin")
	if origin == "" {
		origin = c.Request.Host
	} else {
		// strip scheme (e.g. "https://example.com" → "example.com")
		if i := strings.Index(origin, "://"); i >= 0 {
			origin = origin[i+3:]
		}
	}
	secret, qrURL, err := h.authService.SetupTOTP(user.UserID, origin)
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

func (h *Handler) handleCreateUser(c *gin.Context) {
	var body struct {
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		writeError(c, 400, "validation_error", "invalid request body", nil)
		return
	}
	var errs []fieldError
	if strings.TrimSpace(body.Username) == "" {
		errs = append(errs, fieldError{Field: "username", Reason: "required"})
	}
	if strings.TrimSpace(body.Password) == "" {
		errs = append(errs, fieldError{Field: "password", Reason: "required"})
	}
	if len(errs) > 0 {
		writeError(c, 400, "validation_error", "missing required fields", errs)
		return
	}
	if body.Permissions == nil {
		body.Permissions = []string{}
	}
	user, err := h.authService.CreateUser(body.Username, body.Password, body.Permissions)
	if err != nil {
		if errors.Is(err, service.ErrUsernameExists) {
			writeError(c, 409, "conflict", err.Error(), nil)
			return
		}
		writeError(c, 500, "internal_error", err.Error(), nil)
		return
	}
	writeData(c, 201, h.sanitizeUser(user))
}

func (h *Handler) handleSetUserPermissions(c *gin.Context) {
	var body struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		writeError(c, 400, "validation_error", "invalid request body", nil)
		return
	}
	if body.Permissions == nil {
		body.Permissions = []string{}
	}
	if err := h.authService.SetUserPermissions(c.Param("user_id"), body.Permissions); err != nil {
		h.writeActionError(c, err)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) handleResetUserPassword(c *gin.Context) {
	actor := c.MustGet("current_user").(model.User)
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		writeError(c, 400, "validation_error", "invalid request body", nil)
		return
	}
	if strings.TrimSpace(body.Password) == "" {
		writeError(c, 400, "validation_error", "password is required",
			[]fieldError{{Field: "password", Reason: "required"}})
		return
	}
	if err := h.authService.SetUserPassword(actor, c.Param("user_id"), body.Password); err != nil {
		if errors.Is(err, service.ErrCannotResetOwnPassword) {
			writeError(c, 400, "validation_error", err.Error(), nil)
			return
		}
		h.writeActionError(c, err)
		return
	}
	writeData(c, 200, map[string]any{"accepted": true})
}

func (h *Handler) sanitizeUser(user model.User) map[string]any {
	return map[string]any{
		"user_id":       user.UserID,
		"username":      user.Username,
		"status":        user.Status,
		"last_login_at": user.LastLoginAt,
		"created_at":    user.CreatedAt,
		"updated_at":    user.UpdatedAt,
		"totp_enabled":  user.TOTPEnabled,
		"permissions":   h.authService.Permissions(user),
	}
}
