package store_test

// Redis integration tests. These tests require a running Redis instance.
// Set REDIS_TEST_ADDR (e.g. "localhost:6379") to enable them; otherwise they are skipped.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

// --- helpers ---

func redisClientForTest(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set — skipping Redis integration tests")
	}
	c := store.NewRedisClient(addr, "", 0)
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("redis ping failed: %v", err)
	}
	return c
}

// flushTestKeys removes all test-related keys before each test via a fresh DB.
// We use DB 15 (unlikely to conflict with production data) for tests.
func redisTestClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set — skipping Redis integration tests")
	}
	c := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("redis ping failed: %v", err)
	}
	// Flush the test DB to avoid cross-test pollution.
	if err := c.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush test db: %v", err)
	}
	t.Cleanup(func() { _ = c.FlushDB(context.Background()) })
	return c
}

// --- RedisWorkflowStateStore tests ---

func TestRedisWorkflowStateStore_SaveAndGet(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisWorkflowStateStore(c, time.Minute)
	ctx := context.Background()

	ws := &domain.WorkflowState{
		TaskID:      "task-redis-1",
		DeviceID:    "dev-redis-1",
		CurrentStep: "step1",
		Inputs:      map[string]string{"key": "value"},
	}
	if err := s.Save(ctx, ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if ws.Revision != 1 {
		t.Errorf("expected Revision=1 after first save, got %d", ws.Revision)
	}

	got, err := s.Get(ctx, ws.TaskID, ws.DeviceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CurrentStep != "step1" {
		t.Errorf("expected CurrentStep=step1, got %q", got.CurrentStep)
	}
	if got.Inputs["key"] != "value" {
		t.Errorf("expected Inputs[key]=value, got %q", got.Inputs["key"])
	}
	if got.Revision != 1 {
		t.Errorf("expected stored Revision=1, got %d", got.Revision)
	}
}

func TestRedisWorkflowStateStore_RevisionConflict(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisWorkflowStateStore(c, time.Minute)
	ctx := context.Background()

	ws := &domain.WorkflowState{
		TaskID:   "task-conflict",
		DeviceID: "dev-conflict",
		Inputs:   map[string]string{},
	}
	if err := s.Save(ctx, ws); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	// ws.Revision is now 1.

	// Simulate a stale checkpoint: reset revision to 0 and try to save again.
	wsStale := &domain.WorkflowState{
		TaskID:   ws.TaskID,
		DeviceID: ws.DeviceID,
		Revision: 0, // stale
		Inputs:   map[string]string{},
	}
	err := s.Save(ctx, wsStale)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if !errors.Is(err, store.ErrCheckpointConflict) {
		t.Errorf("expected ErrCheckpointConflict, got %v", err)
	}
}

func TestRedisWorkflowStateStore_RevisionBumpsOnEachSave(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisWorkflowStateStore(c, time.Minute)
	ctx := context.Background()

	ws := &domain.WorkflowState{
		TaskID:   "task-bump",
		DeviceID: "dev-bump",
		Inputs:   map[string]string{},
	}
	for i := uint64(1); i <= 5; i++ {
		if err := s.Save(ctx, ws); err != nil {
			t.Fatalf("Save iteration %d: %v", i, err)
		}
		if ws.Revision != i {
			t.Errorf("expected Revision=%d, got %d", i, ws.Revision)
		}
	}
}

func TestRedisWorkflowStateStore_GetNotFound(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisWorkflowStateStore(c, time.Minute)
	ctx := context.Background()

	_, err := s.Get(ctx, "task-missing", "dev-missing")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRedisWorkflowStateStore_ListActiveByDevice(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisWorkflowStateStore(c, time.Minute)
	ctx := context.Background()
	deviceID := domain.DeviceID("dev-list")

	// Save two active states for the same device.
	ws1 := &domain.WorkflowState{TaskID: "task-list-a", DeviceID: deviceID, CurrentStep: "step1", Inputs: map[string]string{}}
	ws2 := &domain.WorkflowState{TaskID: "task-list-b", DeviceID: deviceID, CurrentStep: "step2", Inputs: map[string]string{}}
	// Save one terminal state.
	ws3 := &domain.WorkflowState{TaskID: "task-list-c", DeviceID: deviceID, CurrentStep: "terminal", Inputs: map[string]string{}}

	for _, ws := range []*domain.WorkflowState{ws1, ws2, ws3} {
		if err := s.Save(ctx, ws); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	active, err := s.ListActiveByDevice(ctx, deviceID)
	if err != nil {
		t.Fatalf("ListActiveByDevice: %v", err)
	}
	if len(active) != 2 {
		t.Errorf("expected 2 active states, got %d", len(active))
	}
	for _, ws := range active {
		if ws.IsTerminal() {
			t.Errorf("ListActiveByDevice returned terminal state: %+v", ws)
		}
	}
}

func TestRedisWorkflowStateStore_TTLRefreshed(t *testing.T) {
	c := redisTestClient(t)
	ttl := 200 * time.Millisecond
	s := store.NewRedisWorkflowStateStore(c, ttl)
	ctx := context.Background()

	ws := &domain.WorkflowState{
		TaskID:   "task-ttl",
		DeviceID: "dev-ttl",
		Inputs:   map[string]string{},
	}
	if err := s.Save(ctx, ws); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Key should be accessible immediately.
	if _, err := s.Get(ctx, ws.TaskID, ws.DeviceID); err != nil {
		t.Fatalf("Get after save: %v", err)
	}

	// Wait for the TTL to elapse.
	time.Sleep(300 * time.Millisecond)

	// Key should now be expired.
	_, err := s.Get(ctx, ws.TaskID, ws.DeviceID)
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound after TTL expiry, got %v", err)
	}
}

// --- RedisTaskStore tests ---

func TestRedisTaskStore_SaveAndGet(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisTaskStore(c, time.Minute)
	ctx := context.Background()

	task := &domain.Task{
		ID:        "task-save-1",
		Goal:      "test goal",
		Status:    domain.TaskStatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.Save(ctx, task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Goal != task.Goal {
		t.Errorf("expected goal=%q, got %q", task.Goal, got.Goal)
	}
	if got.Status != domain.TaskStatusRunning {
		t.Errorf("expected status=running, got %s", got.Status)
	}
}

func TestRedisTaskStore_GetNotFound(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisTaskStore(c, time.Minute)
	ctx := context.Background()

	_, err := s.Get(ctx, "task-does-not-exist")
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRedisTaskStore_List(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisTaskStore(c, time.Minute)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		id := domain.TaskID("task-list-" + string(rune('a'+i)))
		if err := s.Save(ctx, &domain.Task{ID: id, Status: domain.TaskStatusPending}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	tasks, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestRedisTaskStore_ListByDevice(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisTaskStore(c, time.Minute)
	ctx := context.Background()

	deviceA := domain.DeviceID("dev-a")
	deviceB := domain.DeviceID("dev-b")

	_ = s.Save(ctx, &domain.Task{ID: "task-dev-1", AssignedDevice: deviceA, Status: domain.TaskStatusRunning})
	_ = s.Save(ctx, &domain.Task{ID: "task-dev-2", AssignedDevice: deviceA, Status: domain.TaskStatusRunning})
	_ = s.Save(ctx, &domain.Task{ID: "task-dev-3", AssignedDevice: deviceB, Status: domain.TaskStatusRunning})

	tasksA, err := s.ListByDevice(ctx, deviceA)
	if err != nil {
		t.Fatalf("ListByDevice(A): %v", err)
	}
	if len(tasksA) != 2 {
		t.Errorf("expected 2 tasks for device A, got %d", len(tasksA))
	}

	tasksB, err := s.ListByDevice(ctx, deviceB)
	if err != nil {
		t.Fatalf("ListByDevice(B): %v", err)
	}
	if len(tasksB) != 1 {
		t.Errorf("expected 1 task for device B, got %d", len(tasksB))
	}
}

func TestRedisTaskStore_SaveUpdatesExistingTask(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisTaskStore(c, time.Minute)
	ctx := context.Background()

	task := &domain.Task{ID: "task-update", Goal: "original", Status: domain.TaskStatusPending}
	if err := s.Save(ctx, task); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	task.Status = domain.TaskStatusCompleted
	task.Goal = "updated"
	if err := s.Save(ctx, task); err != nil {
		t.Fatalf("update Save: %v", err)
	}

	got, err := s.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.TaskStatusCompleted {
		t.Errorf("expected status=completed, got %s", got.Status)
	}
	if got.Goal != "updated" {
		t.Errorf("expected goal=updated, got %q", got.Goal)
	}
}

func TestRedisWorkflowStateStore_WaitingExpect_RoundTrip(t *testing.T) {
	c := redisTestClient(t)
	s := store.NewRedisWorkflowStateStore(c, time.Minute)
	ctx := context.Background()

	exp := &domain.ExpectDef{
		Kind:        "android.activity.created",
		Package:     "com.example.app",
		ClassSuffix: "MainActivity",
	}
	ws := &domain.WorkflowState{
		TaskID:        "task-expect",
		DeviceID:      "dev-expect",
		WaitingExpect: exp,
		DeadlineAt:    time.Now().Add(5 * time.Second),
		Inputs:        map[string]string{"hostname": "dns.example.com"},
	}
	if err := s.Save(ctx, ws); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get(ctx, ws.TaskID, ws.DeviceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.WaitingExpect == nil {
		t.Fatal("expected WaitingExpect to be non-nil after round-trip")
	}
	if got.WaitingExpect.Package != "com.example.app" {
		t.Errorf("expected WaitingExpect.Package=com.example.app, got %q", got.WaitingExpect.Package)
	}
	if got.Inputs["hostname"] != "dns.example.com" {
		t.Errorf("expected Inputs[hostname]=dns.example.com, got %q", got.Inputs["hostname"])
	}
	if got.DeadlineAt.IsZero() {
		t.Error("expected non-zero DeadlineAt after round-trip")
	}
}
