package dispatcher

import (
	"context"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

// Dispatcher sends device.* commands to connected android-agents and correlates responses.
// Dispatch returns a channel that receives exactly one CommandResult then is closed.
// DeliverResponse is called by the transport layer when a device.* response arrives.
type Dispatcher interface {
	Dispatch(ctx context.Context, cmd domain.Command) (<-chan domain.CommandResult, error)
	DeliverResponse(result domain.CommandResult)
}

// MemoryDispatcher is the in-process Dispatcher implementation.
// It looks up the agent connection from the registry and tracks in-flight RPCs.
type MemoryDispatcher struct {
	reg      registry.AgentRegistry
	inflight *inflightTracker
	outbox   store.CommandOutboxStore
}

func NewMemoryDispatcher(reg registry.AgentRegistry, outboxes ...store.CommandOutboxStore) *MemoryDispatcher {
	outbox := store.CommandOutboxStore(store.NewMemoryCommandOutboxStore())
	if len(outboxes) > 0 && outboxes[0] != nil {
		outbox = outboxes[0]
	}
	return &MemoryDispatcher{
		reg:      reg,
		inflight: newInflightTracker(),
		outbox:   outbox,
	}
}

// Dispatch sends cmd to the agent identified by cmd.DeviceID.
// Returns a read-only channel that will receive the CommandResult exactly once.
// Returns an error immediately if no active connection exists for the device.
func (d *MemoryDispatcher) Dispatch(ctx context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	if cmd.IssuedAt.IsZero() {
		cmd.IssuedAt = time.Now()
	}
	if err := d.outbox.SaveIssued(ctx, cmd); err != nil {
		return nil, fmt.Errorf("persist command issue: %w", err)
	}

	_, conn, ok := d.reg.GetByDevice(cmd.DeviceID)
	if !ok {
		err := fmt.Errorf("no active connection for device %s", cmd.DeviceID)
		_ = d.outbox.MarkDispatchFailed(ctx, cmd.ID, err.Error(), time.Now())
		return nil, err
	}

	ch := d.inflight.register(cmd.ID)

	if err := conn.SendRequest(cmd.ID, string(cmd.Kind), cmd.Params); err != nil {
		d.inflight.cancel(cmd.ID)
		_ = d.outbox.MarkDispatchFailed(ctx, cmd.ID, err.Error(), time.Now())
		return nil, fmt.Errorf("send request to device %s: %w", cmd.DeviceID, err)
	}
	if err := d.outbox.MarkDispatched(ctx, cmd.ID, time.Now()); err != nil {
		d.inflight.cancel(cmd.ID)
		return nil, fmt.Errorf("persist dispatched command %s: %w", cmd.ID, err)
	}

	return ch, nil
}

// DeliverResponse correlates an inbound response with its pending Dispatch call.
// Called by the WebSocket read loop; safe to call from any goroutine.
func (d *MemoryDispatcher) DeliverResponse(result domain.CommandResult) {
	if result.ReceivedAt.IsZero() {
		result.ReceivedAt = time.Now()
	}
	_ = d.outbox.MarkDelivered(context.Background(), result)
	d.inflight.deliver(result)
}
