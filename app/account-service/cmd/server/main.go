package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/autosdk/ppp/account-service/internal/handler"
	"github.com/autosdk/ppp/account-service/internal/store"
	"github.com/autosdk/ppp/account-service/internal/usecase"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg := loadConfig()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	var accountStore store.AccountStore
	if cfg.DatabaseURL == "" {
		log.Warn("DATABASE_URL not set — using in-memory store (data will not persist)")
		accountStore = store.NewMemoryAccountStore()
	} else {
		db, err := sql.Open("pgx", cfg.DatabaseURL)
		if err != nil {
			log.Error("failed to open database", "err", err)
			os.Exit(1)
		}
		if err := db.PingContext(context.Background()); err != nil {
			log.Error("database ping failed", "err", err)
			os.Exit(1)
		}
		accountStore = store.NewPostgresAccountStore(db)
		log.Info("account store: postgres")
	}

	accountUC := usecase.NewAccountRegistry(accountStore, log)
	accountHandler := handler.NewAccountHandler(accountUC, log)
	toolHandler := handler.NewToolServerHandler(accountUC, log)

	mux := http.NewServeMux()
	mux.Handle("/accounts/", accountHandler)
	mux.Handle("/devices/", accountHandler)
	mux.Handle("/v1/tools", toolHandler)
	mux.Handle("/v1/tools/", toolHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: cfg.Addr, Handler: mux}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		select {
		case sig := <-sigCh:
			log.Info("shutdown signal received", "signal", sig)
			cancel()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer shutdownCancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				log.Error("http server shutdown error", "err", err)
			}
		case <-ctx.Done():
		}
	}()

	log.Info("account-service starting", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server failed", "err", err)
		cancel()
		os.Exit(1)
	}
	log.Info("account-service stopped")
}
