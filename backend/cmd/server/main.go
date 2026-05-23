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
	"go.uber.org/zap"

	"backend/internal/app"
	"backend/internal/logger"
)

func main() {
	release := flag.Bool("release", false, "run in release mode")
	addr := flag.String("addr", "", "http listen address override")
	configPath := flag.String("config", filepath.Join("configs", "local.yaml"), "config file path")
	flag.Parse()

	v := app.LoadConfig(*configPath)
	if *addr != "" {
		v.Set("http.addr", *addr)
	}

	logger.Init(filepath.Join(filepath.Dir(*configPath), "..", "logs"), *release)
	defer logger.Sync()

	if *release {
		gin.SetMode(gin.ReleaseMode)
	}

	application, err := app.New(v)
	if err != nil {
		logger.L.Fatal("init app", zap.Error(err))
	}
	defer application.DocumentService.Close()

	server := &http.Server{
		Addr:              v.GetString("http.addr"),
		Handler:           application.Handler.Routes(),
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

	logger.L.Info("server starting",
		zap.Bool("release", *release),
		zap.String("addr", v.GetString("http.addr")),
		zap.String("config", *configPath),
	)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}
