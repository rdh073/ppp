# ROADMAP

This file is the continuity document for the workspace.

Purpose:

- preserve implementation context across sessions
- record what is already real in code, not only what was discussed
- define the next phases without leaving dead code or abandoned partial work

Detailed architecture remains in [plans/event-driven-argo-like-architecture-v2.md](/home/xtrzy/Workspace/ppp/plans/event-driven-argo-like-architecture-v2.md).

## Scope

Two subprojects:

- `app/android-agent`: execution edge on Android
- `app/server-agent`: Go control plane and workflow runtime

## Non-Negotiable Invariants

These must remain true unless the architecture doc is deliberately revised.

1. `android-agent` does not own workflow orchestration or task lifecycle.
2. `server-agent` is the source of truth for workflow state and task progression.
3. `deviceId` is canonical identity. `adbSerial` is transport locator only.
4. Device-originated side effects happen only after event acceptance, dedup, and watermark checks.
5. Per-device workflow transitions and UI-mutating commands are serialized by `deviceId`.
6. `ToolCall` is a server-side workflow capability, not a `device.*` command.
7. Deterministic local tools should be used before LangGraph or LLM-backed tools.
8. No placeholder success path is allowed in production wiring.

## Source Documents

Read these first when resuming work:

1. [AGENTS.md](/home/xtrzy/Workspace/ppp/AGENTS.md)
2. [plans/event-driven-argo-like-architecture-v2.md](/home/xtrzy/Workspace/ppp/plans/event-driven-argo-like-architecture-v2.md)
3. [app/server-agent/CLAUDE.md](/home/xtrzy/Workspace/ppp/app/server-agent/CLAUDE.md)
4. This file

## Current Implementation Snapshot

### Android-Agent

Implemented:

- emits only `android.accessibility.disabled`
- persists outbound event sequence
- persists pending critical accessibility-disabled notification
- retries notification send after reconnect

Key files:

- `app/android-agent/app/src/main/java/com/autosdk/agent/service/AgentAccessibilityService.kt`
- `app/android-agent/app/src/main/java/com/autosdk/agent/state/AgentStateStore.kt`
- `app/android-agent/app/src/main/java/com/autosdk/agent/transport/WebSocketAgentTransport.kt`

### Server-Agent Event Reliability

Implemented:

- registered-only acceptance for `android.*`
- deterministic device event identity
- stale and duplicate events short-circuit with `ErrEventDropped`
- side effects execute only after accepted event
- ADB accessibility recovery validates `android_id` against `deviceId`
- ADB target supports `adbSerial`, env mapping, and single-device fallback

Key files:

- `app/server-agent/internal/transport/ws/server.go`
- `app/server-agent/internal/usecase/event_ingestion.go`
- `app/server-agent/internal/orchestrator/orchestrator.go`
- `app/server-agent/internal/usecase/accessibility_auto_enabler.go`

### Server-Agent Tool Runtime

Implemented:

- fail-closed manifest-backed tool registry
- local deterministic tool catalog
- manifest timeout handling
- input and output validation hooks
- node-level failure routing for unsupported tools, invalid params, invalid result, and timeout

Production tools currently wired:

1. `identity.generate_indonesian_name`
2. `identity.generate_email`
3. `credential.generate_password`
4. `identity.generate_birth_date`

Key files:

- `app/server-agent/internal/workflow/nodes/toolcall.go`
- `app/server-agent/internal/tools/catalog.go`
- `app/server-agent/cmd/server/main.go`

### Server-Agent Workflow Adoption

Implemented:

- built-in workflow path `local-identity-profile`
- `DecideNode` seeds deterministic `ToolCall` steps for that workflow
- `DecideNode` consumes `tool_result` back into durable workflow artifacts
- orchestrator auto-advances internal nodes so `Decide -> ToolCall -> Decide -> Terminal` can complete in one event tick

Key files:

- `app/server-agent/internal/workflow/defaults.go`
- `app/server-agent/internal/workflow/nodes/decide.go`
- `app/server-agent/internal/orchestrator/orchestrator.go`
- `app/server-agent/internal/orchestrator/orchestrator_test.go`

## Phase Status

### Phase 0: Baseline Alignment

Status: done

Outcome:

- architecture and implementation language aligned
- pub/sub seam made explicit
- `ToolCall` documented as first-class workflow capability

Reference:

- [plans/event-driven-argo-like-architecture-v2.md](/home/xtrzy/Workspace/ppp/plans/event-driven-argo-like-architecture-v2.md)

### Phase 1: Fail-Closed Tool Runtime Contract

Status: done

Outcome:

- production no longer uses silent noop tool execution
- tool runtime is manifest-backed and fail-closed

Implemented in:

- [toolcall.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/workflow/nodes/toolcall.go)
- [main.go](/home/xtrzy/Workspace/ppp/app/server-agent/cmd/server/main.go)

Validation:

- `go test ./...` in `app/server-agent`

### Phase 2: Deterministic Tool Catalog MVP

Status: done

Outcome:

- local deterministic tools exist and are wired in production
- tool inputs and outputs are validated before artifact persistence

Implemented in:

- [catalog.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/tools/catalog.go)
- [catalog_test.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/tools/catalog_test.go)
- [nodes_test.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/workflow/nodes/nodes_test.go)

Validation:

- `go test ./...` in `app/server-agent`

## Next Phases

### Phase 2.5: Workflow Adoption of Current Tools

Status: done

Outcome:

- at least one shipped workflow path uses real deterministic `ToolCall` steps
- `tool_result` is consumed by downstream workflow logic
- end-to-end orchestrator coverage exists for the shipped path

Implemented in:

- [defaults.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/workflow/defaults.go)
- [decide.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/workflow/nodes/decide.go)
- [orchestrator.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/orchestrator/orchestrator.go)
- [orchestrator_test.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/orchestrator/orchestrator_test.go)

Validation:

- `go test ./...` in `app/server-agent`

### Phase 3: LangGraph or LLM Tool Adapter

Status: next

Goal:

- add model-backed tools only where deterministic logic is insufficient

Candidates:

1. natural-language email body generation
2. profile or persona enrichment
3. fuzzy policy or risk classification

Non-candidates:

1. password generation
2. birth-date generation
3. simple email address composition

Required end state:

- model-backed tools remain behind `ToolRegistry`
- deterministic tools stay local
- every model-backed tool has timeout, validation, fallback branch, and trace logging

### Phase 4: Durable Event Plane

Status: planned

Goal:

- persist accepted events, dead-letter records, and command outbox state

Required end state:

- no correctness-critical event exists only in memory
- device events, recovery events, and internal tool results follow the same acceptance path

### Phase 5: Durable Workflow Runtime

Status: planned

Goal:

- make workflow execution resumable and deterministic after restart

Required end state:

- workflow checkpoints are durable
- artifact contracts are documented and tested

### Phase 6: Redis Streams Scale-Out

Status: planned

Goal:

- externalize the pub/sub seam without changing workflow semantics

Required end state:

- `events.accepted`, `workflow.wakeup`, and `events.deadletter` have real producers and consumers
- any in-process pub/sub fallback is explicit dev-only mode, not dead parallel code

### Phase 7: Operational Hardening

Status: planned

Goal:

- make the system debuggable and predictable under failure and load

Required end state:

- dashboard coverage for lane health, ingest lag, retries, DLQ growth, and tool-call latency
- operator replay tooling exists for dead-letter events

## Continuity Protocol

This section exists specifically to avoid loss of context and loss of implementation.

### Before starting a phase

1. read this file
2. read the detailed architecture doc
3. inspect current production wiring in touched modules
4. choose the smallest phase slice that leaves the workspace shippable

### While a phase is in progress

1. do not create new interfaces without a real implementation in the same phase unless the interface stays local
2. do not leave `noop`, fake success, or unused queue/stream names on production paths
3. do not leave orphan artifact keys or node kinds without workflow and test coverage
4. if work must pause mid-phase, update this file with:
   - current status
   - touched files
   - remaining blockers

### At the end of a phase

1. run relevant tests
2. update this file first
3. update the detailed architecture doc if the design changed
4. remove superseded code in the same phase
5. record the next phase clearly

## Definition of Done for Any Phase

A phase is done only if all are true:

1. runtime wiring is coherent
2. tests pass for touched areas
3. no dead placeholder path remains in production
4. this file reflects the new workspace state
5. the next phase can start without reconstructing context from chat history

## Immediate Next Action

Implement Phase 3:

1. add one model-backed tool behind `ToolRegistry`
2. keep deterministic tools local and unchanged
3. validate model output before artifact persistence
4. add explicit fallback behavior in workflow logic when the model-backed tool fails
