package orchestrator

import (
	"sync"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// watermarkTracker records the highest sequence number processed per agent.
// Events with SeqNo <= current watermark are stale and must be dropped.
type watermarkTracker struct {
	mu   sync.RWMutex
	high map[domain.DeviceID]uint64
}

func newWatermarkTracker() *watermarkTracker {
	return &watermarkTracker{high: make(map[domain.DeviceID]uint64)}
}

// Accept returns true if the event should be processed.
// SeqNo == 0 means an internal event (always accepted).
func (w *watermarkTracker) Accept(deviceID domain.DeviceID, seqNo uint64) bool {
	if seqNo == 0 {
		return true
	}
	w.mu.RLock()
	cur := w.high[deviceID]
	w.mu.RUnlock()
	return seqNo > cur
}

// Advance updates the watermark to seqNo if seqNo is higher.
func (w *watermarkTracker) Advance(deviceID domain.DeviceID, seqNo uint64) {
	if seqNo == 0 {
		return
	}
	w.mu.Lock()
	if seqNo > w.high[deviceID] {
		w.high[deviceID] = seqNo
	}
	w.mu.Unlock()
}
