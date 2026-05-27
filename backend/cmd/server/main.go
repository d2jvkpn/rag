package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"backend/internal/app"
	"backend/internal/infra"
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

	infra.Init(filepath.Join(filepath.Dir(*configPath), "..", "logs"), *release)
	defer infra.Sync()

	if *release {
		gin.SetMode(gin.ReleaseMode)
	}

	application, err := app.New(v)
	if err != nil {
		infra.L.Fatal("init app", zap.Error(err))
	}

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
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			infra.L.Warn("http server shutdown", zap.Error(err))
		}
	}()

	logFields := []zap.Field{
		zap.Bool("release", *release),
		zap.String("addr", v.GetString("http.addr")),
		zap.String("config", *configPath),
	}
	if bp := v.GetString("http.base_path"); bp != "" {
		logFields = append(logFields, zap.String("base_path", bp))
	}
	infra.L.Info("server starting", logFields...)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if shutdownErr := application.Shutdown(shutdownCtx); shutdownErr != nil {
			infra.L.Warn("application shutdown after listen failure", zap.Error(shutdownErr))
		}
		infra.L.Fatal("listen", zap.Error(err))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		infra.L.Warn("application shutdown", zap.Error(err))
	}
}
