package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/accountmanager"
	"github.com/autosdk/ppp/server-agent/internal/appport"
	"github.com/autosdk/ppp/server-agent/internal/campaigns"
	"github.com/autosdk/ppp/server-agent/internal/devicectrl"
	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/eventing"
	"github.com/autosdk/ppp/server-agent/internal/eventruntime"
	"github.com/autosdk/ppp/server-agent/internal/handler"
	infrallm "github.com/autosdk/ppp/server-agent/internal/infra/llm"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
	"github.com/autosdk/ppp/server-agent/internal/projection"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
	toolcatalog "github.com/autosdk/ppp/server-agent/internal/tools/loader"
	"github.com/autosdk/ppp/server-agent/internal/transport/ws"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
	"github.com/autosdk/ppp/server-agent/internal/workflowruntime"

	_ "github.com/jackc/pgx/v5/stdlib" // register "pgx" database/sql driver
)

func main() {
	cfg, log := loadConfig()
	stores := wireStores(cfg, log)
	mux, cancel := wireApp(cfg, stores, log)
	run(mux, cancel, cfg, log)
}

// loadConfig parses flags, loads config, and initialises the logger.
func loadConfig() (*Config, *slog.Logger) {
	configPath := flag.String("config", "", "path to config.toml (optional; defaults and env vars apply when omitted)")
	addrFlag := flag.String("addr", "", "HTTP listen address (overrides config; default :3000)")
	dataDirFlag := flag.String("data-dir", "", fmt.Sprintf("persisted runtime data directory (overrides config; default %s)", filepath.Join(".", "var")))
	toolDirFlag := flag.String("tool-dir", "", fmt.Sprintf("tool catalog directory (overrides config; default %s)", filepath.Join(".", "config", "tools")))
	workflowDirFlag := flag.String("workflow-dir", "", "YAML workflow definitions directory (overrides config; default empty = in-memory only)")
	workflowPollFlag := flag.Duration("workflow-poll", 0, "workflow-dir polling interval (overrides config; default 5s)")
	flag.Parse()

	bootstrapLog := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		bootstrapLog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	visitedFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { visitedFlags[f.Name] = true })
	cfg.ApplyFlagOverrides(addrFlag, dataDirFlag, toolDirFlag, workflowDirFlag, workflowPollFlag, visitedFlags)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.SlogLevel(),
	}))
	return cfg, log
}

// storeBundle groups all persistent store handles.
type storeBundle struct {
	task       store.TaskStore
	state      store.WorkflowStateStore
	queue      store.TaskQueue
	binding    store.DeviceBindingStore
	eventPlane store.EventPlaneStore
	outbox     store.CommandOutboxStore
	// Prune helpers are non-nil only for store drivers that support pruning (file).
	eventPlanePruner interface {
		PruneAccepted(ctx context.Context, maxAge time.Duration) (int, error)
		PruneDeadLetters(ctx context.Context, maxAge time.Duration) (int, error)
	}
	outboxPruner interface {
		PruneCommandOutbox(ctx context.Context, maxAge time.Duration) (int, error)
	}
}

// wireStores selects and opens the storage backend based on cfg.Store.Driver.
// Exits the process if any store fails to open.
func wireStores(cfg *Config, log *slog.Logger) storeBundle {
	var b storeBundle

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
		b.task = store.NewRedisTaskStore(redisClient, stateTTL)
		b.state = store.NewRedisWorkflowStateStore(redisClient, stateTTL)
		b.queue = store.NewRedisTaskQueue(redisClient)
		b.binding = store.NewRedisDeviceBindingStore(redisClient)
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
		fileBindingStore, err := store.NewFileDeviceBindingStore(cfg.Server.DataDir)
		if err != nil {
			log.Error("failed to open device binding store", "dir", cfg.Server.DataDir, "err", err)
			os.Exit(1)
		}
		b.task = fileTaskStore
		b.state = fileStateStore
		b.queue = fileQueue
		b.binding = fileBindingStore
		log.Info("state store: file", "dir", cfg.Server.DataDir)
	}

	eventStore, err := store.NewFileEventPlaneStore(cfg.Server.DataDir)
	if err != nil {
		log.Error("failed to open event plane store", "dir", cfg.Server.DataDir, "err", err)
		os.Exit(1)
	}
	outbox, err := store.NewFileCommandOutboxStore(cfg.Server.DataDir)
	if err != nil {
		log.Error("failed to open command outbox store", "dir", cfg.Server.DataDir, "err", err)
		os.Exit(1)
	}
	b.eventPlane = eventStore
	b.eventPlanePruner = eventStore
	b.outbox = outbox
	b.outboxPruner = outbox
	return b
}

// infraDeps groups core infrastructure handles created by wireInfra.
type infraDeps struct {
	reg             *registry.Registry
	metricsRegistry *telemetry.Registry
	disp            *dispatcher.MemoryDispatcher
	defStore        workflow.DefStore
	fsDefStore      *workflow.FSDefStore
	engine          *workflow.Engine
	orch            *orchestrator.Orchestrator
	runtime         eventruntime.Runtime
}

// llmDeps groups the LLM-backed components created by wireLLM.
type llmDeps struct {
	captchaHandler *handler.CaptchaHandler
	agentLoop      llm.AgentLoop
}

// wireApp constructs all use-cases, handlers, and the HTTP mux, starts
// background goroutines, and returns the mux alongside a cancel func that
// signals all background work to stop.
func wireApp(cfg *Config, stores storeBundle, log *slog.Logger) (http.Handler, context.CancelFunc) {
	serverCtx, serverCancel := context.WithCancel(context.Background())

	infra := wireInfra(serverCtx, serverCancel, cfg, stores, log)
	llmD := wireLLM(cfg, log)

	// --- use cases ---
	assigner := workflowruntime.NewDeviceAssigner(stores.task, stores.queue, infra.reg, infra.runtime, log)
	autoEnabler := devicectrl.NewAdbAccessibilityAutoEnabler(
		cfg.ADB.Host,
		fmt.Sprintf("%d", cfg.ADB.Port),
		cfg.ADB.AccessibilityComponent,
		formatSerialByDevice(cfg.ADB.SerialByDevice),
	)
	bindingManager := devicectrl.NewDeviceBindingManager(stores.binding, autoEnabler, infra.metricsRegistry, log)
	go bindingManager.Run(serverCtx, cfg.ADB.ReconcileInterval.D())

	eventUC := eventing.NewEventIngestionWithBindings(infra.runtime, bindingManager, log)
	lifecycleUC := devicectrl.NewAgentLifecycle(infra.reg, infra.runtime, bindingManager, eventUC.ForgetDevice, log)
	lifecycleUC.SetAssigner(assigner) // circular dep: DeviceAssigner ↔ AgentLifecycle

	taskUC := workflowruntime.NewTaskControl(stores.task, stores.state, infra.runtime, infra.reg, log)
	taskUC.SetAssigner(assigner)
	taskUC.SetCommandOutbox(stores.outbox)

	infra.orch.SetOnTaskTerminal(assigner.OnTaskTerminal)
	eventPlaneUC := eventing.NewEventPlaneControl(stores.eventPlane, infra.runtime, eventUC, log, infra.metricsRegistry)

	adbShellRunner := devicectrl.NewAdbShellRunner(cfg.ADB.Host, fmt.Sprintf("%d", cfg.ADB.Port), bindingManager)
	mux := wireMux(cfg, stores, infra, llmD, bindingManager, adbShellRunner, eventUC, lifecycleUC, taskUC, eventPlaneUC, log)

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
				if p := stores.eventPlanePruner; p != nil {
					if n, err := p.PruneAccepted(context.Background(), cfg.Pruning.AcceptedEventsAge.D()); err != nil {
						log.Warn("prune accepted events failed", "err", err)
					} else if n > 0 {
						log.Info("pruned accepted events", "removed", n)
					}
					if n, err := p.PruneDeadLetters(context.Background(), cfg.Pruning.DeadLettersAge.D()); err != nil {
						log.Warn("prune dead letters failed", "err", err)
					} else if n > 0 {
						log.Info("pruned dead letters", "removed", n)
					}
				}
				if p := stores.outboxPruner; p != nil {
					if n, err := p.PruneCommandOutbox(context.Background(), cfg.Pruning.CommandOutboxAge.D()); err != nil {
						log.Warn("prune command outbox failed", "err", err)
					} else if n > 0 {
						log.Info("pruned command outbox", "removed", n)
					}
				}
			}
		}
	}()

	return withCORS(mux), serverCancel
}

// wireInfra creates core infrastructure: device registry, dispatcher, workflow engine,
// orchestrator, runtime recovery, and event runtime. Starts the deadline watchdog goroutine.
func wireInfra(serverCtx context.Context, serverCancel context.CancelFunc, cfg *Config, stores storeBundle, log *slog.Logger) infraDeps {
	reg := registry.New()
	metricsRegistry := telemetry.NewRegistry()
	disp := dispatcher.NewMemoryDispatcher(reg, stores.outbox, metricsRegistry)

	// --- workflow def store ---
	var defStore workflow.DefStore
	var fsDefStore *workflow.FSDefStore
	if cfg.Server.WorkflowDir != "" {
		fs, err := workflow.NewFSDefStore(cfg.Server.WorkflowDir, log)
		if err != nil {
			serverCancel()
			log.Error("failed to load workflow dir", "dir", cfg.Server.WorkflowDir, "err", err)
			os.Exit(1)
		}
		fs.Watch(context.Background(), cfg.Server.WorkflowPoll.D())
		fsDefStore = fs
		defStore = fs
	} else {
		defStore = workflow.NewMemoryDefStore()
	}

	// --- tool catalog ---
	toolCatalog, err := toolcatalog.LoadCatalog(context.Background(), cfg.Server.ToolDir, log, toolcatalog.ModelToolConfig{
		APIURL: cfg.Tools.LLM.APIURL,
		APIKey: cfg.Tools.LLM.APIKey,
		Model:  cfg.Tools.LLM.Model,
	})
	if err != nil {
		serverCancel()
		log.Error("failed to load tool catalog", "dir", cfg.Server.ToolDir, "err", err)
		os.Exit(1)
	}

	// --- workflow engine + orchestrator ---
	engine := workflow.NewEngine(defStore, disp, toolCatalog.Registry)
	orch := orchestrator.New(stores.task, stores.state, engine, log, stores.eventPlane)
	orch.SetOperationalMetrics(metricsRegistry)
	watchdog := orchestrator.NewDeadlineWatchdog(orch.ProcessAcceptedEvent, 500*time.Millisecond)
	orch.SetDeadlineWatchdog(watchdog)
	go watchdog.Run(serverCtx)

	// --- runtime recovery ---
	recoveryUC := workflowruntime.NewRuntimeRecovery(stores.task, stores.state, log)
	recoveryUC.SetQueue(stores.queue)
	recoveryReport, err := recoveryUC.Recover(context.Background())
	if err != nil {
		serverCancel()
		log.Error("runtime recovery failed", "err", err)
		os.Exit(1)
	}
	log.Info("runtime recovery complete",
		"tasksScanned", recoveryReport.TasksScanned,
		"statesBootstrapped", recoveryReport.StatesBootstrapped,
		"tasksReconciled", recoveryReport.TasksReconciled,
		"tasksRequeued", recoveryReport.TasksRequeued,
	)

	// --- event runtime ---
	var runtime eventruntime.Runtime
	switch cfg.EventRuntime.Mode {
	case "inline":
		runtime = eventruntime.NewInlineRuntime(stores.eventPlane, orch, log, metricsRegistry)
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
			serverCancel()
			log.Error("failed to configure redis streams runtime", "err", err)
			os.Exit(1)
		}
		runtime = eventruntime.NewQueuedRuntime(stores.eventPlane, orch, bus, log, metricsRegistry)
	default:
		serverCancel()
		log.Error("unsupported event runtime mode", "mode", cfg.EventRuntime.Mode)
		os.Exit(1)
	}
	if err := runtime.Start(context.Background()); err != nil {
		serverCancel()
		log.Error("event runtime start failed", "mode", cfg.EventRuntime.Mode, "err", err)
		os.Exit(1)
	}
	log.Info("event runtime ready", "mode", cfg.EventRuntime.Mode)

	return infraDeps{
		reg:             reg,
		metricsRegistry: metricsRegistry,
		disp:            disp,
		defStore:        defStore,
		fsDefStore:      fsDefStore,
		engine:          engine,
		orch:            orch,
		runtime:         runtime,
	}
}

// wireLLM creates the LLM-backed components: captcha vision client, agent loop,
// caption generator, and image generator. All configuration is sourced from
// cfg.LLM (TOML provider registry + concern routing).
func wireLLM(cfg *Config, log *slog.Logger) llmDeps {
	// Seed AUTO_TOOL_* env vars from TOML provider registry so the existing
	// providers.yaml mechanism picks them up without config duplication.
	cfg.LLM.SeedToolEnvVars()

	// --- Vision concern (captcha solver) ---
	var captchaVision llm.VisionModelClient
	if visionCfg, ok := cfg.LLM.ResolveProvider(cfg.LLM.Vision); ok {
		apiVersion := visionCfg.APIVersion
		if apiVersion == "" {
			apiVersion = "2023-06-01"
		}
		captchaVision = llm.NewAnthropicVisionClient(
			llm.ModelToolConfig{APIURL: visionCfg.APIURL, APIKey: visionCfg.APIKey, Model: visionCfg.Model},
			apiVersion,
			log,
		)
	} else {
		// Nil-safe: captcha handler returns 503 when unconfigured.
		captchaVision = llm.NewAnthropicVisionClient(llm.ModelToolConfig{}, "", log)
	}
	captchaH := handler.NewCaptchaHandler(captchaVision, log)

	// --- Agent loop concern ---
	agentLoopKind := cfg.LLM.AgentLoop.Provider
	if agentLoopKind == "" {
		agentLoopKind = "anthropic"
	}
	var agentLoopCfg llm.ModelToolConfig
	var agentLoopAPIVersion string
	if p, ok := cfg.LLM.ResolveProvider(cfg.LLM.AgentLoop); ok {
		agentLoopCfg = llm.ModelToolConfig{APIURL: p.APIURL, APIKey: p.APIKey, Model: p.Model}
		agentLoopAPIVersion = p.APIVersion
	}
	agentLoop := llm.NewAgentLoop(agentLoopKind, agentLoopCfg, agentLoopAPIVersion, log)

	return llmDeps{captchaHandler: captchaH, agentLoop: agentLoop}
}

// wireCaptionGenerator creates a caption generator wired from the LLM concern config.
func wireCaptionGenerator(cfg *Config) (*infrallm.LLMCaptionGenerator, error) {
	toCaptionCfg := func(p LLMProviderConfig, kind string) infrallm.CaptionProviderConfig {
		return infrallm.CaptionProviderConfig{
			Kind:       kind,
			APIURL:     p.APIURL,
			APIKey:     p.APIKey,
			Model:      p.Model,
			APIVersion: p.APIVersion,
		}
	}
	var primary, fallback infrallm.CaptionProviderConfig
	configured := false
	if p, ok := cfg.LLM.ResolveProvider(cfg.LLM.TextGeneration); ok && p.APIKey != "" {
		primary = toCaptionCfg(p, cfg.LLM.TextGeneration.Provider)
		configured = true
	}
	if fb, ok := cfg.LLM.ResolveFallback(cfg.LLM.TextGeneration); ok && fb.APIKey != "" {
		fallback = toCaptionCfg(fb, cfg.LLM.TextGeneration.Fallback)
		configured = true
	}
	if !configured {
		return nil, fmt.Errorf("text generation: no provider configured (set [llm.providers.*] + [llm.text_generation])")
	}
	return infrallm.NewLLMCaptionGenerator(primary, fallback), nil
}

// wireImageGenerator creates an image generator wired from the LLM concern config.
func wireImageGenerator(cfg *Config) (*infrallm.DallE3ImageGenerator, error) {
	if p, ok := cfg.LLM.ResolveProvider(cfg.LLM.ImageGeneration); ok {
		return infrallm.NewDallE3ImageGenerator(infrallm.ImageGeneratorConfig{
			APIKey: p.APIKey,
			Model:  p.Model,
			APIURL: p.APIURL,
		})
	}
	return nil, fmt.Errorf("image generation: no provider configured (set [llm.providers.openai] + [llm.image_generation])")
}

// wireMux creates all HTTP handlers, registers routes, and returns the mux.
func wireMux(
	cfg *Config,
	stores storeBundle,
	infra infraDeps,
	llmD llmDeps,
	bindingManager *devicectrl.DeviceBindingManager,
	adbShellRunner *devicectrl.AdbShellRunner,
	eventUC appport.EventIngestion,
	lifecycleUC appport.AgentLifecycle,
	taskUC appport.TaskControl,
	eventPlaneUC appport.EventPlaneControl,
	log *slog.Logger,
) http.Handler {
	agentHandler := handler.NewAgentHandler(lifecycleUC, log)
	recordingStore := devicectrl.NewRecordingStore()
	macroLibrary, err := store.NewFileMacroStore(cfg.Server.DataDir)
	if err != nil {
		log.Error("failed to open macro library", "dir", cfg.Server.DataDir, "err", err)
		os.Exit(1)
	}
	deviceHandler := devicectrl.NewDeviceHandler(infra.reg, log, bindingManager).
		WithDispatcher(infra.disp).
		WithRecording(recordingStore).
		WithRecordingLibrary(macroLibrary).
		WithAgentLoop(llmD.agentLoop)
	macroLibraryHandler := handler.NewMacroLibraryHandler(macroLibrary, cfg.Server.WorkflowDir)
	if infra.fsDefStore != nil {
		macroLibraryHandler = macroLibraryHandler.WithReloader(infra.fsDefStore)
	}
	var personaStore store.PersonaStore
	var accountStore store.AccountStore
	if dbURL := os.Getenv("AUTO_ACCOUNT_DATABASE_URL"); dbURL != "" {
		db, err := openAccountDB(dbURL, log)
		if err != nil {
			log.Error("failed to open account database", "err", err)
			os.Exit(1)
		}
		accountStore = store.NewPostgresAccountStore(db)
		personaStore = store.NewPostgresPersonaStore(db)
		log.Info("account store: postgres")
	} else {
		ps, err := store.NewFilePersonaStore(cfg.Server.DataDir)
		if err != nil {
			log.Error("failed to open persona store", "dir", cfg.Server.DataDir, "err", err)
			os.Exit(1)
		}
		as, err := store.NewFileAccountStore(cfg.Server.DataDir)
		if err != nil {
			log.Error("failed to open account store", "dir", cfg.Server.DataDir, "err", err)
			os.Exit(1)
		}
		personaStore = ps
		accountStore = as
		log.Info("account store: file-backed", "dir", cfg.Server.DataDir)
	}
	projectionStore, err := store.NewFileProjectionEventStore(cfg.Server.DataDir, 0)
	if err != nil {
		log.Error("failed to open projection event store", "dir", cfg.Server.DataDir, "err", err)
		os.Exit(1)
	}
	projectionHub := projection.NewHub(projectionStore, log)
	deviceHandler.WithProjectionHub(projectionHub)
	projectedPersonaStore := accountmanager.NewProjectedPersonaStore(personaStore, projectionHub)
	projectedAccountStore := accountmanager.NewProjectedAccountStore(accountStore, projectionHub)
	personaService := accountmanager.NewPersonaService(projectedPersonaStore)
	accountService := accountmanager.NewAccountService(projectedAccountStore)
	personaHandler := accountmanager.NewPersonaHandler(personaService, "/account-manager/personas")
	accountHandler := accountmanager.NewAccountHandler(accountService, "/account-manager/accounts")
	accountToolHandler := accountmanager.NewAccountToolHandler(accountService)
	// Derive base URL for account-creation scripts (account_service_endpoint).
	// Scripts call back to register accounts; this must resolve to the server itself.
	accountCreationBaseURL := "http://localhost" + cfg.Server.Addr
	if !strings.HasPrefix(cfg.Server.Addr, ":") {
		accountCreationBaseURL = "http://" + cfg.Server.Addr
	}
	accountCreationService := accountmanager.NewAccountCreationService(taskUC, projectedPersonaStore, projectedAccountStore, projectionHub, accountCreationBaseURL, log)
	accountCreationHandler := accountmanager.NewAccountCreationHandler(accountCreationService, "/account-manager/account-creations")
	googleLoginCfg := accountmanager.GoogleLoginConfig()
	loginService := accountmanager.NewLoginRunService(taskUC, projectedAccountStore, adbShellRunner, projectionHub, log, googleLoginCfg.ServiceCfg)
	loginHandler := accountmanager.NewLoginRunHandler(loginService, googleLoginCfg.PathPrefix)
	instagramLoginCfg := accountmanager.InstagramLoginConfig()
	igLoginService := accountmanager.NewLoginRunService(taskUC, projectedAccountStore, adbShellRunner, projectionHub, log, instagramLoginCfg.ServiceCfg)
	igLoginHandler := accountmanager.NewLoginRunHandler(igLoginService, instagramLoginCfg.PathPrefix)
	var captionGen campaigns.CaptionGenerator
	if c, err := wireCaptionGenerator(cfg); err != nil {
		log.Info("LLM caption generation disabled", "reason", err)
	} else {
		captionGen = c
	}
	var imgGen campaigns.ImageGenerator
	if i, err := wireImageGenerator(cfg); err != nil {
		log.Info("DALL-E 3 image generation disabled", "reason", err)
	} else {
		imgGen = i
	}
	postService := campaigns.NewPostCampaignService(taskUC, projectedAccountStore, captionGen, imgGen, adbShellRunner, projectionHub, cfg.Server.DataDir, log)
	postHandler := campaigns.NewPostCampaignHandler(postService, "/campaigns/posts")
	projectionStreamHandler := handler.NewProjectionStreamHandler(projectionHub)
	taskHandler := handler.NewTaskHandler(taskUC, log)
	workflowHandler := handler.NewWorkflowHandler(infra.defStore, log)
	eventPlaneHandler := handler.NewEventPlaneHandler(eventPlaneUC, log)
	metricsHandler := handler.NewMetricsHandler(infra.metricsRegistry)
	openAPISpecHandler := handler.NewOpenAPISpecHandler()
	swaggerUIHandler := handler.NewSwaggerUIHandler()
	agentServer := ws.NewAgentServer(agentHandler, eventUC, infra.reg, infra.disp, log)

	mux := http.NewServeMux()
	mux.Handle("/captcha/", llmD.captchaHandler)
	mux.Handle("/ws/agent", agentServer)
	mux.Handle("/devices", deviceHandler)
	mux.Handle("/devices/", deviceHandler)
	mux.Handle("/tasks", taskHandler)
	mux.Handle("/tasks/", taskHandler)
	mux.Handle("/workflows", workflowHandler)
	mux.Handle("/workflows/", workflowHandler)
	mux.Handle("/events", eventPlaneHandler)
	mux.Handle("/events/", eventPlaneHandler)
	mux.Handle("/events/stream", projectionStreamHandler)
	mux.Handle("/macros", macroLibraryHandler)
	mux.Handle("/macros/", macroLibraryHandler)
	mux.Handle("/account-manager/personas", personaHandler)
	mux.Handle("/account-manager/personas/", personaHandler)
	mux.Handle("/account-manager/accounts", accountHandler)
	mux.Handle("/account-manager/accounts/", accountHandler)
	mux.Handle("/account-manager/account-creations", accountCreationHandler)
	mux.Handle("/account-manager/account-creations/", accountCreationHandler)
	mux.Handle("/v1/tools/", accountToolHandler)
	mux.Handle("/account-manager/logins/google", loginHandler)
	mux.Handle("/account-manager/logins/google/", loginHandler)
	mux.Handle("/account-manager/logins/instagram", igLoginHandler)
	mux.Handle("/account-manager/logins/instagram/", igLoginHandler)
	mux.Handle("/campaigns/posts", postHandler)
	mux.Handle("/campaigns/posts/", postHandler)
	mux.Handle("/metrics", metricsHandler)
	mux.Handle("/openapi.json", openAPISpecHandler)
	mux.Handle("/swagger", swaggerUIHandler)
	mux.Handle("/swagger/", swaggerUIHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// openAccountDB opens and migrates a PostgreSQL database for account/persona storage.
func openAccountDB(dsn string, log *slog.Logger) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if err := store.MigratePostgres(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// run starts the HTTP server and blocks until a shutdown signal is received.
func run(h http.Handler, cancel context.CancelFunc, cfg *Config, log *slog.Logger) {
	shutdownTimeout := cfg.Server.ShutdownTimeout.D()
	if shutdownTimeout <= 0 {
		shutdownTimeout = 15 * time.Second
	}
	srv := &http.Server{Addr: cfg.Server.Addr, Handler: h}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		sig := <-sigCh
		log.Info("shutdown signal received", "signal", sig)
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("http server shutdown error", "err", err)
		}
	}()

	log.Info("server-agent starting", "addr", cfg.Server.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server failed", "err", err)
		cancel()
		os.Exit(1)
	}
	log.Info("server-agent stopped")
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
