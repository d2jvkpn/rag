package app

import (
	"path/filepath"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"backend/internal/api"
	"backend/internal/llm"
	"backend/internal/infra"
	"backend/internal/repository"
	"backend/internal/service"
)

type App struct {
	Handler         *api.Handler
	DocumentService *service.DocumentService
}

func New(v *viper.Viper) (*App, error) {
	store, err := initStore(v, readAccounts(v))
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

func readAccounts(v *viper.Viper) []repository.AccountSeed {
	var raw []struct {
		Username string `mapstructure:"username"`
		Password string `mapstructure:"password"`
	}
	_ = v.UnmarshalKey("accounts", &raw)
	out := make([]repository.AccountSeed, 0, len(raw))
	for _, a := range raw {
		if a.Username != "" && a.Password != "" {
			out = append(out, repository.AccountSeed{Username: a.Username, Password: a.Password})
		}
	}
	return out
}

func initStore(v *viper.Viper, accounts []repository.AccountSeed) (repository.Store, error) {
	if dsn := v.GetString("database.dsn"); dsn != "" {
		infra.L.Info("store: postgres")
		return repository.NewPostgresStore(dsn, accounts)
	}
	statePath := v.GetString("app.state_path")
	if statePath == "" {
		statePath = filepath.Join(v.GetString("app.data_dir"), "app-state.json")
	}
	infra.L.Info("store: json file", zap.String("path", statePath))
	return repository.NewJSONStore(statePath, accounts)
}

func initBlacklist(v *viper.Viper) service.TokenBlacklist {
	if dsn := v.GetString("redis.dsn"); dsn != "" {
		infra.L.Info("token blacklist: redis", zap.String("dsn", dsn))
		opt, err := redis.ParseURL(dsn)
		if err != nil {
			infra.L.Fatal("parse redis dsn", zap.Error(err))
		}
		return service.NewRedisBlacklist(redis.NewClient(opt))
	}
	infra.L.Info("token blacklist: memory")
	return service.NewMemoryBlacklist()
}

func buildServiceOpts(v *viper.Viper) []func(*service.DocumentService) {
	var opts []func(*service.DocumentService)

	embedBaseURL := v.GetString("embedder.base_url")
	embedAPIKey := v.GetString("embedder.api_key")
	if embedBaseURL != "" && embedAPIKey != "" {
		model := v.GetString("embedder.model")
		infra.L.Info("embedder: openai-compatible", zap.String("model", model))
		opts = append(opts, service.WithEmbedder(llm.NewOpenAIEmbedder(embedBaseURL, embedAPIKey, model)))
	} else {
		infra.L.Info("embedder: noop")
	}

	if addr := v.GetString("milvus.addr"); addr != "" {
		db := v.GetString("milvus.db")
		var rawCols []struct {
			Collection   string `mapstructure:"collection"`
			Dim          int    `mapstructure:"dim"`
			ChunkSize    int    `mapstructure:"chunk_size"`
			ChunkOverlap int    `mapstructure:"chunk_overlap"`
			MinChunks    int    `mapstructure:"min_chunks"`
			Analyzer     string `mapstructure:"analyzer"`
		}
		if err := v.UnmarshalKey("milvus.collections", &rawCols); err != nil || len(rawCols) == 0 {
			infra.L.Fatal("milvus.collections is required and must be a non-empty list")
		}
		cols := make([]llm.CollectionConfig, len(rawCols))
		for i, c := range rawCols {
			if c.ChunkSize == 0 {
				c.ChunkSize = service.DefaultChunkSize
			}
			if c.ChunkOverlap == 0 {
				c.ChunkOverlap = service.DefaultChunkOverlap
			}
			if c.MinChunks == 0 {
				c.MinChunks = service.DefaultMinChunks
			}
			analyzer := c.Analyzer
			if analyzer == "" {
				analyzer = "chinese"
			}
			cols[i] = llm.CollectionConfig{
				Name:         c.Collection,
				Dim:          c.Dim,
				ChunkSize:    c.ChunkSize,
				ChunkOverlap: c.ChunkOverlap,
				MinChunks:    c.MinChunks,
				Analyzer:     analyzer,
			}
			infra.L.Info("vectorstore: milvus collection",
				zap.String("collection", c.Collection),
				zap.Int("dim", c.Dim),
				zap.Int("chunk_size", c.ChunkSize),
				zap.Int("chunk_overlap", c.ChunkOverlap),
				zap.Int("min_chunks", c.MinChunks),
				zap.String("analyzer", analyzer),
			)
		}
		vs, err := llm.NewMilvus(addr, db, cols)
		if err != nil {
			infra.L.Fatal("init milvus", zap.Error(err))
		}
		opts = append(opts, service.WithVectorStore(vs))
		opts = append(opts, service.WithCollectionConfigs(cols))
	} else {
		infra.L.Info("vectorstore: noop")
	}

	llmBaseURL := v.GetString("llm.base_url")
	llmAPIKey := v.GetString("llm.api_key")
	if llmBaseURL != "" && llmAPIKey != "" {
		model := v.GetString("llm.model")
		infra.L.Info("llm: openai-compatible", zap.String("model", model))
		opts = append(opts, service.WithLLM(llm.NewOpenAILLM(llmBaseURL, llmAPIKey, model)))
	} else {
		infra.L.Info("llm: noop (no answer generation)")
	}

	return opts
}
