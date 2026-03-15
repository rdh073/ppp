# server-agent: Scalable Control Plane Architecture

> **Status: Implemented.** This document was written as a forward plan. The implementation is now present in `app/server-agent/`. The package layout below reflects the actual codebase state. See `app/server-agent/CLAUDE.md` for build/test commands and `plans/event-driven-argo-like-architecture-v2.md` for the full phase roadmap.

## Actual Package Layout

```
internal/
  domain/          — pure types, no internal imports
    session.go     SessionID, AgentSession
    task.go        Task, TaskID, TaskStatus (IsTerminal, IsActive)
    workflow.go    WorkflowState, NodeKind enum, WorkflowStatus
    workflowdef.go WorkflowDef, StepDef (Action/ToolCall/Expect/Trigger fields)
    event.go       Event, EventKind, payload variants
    snapshot.go    UiSnapshot, UiTarget, UiSemantics
    command.go     Command, CommandKind, CommandResult, CommandError
    id.go          ID generation helpers

  registry/
    registry.go    MemoryRegistry + Sender
    port.go        AgentRegistry interface

  store/
    port.go        TaskStore (incl. ListActiveByDevice), WorkflowStateStore,
                   TaskQueue, CommandOutboxStore, EventPlaneStore interfaces
    memory.go      in-memory implementations
    file.go        file-backed implementations (used in production)
    redis.go       Redis-backed implementations
    helpers.go     shared store helpers
    errors.go      ErrNotFound and other store sentinels
    event_plane_query.go  EventPlaneStore query types

  dispatcher/
    dispatcher.go  MemoryDispatcher + inflight correlation
    inflight.go    inflightTracker (commandID → chan CommandResult)

  workflow/
    engine.go       Engine: Handle(), advance(), resolveDef()
    node.go         Node interface (Handle(ctx, Input) (Output, error))
    action_node.go  ActionNode — issues device.execute commands
    toolcall_node.go ToolCallNode — runs server-side tool calls via ToolRegistry
    snapshot_matcher.go SnapshotMatchesExpect + snapshot match functions
    matcher.go      MatchEvent, MatchExpect, event-stream matching
    defstore.go     DefStore interface + MemoryDefStore
    fsdefstore.go   FSDefStore (file-backed; wraps DefStore; retains last-valid def on error)
    validate.go     Validate() — validates WorkflowDef including timeout parsing
    interpolate.go  InterpolateStrict — {{input.key}} placeholder resolution
    command.go      workflow command helpers
    ports.go        internal workflow port types

  orchestrator/
    orchestrator.go  ProcessAcceptedEvent, processTasksForEvent, per-device mutex
    watchdog.go      stale-task watchdog

  usecase/
    agent_lifecycle.go        Hello/Resume/Heartbeat/Disconnect → fires AgentOnline/Offline
    event_ingestion.go        IngestUiObservation → orchestrator.ProcessAcceptedEvent
    task_control.go           CreateTask/CancelTask/GetTask
    runtime_recovery.go       startup bootstrap recovery from durable state
    accessibility_auto_enabler.go  ADB-based accessibility re-enable
    device_assigner.go        device→task assignment logic
    event_plane_control.go    event plane replay/inspection control

  handler/
    agent.go        WebSocket JSON-RPC handler (delegates to AgentLifecycleUseCase)
    task.go         REST: POST/GET/DELETE /tasks
    workflow.go     REST: workflow def listing and management
    event_plane.go  REST: /events/accepted, /events/deadletters (pagination + filtering)
    metrics.go      GET /metrics (Prometheus)
    openapi.go      OpenAPI spec endpoint

  transport/ws/
    server.go    WebSocket upgrade, read/write loops, dispatcher injection
    conn.go      per-connection state
    jsonrpc.go   JSON-RPC 2.0 framing helpers

  eventruntime/
    runtime.go          EventRuntime interface + inline in-process implementation
    redis_streams.go    Redis Streams consumer-group implementation
                        (AUTO_EVENT_RUNTIME=redis-streams)

  tools/
    registry.go         ToolRegistry interface + StaticRegistry / fail-closed wiring
    catalog.go          tool catalog: manifest-backed, local + remote tools
    catalog_loader.go   loads tool manifests from config/tools/
    types.go            ToolManifest, ToolInput/Output schema types
    llm.go              LLM/prompt-backed tool adapter (openai, anthropic, gemini, deepseek)
    vision.go           vision tool adapter
    schema.go           JSON schema validation for tool I/O
    exampleprovider/    reference HTTP provider implementation

  telemetry/
    registry.go    Prometheus metric registration and helpers
```

## Dependency Direction (strictly inward)

```
transport/ws → handler → usecase → orchestrator → {store, workflow, dispatcher}
                                   workflow → dispatcher → registry → domain
domain ← (all packages)
```

## Idempotency

| Scenario | Key | Mechanism |
|---|---|---|
| Duplicate UI event | `deviceID:seqNo` | dedupTracker (store-backed) |
| Stale event | `seqNo <= watermark` | watermark per agent (store-backed) |
| Duplicate command response | `commandID` | inflightTracker no-op on closed channel |
| Agent re-registering | `deviceID` in registry | Remove old, Add new |
| Crash recovery | `WorkflowState.CurrentNode` | Load from store; startup bootstrap via `RuntimeRecovery` |

## Verification

```bash
cd app/server-agent
go vet ./...
go test ./internal/workflow/...
go test ./internal/orchestrator/...
go test ./internal/store/...
go test ./...
```
