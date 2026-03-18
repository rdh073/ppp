---
name: ppp-system-mandates
description: Core architectural rules and repository conventions for the PPP multi-agent system.
---

# PPP System Mandates (GEMINI.md)

This document defines the non-negotiable architectural standards and repository conventions for the PPP project. These rules take precedence over general defaults.

## 1. Architectural Philosophy: "Argo-Clean"

We combine **Clean Architecture** with an **Argo-like event-driven runtime**.

### Dependency Direction (Strict)
All dependencies must point **inward** toward the Domain layer.
`Transport (REST/WS) -> Handler -> Usecase -> Orchestrator/Domain -> {Store, Dispatcher, EventPlane}`

*   **Domain**: Data structures and interfaces. No logic dependencies.
*   **Usecase**: Business logic orchestration.
*   **Store**: Implementation detail (Postgres/Redis/File).
*   **Handler**: Mapping transport requests to usecases.

### Event-Driven Loop
The system operates as a reconciliation loop:
1.  **Observe**: Ingest events from Redis Streams or WebSockets.
2.  **Identify**: Match events to active `Inflight` tasks or `Snapshots`.
3.  **Plan**: Evaluate the next step in the Workflow DAG.
4.  **Act**: Dispatch commands via the `Dispatcher`.

## 2. Repository Structure (Monorepo)

*   `/app/account-service`: Go service managing account lifecycle (Google, Instagram). Uses Postgres.
*   `/app/server-agent`: Go orchestrator. Manages Workflow DAGs, Redis Streams, and Tool Providers.
*   `/app/android-agent`: Kotlin application. The execution edge for UI automation.
*   `/plans`: Design documents and phase roadmaps.
*   `/scripts`: Utility scripts (APK UI dumping, setup).

## 3. Go (Backend) Conventions

*   **Explicit Error Handling**: Always return `error` as the last result. Wrap errors with context: `fmt.Errorf("failed to process step: %w", err)`.
*   **Interfaces**: Define interfaces in the layer that *uses* them (Domain/Usecase), implement them in the outer layers (Store/Handler).
*   **Naming**: Use `camelCase` for internal variables and `PascalCase` for exported symbols. Single-letter receivers for methods (e.g., `(s *Store)`).
*   **Dependency Injection**: Use constructor functions (`NewSomething(...)`) and pass interfaces.

## 4. Kotlin (Android) Conventions

*   **Coroutines**: Use Coroutines for all asynchronous operations (WebSocket, UI inspection).
*   **JSON-RPC**: The communication protocol between the Server and Agent must strictly adhere to the defined JSON-RPC 2.0 schema.
*   **UI Inspection**: Observations should be factual and based on the Accessibility tree. Do not embed business logic in the Agent.

## 5. Operational Hardening (Phase 7 Focus)

*   **Idempotency**: All `Action` steps in workflows must be idempotent.
*   **Expect/Timeout**: Every `Action` that triggers a UI change **must** be followed by an `Expect` step with a reasonable `timeout`.
*   **Snapshots**: State must be restorable from a `Snapshot` via the `Store` interface.
*   **Store Parity**: `server-agent` must eventually support `Postgres` for long-term task history (Phase 7 gap).

## 6. Prohibitions

*   ❌ **No Logic in Handlers**: Handlers only parse input and call usecases.
*   ❌ **No Direct Store-to-Store Calls**: Services must communicate via APIs or Events, not by reaching into another service's database.
*   ❌ **No Workflow Bypassing**: Do not hardcode sequences; use the `WorkflowDefinition` DAG.
