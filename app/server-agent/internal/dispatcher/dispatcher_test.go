package dispatcher_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
)

// fakeSender implements registry.Sender, recording calls.
type fakeSender struct{ sent []string }

func (f *fakeSender) SendRequest(id, method string, _ any) error {
	f.sent = append(f.sent, id+":"+method)
	return nil
}
func (f *fakeSender) SendSuccess(_ string, _ any) error          { return nil }
func (f *fakeSender) SendError(_ string, _ int, _ string) error  { return nil }
func (f *fakeSender) Close() error                               { return nil }

// fakeRegistry always returns the given sender for any device lookup.
type fakeRegistry struct {
	sender registry.Sender
}

func (r *fakeRegistry) Add(_ *domain.Session, _ registry.Sender) error { return nil }
func (r *fakeRegistry) Remove(_ domain.SessionID)                       {}
func (r *fakeRegistry) GetBySession(_ domain.SessionID) (*domain.Session, registry.Sender, bool) {
	return nil, nil, false
}
func (r *fakeRegistry) GetByDevice(_ domain.DeviceID) (*domain.Session, registry.Sender, bool) {
	return &domain.Session{}, r.sender, true
}

func TestDispatch_ReceivesResult(t *testing.T) {
	sender := &fakeSender{}
	d := dispatcher.NewMemoryDispatcher(&fakeRegistry{sender: sender})

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
	d := dispatcher.NewMemoryDispatcher(&fakeRegistry{sender: &fakeSender{}})

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
