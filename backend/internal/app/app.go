package app

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"backend/internal/api"
	"backend/internal/infra"
	"backend/internal/llm"
	"backend/internal/repository"
	"backend/internal/service"
)

type App struct {
	Handler         *api.Handler
	DocumentService *service.DocumentService
	store           repository.Store
	blacklist       service.TokenBlacklist
	vectorStore     llm.VectorStore
}

func New(v *viper.Viper) (*App, error) {
	accounts := readAccounts(v)
	store, err := initStore(v, accounts)
	if err != nil {
		return nil, err
	}

	opts := buildServiceOpts(v)
	documentService, err := service.NewDocumentService(v, store, opts...)
	if err != nil {
		return nil, err
	}

	tokenTTL, err := time.ParseDuration(v.GetString("http.jwt_token_ttl"))
	if err != nil || tokenTTL <= 0 {
		tokenTTL = 0 // falls back to defaultTokenTTL in NewAuthService
	}
	blacklist := initBlacklist(v)
	authService := service.NewAuthService(
		store,
		v.GetString("http.jwt_secret"),
		tokenTTL,
		accounts,
		blacklist,
	)
	handler := api.NewHandler(v, authService, documentService)

	return &App{
		Handler:         handler,
		DocumentService: documentService,
		store:           store,
		blacklist:       blacklist,
		vectorStore:     documentService.VectorStore(),
	}, nil
}

func (a *App) Shutdown(ctx context.Context) error {
	var err error

	if a.DocumentService != nil {
		err = errors.Join(err, a.DocumentService.Shutdown(ctx))
	}
	if c, ok := a.vectorStore.(interface{ Close(context.Context) error }); ok {
		err = errors.Join(err, c.Close(ctx))
	}
	if c, ok := a.blacklist.(interface{ Close() error }); ok {
		err = errors.Join(err, c.Close())
	}
	if c, ok := a.store.(interface{ Close() error }); ok {
		err = errors.Join(err, c.Close())
	}

	return err
}

func readAccounts(v *viper.Viper) []repository.AccountSeed {
	var raw []struct {
		Username    string   `mapstructure:"username"`
		Password    string   `mapstructure:"password"`
		Permissions []string `mapstructure:"permissions"`
	}
	_ = v.UnmarshalKey("accounts", &raw)
	out := make([]repository.AccountSeed, 0, len(raw))
	for _, a := range raw {
		if a.Username != "" && a.Password != "" {
			out = append(out, repository.AccountSeed{
				Username:    a.Username,
				Password:    a.Password,
				Permissions: append([]string(nil), a.Permissions...),
			})
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
		batchSize := v.GetInt("embedder.batch_size")
		infra.L.Info(
			"embedder: openai-compatible",
			zap.String("model", model),
			zap.Int("batch_size", batchSize),
		)
		opts = append(
			opts,
			service.WithEmbedder(
				llm.NewOpenAIEmbedder(embedBaseURL, embedAPIKey, model, batchSize),
			),
		)
	} else {
		infra.L.Info("embedder: noop")
	}

	if addr := v.GetString("milvus.addr"); addr != "" {
		db := v.GetString("milvus.db")
		infra.L.Info("vectorstore: milvus", zap.String("addr", addr), zap.String("db", db))
		vs, err := llm.NewMilvus(addr, db, nil)
		if err != nil {
			infra.L.Fatal("init milvus", zap.Error(err))
		}
		opts = append(opts, service.WithVectorStore(vs))
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
