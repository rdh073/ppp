# ADR 0001: Intent-Bounded Context Map

- Status: Accepted
- Date: 2026-03-22

## Context

The repository was mixing multiple operator intents under ambiguous terms such as `campaign`.
That made the dashboard navigation unclear, pushed unrelated API shapes into the same namespace,
and increased coupling between server orchestration and UI read paths.

The system has three runtime surfaces with different responsibilities:

- `dashboard`: operator console
- `server-agent`: business orchestration and control plane
- `android-agent`: device execution edge

Without an explicit context map, naming drift and dependency drift are both likely.

## Decision

The system uses intent-bounded contexts:

- `Account Manager`
  - owns `Login`, `Create`, `Accounts`, `Personas`
  - server namespace: `/account-manager/*`
- `Campaigns`
  - owns post/publication campaigns only
  - server namespace: `/campaigns/posts`
- `Device Control`
  - owns device observe/query/execute/script capabilities
  - business vocabulary does not belong here
- `Workflow Runtime`
  - owns long-running orchestration, retries, and terminal progression
- `Projection/Eventing`
  - owns read-model publication and dashboard-facing stream contracts

Responsibility split:

- `dashboard` sends intent commands and renders read models
- `server-agent` owns business state progression
- `android-agent` stays device-scoped and must not carry account/campaign business semantics

## Consequences

- Account creation and post campaign must not share public naming just because they are both long-running.
- Dashboard feature modules are expected to stay isolated by context.
- Cross-feature imports in the dashboard are allowed only through `features/*/api` public adapters.
- Android-source vocabulary is guarded against business-domain leakage.
- Future endpoints, DTOs, and panels should be named by operator intent first.
