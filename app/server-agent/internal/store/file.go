package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

const (
	tasksSnapshotFilename          = "tasks.json"
	workflowStatesSnapshotFilename = "workflow_states.json"
	eventPlaneSnapshotFilename     = "event_plane.json"
	commandOutboxSnapshotFilename  = "command_outbox.json"
)

type FileTaskStore struct {
	mu    sync.RWMutex
	path  string
	tasks map[domain.TaskID]*domain.Task
}

func NewFileTaskStore(dir string) (*FileTaskStore, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	store := &FileTaskStore{
		path:  filepath.Join(dir, tasksSnapshotFilename),
		tasks: make(map[domain.TaskID]*domain.Task),
	}
	if err := loadJSONFile(store.path, &store.tasks); err != nil {
		return nil, fmt.Errorf("load task snapshot: %w", err)
	}
	return store, nil
}

func (s *FileTaskStore) Save(_ context.Context, task *domain.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = cloneTask(task)
	return s.persistLocked()
}

func (s *FileTaskStore) Get(_ context.Context, id domain.TaskID) (*domain.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	return cloneTask(task), nil
}

func (s *FileTaskStore) ListByDevice(_ context.Context, deviceID domain.DeviceID) ([]*domain.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Task
	for _, task := range s.tasks {
		if task.AssignedDevice == deviceID {
			out = append(out, cloneTask(task))
		}
	}
	return out, nil
}

func (s *FileTaskStore) persistLocked() error {
	snapshot := make(map[domain.TaskID]*domain.Task, len(s.tasks))
	for id, task := range s.tasks {
		snapshot[id] = cloneTask(task)
	}
	return writeJSONFileAtomically(s.path, snapshot)
}

type FileWorkflowStateStore struct {
	mu     sync.RWMutex
	path   string
	states map[stateKey]*domain.WorkflowState
}

type fileWorkflowStateSnapshot map[string]*domain.WorkflowState

func NewFileWorkflowStateStore(dir string) (*FileWorkflowStateStore, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	store := &FileWorkflowStateStore{
		path:   filepath.Join(dir, workflowStatesSnapshotFilename),
		states: make(map[stateKey]*domain.WorkflowState),
	}

	var snapshot fileWorkflowStateSnapshot
	if err := loadJSONFile(store.path, &snapshot); err != nil {
		return nil, fmt.Errorf("load workflow state snapshot: %w", err)
	}
	for compositeKey, state := range snapshot {
		taskID, deviceID, err := parseStateCompositeKey(compositeKey)
		if err != nil {
			return nil, fmt.Errorf("parse workflow state key %q: %w", compositeKey, err)
		}
		store.states[stateKey{taskID: taskID, deviceID: deviceID}] = cloneWorkflowState(state)
	}
	return store, nil
}

func (s *FileWorkflowStateStore) Save(_ context.Context, ws *domain.WorkflowState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[stateKey{taskID: ws.TaskID, deviceID: ws.DeviceID}] = cloneWorkflowState(ws)
	return s.persistLocked()
}

func (s *FileWorkflowStateStore) Get(_ context.Context, taskID domain.TaskID, deviceID domain.DeviceID) (*domain.WorkflowState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ws, ok := s.states[stateKey{taskID: taskID, deviceID: deviceID}]
	if !ok {
		return nil, fmt.Errorf("workflow state not found: task=%s device=%s", taskID, deviceID)
	}
	return cloneWorkflowState(ws), nil
}

func (s *FileWorkflowStateStore) ListActiveByDevice(_ context.Context, deviceID domain.DeviceID) ([]*domain.WorkflowState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.WorkflowState
	for key, state := range s.states {
		if key.deviceID == deviceID && state.CurrentNode != domain.NodeKindTerminal {
			out = append(out, cloneWorkflowState(state))
		}
	}
	return out, nil
}

func (s *FileWorkflowStateStore) persistLocked() error {
	snapshot := make(fileWorkflowStateSnapshot, len(s.states))
	for key, state := range s.states {
		snapshot[stateCompositeKey(key.taskID, key.deviceID)] = cloneWorkflowState(state)
	}
	return writeJSONFileAtomically(s.path, snapshot)
}

type FileEventPlaneStore struct {
	mu       sync.Mutex
	path     string
	snapshot eventPlaneSnapshot
}

func NewFileEventPlaneStore(dir string) (*FileEventPlaneStore, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	store := &FileEventPlaneStore{
		path: filepath.Join(dir, eventPlaneSnapshotFilename),
		snapshot: eventPlaneSnapshot{
			Cursors: make(map[domain.DeviceID]deviceEventCursor),
		},
	}
	if err := loadJSONFile(store.path, &store.snapshot); err != nil {
		return nil, fmt.Errorf("load event plane snapshot: %w", err)
	}
	if store.snapshot.Cursors == nil {
		store.snapshot.Cursors = make(map[domain.DeviceID]deviceEventCursor)
	}
	return store, nil
}

func (s *FileEventPlaneStore) Accept(_ context.Context, event domain.Event) (domain.EventAcceptance, error) {
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
	if err := s.persistLocked(); err != nil {
		return "", err
	}
	return domain.EventAcceptanceAccepted, nil
}

func (s *FileEventPlaneStore) RecordDeadLetter(_ context.Context, record domain.DeadLetterRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now()
	}
	s.snapshot.DeadLetters = append(s.snapshot.DeadLetters, cloneDeadLetterRecord(record))
	return s.persistLocked()
}

func (s *FileEventPlaneStore) ListAccepted(_ context.Context) ([]domain.AcceptedEventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.AcceptedEventRecord, 0, len(s.snapshot.Accepted))
	for _, record := range s.snapshot.Accepted {
		out = append(out, cloneAcceptedEventRecord(record))
	}
	return out, nil
}

func (s *FileEventPlaneStore) ListDeadLetters(_ context.Context) ([]domain.DeadLetterRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.DeadLetterRecord, 0, len(s.snapshot.DeadLetters))
	for _, record := range s.snapshot.DeadLetters {
		out = append(out, cloneDeadLetterRecord(record))
	}
	return out, nil
}

func (s *FileEventPlaneStore) persistLocked() error {
	snapshot := eventPlaneSnapshot{
		Cursors:     make(map[domain.DeviceID]deviceEventCursor, len(s.snapshot.Cursors)),
		Accepted:    make([]domain.AcceptedEventRecord, 0, len(s.snapshot.Accepted)),
		DeadLetters: make([]domain.DeadLetterRecord, 0, len(s.snapshot.DeadLetters)),
	}
	for deviceID, cursor := range s.snapshot.Cursors {
		snapshot.Cursors[deviceID] = deviceEventCursor{
			HighSeqNo: cursor.HighSeqNo,
			SeenIDs:   append([]string(nil), cursor.SeenIDs...),
		}
	}
	for _, record := range s.snapshot.Accepted {
		snapshot.Accepted = append(snapshot.Accepted, cloneAcceptedEventRecord(record))
	}
	for _, record := range s.snapshot.DeadLetters {
		snapshot.DeadLetters = append(snapshot.DeadLetters, cloneDeadLetterRecord(record))
	}
	return writeJSONFileAtomically(s.path, snapshot)
}

type FileCommandOutboxStore struct {
	mu       sync.Mutex
	path     string
	snapshot commandOutboxSnapshot
}

func NewFileCommandOutboxStore(dir string) (*FileCommandOutboxStore, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	store := &FileCommandOutboxStore{
		path: filepath.Join(dir, commandOutboxSnapshotFilename),
		snapshot: commandOutboxSnapshot{
			Records: make(map[string]*domain.CommandOutboxRecord),
		},
	}
	if err := loadJSONFile(store.path, &store.snapshot); err != nil {
		return nil, fmt.Errorf("load command outbox snapshot: %w", err)
	}
	if store.snapshot.Records == nil {
		store.snapshot.Records = make(map[string]*domain.CommandOutboxRecord)
	}
	return store, nil
}

func (s *FileCommandOutboxStore) SaveIssued(_ context.Context, cmd domain.Command) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Records[cmd.ID] = &domain.CommandOutboxRecord{
		Command:   cloneCommand(cmd),
		Status:    domain.CommandOutboxStatusIssued,
		UpdatedAt: nowOr(cmd.IssuedAt),
	}
	return s.persistLocked()
}

func (s *FileCommandOutboxStore) MarkDispatched(_ context.Context, commandID string, dispatchedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.snapshot.Records[commandID]
	if !ok {
		return fmt.Errorf("command outbox record not found: %s", commandID)
	}
	record.Status = domain.CommandOutboxStatusDispatched
	record.LastError = ""
	record.UpdatedAt = nowOr(dispatchedAt)
	return s.persistLocked()
}

func (s *FileCommandOutboxStore) MarkDispatchFailed(_ context.Context, commandID string, errMessage string, failedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.snapshot.Records[commandID]
	if !ok {
		return fmt.Errorf("command outbox record not found: %s", commandID)
	}
	record.Status = domain.CommandOutboxStatusDispatchFailed
	record.LastError = errMessage
	record.UpdatedAt = nowOr(failedAt)
	return s.persistLocked()
}

func (s *FileCommandOutboxStore) MarkDelivered(_ context.Context, result domain.CommandResult) error {
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
	return s.persistLocked()
}

func (s *FileCommandOutboxStore) Get(_ context.Context, commandID string) (*domain.CommandOutboxRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.snapshot.Records[commandID]
	if !ok {
		return nil, fmt.Errorf("command outbox record not found: %s", commandID)
	}
	return cloneCommandOutboxRecord(record), nil
}

func (s *FileCommandOutboxStore) List(_ context.Context) ([]*domain.CommandOutboxRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*domain.CommandOutboxRecord, 0, len(s.snapshot.Records))
	for _, record := range s.snapshot.Records {
		out = append(out, cloneCommandOutboxRecord(record))
	}
	return out, nil
}

func (s *FileCommandOutboxStore) persistLocked() error {
	snapshot := commandOutboxSnapshot{
		Records: make(map[string]*domain.CommandOutboxRecord, len(s.snapshot.Records)),
	}
	for id, record := range s.snapshot.Records {
		snapshot.Records[id] = cloneCommandOutboxRecord(record)
	}
	return writeJSONFileAtomically(s.path, snapshot)
}

func stateCompositeKey(taskID domain.TaskID, deviceID domain.DeviceID) string {
	return string(taskID) + "::" + string(deviceID)
}

func parseStateCompositeKey(composite string) (domain.TaskID, domain.DeviceID, error) {
	for i := 0; i < len(composite)-1; i++ {
		if composite[i] == ':' && composite[i+1] == ':' {
			return domain.TaskID(composite[:i]), domain.DeviceID(composite[i+2:]), nil
		}
	}
	return "", "", fmt.Errorf("invalid composite key")
}
