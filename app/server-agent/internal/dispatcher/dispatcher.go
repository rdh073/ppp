package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
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
	metrics  *telemetry.Registry
}

func NewMemoryDispatcher(
	reg registry.AgentRegistry,
	outbox store.CommandOutboxStore,
	metrics ...*telemetry.Registry,
) *MemoryDispatcher {
	if outbox == nil {
		outbox = store.NewMemoryCommandOutboxStore()
	}
	var registryMetrics *telemetry.Registry
	if len(metrics) > 0 {
		registryMetrics = metrics[0]
	}
	if registryMetrics == nil {
		registryMetrics = telemetry.NewRegistry()
	}
	return &MemoryDispatcher{
		reg:      reg,
		inflight: newInflightTracker(),
		outbox:   outbox,
		metrics:  registryMetrics,
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
		d.metrics.ObserveCommand(string(cmd.Kind), telemetry.CommandOutcomeDispatchFailed, time.Since(cmd.IssuedAt))
		return nil, err
	}

	ch := d.inflight.register(cmd)
	d.metrics.IncCommandInflight()

	if err := conn.SendRequest(cmd.ID, string(cmd.Kind), cmd.Params); err != nil {
		if _, ok := d.inflight.cancel(cmd.ID); ok {
			d.metrics.DecCommandInflight()
		}
		_ = d.outbox.MarkDispatchFailed(ctx, cmd.ID, err.Error(), time.Now())
		d.metrics.ObserveCommand(string(cmd.Kind), telemetry.CommandOutcomeDispatchFailed, time.Since(cmd.IssuedAt))
		return nil, fmt.Errorf("send request to device %s: %w", cmd.DeviceID, err)
	}
	if err := d.outbox.MarkDispatched(ctx, cmd.ID, time.Now()); err != nil {
		if _, ok := d.inflight.cancel(cmd.ID); ok {
			d.metrics.DecCommandInflight()
		}
		d.metrics.ObserveCommand(string(cmd.Kind), telemetry.CommandOutcomeDispatchFailed, time.Since(cmd.IssuedAt))
		return nil, fmt.Errorf("persist dispatched command %s: %w", cmd.ID, err)
	}
	if ctx.Done() != nil {
		go d.watchCommandContext(ctx, cmd)
	}

	return ch, nil
}

// DeliverResponse correlates an inbound response with its pending Dispatch call.
// Called by the WebSocket read loop; safe to call from any goroutine.
func (d *MemoryDispatcher) DeliverResponse(result domain.CommandResult) {
	if result.ReceivedAt.IsZero() {
		result.ReceivedAt = time.Now()
	}
	cmd, ok := d.inflight.deliver(result)
	if !ok {
		return
	}
	_ = d.outbox.MarkDelivered(context.Background(), result)
	d.metrics.DecCommandInflight()
	outcome := telemetry.CommandOutcomeRespondedSuccess
	if !result.Success {
		outcome = telemetry.CommandOutcomeRespondedError
	}
	d.metrics.ObserveCommand(string(cmd.Kind), outcome, result.ReceivedAt.Sub(cmd.IssuedAt))
}

func (d *MemoryDispatcher) watchCommandContext(ctx context.Context, cmd domain.Command) {
	<-ctx.Done()
	storedCmd, ok := d.inflight.cancel(cmd.ID)
	if !ok {
		return
	}

	d.metrics.DecCommandInflight()

	outcome := telemetry.CommandOutcomeCanceled
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		outcome = telemetry.CommandOutcomeTimedOut
		d.metrics.RecordCommandTimeout(string(storedCmd.Kind))
	}
	d.metrics.ObserveCommand(string(storedCmd.Kind), outcome, time.Since(storedCmd.IssuedAt))
	_ = d.outbox.MarkDispatchFailed(context.Background(), storedCmd.ID, ctx.Err().Error(), time.Now())
}
