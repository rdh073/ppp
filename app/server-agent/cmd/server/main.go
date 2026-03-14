package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/eventruntime"
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
	dataDir := flag.String("data-dir", filepath.Join(".", "var"), "directory for persisted runtime data")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// --- infrastructure ---
	reg := registry.New()
	taskStore, err := store.NewFileTaskStore(*dataDir)
	if err != nil {
		log.Error("failed to open task store", "dir", *dataDir, "err", err)
		os.Exit(1)
	}
	stateStore, err := store.NewFileWorkflowStateStore(*dataDir)
	if err != nil {
		log.Error("failed to open workflow state store", "dir", *dataDir, "err", err)
		os.Exit(1)
	}
	eventStore, err := store.NewFileEventPlaneStore(*dataDir)
	if err != nil {
		log.Error("failed to open event plane store", "dir", *dataDir, "err", err)
		os.Exit(1)
	}
	commandOutbox, err := store.NewFileCommandOutboxStore(*dataDir)
	if err != nil {
		log.Error("failed to open command outbox store", "dir", *dataDir, "err", err)
		os.Exit(1)
	}
	disp := dispatcher.NewMemoryDispatcher(reg, commandOutbox)

	// --- workflow def store ---
	var defStore workflow.DefStore
	mem := workflow.NewMemoryDefStore()
	_ = mem.Put(context.Background(), workflow.DefaultWorkflowDef.Name, workflow.DefaultWorkflowDef)
	_ = mem.Put(context.Background(), workflow.LocalIdentityProfileWorkflowDef.Name, workflow.LocalIdentityProfileWorkflowDef)
	_ = mem.Put(context.Background(), workflow.LocalIdentityWelcomeEmailWorkflowDef.Name, workflow.LocalIdentityWelcomeEmailWorkflowDef)

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
	toolRegistry := toolcatalog.NewDefaultToolRegistry(log, toolcatalog.ModelToolConfig{
		APIURL: os.Getenv("AUTO_TOOL_LLM_API_URL"),
		APIKey: os.Getenv("AUTO_TOOL_LLM_API_KEY"),
		Model:  os.Getenv("AUTO_TOOL_LLM_MODEL"),
	})
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
	orch := orchestrator.New(taskStore, stateStore, runner, log, eventStore)

	// --- use cases ---
	recoveryUC := usecase.NewRuntimeRecovery(taskStore, stateStore, log)
	recoveryReport, err := recoveryUC.Recover(context.Background())
	if err != nil {
		log.Error("runtime recovery failed", "err", err)
		os.Exit(1)
	}
	log.Info("runtime recovery complete",
		"tasksScanned", recoveryReport.TasksScanned,
		"statesBootstrapped", recoveryReport.StatesBootstrapped,
		"tasksReconciled", recoveryReport.TasksReconciled,
	)

	runtimeMode := strings.TrimSpace(os.Getenv("AUTO_EVENT_RUNTIME"))
	if runtimeMode == "" {
		runtimeMode = "inline"
	}

	var runtime eventruntime.Runtime
	switch runtimeMode {
	case "inline":
		runtime = eventruntime.NewInlineRuntime(eventStore, orch, log)
	case "redis-streams":
		partitions, err := intEnv("AUTO_EVENT_BUS_PARTITIONS", 8)
		if err != nil {
			log.Error("invalid AUTO_EVENT_BUS_PARTITIONS", "err", err)
			os.Exit(1)
		}
		redisDB, err := intEnv("AUTO_REDIS_DB", 0)
		if err != nil {
			log.Error("invalid AUTO_REDIS_DB", "err", err)
			os.Exit(1)
		}
		bus, err := eventruntime.NewRedisStreamsBus(eventruntime.RedisStreamsConfig{
			Addr:           stringEnv("AUTO_REDIS_ADDR", "localhost:6379"),
			Password:       os.Getenv("AUTO_REDIS_PASSWORD"),
			DB:             redisDB,
			Partitions:     partitions,
			Group:          stringEnv("AUTO_REDIS_GROUP", "server-agent"),
			ConsumerPrefix: stringEnv("AUTO_REDIS_CONSUMER_PREFIX", "server-agent"),
		}, log)
		if err != nil {
			log.Error("failed to configure redis streams runtime", "err", err)
			os.Exit(1)
		}
		orch.SetEmittedEventPublisher(bus)
		runtime = eventruntime.NewQueuedRuntime(eventStore, orch, bus, log)
	default:
		log.Error("unsupported AUTO_EVENT_RUNTIME", "mode", runtimeMode)
		os.Exit(1)
	}
	if err := runtime.Start(context.Background()); err != nil {
		log.Error("event runtime start failed", "mode", runtimeMode, "err", err)
		os.Exit(1)
	}
	log.Info("event runtime ready", "mode", runtimeMode)

	lifecycleUC := usecase.NewAgentLifecycle(reg, runtime, log)
	taskUC := usecase.NewTaskControl(taskStore, stateStore, runtime, reg, log)
	autoEnabler := usecase.NewAdbAccessibilityAutoEnabler(
		os.Getenv("AUTO_ADB_SERVER_HOST"),
		os.Getenv("AUTO_ADB_SERVER_PORT"),
		os.Getenv("AUTO_AGENT_ACCESSIBILITY_COMPONENT"),
		os.Getenv("AUTO_ADB_SERIAL_BY_DEVICE"),
	)
	eventUC := usecase.NewEventIngestion(runtime, autoEnabler)

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

func stringEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func intEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}
