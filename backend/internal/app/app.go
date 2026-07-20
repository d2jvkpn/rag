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

	"github.com/d2jvkpn/rag/backend/internal/api"
	"github.com/d2jvkpn/rag/backend/internal/infra"
	"github.com/d2jvkpn/rag/backend/internal/repository"
	"github.com/d2jvkpn/rag/backend/internal/service"
	"github.com/d2jvkpn/rag/backend/pkg/rag"
)

type App struct {
	Handler         *api.Handler
	DocumentService *service.DocumentService
	store           repository.Store
	blacklist       service.TokenBlacklist
	vectorStore     rag.VectorStore
}

func New(v *viper.Viper) (*App, error) {
	var (
		err             error
		initAccount     repository.InitAccount
		store           repository.Store
		syncResult      repository.AccountSyncResult
		opts            []func(*service.DocumentService)
		documentService *service.DocumentService
		blacklist       service.TokenBlacklist
		tokenTTL        time.Duration
		authService     *service.AuthService
		handler         *api.Handler
	)

	initAccount = readInitAccount(v)

	if store, syncResult, err = initStore(v, initAccount); err != nil {
		return nil, err
	}
	infra.L.Info("init_account",
		zap.String("username", syncResult.Username),
		zap.String("action", syncResult.Action),
	)

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

func readInitAccount(v *viper.Viper) repository.InitAccount {
	return repository.InitAccount{
		Username: v.GetString("init_account.username"),
		Password: v.GetString("init_account.password"),
	}
}

func initStore(v *viper.Viper, account repository.InitAccount) (repository.Store, repository.AccountSyncResult, error) {
	var str string

	if str = v.GetString("database.dsn"); str != "" {
		infra.L.Info("store: postgres")
		return repository.NewPostgresStore(str, account)
	}

	str = v.GetString("app.state_path")
	if str == "" {
		str = filepath.Join(v.GetString("app.data_dir"), "app-state.json")
	}
	infra.L.Info("store: json file", zap.String("path", str))

	return repository.NewJSONStore(str, account)
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
		apiKey       string
		milvus       *rag.Milvus
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
			rag.NewOpenAIEmbedder(embedBaseURL, embedAPIKey, model, batchSize),
		)

		opts = append(opts, v)
	} else {
		infra.L.Info("embedder: noop")
	}

	if addr = v.GetString("milvus.addr"); addr != "" {
		db = v.GetString("milvus.db")
		apiKey = v.GetString("milvus.api_key")
		infra.L.Info(
			"vectorstore: milvus",
			zap.String("addr", addr),
			zap.String("db", db),
			zap.Bool("api_key_configured", apiKey != ""),
		)

		if milvus, err = rag.NewMilvus(addr, db, apiKey); err != nil {
			return nil, err
		}
		opts = append(opts, service.WithVectorStore(milvus))
	} else {
		infra.L.Info("vectorstore: noop")
	}

	return opts, nil
}
