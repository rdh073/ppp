# CLAUDE.md — server-agent

Go control plane for the ppp agent system. See `plans/server-agent-architecture.md` for design rationale.

## Commands

```bash
# Run (from app/server-agent/)
go run ./cmd/server            # default :3000
go run ./cmd/server -addr :8080
go run ./cmd/server -tool-dir ./config/tools
go run ./cmd/server -workflow-dir ./config/examples/workflows   # load YAML workflow defs
go run ./cmd/tool-provider-example
go run ./cmd/server -tool-dir ./config/examples/http-provider

# Build binary
go build -o bin/server-agent ./cmd/server

# Test
go test ./...
go test ./internal/orchestrator/... -v   # single package

# Vet + tidy
go vet ./...
go mod tidy
```

## Package Layout

```
cmd/server/           wires all components, starts HTTP
internal/
  domain/             pure types — Session, Task, WorkflowState, Event, Command, UiSnapshot
  registry/           MemoryRegistry + Sender interface + AgentRegistry port
  store/              TaskStore + WorkflowStateStore interfaces + in-memory impls
  eventruntime/       event submission boundary; inline vs redis-streams runtime modes
  dispatcher/         Dispatcher interface — routes device.* commands, correlates responses
  workflow/           Engine (event-driven step graph), DefStore/FSDefStore, ToolInvoker port, matcher, interpolate
  orchestrator/       ProcessEvent: per-device lock + watermark + dedup + engine run + checkpoint
  usecase/            AgentLifecycle, TaskControl, RuntimeRecovery — thin orchestration glue
  handler/            JSON-RPC agent handler (thin), HTTP task handler
  transport/ws/       WebSocket upgrade, read loop, JSON-RPC framing
  tools/              startup-loaded tool catalog, providers, schema validation, local and prompt-backed tool adapters
```

**Dependency direction** (strictly inward):
```
transport/ws → handler → usecase → orchestrator → {store, workflow, dispatcher}
                                    workflow/Engine → {dispatcher, ToolInvoker} → registry → domain
```

## HTTP API

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Health check |
| `GET` | `/openapi.json` | OpenAPI 3.1 spec for the HTTP control-plane API |
| `GET` | `/swagger/` | Swagger UI for `/openapi.json` |
| `POST` | `/tasks` | Create task `{goal, deviceId?, workflowName?, inputArtifacts?}` |
| `GET` | `/tasks/{id}` | Get task |
| `DELETE` | `/tasks/{id}` | Cancel task |

The OpenAPI document intentionally covers the HTTP endpoints only. The agent protocol at `/ws/agent` remains JSON-RPC 2.0 over WebSocket and is not represented in the Swagger surface.

## WebSocket Protocol (JSON-RPC 2.0)

Endpoint: `ws://<host>/ws/agent`

**Agent → Server (requests):** `agent.hello`, `agent.resume`, `agent.heartbeat`, `agent.disconnect`

**Server → Agent (requests):** `device.observe`, `device.query`, `device.execute`, `device.capabilities.get`

## Accessibility Auto-Enable (ADB)

`server-agent` now auto-enables the Android accessibility service when it receives:
- `android.accessibility.disabled`

Implementation uses `github.com/electricbubble/gadb` (ADB server protocol), not shelling out to the `adb` binary.

Required runtime assumption:
- ADB server is running and reachable (default `localhost:5037`).
- Typical setup: `adb start-server`

Optional env vars:
- `AUTO_ADB_SERVER_HOST` (default: `localhost`)
- `AUTO_ADB_SERVER_PORT` (default: `5037`)
- `AUTO_AGENT_ACCESSIBILITY_COMPONENT` (default: `com.autosdk.agent/com.autosdk.agent.service.AgentAccessibilityService`)
- `AUTO_ADB_SERIAL_BY_DEVICE` (optional fallback mapping; format: `deviceId1=serial1,deviceId2=serial2`)
- `AUTO_EVENT_RUNTIME` (`inline` by default; set `redis-streams` to externalize ingress events onto Redis Streams)
- `AUTO_REDIS_ADDR` (default: `localhost:6379`)
- `AUTO_REDIS_PASSWORD` (optional)
- `AUTO_REDIS_DB` (default: `0`)
- `AUTO_EVENT_BUS_PARTITIONS` (default: `8`; same `deviceId` always hashes to the same `workflow.wakeup.pNN` stream)
- `AUTO_REDIS_GROUP` (default: `server-agent`)
- `AUTO_REDIS_CONSUMER_PREFIX` (default: `server-agent`)
- `AUTO_REDIS_INSTANCE_ID` (optional; stable worker identity suffix for Redis partition ownership)
- `AUTO_EVENT_BUS_LEASE_TTL` (default: `15s`; Redis partition lease TTL for `workflow.wakeup.pNN.owner`)
- `AUTO_EVENT_BUS_PENDING_IDLE` (default: `45s`; minimum idle time before a worker may reclaim pending wakeups)
- `AUTO_EVENT_BUS_CLAIM_COUNT` (default: `16`; max pending wakeups reclaimed per `XAUTOCLAIM` loop)
- `AUTO_EVENT_BUS_OWNERSHIP_RETRY` (default: `500ms`; backoff before retrying partition ownership)
- `AUTO_TOOL_LLM_API_URL` (optional OpenAI-compatible chat-completions endpoint for model-backed tools)
- `AUTO_TOOL_LLM_API_KEY` (optional bearer token for the model-backed tool endpoint)
- `AUTO_TOOL_LLM_MODEL` (required together with `AUTO_TOOL_LLM_API_URL` to enable model-backed tools)

Tool catalog runtime:

- `-tool-dir` (default: `./config/tools`)
- startup fails closed if providers, manifests, bindings, or prompt files are invalid
- default layout:
  - `config/tools/providers.yaml`
  - `config/tools/manifests/*.yaml`
  - `config/tools/bindings/*.yaml`
  - `config/tools/prompts/*`
- built-in provider kinds currently supported:
  - `builtin`: deterministic local tools plus generic OpenAI-compatible JSON prompt execution
  - `http`: remote tool provider exposing `GET /v1/tools` and `POST /v1/tools/{name}:invoke`
  - `openai`: explicit OpenAI chat-completions prompt tools with structured JSON output
  - `anthropic`: native Anthropic Messages API prompt tools with structured output via forced tool use
  - `gemini`: native Gemini `generateContent` prompt tools with JSON response schema
  - `deepseek`: native DeepSeek chat-completions prompt tools with structured output via forced tool calls
- shipped example remote provider path:
  - server: `cmd/tool-provider-example`
  - example catalog: `config/examples/http-provider`
  - override `AUTO_TOOL_EXAMPLE_BASE_URL` if the example provider is not listening on `http://127.0.0.1:3310`
  - default `config/tools` also ships `identity.generate_alias_email` and `example_remote.generate_alias_email` behind an optional `example-http` provider
  - default `config/tools` also ships `content.generate_welcome_email.openai` behind an optional `openai-native` provider
  - default `config/tools` also ships `content.generate_welcome_email.deepseek` behind an optional `deepseek-native` provider
  - the default catalog keeps that tool visible but disabled until `AUTO_TOOL_EXAMPLE_BASE_URL` is set and discovery succeeds
  - the default catalog keeps `content.generate_welcome_email.openai` visible but disabled until `AUTO_TOOL_OPENAI_API_KEY` and `AUTO_TOOL_OPENAI_MODEL` are set
  - the default catalog keeps `content.generate_welcome_email.deepseek` visible but disabled until `AUTO_TOOL_DEEPSEEK_API_KEY` and `AUTO_TOOL_DEEPSEEK_MODEL` are set
  - note: the example catalog is intentionally minimal and is not a drop-in replacement for `config/tools`
- shipped real-LLM env examples:
  - `config/examples/llm-providers/openai.env.example`
  - `config/examples/llm-providers/anthropic.env.example`
  - `config/examples/llm-providers/gemini.env.example`
  - `config/examples/llm-providers/deepseek.env.example`
  - native catalog examples:
    - `config/examples/llm-providers/catalogs/openai`
    - `config/examples/llm-providers/catalogs/anthropic`
    - `config/examples/llm-providers/catalogs/gemini`
    - `config/examples/llm-providers/catalogs/deepseek`

Runtime persistence:
- `go run ./cmd/server -data-dir ./var`
- Default runtime data directory is `./var` relative to `app/server-agent/`
- On startup, `RuntimeRecovery` scans persisted tasks and workflow state before the server accepts traffic
- On startup, the event runtime is initialized before the server accepts traffic
- The server persists:
  - tasks
  - workflow checkpoints
  - accepted events + dead letters
  - command outbox records

Expected event payload (params) for multi-device safety:

```json
{
  "seqNo": 42,
  "adbSerial": "emulator-5554",
  "serviceComponent": "com.autosdk.agent/com.autosdk.agent.service.AgentAccessibilityService"
}
```

Notes:
- If `adbSerial` is omitted and exactly one device is connected, that device is used.
- If `adbSerial` is omitted and `AUTO_ADB_SERIAL_BY_DEVICE` contains the deviceId mapping, the mapped serial is used.
- If multiple devices are connected and `adbSerial` is missing, auto-enable is rejected with an explicit error.
- `android.*` events are accepted only after successful `agent.hello` / `agent.resume` (registered connection).
- Safety guard: before mutating accessibility settings, server verifies `settings get secure android_id` on the selected adb target matches the registered `deviceId`.
- Production tool runtime now loads tools and workflow bindings from the startup catalog under `config/tools` instead of hardcoding production registry composition in `main.go`.
- Workflows may queue `pending_tool_binding` as the preferred tool invocation contract; legacy `pending_tool` and `pending_tool_params` remain supported for compatibility.
- Tasks may carry `inputArtifacts` on create; missing workflow-state bootstrap now seeds those artifacts into the first checkpoint and into startup-recovery bootstrap.
- Binding-aware `ToolCallNode` maps tool results directly into workflow artifacts and still emits raw `tool.result` for audit and replay.
- Model-backed tools are optional; if `AUTO_TOOL_LLM_API_URL` or `AUTO_TOOL_LLM_MODEL` is unset, prompt-backed tools such as `content.generate_welcome_email` stay visible to workflows but return disabled so workflow-level deterministic fallback can take over.
- The default catalog also ships `content.generate_welcome_email.openai` behind the native `openai` provider kind; it stays visible but disabled until `AUTO_TOOL_OPENAI_API_KEY` and `AUTO_TOOL_OPENAI_MODEL` are set.
- The default catalog also ships `content.generate_welcome_email.deepseek` behind the native `deepseek` provider kind; it stays visible but disabled until `AUTO_TOOL_DEEPSEEK_API_KEY` and `AUTO_TOOL_DEEPSEEK_MODEL` are set.
- Shipped built-in workflow names now include:
  - `local-identity-profile`
  - `local-identity-welcome-email`
  - `android-settings-private-dns`
- The Private DNS workflow requires task input artifact `private_dns_hostname` and currently uses heuristic Settings labels plus scroll detection for UI planning.
- `tool.result` is durably accepted through the same event plane store as device-originated events.
- Workflow checkpoints use a persisted optimistic `Revision` token; stale checkpoint saves fail with a store conflict instead of silently overwriting newer state.
- Startup recovery bootstraps missing workflow checkpoints for assigned non-terminal tasks and reconciles persisted terminal workflow state back into task status.
- Bootstrapped checkpoints are tagged with artifacts `recovery_bootstrap=true` and `recovery_bootstrap_reason=startup_missing_checkpoint`.
- `AUTO_EVENT_RUNTIME=inline` is the explicit development mode: accept event, then process it in-process immediately.
- `AUTO_EVENT_RUNTIME=redis-streams` publishes accepted ingress events to `events.accepted` as an audit mirror and to partitioned wakeup streams `workflow.wakeup.pNN`; only the wakeup streams are consumed by worker goroutines through Redis consumer groups.
- In `redis-streams` mode, partition ownership is coordinated with Redis lease keys `workflow.wakeup.pNN.owner`; only the current lease holder drains and processes that lane.
- Each partition worker recovers in this order: drain its own pending entries, claim idle pending entries with `XAUTOCLAIM`, then read new entries.
- In `redis-streams` mode, ingress accepted events and accepted-event replay fall back inline if wakeup publication fails after durable acceptance.
- Internal emitted events such as `tool.result` are externalized onto Redis Streams in `AUTO_EVENT_RUNTIME=redis-streams`; the orchestrator checkpoints state, publishes the internal event, and stops inline auto-advance until a worker replays that accepted event.
- Accepted events and dead letters can be inspected and replayed through:
  - `GET /events/accepted`
  - `GET /events/accepted/{eventId}`
  - `POST /events/accepted/{eventId}/replay`
  - `GET /events/deadletters`
  - `GET /events/deadletters/{deadLetterId}`
  - `POST /events/deadletters/{deadLetterId}/replay`
- Operational counters are exposed through:
- Operational metrics are exposed through:
  - `GET /metrics`
- List endpoints support:
  - `limit` (default `100`, max `500`)
  - `offset`
  - `cursor` (opaque keyset token returned as `nextCursor`)
  - `order=asc|desc` (default `desc`)
  - `from` and `to` RFC3339 bounds applied to the event-plane record timestamp
  - accepted filters: `deviceId`, `kind`, `source`
  - dead-letter filters: `deviceId`, `kind`, `source`, `eventId`
- List endpoints return paginated envelopes with `items`, `total`, `offset`, `limit`, `hasMore`, and optional `nextCursor`.
- `from` and `to` are inclusive:
  - `/events/accepted` filters on `acceptedAt`
  - `/events/deadletters` filters on `recordedAt`
- `nextCursor` is anchored on the last record timestamp plus record id for the current sort order.
- event-plane list filtering and pagination now run through `store.EventPlaneStore` query methods.
- Current Prometheus metric families are:
  - `autosdk_server_wakeup_publish_fallback_total{path=...}`
  - `autosdk_server_redis_partition_lease_lost_total`
  - `autosdk_server_event_replay_total{path=...,outcome=...}`
  - `autosdk_server_workflow_wakeup_queue_depth`
  - `autosdk_server_workflow_wakeup_partition_depth{partition=...}`
  - `autosdk_server_device_lane_active`
  - `autosdk_server_command_inflight`
  - `autosdk_server_command_timeout_total{kind=...}`
  - `autosdk_server_event_ingest_lag_seconds{source=...}`
  - `autosdk_server_workflow_node_duration_seconds{node=...,outcome=...}`
  - `autosdk_server_tool_call_duration_seconds{tool=...,outcome=...}`
  - `autosdk_server_command_duration_seconds{kind=...,outcome=...}`
- Queue depth is sampled only in `AUTO_EVENT_RUNTIME=redis-streams` and is reported as Redis consumer-group `lag + pending` across `workflow.wakeup.pNN`.
- Per-partition queue saturation is exposed as `autosdk_server_workflow_wakeup_partition_depth{partition="pNN"}`.
- Active device-lane saturation is measured in `orchestrator.ProcessAcceptedEvent`.
- In-flight command saturation is measured in `dispatcher.MemoryDispatcher`.
- Explicit command timeouts are counted in `dispatcher.MemoryDispatcher`; timeout cleanup removes the command from inflight tracking instead of waiting indefinitely for a response.
- Accepted-event ingest lag is measured when `orchestrator.ProcessAcceptedEvent` starts after device-lane serialization.
- Workflow node duration is measured in `workflow.Runner`.
- Tool-call duration is measured in `nodes.ToolCallNode`.
- Command duration is measured from `domain.Command.IssuedAt` to dispatch failure or response delivery in `dispatcher.MemoryDispatcher`.
- `events.accepted` is intentionally not a second workflow trigger queue. It exists as an audit or export stream alongside the durable event-plane store and `/events/accepted` APIs.
- Accepted-event replay does not re-accept a duplicate event. It replays through the current runtime mode: inline processing for `inline`, wakeup requeue for `redis-streams`, with inline fallback if wakeup publication fails.
- Dead-letter replay routes `source=ingestion` records back through notification ingestion and routes orchestrator/runtime dead letters back through accepted-event replay.
- `workflow.NodeOutput.EmittedEvents` contract:
  - emitted events must target the same `deviceId` as the current workflow state
  - emitted events are accepted in slice order
  - `AUTO_EVENT_RUNTIME=inline` drains accepted emitted events in slice order for the current task path
  - `AUTO_EVENT_RUNTIME=redis-streams` publishes wakeups in slice order to the same device partition
  - if wakeup publication fails before any wakeup in the emitted batch has been published, the batch falls back inline
  - if wakeup publication fails after one or more wakeups in the batch have already been published, orchestration fails closed to avoid reordering
- Current limitation: the current file-backed and in-memory event-plane stores own pagination now, but they still answer list queries by linearly scanning snapshot-backed slices.

## Extending

- **New workflow step type:** add a YAML file under `config/examples/workflows/` (or `-workflow-dir`) using the `StepDef` schema (`trigger`, `action`, `tool_call`, `expect`, `on_success`, `on_failure`). No Go code needed for data-driven workflows. For a new built-in workflow, add a YAML file to the default workflow dir or seed it into a `MemoryDefStore` at startup.
- **New agent.* method:** add case in `transport/ws/server.go:dispatch`, add handler in `handler/agent.go`.
- **New tool without central hardcode:** add or update `config/tools/manifests/*.yaml`, `config/tools/bindings/*.yaml`, and optional `config/tools/prompts/*`; reuse `builtin`, `http`, `openai`, `anthropic`, `gemini`, or `deepseek` providers where possible.
- **Example remote provider contract:** use `cmd/tool-provider-example` plus `config/examples/http-provider` as the reference shape for `GET /v1/tools` discovery and `POST /v1/tools/{name}:invoke`.
- **Persist to Redis:** implement `store.TaskStore` + `store.WorkflowStateStore`, swap in `main.go`.
- **Async orchestrator:** the `Dispatcher.Dispatch()` channel is the async seam — no node logic changes.
