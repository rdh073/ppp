# android-agent

Android agent skeleton aligned to the ADR.

## Intent

This app is the execution edge, not the control plane.

It owns:

- boot startup
- accessibility-backed observation
- action dispatch
- transport connection to the control server

It does not own:

- workflow orchestration
- adapter-specific behavior for CLI, MCP, or AI SDK
- server truth about task lifecycle

## Package Layout

- `boot`
  - boot startup entrypoints
- `service`
  - accessibility service boundary
- `transport`
  - WebSocket / JSON-RPC transport boundary
- `observation`
  - local normalized UI models
- `action`
  - local action models

## Event-Driven Reconciliation Loop

`android-agent` proactively publishes semantic UI changes to the server using JSON-RPC 2.0 notifications over WebSocket.

```mermaid
sequenceDiagram
    autonumber
    participant A11y as AccessibilityEvent
    participant Svc as AgentAccessibilityService
    participant Obs as SnapshotBuilder
    participant Coord as AgentStateCoordinator
    participant Reducer as AgentReducer
    participant Fx as AgentEffect.PublishUiEvent
    participant Ws as WebSocketAgentTransport
    participant Server as server-agent

    A11y->>Svc: onAccessibilityEvent(...)
    Svc->>Svc: shouldScheduleSemanticPublish(eventType)
    Svc->>Svc: awaitSettle()
    Svc->>Obs: buildSnapshot(deviceId)
    Obs-->>Svc: UiSnapshot (semanticDigest, targets, metadata)
    Svc->>Svc: seqNo = outboundEventSeqNo.incrementAndGet()
    Svc->>Coord: dispatch(WindowStateChanged(params))
    Coord->>Reducer: reduce(state, event)
    Reducer->>Reducer: guard transport==CONNECTED
    Reducer->>Reducer: guard execution==IDLE
    Reducer->>Reducer: drop if duplicate semanticDigest
    Reducer-->>Coord: AgentEffect.PublishUiEvent("android.screen.changed", params)
    Coord->>Fx: runEffect(PublishUiEvent)
    Fx->>Ws: sendNotification(method, params)
    Ws->>Server: JSON-RPC 2.0 notification (fire-and-forget)
```

Filtering rules in reducer:
- drop when transport is not `CONNECTED` or execution is not `IDLE`
- drop when `ui.semanticDigest` equals `lastPublishedUiDigest`
- otherwise publish `android.screen.changed` and update `lastPublishedUiDigest`
