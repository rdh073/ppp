# server-agent: Scalable Control Plane Architecture

## Context

The current `app/server-agent` handles session lifecycle only (~12% of AGENTS.md spec).
This plan adds: Task domain, Workflow DAG execution, Orchestrator, Dispatcher, and persistence ports.

## Target Package Layout

```
internal/
  domain/          — pure types, no internal imports
    session.go     (exists)
    task.go        Task, TaskID, TaskStatus
    workflow.go    WorkflowState, NodeKind enum
    event.go       Event, EventKind, payload variants
    snapshot.go    UiSnapshot, UiTarget (parsed from device.observe response)
    command.go     Command, CommandKind, CommandResult, CommandError

  registry/
    registry.go    (exists) MemoryRegistry + Sender
    port.go        AgentRegistry interface

  store/
    port.go        TaskStore + WorkflowStateStore interfaces
    memory.go      in-memory implementations

  dispatcher/
    dispatcher.go  Dispatcher interface + MemoryDispatcher
    inflight.go    inflightTracker (commandID → chan CommandResult)

  workflow/
    runner.go      NodeRunner interface, NodeInput, NodeOutput
    nodes/
      observe.go   issue device.observe → Decide
      decide.go    rule table: artifacts → next branch
      act.go       issue device.execute → Verify
      verify.go    compare snapshot hashes → Decide or Resync
      resync.go    issue device.observe, reset errors → Decide
      terminal.go  mark task done/failed

  orchestrator/
    orchestrator.go  ProcessEvent(): per-device lock, watermark, dedup, run node, checkpoint
    watermark.go     per-agent seqNo high-water mark
    dedup.go         per-agent event-ID ring buffer

  usecase/
    agent_lifecycle.go  Hello/Resume/Heartbeat/Disconnect → fires AgentOnline/Offline events
    task_control.go     CreateTask/CancelTask/GetTask
    event_ingestion.go  IngestUiObservation → orchestrator.ProcessEvent

  handler/
    agent.go       (refactor) → delegates to AgentLifecycleUseCase
    task.go        REST: POST/GET/DELETE /tasks

  transport/ws/
    server.go      (extend) deliverResponse() in readLoop + Dispatcher injection
```

## Dependency Direction (strictly inward)

```
transport/ws → handler → usecase → orchestrator → {store, workflow, dispatcher}
                                   workflow/nodes → dispatcher → registry → domain
domain ← (all packages)
```

## Build Order

1. domain types (task, workflow, event, snapshot, command)
2. store interfaces + memory impl
3. registry port (AgentRegistry interface)
4. dispatcher + inflight tracker
5. workflow runner + nodes (observe, decide, act, verify, resync, terminal)
6. orchestrator (ProcessEvent + watermark + dedup)
7. usecase layer + handler refactor
8. task HTTP handler + main.go wiring

## Idempotency

| Scenario | Key | Mechanism |
|---|---|---|
| Duplicate UI event | `deviceID:seqNo` | dedupTracker ring buffer |
| Stale event | `seqNo <= watermark` | watermarkTracker per agent |
| Duplicate command response | `commandID` | inflightTracker no-op on closed channel |
| Agent re-registering | `deviceID` in registry | Remove old, Add new |
| Crash recovery | `WorkflowState.CurrentNode` | Load from store; if mid-flight → Resync |

## Verification

- `go test ./...` after each phase
- Phase 4: unit test Dispatch + DeliverResponse from two goroutines
- Phase 6: feed events into ProcessEvent with fakes; assert state transitions
- Phase 8 E2E: agent connects → POST /tasks → verify device.observe arrives over WebSocket
