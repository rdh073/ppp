package accountmanager

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/appport"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

func TestAccountCreationService_Start_DefaultKindCreatesGoogleAndInstagramTasks(t *testing.T) {
	origPoll := pollTaskUntilTerminal
	t.Cleanup(func() { pollTaskUntilTerminal = origPoll })
	pollTaskUntilTerminal = func(_ context.Context, _ appport.TaskSummaryGetter, taskID domain.TaskID, _ time.Duration, _ *slog.Logger) (*appport.TaskSummary, bool) {
		summary := &appport.TaskSummary{
			Task: &domain.Task{
				ID:     taskID,
				Status: domain.TaskStatusCompleted,
			},
		}
		if taskID == "task-1" {
			summary.OutputArtifacts = map[string]string{
				"accountId": "acct-google-1",
				"email":     "google@example.com",
				"password":  "secret",
				"username":  "g-user",
			}
		}
		return summary, true
	}

	tasks := newFakeTaskControl()
	publisher := newFakeProjectionPublisher()
	service := NewAccountCreationService(tasks, nil, nil, publisher, "http://server.test", newTestLogger())

	run, err := service.Start(context.Background(), StartAccountCreationInput{
		DeviceID: "device-1",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if run.Kind != "google+instagram" {
		t.Fatalf("Start() kind = %q, want google+instagram", run.Kind)
	}

	requests := tasks.waitForCreates(2, time.Second)
	if got, want := requests[0].WorkflowName, "google-account-create-auto-script"; got != want {
		t.Fatalf("google workflow = %q, want %q", got, want)
	}
	if got := requests[0].InputArtifacts["captcha_endpoint"]; got != "http://server.test/captcha/solve" {
		t.Fatalf("captcha_endpoint = %q", got)
	}
	if got := requests[0].InputArtifacts["account_service_endpoint"]; got != "http://server.test" {
		t.Fatalf("account_service_endpoint = %q", got)
	}
	if got, want := requests[1].WorkflowName, "instagram-create-script"; got != want {
		t.Fatalf("instagram workflow = %q, want %q", got, want)
	}
	if got := requests[1].InputArtifacts["google_account_id"]; got != "acct-google-1" {
		t.Fatalf("google_account_id = %q", got)
	}

	waitForCondition(t, time.Second, func() bool {
		current, ok := service.Get(run.ID)
		return ok && current.Status == domain.AccountCreationStatusDone && current.GoogleAccountID == "acct-google-1"
	}, "account creation completion")

	events := publisher.waitForEvents(1, time.Second)
	if got, want := events[0].Topic, "account-manager.account-creations"; got != want {
		t.Fatalf("projection topic = %q, want %q", got, want)
	}
	payload, ok := events[0].Payload.(domain.AccountCreationRun)
	if !ok {
		t.Fatalf("projection payload type = %T, want domain.AccountCreationRun", events[0].Payload)
	}
	if payload.ID != run.ID {
		t.Fatalf("projection payload id = %q, want %q", payload.ID, run.ID)
	}
}

func TestLoginRunService_Start_CreatesAccountAndActivatesItOnSuccess(t *testing.T) {
	origPoll := pollTaskUntilTerminal
	t.Cleanup(func() { pollTaskUntilTerminal = origPoll })
	pollTaskUntilTerminal = func(_ context.Context, _ appport.TaskSummaryGetter, taskID domain.TaskID, _ time.Duration, _ *slog.Logger) (*appport.TaskSummary, bool) {
		return &appport.TaskSummary{
			Task: &domain.Task{
				ID:     taskID,
				Status: domain.TaskStatusCompleted,
			},
		}, true
	}

	dir := t.TempDir()
	accounts, err := store.NewFileAccountStore(dir)
	if err != nil {
		t.Fatalf("NewFileAccountStore: %v", err)
	}
	tasks := newFakeTaskControl()
	publisher := newFakeProjectionPublisher()
	service := NewLoginRunService(tasks, accounts, nil, publisher, newTestLogger(), LoginRunConfig{
		Platform:     "google",
		AccountKind:  "google",
		WorkflowName: "google-login-script",
	})

	run, account, err := service.Start(context.Background(), StartLoginRunInput{
		Email:    "new@example.com",
		Password: "pass-123",
		DeviceID: "device-2",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if account.ID == "" {
		t.Fatal("Start() returned empty account ID")
	}

	requests := tasks.waitForCreates(1, time.Second)
	if got, want := requests[0].WorkflowName, "google-login-script"; got != want {
		t.Fatalf("workflowName = %q, want %q", got, want)
	}
	if got := requests[0].InputArtifacts["email"]; got != "new@example.com" {
		t.Fatalf("email artifact = %q", got)
	}
	if got := requests[0].InputArtifacts["password"]; got != "pass-123" {
		t.Fatalf("password artifact = %q", got)
	}

	waitForCondition(t, time.Second, func() bool {
		current, ok := service.Get(run.ID)
		if !ok || current.Status != domain.LoginRunStatusDone {
			return false
		}
		stored, ok := accounts.GetByID(account.ID)
		return ok && stored.Status == domain.AccountStatusActive && stored.DeviceID == "device-2"
	}, "login run completion")

	events := publisher.waitForEvents(1, time.Second)
	if got, want := events[0].Topic, "account-manager.logins.google"; got != want {
		t.Fatalf("projection topic = %q, want %q", got, want)
	}
}

type fakeTaskControl struct {
	mu     sync.Mutex
	reqs   []CreateTaskRequest
	notify chan struct{}
}

func newFakeTaskControl() *fakeTaskControl {
	return &fakeTaskControl{notify: make(chan struct{})}
}

func (f *fakeTaskControl) CreateTask(_ context.Context, req CreateTaskRequest) (*domain.Task, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	index := len(f.reqs)
	old := f.notify
	f.notify = make(chan struct{})
	f.mu.Unlock()
	close(old)

	return &domain.Task{
		ID:             domain.TaskID(fmt.Sprintf("task-%d", index)),
		Goal:           req.Goal,
		InputArtifacts: req.InputArtifacts,
		AssignedDevice: req.DeviceID,
		WorkflowName:   req.WorkflowName,
		Status:         domain.TaskStatusRunning,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}, nil
}

func (f *fakeTaskControl) ListTasks(context.Context, appport.ListTaskQuery) ([]appport.TaskSummary, error) {
	return nil, nil
}

func (f *fakeTaskControl) GetTask(context.Context, domain.TaskID) (*domain.Task, error) {
	return nil, nil
}

func (f *fakeTaskControl) GetTaskSummary(context.Context, domain.TaskID) (*appport.TaskSummary, error) {
	return nil, nil
}

func (f *fakeTaskControl) CancelTask(context.Context, domain.TaskID) error {
	return nil
}

func (f *fakeTaskControl) waitForCreates(n int, timeout time.Duration) []CreateTaskRequest {
	deadline := time.Now().Add(timeout)
	for {
		f.mu.Lock()
		if len(f.reqs) >= n {
			out := append([]CreateTaskRequest(nil), f.reqs...)
			f.mu.Unlock()
			return out
		}
		ch := f.notify
		f.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		select {
		case <-ch:
		case <-time.After(remaining):
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CreateTaskRequest(nil), f.reqs...)
}

type fakeProjectionPublisher struct {
	mu     sync.Mutex
	events []ProjectionEvent
	notify chan struct{}
}

func newFakeProjectionPublisher() *fakeProjectionPublisher {
	return &fakeProjectionPublisher{notify: make(chan struct{})}
}

func (f *fakeProjectionPublisher) PublishProjection(event ProjectionEvent) {
	f.mu.Lock()
	f.events = append(f.events, event)
	old := f.notify
	f.notify = make(chan struct{})
	f.mu.Unlock()
	close(old)
}

func (f *fakeProjectionPublisher) waitForEvents(n int, timeout time.Duration) []ProjectionEvent {
	deadline := time.Now().Add(timeout)
	for {
		f.mu.Lock()
		if len(f.events) >= n {
			out := append([]ProjectionEvent(nil), f.events...)
			f.mu.Unlock()
			return out
		}
		ch := f.notify
		f.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		select {
		case <-ch:
		case <-time.After(remaining):
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ProjectionEvent(nil), f.events...)
}

func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool, label string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
