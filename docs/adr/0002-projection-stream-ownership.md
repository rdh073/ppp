# ADR 0002: Projection Stream Ownership and Replay

- Status: Accepted
- Date: 2026-03-22

## Context

The dashboard needs live updates for account-manager and campaign read models.
Polling created unnecessary coupling to workflow timing and increased load.

The repository already has an event plane for accepted runtime events, but those events represent
durable domain/runtime facts. Dashboard read-model updates are a different concern:

- they are shaped for UI consumption
- they may be more frequent
- they need reconnect/backfill semantics for SSE clients

Using the same store for both would couple domain-event inspection with UI projection transport.

## Decision

Projection streaming is owned by a dedicated projection layer:

- projection event type: `internal/projection`
- file/memory replay store: `internal/store/projection_events.go`
- SSE endpoint: `GET /events/stream`
- reconnect contract:
  - server emits SSE `id:`
  - clients resume with `Last-Event-ID`
  - server emits `reset` when requested backfill is no longer available

The domain event plane remains separate and continues to own accepted-event inspection/replay.

## Consequences

- Dashboard code consumes read-model events, not raw workflow internals.
- Projection retention can evolve independently from accepted-event retention.
- Server-side projection replay remains a UI/read-model concern, not a business-event concern.
- If projection history is pruned, clients must refetch the current list and reopen the stream.
