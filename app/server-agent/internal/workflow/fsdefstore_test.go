package workflow_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

func TestFSDefStore_LoadsExampleWorkflows(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	store, err := workflow.NewFSDefStore("../../config/examples/workflows", log)
	if err != nil {
		t.Fatalf("load example workflows: %v", err)
	}

	defs, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("expected example workflows to load")
	}

	names := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		names[def.Name] = struct{}{}
	}

	for _, name := range []string{
		"android-settings-private-dns-script",
		"captcha-script",
		"instagram-login-script",
	} {
		if _, ok := names[name]; !ok {
			t.Fatalf("expected workflow %q to load, got names=%v", name, names)
		}
	}
}

