package dispatcher

import (
	"context"
	"fmt"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
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
}

func NewMemoryDispatcher(reg registry.AgentRegistry) *MemoryDispatcher {
	return &MemoryDispatcher{
		reg:      reg,
		inflight: newInflightTracker(),
	}
}

// Dispatch sends cmd to the agent identified by cmd.DeviceID.
// Returns a read-only channel that will receive the CommandResult exactly once.
// Returns an error immediately if no active connection exists for the device.
func (d *MemoryDispatcher) Dispatch(ctx context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	_, conn, ok := d.reg.GetByDevice(cmd.DeviceID)
	if !ok {
		return nil, fmt.Errorf("no active connection for device %s", cmd.DeviceID)
	}

	ch := d.inflight.register(cmd.ID)

	if err := conn.SendRequest(cmd.ID, string(cmd.Kind), cmd.Params); err != nil {
		d.inflight.cancel(cmd.ID)
		return nil, fmt.Errorf("send request to device %s: %w", cmd.DeviceID, err)
	}

	return ch, nil
}

// DeliverResponse correlates an inbound response with its pending Dispatch call.
// Called by the WebSocket read loop; safe to call from any goroutine.
func (d *MemoryDispatcher) DeliverResponse(result domain.CommandResult) {
	d.inflight.deliver(result)
}
