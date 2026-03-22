package campaigns

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/workflowruntime"
)

func TestPostCampaignService_Start_ManualCampaignPublishesAndRunsJob(t *testing.T) {
	origPoll := pollTaskUntilTerminal
	t.Cleanup(func() { pollTaskUntilTerminal = origPoll })
	pollTaskUntilTerminal = func(_ context.Context, _ workflowruntime.TaskSummaryReader, taskID domain.TaskID, _ time.Duration, _ *slog.Logger) (*workflowruntime.TaskSummary, bool) {
		return &workflowruntime.TaskSummary{
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
	account := domain.Account{
		ID:        "acct-1",
		Kind:      "instagram",
		DeviceID:  "device-1",
		Status:    domain.AccountStatusActive,
		CreatedAt: time.Now(),
	}
	if err := accounts.Save(account); err != nil {
		t.Fatalf("Save account: %v", err)
	}

	tasks := newFakeTaskControl()
	publisher := newFakeProjectionPublisher()
	service := NewPostCampaignService(tasks, accounts, nil, nil, nil, publisher, dir, newTestLogger())

	campaign, err := service.Start(context.Background(), StartPostCampaignInput{
		AccountIDs:  []string{account.ID},
		ImageSource: "manual",
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("fake-jpeg")),
		TextSource:  "manual",
		TextContent: "hello from campaign",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	localImagePath := filepath.Join(dir, "posts", campaign.ID+".jpg")
	if _, err := os.Stat(localImagePath); err != nil {
		t.Fatalf("expected saved image at %s: %v", localImagePath, err)
	}

	requests := tasks.waitForCreates(1, time.Second)
	if got, want := requests[0].WorkflowName, "instagram-post-script"; got != want {
		t.Fatalf("workflowName = %q, want %q", got, want)
	}
	if got := requests[0].InputArtifacts["caption"]; got != "hello from campaign" {
		t.Fatalf("caption artifact = %q", got)
	}
	if got := requests[0].InputArtifacts["imagePath"]; got == "" {
		t.Fatal("imagePath artifact is empty")
	}

	waitForCondition(t, time.Second, func() bool {
		current, ok := service.Get(campaign.ID)
		return ok && current.Status == "done" && len(current.Jobs) == 1 && current.Jobs[0].Status == "done"
	}, "post campaign completion")

	events := publisher.waitForEvents(1, time.Second)
	if got, want := events[0].Topic, "campaigns.posts"; got != want {
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

func (f *fakeTaskControl) ListTasks(context.Context, workflowruntime.ListTaskQuery) ([]workflowruntime.TaskSummary, error) {
	return nil, nil
}

func (f *fakeTaskControl) GetTask(context.Context, domain.TaskID) (*domain.Task, error) {
	return nil, nil
}

func (f *fakeTaskControl) GetTaskSummary(context.Context, domain.TaskID) (*workflowruntime.TaskSummary, error) {
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
