package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

// mapPgError translates PostgreSQL unique-violation errors into domain errors.
func mapPgError(err error, kind string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return fmt.Errorf("%w: %s", ErrCheckpointConflict, pgErr.Detail)
	}
	return fmt.Errorf("postgres %s: %w", kind, err)
}

type scanner interface {
	Scan(dest ...any) error
}

// --- TaskStore ---

type PostgresTaskStore struct {
	db *sql.DB
}

func NewPostgresTaskStore(db *sql.DB) *PostgresTaskStore {
	return &PostgresTaskStore{db: db}
}

func (s *PostgresTaskStore) Save(ctx context.Context, t *domain.Task) error {
	inputBytes, err := json.Marshal(t.InputArtifacts)
	if err != nil {
		return fmt.Errorf("marshal input artifacts: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tasks (id, goal, input_artifacts, status, assigned_device, workflow_name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			goal = EXCLUDED.goal,
			input_artifacts = EXCLUDED.input_artifacts,
			status = EXCLUDED.status,
			assigned_device = EXCLUDED.assigned_device,
			workflow_name = EXCLUDED.workflow_name,
			updated_at = EXCLUDED.updated_at
	`, string(t.ID), t.Goal, inputBytes, string(t.Status), string(t.AssignedDevice), t.WorkflowName, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return mapPgError(err, "save task")
	}
	return nil
}

func scanTask(row scanner) (*domain.Task, error) {
	var t domain.Task
	var inputBytes []byte
	var status, assignedDevice string
	if err := row.Scan(&t.ID, &t.Goal, &inputBytes, &status, &assignedDevice, &t.WorkflowName, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Status = domain.TaskStatus(status)
	t.AssignedDevice = domain.DeviceID(assignedDevice)
	if len(inputBytes) > 0 {
		if err := json.Unmarshal(inputBytes, &t.InputArtifacts); err != nil {
			return nil, fmt.Errorf("unmarshal task inputs: %w", err)
		}
	}
	return &t, nil
}

func (s *PostgresTaskStore) Get(ctx context.Context, id domain.TaskID) (*domain.Task, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, goal, input_artifacts, status, assigned_device, workflow_name, created_at, updated_at
		FROM tasks WHERE id = $1
	`, string(id))
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: task %s", ErrNotFound, id)
	}
	return t, err
}

func (s *PostgresTaskStore) List(ctx context.Context) ([]*domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, goal, input_artifacts, status, assigned_device, workflow_name, created_at, updated_at
		FROM tasks ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *PostgresTaskStore) ListByDevice(ctx context.Context, deviceID domain.DeviceID) ([]*domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, goal, input_artifacts, status, assigned_device, workflow_name, created_at, updated_at
		FROM tasks WHERE assigned_device = $1 ORDER BY created_at DESC
	`, string(deviceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *PostgresTaskStore) ListActiveByDevice(ctx context.Context, deviceID domain.DeviceID) ([]*domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, goal, input_artifacts, status, assigned_device, workflow_name, created_at, updated_at
		FROM tasks 
		WHERE assigned_device = $1 
		  AND status NOT IN ('completed', 'failed', 'cancelled')
		ORDER BY created_at DESC
	`, string(deviceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// --- WorkflowStateStore ---

type PostgresWorkflowStateStore struct {
	db *sql.DB
}

func NewPostgresWorkflowStateStore(db *sql.DB) *PostgresWorkflowStateStore {
	return &PostgresWorkflowStateStore{db: db}
}

func (s *PostgresWorkflowStateStore) Save(ctx context.Context, ws *domain.WorkflowState) error {
	waitBytes, err := json.Marshal(ws.WaitingExpect)
	if err != nil {
		return fmt.Errorf("marshal waiting_expect: %w", err)
	}
	inputBytes, err := json.Marshal(ws.Inputs)
	if err != nil {
		return fmt.Errorf("marshal inputs: %w", err)
	}

	var deadline *time.Time
	if !ws.DeadlineAt.IsZero() {
		deadline = &ws.DeadlineAt
	}

	if ws.Revision == 0 {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO workflow_states (task_id, device_id, revision, current_step, retry_count, terminal_success, waiting_expect, deadline_at, inputs, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, string(ws.TaskID), string(ws.DeviceID), 1, ws.CurrentStep, ws.RetryCount, ws.TerminalSuccess, waitBytes, deadline, inputBytes, ws.UpdatedAt)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return fmt.Errorf("%w: task=%s device=%s expected revision 0 got 1", ErrCheckpointConflict, ws.TaskID, ws.DeviceID)
			}
			return mapPgError(err, "insert workflow state")
		}
		ws.Revision = 1
		return nil
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE workflow_states SET
			revision = revision + 1,
			current_step = $4,
			retry_count = $5,
			terminal_success = $6,
			waiting_expect = $7,
			deadline_at = $8,
			inputs = $9,
			updated_at = $10
		WHERE task_id = $1 AND device_id = $2 AND revision = $3
	`, string(ws.TaskID), string(ws.DeviceID), ws.Revision, ws.CurrentStep, ws.RetryCount, ws.TerminalSuccess, waitBytes, deadline, inputBytes, ws.UpdatedAt)
	if err != nil {
		return mapPgError(err, "update workflow state")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if rows == 0 {
		var currentRev uint64
		err := s.db.QueryRowContext(ctx, `SELECT revision FROM workflow_states WHERE task_id = $1 AND device_id = $2`, string(ws.TaskID), string(ws.DeviceID)).Scan(&currentRev)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: workflow state not found for task=%s device=%s", ErrNotFound, ws.TaskID, ws.DeviceID)
			}
			return fmt.Errorf("check current revision: %w", err)
		}
		return fmt.Errorf("%w: task=%s device=%s expected revision %d got %d", ErrCheckpointConflict, ws.TaskID, ws.DeviceID, currentRev, ws.Revision)
	}
	ws.Revision++
	return nil
}

func scanWorkflowState(row scanner) (*domain.WorkflowState, error) {
	var ws domain.WorkflowState
	var taskID, deviceID string
	var waitBytes, inputBytes []byte
	var deadline *time.Time
	if err := row.Scan(&taskID, &deviceID, &ws.Revision, &ws.CurrentStep, &ws.RetryCount, &ws.TerminalSuccess, &waitBytes, &deadline, &inputBytes, &ws.UpdatedAt); err != nil {
		return nil, err
	}
	ws.TaskID = domain.TaskID(taskID)
	ws.DeviceID = domain.DeviceID(deviceID)
	if deadline != nil {
		ws.DeadlineAt = *deadline
	}
	if len(waitBytes) > 0 && string(waitBytes) != "null" {
		if err := json.Unmarshal(waitBytes, &ws.WaitingExpect); err != nil {
			return nil, fmt.Errorf("unmarshal waiting_expect: %w", err)
		}
	}
	if len(inputBytes) > 0 && string(inputBytes) != "null" {
		if err := json.Unmarshal(inputBytes, &ws.Inputs); err != nil {
			return nil, fmt.Errorf("unmarshal inputs: %w", err)
		}
	}
	return &ws, nil
}

func (s *PostgresWorkflowStateStore) Get(ctx context.Context, taskID domain.TaskID, deviceID domain.DeviceID) (*domain.WorkflowState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT task_id, device_id, revision, current_step, retry_count, terminal_success, waiting_expect, deadline_at, inputs, updated_at
		FROM workflow_states WHERE task_id = $1 AND device_id = $2
	`, string(taskID), string(deviceID))
	ws, err := scanWorkflowState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: workflow state task=%s device=%s", ErrNotFound, taskID, deviceID)
	}
	return ws, err
}

func (s *PostgresWorkflowStateStore) ListActiveByDevice(ctx context.Context, deviceID domain.DeviceID) ([]*domain.WorkflowState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, device_id, revision, current_step, retry_count, terminal_success, waiting_expect, deadline_at, inputs, updated_at
		FROM workflow_states WHERE device_id = $1 AND current_step != 'terminal'
	`, string(deviceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []*domain.WorkflowState
	for rows.Next() {
		ws, err := scanWorkflowState(rows)
		if err != nil {
			return nil, err
		}
		states = append(states, ws)
	}
	return states, rows.Err()
}

// --- EventPlaneStore ---

type PostgresEventPlaneStore struct {
	db *sql.DB
}

func NewPostgresEventPlaneStore(db *sql.DB) *PostgresEventPlaneStore {
	return &PostgresEventPlaneStore{db: db}
}

func (s *PostgresEventPlaneStore) Accept(ctx context.Context, event domain.Event) (domain.EventAcceptance, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var highSeqNo uint64
	var seenIDsBytes []byte
	err = tx.QueryRowContext(ctx, `SELECT high_seq_no, seen_ids FROM device_event_cursors WHERE device_id = $1 FOR UPDATE`, string(event.DeviceID)).Scan(&highSeqNo, &seenIDsBytes)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	var seenIDs []string
	if len(seenIDsBytes) > 0 {
		_ = json.Unmarshal(seenIDsBytes, &seenIDs)
	}

	if event.SeqNo > 0 && event.SeqNo <= highSeqNo {
		return domain.EventAcceptanceStale, nil
	}
	if event.ID != "" && containsString(seenIDs, event.ID) {
		return domain.EventAcceptanceDuplicate, nil
	}

	if event.SeqNo > highSeqNo {
		highSeqNo = event.SeqNo
	}
	if event.ID != "" {
		seenIDs = appendDedupID(seenIDs, event.ID, eventDedupHistorySize)
	}

	newSeenIDs, _ := json.Marshal(seenIDs)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO device_event_cursors (device_id, high_seq_no, seen_ids)
		VALUES ($1, $2, $3)
		ON CONFLICT (device_id) DO UPDATE SET
			high_seq_no = EXCLUDED.high_seq_no,
			seen_ids = EXCLUDED.seen_ids
	`, string(event.DeviceID), highSeqNo, newSeenIDs)
	if err != nil {
		return "", err
	}

	payloadBytes := domain.MarshalEventPayload(event)

	internalID := event.ID
	if internalID == "" {
		if event.DeviceID != "" || event.SeqNo > 0 {
			internalID = fmt.Sprintf("%s:%d", event.DeviceID, event.SeqNo)
		} else {
			internalID = time.Now().UTC().Format(time.RFC3339Nano)
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO event_plane_accepted (id, event_id, kind, device_id, seq_no, occurred_at, payload, accepted_at, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, internalID, event.ID, string(event.Kind), string(event.DeviceID), event.SeqNo, event.OccurredAt, payloadBytes, time.Now().UTC(), eventSource(event))
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return domain.EventAcceptanceAccepted, nil
}

func (s *PostgresEventPlaneStore) RecordDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error {
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now().UTC()
	}
	var payload []byte
	if len(record.Payload) > 0 {
		payload = record.Payload
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO event_plane_deadletters (id, event_id, kind, device_id, seq_no, payload, reason, source, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, record.ID, record.EventID, string(record.Kind), string(record.DeviceID), record.SeqNo, payload, record.Reason, record.Source, record.RecordedAt)
	return err
}

func (s *PostgresEventPlaneStore) QueryAccepted(ctx context.Context, query AcceptedEventListQuery) (AcceptedEventPage, error) {
	q, err := normalizeAcceptedEventListQuery(query)
	if err != nil {
		return AcceptedEventPage{}, err
	}
	var conditions []string
	var args []any
	idx := 1

	if q.DeviceID != "" {
		conditions = append(conditions, fmt.Sprintf("device_id = $%d", idx))
		args = append(args, string(q.DeviceID))
		idx++
	}
	if q.Kind != "" {
		conditions = append(conditions, fmt.Sprintf("kind = $%d", idx))
		args = append(args, string(q.Kind))
		idx++
	}
	if q.Source != "" {
		conditions = append(conditions, fmt.Sprintf("source = $%d", idx))
		args = append(args, q.Source)
		idx++
	}
	if !q.From.IsZero() {
		conditions = append(conditions, fmt.Sprintf("accepted_at >= $%d", idx))
		args = append(args, q.From)
		idx++
	}
	if !q.To.IsZero() {
		conditions = append(conditions, fmt.Sprintf("accepted_at <= $%d", idx))
		args = append(args, q.To)
		idx++
	}

	if q.cursor != nil {
		op := "<"
		if q.Order == EventListOrderAsc {
			op = ">"
		}
		conditions = append(conditions, fmt.Sprintf("(accepted_at, id) %s ($%d, $%d)", op, idx, idx+1))
		args = append(args, q.cursor.Timestamp, q.cursor.ID)
		idx += 2
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	orderBy := "DESC"
	if q.Order == EventListOrderAsc {
		orderBy = "ASC"
	}

	querySQL := fmt.Sprintf(`
		SELECT id, event_id, kind, device_id, seq_no, occurred_at, payload, accepted_at, source, COUNT(*) OVER() AS total
		FROM event_plane_accepted
		%s
		ORDER BY accepted_at %s, id %s
		LIMIT $%d OFFSET $%d
	`, whereClause, orderBy, orderBy, idx, idx+1)

	args = append(args, q.Limit, q.Offset)

	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return AcceptedEventPage{}, err
	}
	defer rows.Close()

	var items []domain.AcceptedEventRecord
	total := 0
	lastID := ""
	for rows.Next() {
		var r domain.AcceptedEventRecord
		var id, kind, deviceID string
		var payloadBytes []byte
		if err := rows.Scan(&id, &r.Event.ID, &kind, &deviceID, &r.Event.SeqNo, &r.Event.OccurredAt, &payloadBytes, &r.AcceptedAt, &r.Source, &total); err != nil {
			return AcceptedEventPage{}, err
		}
		r.Event.Kind = domain.EventKind(kind)
		r.Event.DeviceID = domain.DeviceID(deviceID)
		if len(payloadBytes) > 0 {
			r.Event.Payload = json.RawMessage(payloadBytes)
		}
		items = append(items, r)
		lastID = id
	}
	if err := rows.Err(); err != nil {
		return AcceptedEventPage{}, err
	}

	nextCursor := ""
	if len(items) > 0 && q.Offset+len(items) < total {
		nextCursor = encodeEventListCursor(eventListCursor{
			Version:   1,
			Order:     q.Order,
			Timestamp: items[len(items)-1].AcceptedAt.UTC(),
			ID:        lastID,
		})
	}

	return AcceptedEventPage{
		Items:      items,
		Total:      total,
		Offset:     q.Offset,
		Limit:      q.Limit,
		HasMore:    q.Offset+len(items) < total,
		NextCursor: nextCursor,
	}, nil
}

func (s *PostgresEventPlaneStore) QueryDeadLetters(ctx context.Context, query DeadLetterListQuery) (DeadLetterPage, error) {
	q, err := normalizeDeadLetterListQuery(query)
	if err != nil {
		return DeadLetterPage{}, err
	}
	var conditions []string
	var args []any
	idx := 1

	if q.DeviceID != "" {
		conditions = append(conditions, fmt.Sprintf("device_id = $%d", idx))
		args = append(args, string(q.DeviceID))
		idx++
	}
	if q.Kind != "" {
		conditions = append(conditions, fmt.Sprintf("kind = $%d", idx))
		args = append(args, string(q.Kind))
		idx++
	}
	if q.Source != "" {
		conditions = append(conditions, fmt.Sprintf("source = $%d", idx))
		args = append(args, q.Source)
		idx++
	}
	if q.EventID != "" {
		conditions = append(conditions, fmt.Sprintf("event_id = $%d", idx))
		args = append(args, q.EventID)
		idx++
	}
	if !q.From.IsZero() {
		conditions = append(conditions, fmt.Sprintf("recorded_at >= $%d", idx))
		args = append(args, q.From)
		idx++
	}
	if !q.To.IsZero() {
		conditions = append(conditions, fmt.Sprintf("recorded_at <= $%d", idx))
		args = append(args, q.To)
		idx++
	}

	if q.cursor != nil {
		op := "<"
		if q.Order == EventListOrderAsc {
			op = ">"
		}
		conditions = append(conditions, fmt.Sprintf("(recorded_at, id) %s ($%d, $%d)", op, idx, idx+1))
		args = append(args, q.cursor.Timestamp, q.cursor.ID)
		idx += 2
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	orderBy := "DESC"
	if q.Order == EventListOrderAsc {
		orderBy = "ASC"
	}

	querySQL := fmt.Sprintf(`
		SELECT id, event_id, kind, device_id, seq_no, payload, reason, source, recorded_at, COUNT(*) OVER() AS total
		FROM event_plane_deadletters
		%s
		ORDER BY recorded_at %s, id %s
		LIMIT $%d OFFSET $%d
	`, whereClause, orderBy, orderBy, idx, idx+1)

	args = append(args, q.Limit, q.Offset)

	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return DeadLetterPage{}, err
	}
	defer rows.Close()

	var items []domain.DeadLetterRecord
	total := 0
	lastID := ""
	for rows.Next() {
		var r domain.DeadLetterRecord
		var eventID, kind, deviceID string
		var payloadBytes []byte
		if err := rows.Scan(&r.ID, &eventID, &kind, &deviceID, &r.SeqNo, &payloadBytes, &r.Reason, &r.Source, &r.RecordedAt, &total); err != nil {
			return DeadLetterPage{}, err
		}
		r.EventID = eventID
		r.Kind = domain.EventKind(kind)
		r.DeviceID = domain.DeviceID(deviceID)
		if len(payloadBytes) > 0 {
			r.Payload = json.RawMessage(payloadBytes)
		}
		items = append(items, r)
		lastID = r.ID
	}
	if err := rows.Err(); err != nil {
		return DeadLetterPage{}, err
	}

	nextCursor := ""
	if len(items) > 0 && q.Offset+len(items) < total {
		nextCursor = encodeEventListCursor(eventListCursor{
			Version:   1,
			Order:     q.Order,
			Timestamp: items[len(items)-1].RecordedAt.UTC(),
			ID:        lastID,
		})
	}

	return DeadLetterPage{
		Items:      items,
		Total:      total,
		Offset:     q.Offset,
		Limit:      q.Limit,
		HasMore:    q.Offset+len(items) < total,
		NextCursor: nextCursor,
	}, nil
}

func (s *PostgresEventPlaneStore) ListAccepted(ctx context.Context) ([]domain.AcceptedEventRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, event_id, kind, device_id, seq_no, occurred_at, payload, accepted_at, source
		FROM event_plane_accepted ORDER BY accepted_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AcceptedEventRecord
	for rows.Next() {
		var r domain.AcceptedEventRecord
		var id, kind, deviceID string
		var payloadBytes []byte
		if err := rows.Scan(&id, &r.Event.ID, &kind, &deviceID, &r.Event.SeqNo, &r.Event.OccurredAt, &payloadBytes, &r.AcceptedAt, &r.Source); err != nil {
			return nil, err
		}
		r.Event.Kind = domain.EventKind(kind)
		r.Event.DeviceID = domain.DeviceID(deviceID)
		if len(payloadBytes) > 0 {
			r.Event.Payload = json.RawMessage(payloadBytes)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PostgresEventPlaneStore) ListDeadLetters(ctx context.Context) ([]domain.DeadLetterRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, event_id, kind, device_id, seq_no, payload, reason, source, recorded_at
		FROM event_plane_deadletters ORDER BY recorded_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DeadLetterRecord
	for rows.Next() {
		var r domain.DeadLetterRecord
		var eventID, kind, deviceID string
		var payloadBytes []byte
		if err := rows.Scan(&r.ID, &eventID, &kind, &deviceID, &r.SeqNo, &payloadBytes, &r.Reason, &r.Source, &r.RecordedAt); err != nil {
			return nil, err
		}
		r.EventID = eventID
		r.Kind = domain.EventKind(kind)
		r.DeviceID = domain.DeviceID(deviceID)
		if len(payloadBytes) > 0 {
			r.Payload = json.RawMessage(payloadBytes)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- TaskQueue ---

type PostgresTaskQueue struct {
	db *sql.DB
}

func NewPostgresTaskQueue(db *sql.DB) *PostgresTaskQueue {
	return &PostgresTaskQueue{db: db}
}

func (q *PostgresTaskQueue) Enqueue(ctx context.Context, taskID domain.TaskID) error {
	_, err := q.db.ExecContext(ctx, `
		INSERT INTO task_queue (task_id, queued_at)
		VALUES ($1, NOW())
		ON CONFLICT (task_id) DO NOTHING
	`, string(taskID))
	return err
}

func (q *PostgresTaskQueue) Dequeue(ctx context.Context) (domain.TaskID, bool, error) {
	var taskID string
	err := q.db.QueryRowContext(ctx, `
		DELETE FROM task_queue
		WHERE task_id = (
			SELECT task_id
			FROM task_queue
			ORDER BY queued_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING task_id
	`).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return domain.TaskID(taskID), true, nil
}

func (q *PostgresTaskQueue) Remove(ctx context.Context, taskID domain.TaskID) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM task_queue WHERE task_id = $1`, string(taskID))
	return err
}

func (q *PostgresTaskQueue) Snapshot(ctx context.Context) ([]domain.TaskID, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT task_id FROM task_queue ORDER BY queued_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TaskID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, domain.TaskID(id))
	}
	return out, rows.Err()
}

// --- CommandOutboxStore ---

type PostgresCommandOutboxStore struct {
	db *sql.DB
}

func NewPostgresCommandOutboxStore(db *sql.DB) *PostgresCommandOutboxStore {
	return &PostgresCommandOutboxStore{db: db}
}

func (s *PostgresCommandOutboxStore) SaveIssued(ctx context.Context, cmd domain.Command) error {
	paramsBytes, err := json.Marshal(cmd.Params)
	if err != nil {
		return fmt.Errorf("marshal command params: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO command_outbox (command_id, kind, device_id, task_id, params, issued_at, status, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (command_id) DO UPDATE SET
			kind = EXCLUDED.kind,
			device_id = EXCLUDED.device_id,
			task_id = EXCLUDED.task_id,
			params = EXCLUDED.params,
			issued_at = EXCLUDED.issued_at,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`, cmd.ID, string(cmd.Kind), string(cmd.DeviceID), string(cmd.TaskID), paramsBytes, cmd.IssuedAt, string(domain.CommandOutboxStatusIssued), time.Now().UTC())
	return err
}

func (s *PostgresCommandOutboxStore) MarkDispatched(ctx context.Context, commandID string, dispatchedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE command_outbox SET status = $1, last_error = NULL, updated_at = $2
		WHERE command_id = $3
	`, string(domain.CommandOutboxStatusDispatched), dispatchedAt, commandID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("command outbox record not found: %s", commandID)
	}
	return nil
}

func (s *PostgresCommandOutboxStore) MarkDispatchFailed(ctx context.Context, commandID string, errMessage string, failedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE command_outbox SET status = $1, last_error = $2, updated_at = $3
		WHERE command_id = $4
	`, string(domain.CommandOutboxStatusDispatchFailed), errMessage, failedAt, commandID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("command outbox record not found: %s", commandID)
	}
	return nil
}

func (s *PostgresCommandOutboxStore) MarkDelivered(ctx context.Context, result domain.CommandResult) error {
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal command result: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE command_outbox SET status = $1, last_error = NULL, last_result = $2, updated_at = $3
		WHERE command_id = $4
	`, string(domain.CommandOutboxStatusResponded), resultBytes, result.ReceivedAt, result.CommandID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("command outbox record not found: %s", result.CommandID)
	}
	return nil
}

func scanCommandOutboxRecord(row scanner) (*domain.CommandOutboxRecord, error) {
	var r domain.CommandOutboxRecord
	var kind, deviceID, taskID, status string
	var paramsBytes, resultBytes []byte
	var lastError sql.NullString
	if err := row.Scan(&r.Command.ID, &kind, &deviceID, &taskID, &paramsBytes, &r.Command.IssuedAt, &status, &lastError, &resultBytes, &r.UpdatedAt); err != nil {
		return nil, err
	}
	r.Command.Kind = domain.CommandKind(kind)
	r.Command.DeviceID = domain.DeviceID(deviceID)
	r.Command.TaskID = domain.TaskID(taskID)
	if len(paramsBytes) > 0 {
		r.Command.Params = json.RawMessage(paramsBytes)
	}
	r.Status = domain.CommandOutboxStatus(status)
	if lastError.Valid {
		r.LastError = lastError.String
	}
	if len(resultBytes) > 0 {
		var lastResult domain.CommandResult
		if err := json.Unmarshal(resultBytes, &lastResult); err != nil {
			return nil, fmt.Errorf("unmarshal last_result: %w", err)
		}
		r.LastResult = &lastResult
	}
	return &r, nil
}

func (s *PostgresCommandOutboxStore) Get(ctx context.Context, commandID string) (*domain.CommandOutboxRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT command_id, kind, device_id, task_id, params, issued_at, status, last_error, last_result, updated_at
		FROM command_outbox WHERE command_id = $1
	`, commandID)
	r, err := scanCommandOutboxRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: command outbox record %s", ErrNotFound, commandID)
	}
	return r, err
}

func (s *PostgresCommandOutboxStore) List(ctx context.Context) ([]*domain.CommandOutboxRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT command_id, kind, device_id, task_id, params, issued_at, status, last_error, last_result, updated_at
		FROM command_outbox ORDER BY issued_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.CommandOutboxRecord
	for rows.Next() {
		r, err := scanCommandOutboxRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
