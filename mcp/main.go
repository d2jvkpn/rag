package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/d2jvkpn/rag/mcp/internal/mcpserver"
)

func main() {
	var (
		err        error
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

	flag.StringVar(&addr, "addr", ":3062", "http listen address override")
	flag.StringVar(&configPath, "config", "configs/mcp.yaml", "config file path")
	flag.Parse()

	config := mcpserver.LoadConfig(configPath)

	if listener, err = net.Listen("tcp", addr); err != nil {
		slog.Error("listen tcp", "addr", addr, "error", err)
		os.Exit(1)
	}

	application, err := mcpserver.New(config)
	if err != nil {
		slog.Error("init mcp server", "error", err)
		os.Exit(1)
	}

	server = &http.Server{
		Addr:              addr,
		Handler:           application.HTTPHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("http server shutdown", "error", err)
		}
	}()

	slog.Info("mcp server starting", "addr", addr, "config", configPath)
	if err = server.Serve(listener); err != nil && err != http.ErrServerClosed {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if shutdownErr := application.Shutdown(shutdownCtx); shutdownErr != nil {
			slog.Warn("mcp server shutdown after listen failure", "error", shutdownErr)
		}
		cancel()
		slog.Error("listen", "error", err)
		os.Exit(1)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = application.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("mcp server shutdown", "error", err)
	}
}
