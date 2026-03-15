# CLAUDE.md — android-agent

Kotlin Android accessibility-service agent for the ppp system. Runs on-device; connects to the server-agent via WebSocket/JSON-RPC.

## Commands

```bash
# All commands run from app/android-agent/

# Build debug APK
./gradlew assembleDebug

# Install on connected device/emulator
./gradlew installDebug

# Run all unit tests (JVM / Robolectric — no device required)
./gradlew test

# Run a single test class
./gradlew test --tests "com.autosdk.agent.state.AgentReducerTest"

# Run a single test method
./gradlew test --tests "com.autosdk.agent.state.AgentReducerTest.someMethodName"

# Run tests for a package
./gradlew test --tests "com.autosdk.agent.observation.*"
```

Dependencies: `kotlinx-coroutines-android`, `kotlinx-serialization-json`, `okhttp`, `junit4`, `robolectric`. No Hilt/Dagger — wiring is manual inside `AgentAccessibilityService.setupAgentRuntime()`.

## Package Layout

```
com.autosdk.agent/
  boot/         BootCompletedReceiver — restarts agent on device boot
  service/      AgentAccessibilityService — single Android lifecycle anchor; wires all components
  transport/    WebSocketAgentTransport / AgentTransport interface; JSON-RPC 2.0 over WebSocket
  observation/  UiSnapshot, UiTarget, SnapshotBuilder, UiSemantics — accessibility tree → semantic snapshot
  action/       AutomationAction (sealed), Selector, ActionExecutor, ActionResult
  agent/        AgentRuntime (JSON-RPC dispatch), AgentCapabilities (capability list + JSON), AgentAutomationDriver
  state/        Unidirectional state machine — AgentState, AgentEvent, AgentReducer, AgentEffect, AgentStateCoordinator
```

## State Machine

`AgentReducer.reduce(state, event) → AgentReduction(newState, effects)` is a pure function. `AgentStateCoordinator` runs the effects after each reduction.

**Adding a new behaviour:**
1. Add an `AgentEvent` subclass.
2. Handle it in `AgentReducer.reduce` — return new state + effects.
3. If a new side-effect is needed, add an `AgentEffect` subtype and handle it in `AgentStateCoordinator.runEffect`.

**`runEffect` categories:**
- **Tracked** (SendHello, SendResume, SendHeartbeat): use `sendAgentRequest`; outcome flows back as `onRpcSuccess` / `onRpcFailure` → `AgentEvent`.
- **Fire-and-forget** (ConnectSocket, SendDisconnect, PublishUiEvent): `runCatching { ... }.onFailure { log }` — failures are logged but do not drive state transitions.

## Observation / Semantic Projection

`SnapshotBuilder.build(roots, deviceId, packageName, activityName)`:
1. Traverses all accessibility window roots.
2. Filters nodes via `shouldIncludeNode(visible, hasArea, hasId, hasText)` — drops invisible, zero-area, ID-less, text-less containers.
3. Calls `buildSemanticProjection()` from `UiSemantics.kt`.

`buildSemanticProjection()` produces:
- `baseScreenKey` — `package.activity` (e.g. `example.login`)
- `overlayKey` — set when `screenState == "dialog"`
- `activeUiKey` — overlayKey if present, else baseScreenKey
- Per-target `semanticKey` (e.g. `form.primary.email`, `button.sign_in`, `text.network_internet`)
- `UiFormState` list — form readiness, field keys, focused field
- `UiButtonState` list — primary, enabled, visible per button
- `semanticDigest` — SHA-1 of all state fields; use for change detection

## Capabilities

`AgentCapabilities` (`agent/AgentCapabilities.kt`) is the **single source of truth**:
- `buildCapabilityList()` — returns the full `List<Map<String, Any>>`
- `capabilitiesToJson(capabilities)` — serializes to wire-format JSON array

Both `AgentStateCoordinator` (SendHello/SendResume params) and `AgentRuntime` (`device.capabilities.get`) delegate here. Do **not** duplicate capability definitions elsewhere.

## JSON-RPC Protocol

**Server → Agent:**
| Method | Handler | Description |
|---|---|---|
| `device.observe` | `handleObserve` | Build and return current `UiSnapshot` |
| `device.query` | `handleQuery` | Resolve selector, return matching `UiTarget`s |
| `device.execute` | `handleExecute` | observe → act → settle → observe; return `{snapshotBefore, snapshotAfter}` |
| `device.capabilities.get` | `handleCapabilitiesGet` | Return capability list via `AgentCapabilities` |

**Agent → Server:**
| Method | When |
|---|---|
| `agent.hello` | First connection |
| `agent.resume` | Reconnect with existing `sessionId` |
| `agent.heartbeat` | Every 30 s |
| `agent.disconnect` | Graceful disconnect |

**Adding a new server→agent method:** add a `when` branch in `AgentRuntime.handleRequest` and a private `handle*` suspend function.

## Serialization

`UiSnapshotSerializer` and `AgentProtocolDeserializer` are `internal object`s at the bottom of `AgentRuntime.kt`:
- `UiSnapshotSerializer.toJson(snapshot)` / `targetToJson(target)` — snapshot → wire JSON
- `AgentProtocolDeserializer.parseSelector(obj)` / `parseAction(obj)` — wire JSON → domain types

Test these directly without constructing a full `AgentRuntime`.

## Node Recycling

`ActionExecutor` node ownership rules:
- Nodes returned from `withNode { }` are recycled by `withNode`'s `try/finally`. The lambda must **not** recycle the node it receives.
- Nodes returned from `findSelfOrAncestor` are owned by the caller. Click/LongPress handlers recycle them in their own `finally` when an ancestor was climbed to (`clickNode !== node`).

## Server URL

`BuildConfig.SERVER_URL` (default `ws://10.0.2.2:3000/ws/agent` for emulator).

Override at runtime without reinstalling:
```bash
adb shell setprop auto.agent.server_url ws://192.168.x.x:3000/ws/agent
```

## Key Invariants

- The agent never owns task/workflow lifecycle — that belongs to the server-agent.
- Raw accessibility events are triggers only; truth is always a freshly built `UiSnapshot`.
- All state transitions go through `AgentReducer.reduce`; illegal transitions emit `AgentEffect.Log` and return unchanged state.
- Session continuity via `AgentStateStore` / `SharedPreferencesAgentStateStore` — survives process death and reconnects.
