package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/transport/ws"
)

func main() {
	addr := flag.String("addr", ":3000", "HTTP listen address")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	reg := registry.New()
	agentHandler := handler.NewAgentHandler(reg, log)
	agentServer := ws.NewAgentServer(agentHandler, reg, log)

	mux := http.NewServeMux()
	mux.Handle("/ws/agent", agentServer)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Info("server-agent starting", "addr", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}
