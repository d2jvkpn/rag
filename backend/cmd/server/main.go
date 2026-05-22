package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"backend/internal/api"
	"backend/internal/config"
	"backend/internal/repository"
	"backend/internal/service"
)

func main() {
	release := flag.Bool("release", false, "run in release mode")
	addr := flag.String("addr", "", "http listen address override")
	configPath := flag.String("config", filepath.Join("configs", "local.yaml"), "config file path")
	flag.Parse()

	cfg := config.Load(config.LoadOptions{
		Release:    *release,
		ConfigPath: *configPath,
		HTTPAddr:   *addr,
	})

	if cfg.Release {
		gin.SetMode(gin.ReleaseMode)
	}

	store, err := initStore(cfg)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}

	documentService, err := service.NewDocumentService(cfg, store)
	if err != nil {
		log.Fatalf("init document service: %v", err)
	}
	defer documentService.Close()

	authService := service.NewAuthService(store, cfg.JWTSecret)
	handler := api.NewHandler(cfg, authService, documentService)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("server starting release=%t addr=%s config=%s", cfg.Release, cfg.HTTPAddr, cfg.ConfigPath)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}

func initStore(cfg config.Config) (repository.Store, error) {
	if cfg.DatabaseDSN != "" {
		log.Printf("store: postgres dsn configured")
		return repository.NewPostgresStore(cfg.DatabaseDSN, cfg.AdminUsername, cfg.AdminPassword)
	}
	log.Printf("store: using local json file %s", cfg.StatePath)
	return repository.NewJSONStore(cfg.StatePath, cfg.AdminUsername, cfg.AdminPassword)
}
