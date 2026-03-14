package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	toolcatalog "github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/transport/ws"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
)

func main() {
	addr := flag.String("addr", ":3000", "HTTP listen address")
	workflowDir := flag.String("workflow-dir", "", "directory to watch for YAML workflow defs (optional)")
	workflowPoll := flag.Duration("workflow-poll", 5*time.Second, "polling interval for workflow-dir")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// --- infrastructure ---
	reg := registry.New()
	taskStore := store.NewMemoryTaskStore()
	stateStore := store.NewMemoryWorkflowStateStore()
	disp := dispatcher.NewMemoryDispatcher(reg)

	// --- workflow def store ---
	var defStore workflow.DefStore
	mem := workflow.NewMemoryDefStore()
	_ = mem.Put(context.Background(), workflow.DefaultWorkflowDef.Name, workflow.DefaultWorkflowDef)
	_ = mem.Put(context.Background(), workflow.LocalIdentityProfileWorkflowDef.Name, workflow.LocalIdentityProfileWorkflowDef)

	if *workflowDir != "" {
		fs, err := workflow.NewFSDefStore(*workflowDir, log)
		if err != nil {
			log.Error("failed to load workflow dir", "dir", *workflowDir, "err", err)
			os.Exit(1)
		}
		fs.Watch(context.Background(), *workflowPoll)
		defStore = fs
	} else {
		defStore = mem
	}

	// --- workflow runner ---
	toolRegistry := toolcatalog.NewLocalToolRegistry()
	runner := workflow.NewRunner(map[domain.NodeKind]workflow.NodeHandler{
		domain.NodeKindObserve:  nodes.NewObserveNode(disp),
		domain.NodeKindDecide:   nodes.NewDecideNode(),
		domain.NodeKindAct:      nodes.NewActNode(disp),
		domain.NodeKindVerify:   nodes.NewVerifyNode(),
		domain.NodeKindResync:   nodes.NewResyncNode(disp),
		domain.NodeKindToolCall: nodes.NewToolCallNode(toolRegistry),
		domain.NodeKindWait:     nodes.NewWaitNode(),
		domain.NodeKindTerminal: nodes.NewTerminalNode(),
	}, defStore, workflow.DefaultWorkflowName)

	// --- orchestrator ---
	orch := orchestrator.New(taskStore, stateStore, runner, log)

	// --- use cases ---
	lifecycleUC := usecase.NewAgentLifecycle(reg, orch, log)
	taskUC := usecase.NewTaskControl(taskStore, stateStore, orch, reg, log)
	autoEnabler := usecase.NewAdbAccessibilityAutoEnabler(
		os.Getenv("AUTO_ADB_SERVER_HOST"),
		os.Getenv("AUTO_ADB_SERVER_PORT"),
		os.Getenv("AUTO_AGENT_ACCESSIBILITY_COMPONENT"),
		os.Getenv("AUTO_ADB_SERIAL_BY_DEVICE"),
	)
	eventUC := usecase.NewEventIngestion(orch, autoEnabler)

	// --- handlers ---
	agentHandler := handler.NewAgentHandler(lifecycleUC, log)
	taskHandler := handler.NewTaskHandler(taskUC, log)
	workflowHandler := handler.NewWorkflowHandler(defStore, log)
	agentServer := ws.NewAgentServer(agentHandler, eventUC, reg, disp, log)

	// --- HTTP mux ---
	mux := http.NewServeMux()
	mux.Handle("/ws/agent", agentServer)
	mux.Handle("/tasks", taskHandler)
	mux.Handle("/tasks/", taskHandler)
	mux.Handle("/workflows", workflowHandler)
	mux.Handle("/workflows/", workflowHandler)
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
