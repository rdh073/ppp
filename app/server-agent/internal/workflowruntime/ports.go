package workflowruntime

import (
	"context"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// EventProcessor is the minimal runtime port needed by workflow orchestration
// components in this bounded context.
type EventProcessor interface {
	ProcessEvent(ctx context.Context, e domain.Event) error
}
