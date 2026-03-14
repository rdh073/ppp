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
