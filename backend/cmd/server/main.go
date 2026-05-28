package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"backend/internal/app"
	"backend/internal/infra"
)

func main() {
	var (
		err         error
		release     bool
		addr        string
		configPath  string
		basePath    string
		config      *viper.Viper
		listener    net.Listener
		server      *http.Server
		ctx         context.Context
		stop        context.CancelFunc
		shutdownCtx context.Context
		cancel      context.CancelFunc
	)

	flag.BoolVar(&release, "release", false, "run in release mode")
	flag.StringVar(&addr, "addr", ":3061", "http listen address override")
	flag.StringVar(&configPath, "config", "configs/local.yaml", "config file path")
	flag.StringVar(&basePath, "base_path", "", "http base path")
	flag.Parse()

	config = app.LoadConfig(configPath)
	config.Set("app.config", configPath)
	config.Set("app.release", release)
	if basePath != "" {
		config.Set("http.base_path", basePath)
	}

	if listener, err = net.Listen("tcp", addr); err != nil {
		log.Fatalf("listen tcp %s: %v\n", addr, err)
	}

	infra.Init(config)
	defer infra.Sync()

	if release {
		gin.SetMode(gin.ReleaseMode)
	}

	application, err := app.New(config)
	if err != nil {
		infra.L.Fatal("init app", zap.Error(err))
	}

	server = &http.Server{
		Addr:              addr,
		Handler:           application.Handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		var (
			err         error
			shutdownCtx context.Context
			cancel      context.CancelFunc
		)

		shutdownCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = server.Shutdown(shutdownCtx)
		if err != nil && !errors.Is(err, context.Canceled) {
			infra.L.Warn("http server shutdown", zap.Error(err))
		}
	}()

	logFields := []zap.Field{
		zap.Bool("release", release),
		zap.String("addr", addr),
		zap.String("config", configPath),
		zap.String("base_path", config.GetString("http.base_path")),
	}

	infra.L.Info("server starting", logFields...)
	if err = server.Serve(listener); err != nil && err != http.ErrServerClosed {
		shutdownCtx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		if shutdownErr := application.Shutdown(shutdownCtx); shutdownErr != nil {
			infra.L.Warn("application shutdown after listen failure", zap.Error(shutdownErr))
		}
		cancel()
		infra.L.Fatal("listen", zap.Error(err))
	}

	shutdownCtx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = application.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		infra.L.Warn("application shutdown", zap.Error(err))
	}
}
