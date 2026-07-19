package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/d2jvkpn/rag/mcp/internal/infra"
	"github.com/d2jvkpn/rag/mcp/internal/mcpserver"
)

func main() {
	var (
		err        error
		release    bool
		addr       string
		configPath string
		listener   net.Listener
		server     *http.Server
		ctx        context.Context
		stop       context.CancelFunc
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "RAG MCP Retrieval Server\nhttps://github.com/d2jvkpn/rag\n\nUsage of %s:\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.BoolVar(&release, "release", false, "run in release mode")
	flag.StringVar(&addr, "addr", ":3062", "http listen address override")
	flag.StringVar(&configPath, "config", "configs/mcp.yaml", "config file path")
	flag.Parse()

	config := mcpserver.LoadConfig(configPath)
	config.Set("app.release", release)

	infra.Init(config)
	defer infra.Sync()

	if listener, err = net.Listen("tcp", addr); err != nil {
		infra.L.Error("listen tcp", zap.String("addr", addr), zap.Error(err))
		os.Exit(1)
	}

	application, err := mcpserver.New(config)
	if err != nil {
		infra.L.Error("init mcp server", zap.Error(err))
		os.Exit(1)
	}

	server = &http.Server{
		Addr:              addr,
		Handler:           application.HTTPHandler(config.GetString("auth.api_key")),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			infra.L.Warn("http server shutdown", zap.Error(err))
		}
	}()

	infra.L.Info("mcp server starting", zap.Bool("release", release), zap.String("addr", addr), zap.String("config", configPath))
	if err = server.Serve(listener); err != nil && err != http.ErrServerClosed {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if shutdownErr := application.Shutdown(shutdownCtx); shutdownErr != nil {
			infra.L.Warn("mcp server shutdown after listen failure", zap.Error(shutdownErr))
		}
		cancel()
		infra.L.Error("listen", zap.Error(err))
		os.Exit(1)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = application.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		infra.L.Warn("mcp server shutdown", zap.Error(err))
	}
}
