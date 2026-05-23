package app

import (
	"path/filepath"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"backend/internal/api"
	"backend/internal/embedder"
	"backend/internal/llm"
	"backend/internal/logger"
	"backend/internal/repository"
	"backend/internal/service"
	"backend/internal/vectorstore"
)

type App struct {
	Handler         *api.Handler
	DocumentService *service.DocumentService
}

func New(v *viper.Viper) (*App, error) {
	store, err := initStore(v)
	if err != nil {
		return nil, err
	}

	opts := buildServiceOpts(v)
	documentService, err := service.NewDocumentService(v, store, opts...)
	if err != nil {
		return nil, err
	}

	authService := service.NewAuthService(store, v.GetString("http.jwt_secret"), initBlacklist(v))
	handler := api.NewHandler(v, authService, documentService)

	return &App{
		Handler:         handler,
		DocumentService: documentService,
	}, nil
}

func initStore(v *viper.Viper) (repository.Store, error) {
	if dsn := v.GetString("database.dsn"); dsn != "" {
		logger.L.Info("store: postgres")
		return repository.NewPostgresStore(dsn, v.GetString("admin.username"), v.GetString("admin.password"))
	}
	statePath := v.GetString("app.state_path")
	if statePath == "" {
		statePath = filepath.Join(v.GetString("app.data_dir"), "app-state.json")
	}
	logger.L.Info("store: json file", zap.String("path", statePath))
	return repository.NewJSONStore(statePath, v.GetString("admin.username"), v.GetString("admin.password"))
}

func initBlacklist(v *viper.Viper) service.TokenBlacklist {
	if dsn := v.GetString("redis.dsn"); dsn != "" {
		logger.L.Info("token blacklist: redis", zap.String("dsn", dsn))
		opt, err := redis.ParseURL(dsn)
		if err != nil {
			logger.L.Fatal("parse redis dsn", zap.Error(err))
		}
		return service.NewRedisBlacklist(redis.NewClient(opt))
	}
	logger.L.Info("token blacklist: memory")
	return service.NewMemoryBlacklist()
}

func buildServiceOpts(v *viper.Viper) []func(*service.DocumentService) {
	var opts []func(*service.DocumentService)

	embedBaseURL := v.GetString("embedder.base_url")
	embedAPIKey := v.GetString("embedder.api_key")
	if embedBaseURL != "" && embedAPIKey != "" {
		model := v.GetString("embedder.model")
		logger.L.Info("embedder: openai-compatible", zap.String("model", model))
		opts = append(opts, service.WithEmbedder(embedder.NewOpenAI(embedBaseURL, embedAPIKey, model)))
	} else {
		logger.L.Info("embedder: noop")
	}

	if addr := v.GetString("milvus.addr"); addr != "" {
		collection := v.GetString("milvus.collection")
		dim := v.GetInt("milvus.dim")
		logger.L.Info("vectorstore: milvus", zap.String("addr", addr), zap.String("collection", collection))
		vs, err := vectorstore.NewMilvus(addr, collection, dim)
		if err != nil {
			logger.L.Fatal("init milvus", zap.Error(err))
		}
		opts = append(opts, service.WithVectorStore(vs))
	} else {
		logger.L.Info("vectorstore: noop")
	}

	llmBaseURL := v.GetString("llm.base_url")
	llmAPIKey := v.GetString("llm.api_key")
	if llmBaseURL != "" && llmAPIKey != "" {
		model := v.GetString("llm.model")
		logger.L.Info("llm: openai-compatible", zap.String("model", model))
		opts = append(opts, service.WithLLM(llm.NewOpenAI(llmBaseURL, llmAPIKey, model)))
	} else {
		logger.L.Info("llm: noop (no answer generation)")
	}

	return opts
}
