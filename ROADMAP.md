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
- durable accepted-event inbox with persisted watermark and dedup cursors
- stale and duplicate events short-circuit with `ErrEventDropped`
- durable dead-letter records for malformed notifications and workflow-processing failures
- internal `tool.result` events are durably accepted through the same event plane as device-originated events
- side effects execute only after accepted event
- ADB accessibility recovery validates `android_id` against `deviceId`
- ADB target supports `adbSerial`, env mapping, and single-device fallback
- explicit event runtime modes exist: `inline` and `redis-streams`

Key files:

- `app/server-agent/internal/transport/ws/server.go`
- `app/server-agent/internal/usecase/event_ingestion.go`
- `app/server-agent/internal/eventruntime/runtime.go`
- `app/server-agent/internal/eventruntime/redis_streams.go`
- `app/server-agent/internal/orchestrator/orchestrator.go`
- `app/server-agent/internal/usecase/accessibility_auto_enabler.go`
- `app/server-agent/internal/store/file.go`

### Server-Agent Durable Runtime Storage

Implemented:

- production task store is file-backed
- task create now accepts durable `inputArtifacts` that seed workflow state bootstrap
- production workflow-state checkpoints are file-backed
- workflow checkpoints advance with an explicit optimistic revision token
- startup runs explicit runtime recovery before the server accepts traffic
- recovery bootstrap seeds workflow artifacts `recovery_bootstrap` and `recovery_bootstrap_reason`
- production command dispatch lifecycle is recorded in a durable command outbox
- production durable data defaults under `app/server-agent/var` unless `-data-dir` is overridden

Key files:

- `app/server-agent/internal/store/file.go`
- `app/server-agent/internal/store/errors.go`
- `app/server-agent/internal/store/port.go`
- `app/server-agent/internal/usecase/runtime_recovery.go`
- `app/server-agent/internal/dispatcher/dispatcher.go`
- `app/server-agent/cmd/server/main.go`

### Server-Agent Tool Runtime

Implemented:

- fail-closed manifest-backed tool registry
- local deterministic tool catalog
- startup-loaded tool catalog under `app/server-agent/config/tools`
- reference non-builtin HTTP provider example under `app/server-agent/cmd/tool-provider-example` and `app/server-agent/config/examples/http-provider`
- default `config/tools` now includes the shipped remote manifest and binding behind an optional `http` provider gate
- provider-backed production wiring instead of hardcoded registry composition in `main.go`
- declarative workflow tool bindings via `pending_tool_binding`
- generic prompt-backed OpenAI-compatible JSON tool executor for config-only LLM tools
- checked-in env examples now exist for OpenAI, Anthropic compatibility, Gemini OpenAI compatibility, and DeepSeek under `app/server-agent/config/examples/llm-providers`
- native OpenAI, Anthropic, Gemini, and DeepSeek provider kinds now exist for prompt-backed tools
- provider kinds: `builtin`, `http`, `openai`, `anthropic`, `gemini`, and `deepseek`
- manifest timeout handling
- manifest retry budget handling for retryable tool failures
- input and output validation hooks
- node-level failure routing for unsupported tools, invalid params, invalid result, and timeout
- optional-tool fallback handoff via `tool_error` for workflows that deliberately tolerate model failure

Production tools currently wired:

1. `identity.generate_indonesian_name`
2. `identity.generate_email`
3. `credential.generate_password`
4. `identity.generate_birth_date`
5. `content.generate_welcome_email`
6. `content.generate_welcome_email.openai`
7. `content.generate_welcome_email.deepseek`

Key files:

- `app/server-agent/internal/workflow/nodes/toolcall.go`
- `app/server-agent/internal/tools/catalog.go`
- `app/server-agent/internal/tools/catalog_loader.go`
- `app/server-agent/internal/tools/schema.go`
- `app/server-agent/internal/tools/llm.go`
- `app/server-agent/config/tools/`
- `app/server-agent/cmd/server/main.go`

### Server-Agent Workflow Adoption

Implemented:

- built-in workflow path `local-identity-profile`
- built-in workflow path `local-identity-welcome-email`
- built-in workflow path `android-settings-private-dns`
- `DecideNode` seeds deterministic `ToolCall` steps for that workflow
- `DecideNode` consumes `tool_result` back into durable workflow artifacts
- `DecideNode` consumes optional `tool_error` and applies deterministic fallback email content
- `DecideNode` now also plans Android Settings actions for Private DNS using observed UI snapshots
- orchestrator auto-advances internal nodes so `Decide -> ToolCall -> Decide -> Terminal` can complete in one event tick

Key files:

- `app/server-agent/internal/workflow/defaults.go`
- `app/server-agent/internal/workflow/nodes/decide.go`
- `app/server-agent/internal/workflow/nodes/local_identity_workflows.go`
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

Status: done

Outcome:

- production tool wiring now loads provider-backed tools from `config/tools`
- config-only prompt-backed model tools can be added without central registry edits
- the non-builtin `http` provider path is exercised end-to-end by a shipped example provider and example catalog
- the default catalog can now enable that same `http` provider path without switching `-tool-dir`
- model-backed tool output is schema-validated before artifact persistence
- retry budget and trace logging exist for model-backed calls
- shipped workflow fallback exists when the model-backed tool is disabled, times out, or fails
- native OpenAI, Anthropic, Gemini, and DeepSeek prompt-provider catalogs are loadable under `config/examples/llm-providers/catalogs`

Implemented in:

- [catalog_loader.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/tools/catalog_loader.go)
- [schema.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/tools/schema.go)
- [llm.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/tools/llm.go)
- [handler.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/tools/exampleprovider/handler.go)
- [toolcall.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/workflow/nodes/toolcall.go)
- [local_identity_workflows.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/workflow/nodes/local_identity_workflows.go)
- [orchestrator_test.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/orchestrator/orchestrator_test.go)
- [llm_test.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/tools/llm_test.go)
- [catalog_loader_test.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/tools/catalog_loader_test.go)
- [config/tools](/home/xtrzy/Workspace/ppp/app/server-agent/config/tools)
- [cmd/tool-provider-example](/home/xtrzy/Workspace/ppp/app/server-agent/cmd/tool-provider-example)
- [config/examples/http-provider](/home/xtrzy/Workspace/ppp/app/server-agent/config/examples/http-provider)

Runtime config:

- `AUTO_TOOL_LLM_API_URL`
- `AUTO_TOOL_LLM_API_KEY`
- `AUTO_TOOL_LLM_MODEL`
- `-tool-dir ./config/tools`
- `AUTO_TOOL_EXAMPLE_BASE_URL` when running the shipped HTTP provider example on a non-default address

Validation:

- `go test ./...` in `app/server-agent`

### Phase 4: Durable Event Plane

Status: done

Outcome:

- accepted device and internal events are durable
- dead letters are durable
- dedup and watermark state moved out of orchestrator memory into the event plane store
- command dispatch lifecycle is durably recorded in a command outbox
- production startup now uses file-backed task, workflow-state, event-plane, and command-outbox stores

Implemented in:

- [file.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/store/file.go)
- [dispatcher.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/dispatcher/dispatcher.go)
- [orchestrator.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/orchestrator/orchestrator.go)
- [event_ingestion.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/usecase/event_ingestion.go)
- [main.go](/home/xtrzy/Workspace/ppp/app/server-agent/cmd/server/main.go)

Validation:

- `go test ./...` in `app/server-agent`

### Phase 5: Durable Workflow Runtime

Status: done

Outcome:

- workflow checkpoint advancement now uses an explicit optimistic revision token
- missing workflow checkpoints are bootstrapped explicitly during startup recovery
- persisted terminal workflow state reconciles non-terminal task rows during startup recovery
- restart behavior is covered in tests instead of relying on file-backed snapshots implicitly

Implemented in:

- [errors.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/store/errors.go)
- [port.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/store/port.go)
- [memory.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/store/memory.go)
- [file.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/store/file.go)
- [runtime_recovery.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/usecase/runtime_recovery.go)
- [main.go](/home/xtrzy/Workspace/ppp/app/server-agent/cmd/server/main.go)
- [runtime_recovery_test.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/usecase/runtime_recovery_test.go)
- [file_test.go](/home/xtrzy/Workspace/ppp/app/server-agent/internal/store/file_test.go)

Validation:

- `go test ./...` in `app/server-agent`

Remaining note:

- this is the durable runtime for the current single-process filesystem-backed deployment
- distributed replay and external stream scale-out remain Phase 6+

### Phase 6: Redis Streams Scale-Out

Status: completed

Goal:

- externalize the pub/sub seam without changing workflow semantics

Implemented in this slice:

- explicit event runtime boundary added around event acceptance and dispatch
- `AUTO_EVENT_RUNTIME=redis-streams` publishes accepted ingress events to `events.accepted` as an audit mirror
- wakeups are partitioned by `deviceId` into `workflow.wakeup.pNN`
- one worker goroutine per partition consumes from Redis consumer groups
- multi-process partition ownership is guarded by Redis lease keys `workflow.wakeup.pNN.owner`
- each partition worker renews its lease while active and backs off when ownership is held by another instance
- each partition worker first drains its own pending messages, then claims idle pending work with `XAUTOCLAIM`, then reads new messages
- internal emitted events such as `tool.result` now publish to the same wakeup bus instead of continuing inline when publication succeeds
- accepted events and dead letters now have explicit control-plane inspection and replay routes under `/events/*`
- accepted-event replay respects runtime mode: inline processing in dev mode, wakeup requeue in `redis-streams` mode
- dead-letter replay routes ingestion failures back through `EventIngestionUseCase` and orchestrator failures back through accepted-event replay
- multiple emitted internal events from one node step are now accepted and handled in slice order instead of silently dropping earlier entries
- emitted internal events must stay on the current `deviceId` lane; mismatched-device emitted events fail closed
- in `redis-streams` mode, ordered wakeup publication falls back inline only if no prior wakeup in the same emitted batch has already been published; otherwise orchestration fails closed to avoid reordering
- `inline` remains the explicit development fallback mode
- ingress accepted events and accepted-event replay still fall back inline when wakeup publication fails after durable acceptance

Required end state:

- `workflow.wakeup` has real worker consumers
- `events.accepted` and `events.deadletter` are explicit audit or operator streams with inspection and replay paths
- any in-process pub/sub fallback is explicit dev-only mode, not dead parallel code

### Phase 7: Operational Hardening

Status: in progress

Goal:

- make the system debuggable and predictable under failure and load

Implemented in this slice:

- `/events/accepted` and `/events/deadletters` now support `limit`, `offset`, and `order`
- accepted-event list supports exact-match filters: `deviceId`, `kind`, `source`
- dead-letter list supports exact-match filters: `deviceId`, `kind`, `source`, `eventId`
- list endpoints now support inclusive `from` and `to` RFC3339 time filters on the event-plane record timestamp
  - accepted events filter on `acceptedAt`
  - dead letters filter on `recordedAt`
- list endpoints now support opaque cursor pagination through `cursor` request param and `nextCursor` response field
- cursor tokens are keyset-style anchors over record timestamp plus record id, scoped to the current sort order
- cursor pagination and filtering are now executed through the `EventPlaneStore` contract instead of the use-case layer
- list endpoints now return pagination metadata: `items`, `total`, `offset`, `limit`, `hasMore`
- default list limit is `100`; max accepted limit is `500`
- `/metrics` now exposes Prometheus text output for current operational counters
- `/metrics` now also exposes:
  - `autosdk_server_workflow_wakeup_queue_depth` gauge
  - `autosdk_server_workflow_wakeup_partition_depth{partition=...}` gauge
  - `autosdk_server_device_lane_active` gauge
  - `autosdk_server_command_inflight` gauge
  - `autosdk_server_command_timeout_total{kind=...}` counter
  - `autosdk_server_event_ingest_lag_seconds` histogram by `source=device|internal`
  - `autosdk_server_workflow_node_duration_seconds` histogram by `node` and `outcome`
  - `autosdk_server_tool_call_duration_seconds` histogram by `tool` and `outcome`
  - `autosdk_server_command_duration_seconds` histogram by `kind` and `outcome`
- decision locked: `events.accepted` remains an audit mirror, not a second runtime consumer path
- wakeup publish fallback is counted by path:
  - `ingress`
  - `accepted_replay`
  - `emitted_batch`
- Redis partition lease loss is counted after ownership has been acquired
- Redis queue depth is sampled from `XINFO GROUPS` on `workflow.wakeup.pNN` and reported as `lag + pending` for the configured consumer group
- Redis queue depth is also exposed per partition as `autosdk_server_workflow_wakeup_partition_depth{partition="pNN"}`
- active device-lane saturation is observed in `orchestrator.ProcessAcceptedEvent`
- in-flight command saturation is observed in `dispatcher.MemoryDispatcher`
- accepted-event ingest lag is observed when `ProcessAcceptedEvent` starts after device-lane serialization
- workflow node duration is observed in `workflow.Runner`
- tool-call duration is observed in `nodes.ToolCallNode`
- command duration is observed from `Command.IssuedAt` to dispatch failure or agent response delivery in `dispatcher.MemoryDispatcher`
- explicit command timeouts are counted in `dispatcher.MemoryDispatcher`; timeout cancellation now also clears inflight tracking instead of waiting for a late response
- operator-triggered replay is counted by path and outcome:
  - `accepted`
  - `dead_letter_ingestion`
  - `dead_letter_accepted_event`
  - outcomes: `attempted`, `succeeded`, `failed`
- current metric names are:
  - `autosdk_server_wakeup_publish_fallback_total`
  - `autosdk_server_redis_partition_lease_lost_total`
  - `autosdk_server_event_replay_total`
  - `autosdk_server_workflow_wakeup_queue_depth`
  - `autosdk_server_workflow_wakeup_partition_depth`
  - `autosdk_server_device_lane_active`
  - `autosdk_server_command_inflight`
  - `autosdk_server_command_timeout_total`
  - `autosdk_server_event_ingest_lag_seconds`
  - `autosdk_server_workflow_node_duration_seconds`
  - `autosdk_server_tool_call_duration_seconds`
  - `autosdk_server_command_duration_seconds`

Required end state:

- dashboard coverage for lane health, ingest lag, retries, DLQ growth, and tool-call latency
- dashboard coverage now also includes command-plane latency and saturation baselines
- replay tooling remains observable and safe under failure and load
- current limitation: queue depth is only sampled in `AUTO_EVENT_RUNTIME=redis-streams`; inline mode remains zero by design
- current limitation: this slice does not yet expose per-partition lease ownership or explicit command-timeout-rate alerts beyond the raw metrics
- current limitation: the current file-backed and in-memory event-plane stores own pagination now, but they still scan in-memory snapshot slices linearly

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

Start Phase 7:

1. add alert-oriented rollups for command timeout rate and partition skew or starvation on top of the raw metrics
2. optimize event-plane store queries further if linear snapshot scans become a bottleneck
3. keep any future downstream use of `events.accepted` scoped to analytics or integration export rather than workflow control flow
