# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## LOAD RUNTIME GUARDRAIL

- Load skill `coder` when beginning to write code.
- Use Serena tools for code navigation:
  `search_for_pattern`, `find_symbol`, `find_referencing_symbols`, `get_symbols_overview`,
  `insert_after_symbol`, `insert_before_symbol`, `rename_symbol`, `replace_symbol_body`

1. I do not call a major path "working" unless the full boundary is closed.
2. I do not expand minor architecture while major dead code or fake abstractions remain.
3. I separate:
   - critical: workflow core, execution contract, event semantics, recovery
   - support: telemetry, demos, examples, convenience APIs
4. I treat examples as examples, never as proof of runtime completeness.
5. If I cannot close the major boundary, I should say it directly.

---

## Architecture

Read `AGENTS.md` before writing code. Two sub-projects:

**`app/android-agent/`** — Kotlin Android app (execution edge). Runs on-device, connects to the server-agent via WebSocket/JSON-RPC.

**`app/server-agent/`** — Go control plane. Accepts WebSocket connections from android-agents, manages session registry, orchestrates workflow DAG. See `app/server-agent/CLAUDE.md` for its own build/layout docs.

---

## Android-Agent Packages

| Package | Responsibility |
|---|---|
| `boot` | `BootCompletedReceiver` — triggers agent startup on device boot |
| `service` | `AgentAccessibilityService` — single Android lifecycle anchor; wires all components |
| `transport` | `WebSocketAgentTransport` / `AgentTransport` interface; JSON-RPC 2.0 over WebSocket |
| `observation` | `UiSnapshot`, `UiTarget`, `SnapshotBuilder`, `UiSemantics`; accessibility tree → semantic snapshot |
| `action` | `AutomationAction` sealed class, `Selector`, `ActionExecutor`, `ActionResult` |
| `agent` | `AgentRuntime` (dispatches inbound JSON-RPC), `AgentCapabilities` (capability list + serialization), `AgentAutomationDriver` |
| `state` | Unidirectional state machine: `AgentState` + `AgentEvent` → `AgentReducer` → `AgentReduction(state, effects)` → `AgentStateCoordinator` runs effects |

### State machine

`AgentReducer` is a pure function (`state + event → new state + effects`). `AgentStateCoordinator` is the effectful runner. `AgentEffect` is a sealed interface — adding new side-effects means adding a new subtype and handling it in the coordinator.

`AgentStateCoordinator.runEffect` has two effect categories:
- **Tracked** (SendHello, SendResume, SendHeartbeat): use `sendAgentRequest`; outcome handled by `onRpcSuccess`/`onRpcFailure`.
- **Fire-and-forget** (ConnectSocket, SendDisconnect, PublishUiEvent): wrapped in `runCatching + onFailure(log)`.

### Observation / semantic projection

`SnapshotBuilder.build()` traverses the accessibility tree and calls `shouldIncludeNode(visible, hasArea, hasId, hasText)` to filter noise. `buildSemanticProjection()` in `UiSemantics.kt` enriches raw targets with:
- `baseScreenKey` / `overlayKey` / `activeUiKey` — stable screen identifiers
- Per-target `semanticKey` (e.g. `form.primary.email`, `button.sign_in`)
- `UiFormState` and `UiButtonState` for form-awareness
- `semanticDigest` — SHA-1 hash for change detection

### Key invariants

- The agent never owns task/workflow lifecycle — that belongs to the server-agent.
- Raw accessibility events are triggers only; truth is always a freshly built `UiSnapshot`.
- All state transitions must go through `AgentReducer.reduce`; illegal transitions emit `AgentEffect.Log` and return unchanged state.
- Session persistence via `AgentStateStore`/`SharedPreferencesAgentStateStore` for reconnect continuity.
- Node recycling: nodes from `withNode` are recycled by `withNode`'s `try/finally`. Nodes from `findSelfOrAncestor` are owned by the caller and recycled in the handler's own `finally`.

### JSON-RPC methods (server → agent)

- `device.observe` — returns current `UiSnapshot`
- `device.query` — resolves a selector, returns matching `UiTarget`s
- `device.execute` — observe → act → settle → observe; returns `{snapshotBefore, snapshotAfter}`
- `device.capabilities.get` — returns capability list from `AgentCapabilities`

### Agent registration (agent → server)

- `agent.hello` — first connection, server responds with `sessionId`
- `agent.resume` — reconnect with existing `sessionId`; server may reject (falls back to hello)
- `agent.heartbeat` — periodic keepalive (every 30 s)
- `agent.disconnect` — graceful disconnect

**Server URL config:** `BuildConfig.SERVER_URL` (default `ws://10.0.2.2:3000/ws/agent` for emulator). Override at runtime: `adb shell setprop auto.agent.server_url <url>`.

---

## Server-Agent Packages

| Package | Responsibility |
|---|---|
| `domain` | Pure types — `Session`, `Task`, `WorkflowState`, `Event`, `Command`, `UiSnapshot`, `WorkflowDef` |
| `registry` | `MemoryRegistry` + `Sender` interface + `AgentRegistry` port |
| `store` | Store interfaces + in-memory / file / Redis implementations |
| `eventruntime` | Event submission boundary; inline vs redis-streams runtime modes |
| `dispatcher` | `Dispatcher` interface — routes device.* commands, correlates responses |
| `workflow` | Engine (event-driven step graph), DefStore/FSDefStore, Node types, ToolInvoker port, matcher, interpolate, validate |
| `orchestrator` | `ProcessEvent`: per-device lock + watermark + dedup + engine run + checkpoint |
| `usecase` | `AgentLifecycle`, `TaskControl`, `RuntimeRecovery` — thin orchestration glue |
| `handler` | JSON-RPC agent handler (thin), HTTP task handler |
| `transport/ws` | WebSocket upgrade, read loop, JSON-RPC framing |
| `tools` | Startup-loaded tool catalog, providers, schema validation, local and prompt-backed tool adapters |

### Workflow engine

`Engine.Handle(ctx, EngineCommand) → EngineResult` is the single entry point.

Step execution uses the **Node** interface:
```go
type Node interface {
    Execute(ctx context.Context, cmd NodeCommand) (NodeOutput, error)
}
```
- **ActionNode** (`action_node.go`) — steps with `Action` set; dispatches device command, pre-checks `snapshotAfter` against Expect via `SnapshotMatchesExpect()` (in `snapshot_matcher.go`)
- **ToolCallNode** (`toolcall_node.go`) — steps with `ToolCall` set; interpolates params from state Inputs, invokes tool, extracts outputs
- **routingNode** — no-op for pure routing steps (no Action, no ToolCall)

`matcher.go` handles event-stream matching (`MatchEvent`, `MatchExpect`). `snapshot_matcher.go` handles `SnapshotMatchesExpect` (checking `snapshotAfter` from device.execute responses). Both share `matchSemanticUI` / `uiMatchEmpty` helpers.

`FSDefStore` wraps a `DefStore` interface (not a concrete `*MemoryDefStore`):
- `NewFSDefStore(dir, log)` — default MemoryDefStore backing
- `NewFSDefStoreWithBacking(dir, backing, log)` — inject custom backing for tests
- `ReloadNow(ctx)` — manual immediate reload (vs. background `Watch` polling)
- On file read/parse/validate error: retains previous valid def for that file, logs a warning

### Store interfaces (`internal/store/port.go`)

| Interface | Key methods |
|---|---|
| `TaskStore` | Save, Get, List, `ListByDevice`, **`ListActiveByDevice`** |
| `WorkflowStateStore` | Save, Get, `ListActiveByDevice` (recovery on reconnect) |
| `EventPlaneStore` | Accept (dedup/watermark), RecordDeadLetter, QueryAccepted, QueryDeadLetters |
| `TaskQueue` | Enqueue, Dequeue, Remove, Snapshot (FIFO + dedup set) |
| `CommandOutboxStore` | SaveIssued, MarkDispatched, MarkDispatchFailed, MarkDelivered |

`ListActiveByDevice` on `TaskStore` returns only non-terminal tasks. Use it in hot paths (per-event orchestration) to avoid O(n_all) fetches. The orchestrator uses it in `ProcessAcceptedEvent`.

Implementations: `memory.go` (tests/dev), `file.go` (single-process persistence), `redis.go` (distributed).

---

## Workflows (`config/examples/workflows/`)

| Workflow | Description |
|---|---|
| `android-settings-private-dns.yaml` | Opens Settings, navigates to Private DNS, inputs `{{input.private_dns_hostname}}`, saves. Requires `private_dns_hostname` input artifact. |
| `android-semantic-login.yaml` | Waits for login screen via `active_ui_key`, fills `form.primary.email` + `form.primary.password`, taps submit. |
| `captcha-solve.yaml` | Vision tool call → coordinate-tap tool call → sequence of clicks → submit. Uses `screenshot_base64` + `grid_bounds` + `captcha_target`. |

Workflow YAML schema: `trigger / action / tool_call / expect / on_success / on_failure / timeout / max_retry`.
- `trigger: {}` — auto-executes immediately when step is reached (no device event needed)
- `{{input.key}}` — interpolated from `task.inputArtifacts` at execution time

---

## Build & Test

### android-agent (from `app/android-agent/`)

```bash
./gradlew assembleDebug                                           # build debug APK
./gradlew installDebug                                            # install on device/emulator
./gradlew test                                                    # all unit tests (JVM/Robolectric)
./gradlew test --tests "com.autosdk.agent.state.AgentReducerTest" # single class
```

Dependencies: `kotlinx-coroutines-android`, `kotlinx-serialization-json`, `okhttp`, `junit4`, `robolectric`. No Hilt/Dagger — wiring is manual inside `AgentAccessibilityService.setupAgentRuntime()`.

### server-agent (from `app/server-agent/`)

```bash
go run ./cmd/server                                  # start on :3000
go run ./cmd/server -workflow-dir config/examples/workflows
go build -o bin/server-agent ./cmd/server
go test ./...
go vet ./...
```

---

## Code Conventions

**Android-agent:**
- State changes: add to `AgentEvent`, handle in `AgentReducer`, add `AgentEffect` subtype if side-effects needed, handle in `AgentStateCoordinator.runEffect`.
- New JSON-RPC methods: add a `when` branch in `AgentRuntime.handleRequest` and a private `handle*` function.
- New capabilities: add to `AgentCapabilities.buildCapabilityList()` only — do not duplicate in `AgentAccessibilityService` or `AgentRuntime`.
- Serialization helpers: `UiSnapshotSerializer` and `AgentProtocolDeserializer` are `internal object`s at the bottom of `AgentRuntime.kt`; test them directly without constructing a full runtime.
- Tests are JVM-only (Robolectric); no instrumented tests. Test the reducer and coordinator in isolation by faking `AgentTransportDriver`, `AgentHeartbeatScheduler`, etc.

**Server-agent:**
- New workflow step type: add YAML under `config/examples/workflows/`. No Go code needed for data-driven workflows.
- New node behaviour: implement the `Node` interface; wire in `Engine.nodeFor()`.
- New tool: add manifests/bindings under `config/tools/`; no Go code needed.
- New store backend: implement the relevant interface in `store/port.go`; swap in `main.go`.

## Runtime Data

`app/server-agent/var/` — runtime-generated data (tasks, workflow state, event plane, command outbox). Gitignored; do not commit.
