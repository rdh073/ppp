# CLAUDE.md — server-agent

Go control plane for the ppp agent system. See `plans/server-agent-architecture.md` for design rationale.

## Commands

```bash
# Run (from app/server-agent/)
go run ./cmd/server            # default :3000
go run ./cmd/server -addr :8080

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
  workflow/           NodeRunner, NodeInput/Output; nodes/: Observe/Decide/Act/Verify/Resync/Terminal
  orchestrator/       ProcessEvent: per-device lock + watermark + dedup + node run + checkpoint
  usecase/            AgentLifecycle, TaskControl, RuntimeRecovery — thin orchestration glue
  handler/            JSON-RPC agent handler (thin), HTTP task handler
  transport/ws/       WebSocket upgrade, read loop, JSON-RPC framing
```

**Dependency direction** (strictly inward):
```
transport/ws → handler → usecase → orchestrator → {store, workflow, dispatcher}
                                    workflow/nodes → dispatcher → registry → domain
```

## HTTP API

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Health check |
| `POST` | `/tasks` | Create task `{goal, deviceId?}` |
| `GET` | `/tasks/{id}` | Get task |
| `DELETE` | `/tasks/{id}` | Cancel task |

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
- Model-backed tools are optional; if `AUTO_TOOL_LLM_API_URL` or `AUTO_TOOL_LLM_MODEL` is unset, the `content.generate_welcome_email` tool stays visible to workflows but returns disabled so workflow-level deterministic fallback can take over.
- `tool.result` is durably accepted through the same event plane store as device-originated events.
- Workflow checkpoints use a persisted optimistic `Revision` token; stale checkpoint saves fail with a store conflict instead of silently overwriting newer state.
- Startup recovery bootstraps missing workflow checkpoints for assigned non-terminal tasks and reconciles persisted terminal workflow state back into task status.
- Bootstrapped checkpoints are tagged with artifacts `recovery_bootstrap=true` and `recovery_bootstrap_reason=startup_missing_checkpoint`.
- `AUTO_EVENT_RUNTIME=inline` is the explicit development mode: accept event, then process it in-process immediately.
- `AUTO_EVENT_RUNTIME=redis-streams` publishes accepted ingress events to `events.accepted` and partitioned wakeup streams `workflow.wakeup.pNN`, then worker goroutines consume them through Redis consumer groups.
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
- List endpoints support:
  - `limit` (default `100`, max `500`)
  - `offset`
  - `order=asc|desc` (default `desc`)
  - accepted filters: `deviceId`, `kind`, `source`
  - dead-letter filters: `deviceId`, `kind`, `source`, `eventId`
- List endpoints return paginated envelopes with `items`, `total`, `offset`, `limit`, and `hasMore`.
- Accepted-event replay does not re-accept a duplicate event. It replays through the current runtime mode: inline processing for `inline`, wakeup requeue for `redis-streams`, with inline fallback if wakeup publication fails.
- Dead-letter replay routes `source=ingestion` records back through notification ingestion and routes orchestrator/runtime dead letters back through accepted-event replay.
- `workflow.NodeOutput.EmittedEvents` contract:
  - emitted events must target the same `deviceId` as the current workflow state
  - emitted events are accepted in slice order
  - `AUTO_EVENT_RUNTIME=inline` drains accepted emitted events in slice order for the current task path
  - `AUTO_EVENT_RUNTIME=redis-streams` publishes wakeups in slice order to the same device partition
  - if wakeup publication fails before any wakeup in the emitted batch has been published, the batch falls back inline
  - if wakeup publication fails after one or more wakeups in the batch have already been published, orchestration fails closed to avoid reordering
- Current limitation: event-plane list APIs do not yet support time-range filtering or cursor pagination.

## Extending

- **New workflow node:** implement `workflow.NodeHandler`, register in `main.go` `buildNodeHandlers`.
- **New agent.* method:** add case in `transport/ws/server.go:dispatch`, add handler in `handler/agent.go`.
- **Persist to Redis:** implement `store.TaskStore` + `store.WorkflowStateStore`, swap in `main.go`.
- **Async orchestrator:** the `Dispatcher.Dispatch()` channel is the async seam — no node logic changes.
