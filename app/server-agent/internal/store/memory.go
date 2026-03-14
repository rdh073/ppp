package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

const eventDedupHistorySize = 512

// MemoryTaskStore is a thread-safe in-memory TaskStore.
type MemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[domain.TaskID]*domain.Task
}

func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{tasks: make(map[domain.TaskID]*domain.Task)}
}

func (s *MemoryTaskStore) Save(_ context.Context, t *domain.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = cloneTask(t)
	return nil
}

func (s *MemoryTaskStore) Get(_ context.Context, id domain.TaskID) (*domain.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("%w: task %s", ErrNotFound, id)
	}
	return cloneTask(t), nil
}

func (s *MemoryTaskStore) List(_ context.Context) ([]*domain.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		out = append(out, cloneTask(task))
	}
	return out, nil
}

func (s *MemoryTaskStore) ListByDevice(_ context.Context, deviceID domain.DeviceID) ([]*domain.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Task
	for _, t := range s.tasks {
		if t.AssignedDevice == deviceID {
			out = append(out, cloneTask(t))
		}
	}
	return out, nil
}

// MemoryWorkflowStateStore is a thread-safe in-memory WorkflowStateStore.
type MemoryWorkflowStateStore struct {
	mu     sync.RWMutex
	states map[stateKey]*domain.WorkflowState
}

type stateKey struct {
	taskID   domain.TaskID
	deviceID domain.DeviceID
}

func NewMemoryWorkflowStateStore() *MemoryWorkflowStateStore {
	return &MemoryWorkflowStateStore{states: make(map[stateKey]*domain.WorkflowState)}
}

func (s *MemoryWorkflowStateStore) Save(_ context.Context, ws *domain.WorkflowState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := stateKey{ws.TaskID, ws.DeviceID}
	current, exists := s.states[key]
	switch {
	case !exists && ws.Revision != 0:
		return fmt.Errorf("%w: task=%s device=%s expected revision 0 got %d",
			ErrCheckpointConflict, ws.TaskID, ws.DeviceID, ws.Revision)
	case exists && ws.Revision != current.Revision:
		return fmt.Errorf("%w: task=%s device=%s expected revision %d got %d",
			ErrCheckpointConflict, ws.TaskID, ws.DeviceID, current.Revision, ws.Revision)
	}

	next := cloneWorkflowState(ws)
	next.Revision++
	s.states[key] = next
	ws.Revision = next.Revision
	return nil
}

func (s *MemoryWorkflowStateStore) Get(_ context.Context, taskID domain.TaskID, deviceID domain.DeviceID) (*domain.WorkflowState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ws, ok := s.states[stateKey{taskID, deviceID}]
	if !ok {
		return nil, fmt.Errorf("%w: workflow state task=%s device=%s", ErrNotFound, taskID, deviceID)
	}
	return cloneWorkflowState(ws), nil
}

func (s *MemoryWorkflowStateStore) ListActiveByDevice(_ context.Context, deviceID domain.DeviceID) ([]*domain.WorkflowState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.WorkflowState
	for k, ws := range s.states {
		if k.deviceID == deviceID && !ws.IsTerminal() {
			out = append(out, cloneWorkflowState(ws))
		}
	}
	return out, nil
}

type deviceEventCursor struct {
	HighSeqNo uint64   `json:"highSeqNo"`
	SeenIDs   []string `json:"seenIds"`
}

type eventPlaneSnapshot struct {
	Cursors     map[domain.DeviceID]deviceEventCursor `json:"cursors"`
	Accepted    []domain.AcceptedEventRecord          `json:"accepted"`
	DeadLetters []domain.DeadLetterRecord             `json:"deadLetters"`
}

// MemoryEventPlaneStore keeps accepted events and dead letters in memory while
// applying the same durable API used by the file-backed implementation.
type MemoryEventPlaneStore struct {
	mu       sync.Mutex
	snapshot eventPlaneSnapshot
}

func NewMemoryEventPlaneStore() *MemoryEventPlaneStore {
	return &MemoryEventPlaneStore{
		snapshot: eventPlaneSnapshot{
			Cursors: make(map[domain.DeviceID]deviceEventCursor),
		},
	}
}

func (s *MemoryEventPlaneStore) Accept(_ context.Context, event domain.Event) (domain.EventAcceptance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cursor := s.snapshot.Cursors[event.DeviceID]
	if event.SeqNo > 0 && event.SeqNo <= cursor.HighSeqNo {
		return domain.EventAcceptanceStale, nil
	}
	if event.ID != "" && containsString(cursor.SeenIDs, event.ID) {
		return domain.EventAcceptanceDuplicate, nil
	}

	if event.SeqNo > cursor.HighSeqNo {
		cursor.HighSeqNo = event.SeqNo
	}
	if event.ID != "" {
		cursor.SeenIDs = appendDedupID(cursor.SeenIDs, event.ID, eventDedupHistorySize)
	}
	s.snapshot.Cursors[event.DeviceID] = cursor
	s.snapshot.Accepted = append(s.snapshot.Accepted, cloneAcceptedEventRecord(domain.AcceptedEventRecord{
		Event:      event,
		AcceptedAt: time.Now(),
		Source:     eventSource(event),
	}))

	return domain.EventAcceptanceAccepted, nil
}

func (s *MemoryEventPlaneStore) RecordDeadLetter(_ context.Context, record domain.DeadLetterRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now()
	}
	s.snapshot.DeadLetters = append(s.snapshot.DeadLetters, cloneDeadLetterRecord(record))
	return nil
}

func (s *MemoryEventPlaneStore) QueryAccepted(_ context.Context, query AcceptedEventListQuery) (AcceptedEventPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return queryAcceptedRecords(s.snapshot.Accepted, query)
}

func (s *MemoryEventPlaneStore) QueryDeadLetters(_ context.Context, query DeadLetterListQuery) (DeadLetterPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return queryDeadLetterRecords(s.snapshot.DeadLetters, query)
}

func (s *MemoryEventPlaneStore) ListAccepted(_ context.Context) ([]domain.AcceptedEventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.AcceptedEventRecord, 0, len(s.snapshot.Accepted))
	for _, record := range s.snapshot.Accepted {
		out = append(out, cloneAcceptedEventRecord(record))
	}
	return out, nil
}

func (s *MemoryEventPlaneStore) ListDeadLetters(_ context.Context) ([]domain.DeadLetterRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.DeadLetterRecord, 0, len(s.snapshot.DeadLetters))
	for _, record := range s.snapshot.DeadLetters {
		out = append(out, cloneDeadLetterRecord(record))
	}
	return out, nil
}

type commandOutboxSnapshot struct {
	Records map[string]*domain.CommandOutboxRecord `json:"records"`
}

// MemoryCommandOutboxStore records outbound command state transitions in memory.
type MemoryCommandOutboxStore struct {
	mu       sync.Mutex
	snapshot commandOutboxSnapshot
}

func NewMemoryCommandOutboxStore() *MemoryCommandOutboxStore {
	return &MemoryCommandOutboxStore{
		snapshot: commandOutboxSnapshot{
			Records: make(map[string]*domain.CommandOutboxRecord),
		},
	}
}

func (s *MemoryCommandOutboxStore) SaveIssued(_ context.Context, cmd domain.Command) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Records[cmd.ID] = &domain.CommandOutboxRecord{
		Command:   cloneCommand(cmd),
		Status:    domain.CommandOutboxStatusIssued,
		UpdatedAt: nowOr(cmd.IssuedAt),
	}
	return nil
}

func (s *MemoryCommandOutboxStore) MarkDispatched(_ context.Context, commandID string, dispatchedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.snapshot.Records[commandID]
	if !ok {
		return fmt.Errorf("command outbox record not found: %s", commandID)
	}
	record.Status = domain.CommandOutboxStatusDispatched
	record.LastError = ""
	record.UpdatedAt = nowOr(dispatchedAt)
	return nil
}

func (s *MemoryCommandOutboxStore) MarkDispatchFailed(_ context.Context, commandID string, errMessage string, failedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.snapshot.Records[commandID]
	if !ok {
		return fmt.Errorf("command outbox record not found: %s", commandID)
	}
	record.Status = domain.CommandOutboxStatusDispatchFailed
	record.LastError = errMessage
	record.UpdatedAt = nowOr(failedAt)
	return nil
}

func (s *MemoryCommandOutboxStore) MarkDelivered(_ context.Context, result domain.CommandResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.snapshot.Records[result.CommandID]
	if !ok {
		return fmt.Errorf("command outbox record not found: %s", result.CommandID)
	}
	record.Status = domain.CommandOutboxStatusResponded
	record.LastError = ""
	record.LastResult = cloneCommandResult(&result)
	record.UpdatedAt = nowOr(result.ReceivedAt)
	return nil
}

func (s *MemoryCommandOutboxStore) Get(_ context.Context, commandID string) (*domain.CommandOutboxRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.snapshot.Records[commandID]
	if !ok {
		return nil, fmt.Errorf("%w: command outbox record %s", ErrNotFound, commandID)
	}
	return cloneCommandOutboxRecord(record), nil
}

func (s *MemoryCommandOutboxStore) List(_ context.Context) ([]*domain.CommandOutboxRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*domain.CommandOutboxRecord, 0, len(s.snapshot.Records))
	for _, record := range s.snapshot.Records {
		out = append(out, cloneCommandOutboxRecord(record))
	}
	return out, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func appendDedupID(values []string, id string, limit int) []string {
	if id == "" || containsString(values, id) {
		return values
	}
	values = append(values, id)
	if limit > 0 && len(values) > limit {
		values = append([]string(nil), values[len(values)-limit:]...)
	}
	return values
}

func eventSource(event domain.Event) string {
	if event.Kind.IsDeviceOriginated() {
		return "device"
	}
	return "internal"
}


// MemoryTaskQueue is a thread-safe in-memory TaskQueue (FIFO with dedup).
// Intended for tests and single-process development; state is lost on restart.
type MemoryTaskQueue struct {
	mu    sync.Mutex
	queue []domain.TaskID
	set   map[domain.TaskID]struct{}
}

func NewMemoryTaskQueue() *MemoryTaskQueue {
	return &MemoryTaskQueue{set: make(map[domain.TaskID]struct{})}
}

func (q *MemoryTaskQueue) Enqueue(_ context.Context, taskID domain.TaskID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.set[taskID]; exists {
		return nil // idempotent
	}
	q.queue = append(q.queue, taskID)
	q.set[taskID] = struct{}{}
	return nil
}

func (q *MemoryTaskQueue) Dequeue(_ context.Context) (domain.TaskID, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.queue) > 0 {
		id := q.queue[0]
		q.queue = q.queue[1:]
		if _, ok := q.set[id]; ok {
			delete(q.set, id)
			return id, true, nil
		}
		// item was Remove()d while in the queue slice; skip tombstone
	}
	return "", false, nil
}

func (q *MemoryTaskQueue) Remove(_ context.Context, taskID domain.TaskID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.set, taskID)
	// slice entry becomes a tombstone; Dequeue skips items absent from set
	return nil
}

func (q *MemoryTaskQueue) Snapshot(_ context.Context) ([]domain.TaskID, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []domain.TaskID
	for _, id := range q.queue {
		if _, ok := q.set[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}
