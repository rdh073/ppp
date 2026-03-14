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
  dispatcher/         Dispatcher interface — routes device.* commands, correlates responses
  workflow/           NodeRunner, NodeInput/Output; nodes/: Observe/Decide/Act/Verify/Resync/Terminal
  orchestrator/       ProcessEvent: per-device lock + watermark + dedup + node run + checkpoint
  usecase/            AgentLifecycle, TaskControl — thin orchestration glue
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

## Extending

- **New workflow node:** implement `workflow.NodeHandler`, register in `main.go` `buildNodeHandlers`.
- **New agent.* method:** add case in `transport/ws/server.go:dispatch`, add handler in `handler/agent.go`.
- **Persist to Redis:** implement `store.TaskStore` + `store.WorkflowStateStore`, swap in `main.go`.
- **Async orchestrator:** the `Dispatcher.Dispatch()` channel is the async seam — no node logic changes.
