## GUARDRAIL

- Load skill `coder` when beginning to write code.
- use some usefull serena tool:
`search_for_pattern`
`find_symbol`
`find_referencing_symbols`
`get_symbols_overview`
`insert_after_symbol`
`insert_before_symbol`
`rename_symbol`
`replace_symbol_body`

## Architecture

Read `AGENTS.md` before writing code. Two sub-projects:

**`app/android-agent/`** — Kotlin Android app (execution edge). Runs on-device, connects to the server-agent via WebSocket/JSON-RPC.

**`app/server-agent/`** — Go control plane. Accepts WebSocket connections from android-agents, manages session registry, will orchestrate workflow DAG. See `app/server-agent/CLAUDE.md` for its own build/layout docs.

The android-agent owns:
- `boot` — `BootCompletedReceiver` triggers on device boot
- `service` — `AgentAccessibilityService` is the single Android lifecycle anchor; wires all components
- `transport` — `WebSocketAgentTransport` / `AgentTransport` interface; JSON-RPC over WebSocket
- `observation` — `UiSnapshot`, `UiTarget`, `SnapshotBuilder`; normalized accessibility tree snapshots
- `action` — `AutomationAction` sealed class, `Selector`, `ActionExecutor`, `ActionResult`
- `agent` — `AgentRuntime` dispatches inbound JSON-RPC requests from the server; `AgentAutomationDriver` executes actions
- `state` — Unidirectional state machine: `AgentState` + `AgentEvent` → `AgentReducer` → `AgentReduction(state, effects)` → `AgentStateCoordinator` runs effects

**State machine pattern:** `AgentReducer` is a pure function (`state + event → new state + effects`). `AgentStateCoordinator` is the effectful runner. `AgentEffect` is a sealed interface — adding new side-effects means adding a new `AgentEffect` subtype and handling it in the coordinator.

**Key invariants:**
- The agent never owns task/workflow lifecycle — that belongs to the server-agent (Go, not in this repo).
- Raw accessibility events are triggers only; truth is always a freshly built `UiSnapshot`.
- All state transitions must go through `AgentReducer.reduce`; illegal transitions emit `AgentEffect.Log` and return unchanged state.
- Session persistence via `AgentStateStore`/`SharedPreferencesAgentStateStore` for reconnect continuity.

**JSON-RPC methods (server → agent):**
- `device.observe` — returns current `UiSnapshot`
- `device.query` — resolves a selector, returns matching `UiTarget`s
- `device.execute` — observe → act → settle → observe; returns `{snapshotBefore, snapshotAfter}`
- `device.capabilities.get` — returns capability list

**Agent registration (agent → server):**
- `agent.hello` — first connection, server responds with `sessionId`
- `agent.resume` — reconnect with existing `sessionId`; server may reject (falls back to hello)
- `agent.heartbeat` — periodic keepalive (every 30 s)
- `agent.disconnect` — graceful disconnect

**Server URL config:** `BuildConfig.SERVER_URL` (default `ws://10.0.2.2:3000/ws/agent` for emulator). Override at runtime: `adb shell setprop auto.agent.server_url <url>`.

## Build & Test

### android-agent (from `app/android-agent/`)

```bash
# Build debug APK
./gradlew assembleDebug

# Run unit tests (JVM, Robolectric)
./gradlew test

# Run a single test class
./gradlew test --tests "com.autosdk.agent.state.AgentReducerTest"

# Run a single test method
./gradlew test --tests "com.autosdk.agent.state.AgentReducerTest.someMethodName"

# Install on connected device/emulator
./gradlew installDebug
```

Dependencies: `kotlinx-coroutines-android`, `kotlinx-serialization-json`, `okhttp`, `junit4`, `robolectric`. No Hilt/Dagger — wiring is manual inside `AgentAccessibilityService.setupAgentRuntime()`.

### server-agent (from `app/server-agent/`)

```bash
go run ./cmd/server        # start on :3000
go build -o bin/server-agent ./cmd/server
go test ./...
go vet ./...
```

## Code Conventions
- State changes: add to `AgentEvent`, handle in `AgentReducer`, add `AgentEffect` types if side-effects are needed, handle in `AgentStateCoordinator.runEffect`.
- New JSON-RPC methods: add a `when` branch in `AgentRuntime.handleRequest` and a private `handle*` function.
- Tests are JVM-only (Robolectric); no instrumented tests. Test the reducer and coordinator in isolation by faking `AgentTransportDriver`, `AgentHeartbeatScheduler`, etc.
