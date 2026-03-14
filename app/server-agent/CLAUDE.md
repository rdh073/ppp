# CLAUDE.md — server-agent

Go control plane for the ppp agent system.

## Commands

```bash
# Run (from app/server-agent/)
go run ./cmd/server            # default :3000
go run ./cmd/server -addr :8080

# Build binary
go build -o bin/server-agent ./cmd/server

# Test
go test ./...
go test ./internal/handler/... -v   # single package

# Vet + tidy
go vet ./...
go mod tidy
```

## Package Layout

```
cmd/server/         entrypoint — wires registry, handler, ws server, starts HTTP
internal/
  domain/           Session, SessionID, DeviceID, Capability — no imports from internal
  registry/         MemoryRegistry — maps SessionID/DeviceID → (Session, Sender conn)
  handler/          AgentHandler — use case for hello/resume/heartbeat/disconnect
  transport/ws/     WebSocket adapter — upgrade, read loop, JSON-RPC framing
```

Dependency direction: `transport/ws` → `handler` → `registry` → `domain`.

## Protocol (JSON-RPC 2.0 over WebSocket)

Endpoint: `ws://<host>/ws/agent`

**Agent → Server (requests the server handles):**
| Method | Params |
|---|---|
| `agent.hello` | `{deviceId, agentInstanceId, capabilities[]}` |
| `agent.resume` | `{deviceId, sessionId, capabilities[]}` |
| `agent.heartbeat` | `{deviceId, sessionId}` |
| `agent.disconnect` | `{deviceId, sessionId}` |

**Server → Agent (requests for device control, stubbed for now):**
`device.observe`, `device.query`, `device.execute`, `device.capabilities.get`

Success response shape: `{jsonrpc:"2.0", id, result:{accepted:true, sessionId?}}`
Error response shape: `{jsonrpc:"2.0", id, error:{code, message}}`

## Extending

- **New agent.* method:** add a `Handle*` method to `handler.AgentHandler`, then add a `case` in `transport/ws/server.go:dispatch`.
- **Dispatch device.* command to agent:** call `registry.GetBySession(id)` to get the `Sender`, then `sender.SendRequest(id, "device.execute", params)`. Handle the response in the ws read loop.
- **Persist sessions (Redis):** implement `registry.Sender` and swap `registry.New()` in `main.go`. The handler and transport are not affected.
