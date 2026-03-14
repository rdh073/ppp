package dispatcher_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
)

// fakeSender implements registry.Sender, recording calls.
type fakeSender struct{ sent []string }

func (f *fakeSender) SendRequest(id, method string, _ any) error {
	f.sent = append(f.sent, id+":"+method)
	return nil
}
func (f *fakeSender) SendSuccess(_ string, _ any) error         { return nil }
func (f *fakeSender) SendError(_ string, _ int, _ string) error { return nil }
func (f *fakeSender) Close() error                              { return nil }

// fakeRegistry always returns the given sender for any device lookup.
type fakeRegistry struct {
	sender registry.Sender
}

func (r *fakeRegistry) Add(_ *domain.Session, _ registry.Sender) error { return nil }
func (r *fakeRegistry) Remove(_ domain.SessionID)                      {}
func (r *fakeRegistry) GetBySession(_ domain.SessionID) (*domain.Session, registry.Sender, bool) {
	return nil, nil, false
}
func (r *fakeRegistry) GetByDevice(_ domain.DeviceID) (*domain.Session, registry.Sender, bool) {
	return &domain.Session{}, r.sender, true
}

type failingSender struct{ err error }

func (f *failingSender) SendRequest(string, string, any) error { return f.err }
func (f *failingSender) SendSuccess(_ string, _ any) error     { return nil }
func (f *failingSender) SendError(_ string, _ int, _ string) error {
	return nil
}
func (f *failingSender) Close() error { return nil }

func TestDispatch_ReceivesResult(t *testing.T) {
	sender := &fakeSender{}
	d := dispatcher.NewMemoryDispatcher(&fakeRegistry{sender: sender}, nil)

	cmd := domain.Command{
		ID:       "cmd-1",
		Kind:     domain.CommandKindObserve,
		DeviceID: "dev-1",
		Params:   json.RawMessage(`{}`),
		IssuedAt: time.Now(),
	}

	ch, err := d.Dispatch(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	// Verify SendRequest was called.
	if len(sender.sent) != 1 || sender.sent[0] != "cmd-1:device.observe" {
		t.Errorf("unexpected sent calls: %v", sender.sent)
	}

	// Simulate agent responding from another goroutine.
	go func() {
		time.Sleep(10 * time.Millisecond)
		d.DeliverResponse(domain.CommandResult{
			CommandID: "cmd-1",
			Success:   true,
			Raw:       json.RawMessage(`{"ok":true}`),
		})
	}()

	select {
	case got := <-ch:
		if got.CommandID != "cmd-1" || !got.Success {
			t.Errorf("unexpected result: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
	}
}

func TestDispatch_DuplicateDeliveryIsNoop(t *testing.T) {
	d := dispatcher.NewMemoryDispatcher(&fakeRegistry{sender: &fakeSender{}}, nil)

	cmd := domain.Command{
		ID: "cmd-dup", Kind: domain.CommandKindObserve,
		DeviceID: "dev-1", Params: json.RawMessage(`{}`), IssuedAt: time.Now(),
	}

	ch, err := d.Dispatch(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}

	result := domain.CommandResult{CommandID: "cmd-dup", Success: true}
	d.DeliverResponse(result)
	// Second delivery must not panic or block.
	d.DeliverResponse(result)

	select {
	case got := <-ch:
		if got.CommandID != "cmd-dup" {
			t.Errorf("unexpected result: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestDispatch_PersistsCommandOutboxLifecycle(t *testing.T) {
	sender := &fakeSender{}
	outbox := store.NewMemoryCommandOutboxStore()
	d := dispatcher.NewMemoryDispatcher(&fakeRegistry{sender: sender}, outbox)

	cmd := domain.Command{
		ID:       "cmd-outbox",
		Kind:     domain.CommandKindObserve,
		DeviceID: "dev-1",
		Params:   json.RawMessage(`{}`),
		IssuedAt: time.Now(),
	}

	ch, err := d.Dispatch(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	d.DeliverResponse(domain.CommandResult{
		CommandID:  cmd.ID,
		DeviceID:   cmd.DeviceID,
		Success:    true,
		Raw:        json.RawMessage(`{"ok":true}`),
		ReceivedAt: time.Now(),
	})
	<-ch

	record, err := outbox.Get(context.Background(), cmd.ID)
	if err != nil {
		t.Fatalf("outbox Get: %v", err)
	}
	if record.Status != domain.CommandOutboxStatusResponded {
		t.Fatalf("expected responded status, got %s", record.Status)
	}
}

func TestDispatch_RecordsCommandInflightAndLatencyMetrics(t *testing.T) {
	sender := &fakeSender{}
	metrics := telemetry.NewRegistry()
	d := dispatcher.NewMemoryDispatcher(&fakeRegistry{sender: sender}, nil, metrics)

	issuedAt := time.Now().Add(-150 * time.Millisecond)
	cmd := domain.Command{
		ID:       "cmd-metric-success",
		Kind:     domain.CommandKindObserve,
		DeviceID: "dev-1",
		Params:   json.RawMessage(`{}`),
		IssuedAt: issuedAt,
	}

	ch, err := d.Dispatch(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if got := metrics.Snapshot().CommandInflight; got != 1 {
		t.Fatalf("expected command inflight 1 after dispatch, got %d", got)
	}

	d.DeliverResponse(domain.CommandResult{
		CommandID:  cmd.ID,
		DeviceID:   cmd.DeviceID,
		Success:    true,
		Raw:        json.RawMessage(`{"ok":true}`),
		ReceivedAt: issuedAt.Add(300 * time.Millisecond),
	})
	<-ch

	if got := metrics.Snapshot().CommandInflight; got != 0 {
		t.Fatalf("expected command inflight 0 after response, got %d", got)
	}
	body := metrics.RenderPrometheus()
	if !strings.Contains(body, `autosdk_server_command_duration_seconds_count{kind="device.observe",outcome="responded_success"} 1`) {
		t.Fatalf("expected command latency metric, got:\n%s", body)
	}
}

func TestDispatch_SendFailure_RecordsDispatchFailedMetricWithoutLeak(t *testing.T) {
	metrics := telemetry.NewRegistry()
	d := dispatcher.NewMemoryDispatcher(
		&fakeRegistry{sender: &failingSender{err: errors.New("send failed")}},
		nil,
		metrics,
	)

	cmd := domain.Command{
		ID:       "cmd-metric-fail",
		Kind:     domain.CommandKindExecute,
		DeviceID: "dev-1",
		Params:   json.RawMessage(`{}`),
		IssuedAt: time.Now().Add(-50 * time.Millisecond),
	}

	if _, err := d.Dispatch(context.Background(), cmd); err == nil {
		t.Fatal("expected dispatch error")
	}
	if got := metrics.Snapshot().CommandInflight; got != 0 {
		t.Fatalf("expected no inflight leak, got %d", got)
	}
	body := metrics.RenderPrometheus()
	if !strings.Contains(body, `autosdk_server_command_duration_seconds_count{kind="device.execute",outcome="dispatch_failed"} 1`) {
		t.Fatalf("expected dispatch_failed metric, got:\n%s", body)
	}
}

func TestDispatch_ContextTimeout_RecordsTimeoutMetricWithoutInflightLeak(t *testing.T) {
	sender := &fakeSender{}
	outbox := store.NewMemoryCommandOutboxStore()
	metrics := telemetry.NewRegistry()
	d := dispatcher.NewMemoryDispatcher(&fakeRegistry{sender: sender}, outbox, metrics)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	cmd := domain.Command{
		ID:       "cmd-timeout",
		Kind:     domain.CommandKindObserve,
		DeviceID: "dev-1",
		Params:   json.RawMessage(`{}`),
		IssuedAt: time.Now().Add(-20 * time.Millisecond),
	}

	ch, err := d.Dispatch(ctx, cmd)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if got := metrics.Snapshot().CommandInflight; got != 1 {
		t.Fatalf("expected command inflight 1 after dispatch, got %d", got)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel on timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inflight cancellation")
	}

	deadline := time.Now().Add(time.Second)
	for {
		record, err := outbox.Get(context.Background(), cmd.ID)
		if err != nil {
			t.Fatalf("outbox Get: %v", err)
		}
		if record.Status == domain.CommandOutboxStatusDispatchFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected dispatch_failed status after timeout, got %s", record.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := metrics.Snapshot().CommandInflight; got != 0 {
		t.Fatalf("expected no inflight leak after timeout, got %d", got)
	}
	body := metrics.RenderPrometheus()
	for _, needle := range []string{
		`autosdk_server_command_timeout_total{kind="device.observe"} 1`,
		`autosdk_server_command_duration_seconds_count{kind="device.observe",outcome="timed_out"} 1`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected timeout metrics in body, missing %q:\n%s", needle, body)
		}
	}
}
