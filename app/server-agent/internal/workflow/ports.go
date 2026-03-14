package workflow

import (
	"context"
	"encoding/json"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// ToolInvoker is the port the engine uses to call registered tools.
// It is satisfied by tools.ToolRegistry (outer layer); the engine never
// imports the tools package directly.
type ToolInvoker interface {
	Invoke(ctx context.Context, toolName string, params json.RawMessage) (json.RawMessage, error)
}

// ActionDispatcher is the port the engine uses to dispatch device commands.
// It is satisfied by dispatcher.Dispatcher (outer layer); the engine never
// imports the dispatcher package directly.
//
// dispatcher.Dispatcher has a superset interface (also has DeliverResponse);
// Go structural subtyping means it satisfies ActionDispatcher with no adapter.
type ActionDispatcher interface {
	Dispatch(ctx context.Context, cmd domain.Command) (<-chan domain.CommandResult, error)
}
