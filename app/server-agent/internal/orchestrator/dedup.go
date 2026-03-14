package orchestrator

import (
	"sync"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

const dedupRingSize = 512

// dedupTracker absorbs at-least-once redelivery of events using a fixed-size
// ring buffer per device. It does not require external storage.
type dedupTracker struct {
	mu   sync.Mutex
	seen map[domain.DeviceID]*ringSet
}

func newDedupTracker() *dedupTracker {
	return &dedupTracker{seen: make(map[domain.DeviceID]*ringSet)}
}

func (d *dedupTracker) IsDuplicate(deviceID domain.DeviceID, eventID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	rs := d.ringFor(deviceID)
	return rs.contains(eventID)
}

func (d *dedupTracker) Mark(deviceID domain.DeviceID, eventID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ringFor(deviceID).add(eventID)
}

func (d *dedupTracker) ringFor(deviceID domain.DeviceID) *ringSet {
	if rs, ok := d.seen[deviceID]; ok {
		return rs
	}
	rs := newRingSet(dedupRingSize)
	d.seen[deviceID] = rs
	return rs
}

// ringSet is a fixed-capacity set implemented as a circular overwrite buffer.
type ringSet struct {
	buf   []string
	index map[string]struct{}
	pos   int
	cap   int
}

func newRingSet(cap int) *ringSet {
	return &ringSet{
		buf:   make([]string, cap),
		index: make(map[string]struct{}, cap),
		cap:   cap,
	}
}

func (r *ringSet) contains(id string) bool {
	_, ok := r.index[id]
	return ok
}

func (r *ringSet) add(id string) {
	if r.contains(id) {
		return
	}
	// Evict the oldest entry at the current position.
	if old := r.buf[r.pos]; old != "" {
		delete(r.index, old)
	}
	r.buf[r.pos] = id
	r.index[id] = struct{}{}
	r.pos = (r.pos + 1) % r.cap
}
