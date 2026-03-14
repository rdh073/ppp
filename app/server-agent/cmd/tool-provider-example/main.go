package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/tools/exampleprovider"
)

func main() {
	addr := flag.String("addr", ":3310", "HTTP listen address")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	server := &http.Server{
		Addr:              *addr,
		Handler:           exampleprovider.NewHandler(log),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Info("tool-provider-example starting", "addr", *addr, "toolName", exampleprovider.ToolName)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("tool-provider-example failed", "err", err)
		os.Exit(1)
	}
	log.Info("tool-provider-example stopped")
}
