package app

import (
	"context"
	"errors"
	"fmt"
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
	var (
		err             error
		accounts        []repository.AccountSeed
		store           repository.Store
		opts            []func(*service.DocumentService)
		documentService *service.DocumentService
		blacklist       service.TokenBlacklist
		tokenTTL        time.Duration
		authService     *service.AuthService
		handler         *api.Handler
	)

	if accounts, err = readAccounts(v); err != nil {
		return nil, err
	}

	if store, err = initStore(v, accounts); err != nil {
		return nil, err
	}

	if opts, err = buildServiceOpts(v); err != nil {
		return nil, err
	}
	documentService, err = service.NewDocumentService(v, store, opts...)
	if err != nil {
		return nil, err
	}

	v.SetDefault("http.jwt_token_ttl", "8h")
	tokenTTL, err = time.ParseDuration(v.GetString("http.jwt_token_ttl"))
	if err != nil || tokenTTL <= 0 {
		return nil, errors.New("invalid http.jwt_token_ttl: must be a positive duration")
	}

	if blacklist, err = initBlacklist(v); err != nil {
		return nil, err
	}

	authService = service.NewAuthService(
		store,
		v.GetString("http.jwt_secret"),
		tokenTTL,
		accounts,
		blacklist,
	)
	handler = api.NewHandler(v, authService, documentService)

	return &App{
		Handler:         handler,
		DocumentService: documentService,
		store:           store,
		blacklist:       blacklist,
		vectorStore:     documentService.VectorStore(),
	}, nil
}

func (a *App) Shutdown(ctx context.Context) (err error) {
	var (
		ok bool
		fn interface{ Close(context.Context) error }
		c  interface{ Close() error }
	)

	if a.DocumentService != nil {
		err = errors.Join(err, a.DocumentService.Shutdown(ctx))
	}

	if fn, ok = a.vectorStore.(interface{ Close(context.Context) error }); ok {
		err = errors.Join(err, fn.Close(ctx))
	}

	if c, ok = a.blacklist.(interface{ Close() error }); ok {
		err = errors.Join(err, c.Close())
	}

	if c, ok = a.store.(interface{ Close() error }); ok {
		err = errors.Join(err, c.Close())
	}

	return err
}

func readAccounts(v *viper.Viper) (accounts []repository.AccountSeed, err error) {
	var raw []repository.AccountSeed

	if err = v.UnmarshalKey("accounts", &raw); err != nil {
		return nil, err
	}

	accounts = make([]repository.AccountSeed, 0, len(raw))
	for _, v := range raw {
		if v.Username != "" && v.Password != "" {
			accounts = append(accounts, v)
		}
	}

	return accounts, nil
}

func initStore(v *viper.Viper, accounts []repository.AccountSeed) (repository.Store, error) {
	var str string

	if str = v.GetString("database.dsn"); str != "" {
		infra.L.Info("store: postgres")
		return repository.NewPostgresStore(str, accounts)
	}

	str = v.GetString("app.state_path")
	if str == "" {
		str = filepath.Join(v.GetString("app.data_dir"), "app-state.json")
	}
	infra.L.Info("store: json file", zap.String("path", str))

	return repository.NewJSONStore(str, accounts)
}

func initBlacklist(v *viper.Viper) (blacklist service.TokenBlacklist, err error) {
	var (
		str  string
		opts *redis.Options
	)

	if str = v.GetString("redis.dsn"); str != "" {
		infra.L.Info("token blacklist: redis", zap.String("dsn", str))
		if opts, err = redis.ParseURL(str); err != nil {
			// infra.L.Fatal("parse redis dsn", zap.Error(err))
			return nil, fmt.Errorf("parse redis dsn: %w", err)
		}
		return service.NewRedisBlacklist(redis.NewClient(opts)), nil
	}

	infra.L.Info("token blacklist: memory")
	return service.NewMemoryBlacklist(), nil
}

func buildServiceOpts(v *viper.Viper) (opts []func(*service.DocumentService), err error) {
	var (
		embedBaseURL string
		embedAPIKey  string
		addr         string
		db           string
		milvus       *llm.Milvus
	)

	embedBaseURL = v.GetString("embedder.base_url")
	embedAPIKey = v.GetString("embedder.api_key")
	if embedBaseURL != "" && embedAPIKey != "" {
		model := v.GetString("embedder.model")
		batchSize := v.GetInt("embedder.batch_size")
		infra.L.Info(
			"embedder: openai-compatible",
			zap.String("model", model),
			zap.Int("batch_size", batchSize),
		)
		v := service.WithEmbedder(
			llm.NewOpenAIEmbedder(embedBaseURL, embedAPIKey, model, batchSize),
		)

		opts = append(opts, v)
	} else {
		infra.L.Info("embedder: noop")
	}

	if addr = v.GetString("milvus.addr"); addr != "" {
		db = v.GetString("milvus.db")
		infra.L.Info("vectorstore: milvus", zap.String("addr", addr), zap.String("db", db))

		if milvus, err = llm.NewMilvus(addr, db, nil); err != nil {
			return nil, err
		}
		opts = append(opts, service.WithVectorStore(milvus))
	} else {
		infra.L.Info("vectorstore: noop")
	}

	return opts, nil
}
