package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/eventruntime"
	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
	toolcatalog "github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/transport/ws"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

func main() {
	configPath := flag.String("config", "", "path to config.toml (optional; defaults and env vars apply when omitted)")
	addrFlag := flag.String("addr", "", "HTTP listen address (overrides config; default :3000)")
	dataDirFlag := flag.String("data-dir", "", fmt.Sprintf("persisted runtime data directory (overrides config; default %s)", filepath.Join(".", "var")))
	toolDirFlag := flag.String("tool-dir", "", fmt.Sprintf("tool catalog directory (overrides config; default %s)", filepath.Join(".", "config", "tools")))
	workflowDirFlag := flag.String("workflow-dir", "", "YAML workflow definitions directory (overrides config; default empty = in-memory only)")
	workflowPollFlag := flag.Duration("workflow-poll", 0, "workflow-dir polling interval (overrides config; default 5s)")
	flag.Parse()

	// Bootstrap logger for errors before the config-driven logger is ready.
	bootstrapLog := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		bootstrapLog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// Apply any flags that were explicitly set on the command line.
	visitedFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { visitedFlags[f.Name] = true })
	cfg.ApplyFlagOverrides(addrFlag, dataDirFlag, toolDirFlag, workflowDirFlag, workflowPollFlag, visitedFlags)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.SlogLevel(),
	}))

	// --- infrastructure ---
	reg := registry.New()
	metricsRegistry := telemetry.NewRegistry()

	var taskStore store.TaskStore
	var stateStore store.WorkflowStateStore
	var taskQueue store.TaskQueue
	switch cfg.Store.Driver {
	case "redis":
		redisClient := store.NewRedisClient(
			cfg.Redis.Addr,
			os.Getenv("AUTO_REDIS_PASSWORD"),
			cfg.Redis.DB,
		)
		if err := redisClient.Ping(context.Background()).Err(); err != nil {
			log.Error("redis ping failed", "err", err)
			os.Exit(1)
		}
		stateTTL := cfg.Store.StateTTL.D()
		if stateTTL == 0 {
			stateTTL = store.DefaultStateTTL
		}
		taskStore = store.NewRedisTaskStore(redisClient, stateTTL)
		stateStore = store.NewRedisWorkflowStateStore(redisClient, stateTTL)
		taskQueue = store.NewRedisTaskQueue(redisClient)
		log.Info("state store: redis", "addr", cfg.Redis.Addr, "ttl", stateTTL)
	default: // "file"
		fileTaskStore, err := store.NewFileTaskStore(cfg.Server.DataDir)
		if err != nil {
			log.Error("failed to open task store", "dir", cfg.Server.DataDir, "err", err)
			os.Exit(1)
		}
		fileStateStore, err := store.NewFileWorkflowStateStore(cfg.Server.DataDir)
		if err != nil {
			log.Error("failed to open workflow state store", "dir", cfg.Server.DataDir, "err", err)
			os.Exit(1)
		}
		fileQueue, err := store.NewFileTaskQueue(cfg.Server.DataDir)
		if err != nil {
			log.Error("failed to open task queue", "dir", cfg.Server.DataDir, "err", err)
			os.Exit(1)
		}
		taskStore = fileTaskStore
		stateStore = fileStateStore
		taskQueue = fileQueue
		log.Info("state store: file", "dir", cfg.Server.DataDir)
	}

	eventStore, err := store.NewFileEventPlaneStore(cfg.Server.DataDir)
	if err != nil {
		log.Error("failed to open event plane store", "dir", cfg.Server.DataDir, "err", err)
		os.Exit(1)
	}
	commandOutbox, err := store.NewFileCommandOutboxStore(cfg.Server.DataDir)
	if err != nil {
		log.Error("failed to open command outbox store", "dir", cfg.Server.DataDir, "err", err)
		os.Exit(1)
	}
	disp := dispatcher.NewMemoryDispatcher(reg, commandOutbox, metricsRegistry)

	// --- workflow def store ---
	var defStore workflow.DefStore
	mem := workflow.NewMemoryDefStore()

	if cfg.Server.WorkflowDir != "" {
		fs, err := workflow.NewFSDefStore(cfg.Server.WorkflowDir, log)
		if err != nil {
			log.Error("failed to load workflow dir", "dir", cfg.Server.WorkflowDir, "err", err)
			os.Exit(1)
		}
		fs.Watch(context.Background(), cfg.Server.WorkflowPoll.D())
		defStore = fs
	} else {
		defStore = mem
	}

	// --- tool catalog ---
	toolCatalog, err := toolcatalog.LoadCatalog(context.Background(), cfg.Server.ToolDir, log, toolcatalog.ModelToolConfig{
		APIURL: cfg.Tools.LLM.APIURL,
		APIKey: cfg.Tools.LLM.APIKey,
		Model:  cfg.Tools.LLM.Model,
	})
	if err != nil {
		log.Error("failed to load tool catalog", "dir", cfg.Server.ToolDir, "err", err)
		os.Exit(1)
	}

	// --- workflow engine ---
	engine := workflow.NewEngine(defStore, disp, toolCatalog.Registry)

	// --- orchestrator ---
	orch := orchestrator.New(taskStore, stateStore, engine, log, eventStore)
	orch.SetOperationalMetrics(metricsRegistry)

	// --- deadline watchdog ---
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()
	watchdog := orchestrator.NewDeadlineWatchdog(orch.ProcessAcceptedEvent, 500*time.Millisecond)
	orch.SetDeadlineWatchdog(watchdog)
	go watchdog.Run(serverCtx)

	// --- use cases ---
	recoveryUC := usecase.NewRuntimeRecovery(taskStore, stateStore, log)
	recoveryUC.SetQueue(taskQueue)
	recoveryReport, err := recoveryUC.Recover(context.Background())
	if err != nil {
		log.Error("runtime recovery failed", "err", err)
		os.Exit(1)
	}
	log.Info("runtime recovery complete",
		"tasksScanned", recoveryReport.TasksScanned,
		"statesBootstrapped", recoveryReport.StatesBootstrapped,
		"tasksReconciled", recoveryReport.TasksReconciled,
		"tasksRequeued", recoveryReport.TasksRequeued,
	)

	var runtime eventruntime.Runtime
	switch cfg.EventRuntime.Mode {
	case "inline":
		runtime = eventruntime.NewInlineRuntime(eventStore, orch, log, metricsRegistry)
	case "redis-streams":
		bus, err := eventruntime.NewRedisStreamsBus(eventruntime.RedisStreamsConfig{
			Addr:              cfg.Redis.Addr,
			Password:          os.Getenv("AUTO_REDIS_PASSWORD"),
			DB:                cfg.Redis.DB,
			Partitions:        cfg.EventBus.Partitions,
			Group:             cfg.EventBus.Group,
			ConsumerPrefix:    cfg.EventBus.ConsumerPrefix,
			InstanceID:        cfg.EventBus.InstanceID,
			LeaseTTL:          cfg.EventBus.LeaseTTL.D(),
			PendingIdle:       cfg.EventBus.PendingIdle.D(),
			PendingClaimCount: int64(cfg.EventBus.ClaimCount),
			OwnershipRetry:    cfg.EventBus.OwnershipRetry.D(),
		}, log, metricsRegistry)
		if err != nil {
			log.Error("failed to configure redis streams runtime", "err", err)
			os.Exit(1)
		}
		runtime = eventruntime.NewQueuedRuntime(eventStore, orch, bus, log, metricsRegistry)
	default:
		log.Error("unsupported event runtime mode", "mode", cfg.EventRuntime.Mode)
		os.Exit(1)
	}
	if err := runtime.Start(context.Background()); err != nil {
		log.Error("event runtime start failed", "mode", cfg.EventRuntime.Mode, "err", err)
		os.Exit(1)
	}
	log.Info("event runtime ready", "mode", cfg.EventRuntime.Mode)

	// --- device assigner (routes pending tasks ↔ idle devices) ---
	assigner := usecase.NewDeviceAssigner(taskStore, taskQueue, reg, runtime, log)

	lifecycleUC := usecase.NewAgentLifecycle(reg, runtime, log)
	lifecycleUC.SetAssigner(assigner)

	taskUC := usecase.NewTaskControl(taskStore, stateStore, runtime, reg, log)
	taskUC.SetAssigner(assigner)

	orch.SetOnTaskTerminal(assigner.OnTaskTerminal)
	autoEnabler := usecase.NewAdbAccessibilityAutoEnabler(
		cfg.ADB.Host,
		fmt.Sprintf("%d", cfg.ADB.Port),
		cfg.ADB.AccessibilityComponent,
		formatSerialByDevice(cfg.ADB.SerialByDevice),
	)
	eventUC := usecase.NewEventIngestion(runtime, autoEnabler)
	lifecycleUC.SetForgetDevice(eventUC.ForgetDevice)
	eventPlaneUC := usecase.NewEventPlaneControl(eventStore, runtime, eventUC, log, metricsRegistry)

	// --- handlers ---
	agentHandler := handler.NewAgentHandler(lifecycleUC, log)
	deviceHandler := handler.NewDeviceHandler(reg, log)
	taskHandler := handler.NewTaskHandler(taskUC, log)
	workflowHandler := handler.NewWorkflowHandler(defStore, log)
	eventPlaneHandler := handler.NewEventPlaneHandler(eventPlaneUC, log)
	metricsHandler := handler.NewMetricsHandler(metricsRegistry)
	openAPISpecHandler := handler.NewOpenAPISpecHandler()
	swaggerUIHandler := handler.NewSwaggerUIHandler()
	agentServer := ws.NewAgentServer(agentHandler, eventUC, reg, disp, log)

	// --- HTTP mux ---
	mux := http.NewServeMux()
	mux.Handle("/ws/agent", agentServer)
	mux.Handle("/devices", deviceHandler)
	mux.Handle("/devices/", deviceHandler)
	mux.Handle("/tasks", taskHandler)
	mux.Handle("/tasks/", taskHandler)
	mux.Handle("/workflows", workflowHandler)
	mux.Handle("/workflows/", workflowHandler)
	mux.Handle("/events", eventPlaneHandler)
	mux.Handle("/events/", eventPlaneHandler)
	mux.Handle("/metrics", metricsHandler)
	mux.Handle("/openapi.json", openAPISpecHandler)
	mux.Handle("/swagger", swaggerUIHandler)
	mux.Handle("/swagger/", swaggerUIHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// --- periodic data pruning (prevent unbounded file-store growth) ---
	pruneInterval := cfg.Pruning.Interval.D()
	if pruneInterval <= 0 {
		pruneInterval = time.Hour
	}
	go func() {
		t := time.NewTicker(pruneInterval)
		defer t.Stop()
		for {
			select {
			case <-serverCtx.Done():
				return
			case <-t.C:
				if n, err := eventStore.PruneAccepted(context.Background(), cfg.Pruning.AcceptedEventsAge.D()); err != nil {
					log.Warn("prune accepted events failed", "err", err)
				} else if n > 0 {
					log.Info("pruned accepted events", "removed", n)
				}
				if n, err := eventStore.PruneDeadLetters(context.Background(), cfg.Pruning.DeadLettersAge.D()); err != nil {
					log.Warn("prune dead letters failed", "err", err)
				} else if n > 0 {
					log.Info("pruned dead letters", "removed", n)
				}
				if n, err := commandOutbox.PruneCommandOutbox(context.Background(), cfg.Pruning.CommandOutboxAge.D()); err != nil {
					log.Warn("prune command outbox failed", "err", err)
				} else if n > 0 {
					log.Info("pruned command outbox", "removed", n)
				}
			}
		}
	}()

	shutdownTimeout := cfg.Server.ShutdownTimeout.D()
	if shutdownTimeout <= 0 {
		shutdownTimeout = 15 * time.Second
	}
	srv := &http.Server{Addr: cfg.Server.Addr, Handler: mux}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		select {
		case sig := <-sigCh:
			log.Info("shutdown signal received", "signal", sig)
			serverCancel()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer shutdownCancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				log.Error("http server shutdown error", "err", err)
			}
		case <-serverCtx.Done():
		}
	}()

	log.Info("server-agent starting", "addr", cfg.Server.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server failed", "err", err)
		serverCancel()
		os.Exit(1)
	}
	log.Info("server-agent stopped")
}
