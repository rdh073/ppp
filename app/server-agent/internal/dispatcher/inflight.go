package dispatcher

import (
	"sync"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// inflightTracker maps command ID to the response channel registered by Dispatch.
// It is safe for concurrent use.
type inflightTracker struct {
	mu      sync.Mutex
	pending map[string]chan domain.CommandResult
}

func newInflightTracker() *inflightTracker {
	return &inflightTracker{pending: make(map[string]chan domain.CommandResult)}
}

// register creates a buffered channel for the given command ID and returns it.
// The channel is buffered so DeliverResponse never blocks even if the caller
// has already timed out and discarded the channel.
func (t *inflightTracker) register(id string) <-chan domain.CommandResult {
	ch := make(chan domain.CommandResult, 1)
	t.mu.Lock()
	t.pending[id] = ch
	t.mu.Unlock()
	return ch
}

// deliver sends the result to the registered channel and removes the entry.
// It is a no-op if the channel was already closed or never registered
// (handles duplicate at-least-once delivery safely).
func (t *inflightTracker) deliver(result domain.CommandResult) {
	t.mu.Lock()
	ch, ok := t.pending[result.CommandID]
	if ok {
		delete(t.pending, result.CommandID)
	}
	t.mu.Unlock()

	if ok {
		ch <- result
		close(ch)
	}
}

// cancel closes and removes the channel for the given command ID without sending a result.
// Used for timeout / task cancellation paths.
func (t *inflightTracker) cancel(id string) {
	t.mu.Lock()
	ch, ok := t.pending[id]
	if ok {
		delete(t.pending, id)
	}
	t.mu.Unlock()

	if ok {
		close(ch)
	}
}
