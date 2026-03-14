package eventruntime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
)

type fakeAcceptedProcessor struct {
	events []domain.Event
	err    error
}

func (p *fakeAcceptedProcessor) ProcessAcceptedEvent(_ context.Context, e domain.Event) error {
	p.events = append(p.events, e)
	return p.err
}

type setNXCall struct {
	key   string
	value any
	exp   time.Duration
}

type evalCall struct {
	script string
	keys   []string
	args   []any
}

type xackCall struct {
	stream string
	group  string
	ids    []string
}

type fakeRedisStreamClient struct {
	setNXCalls      []setNXCall
	evalCalls       []evalCall
	xreadGroupCalls []*redis.XReadGroupArgs
	xautoClaimCalls []*redis.XAutoClaimArgs
	xackCalls       []xackCall
	xinfoGroupCalls []string

	setNXFn       func(key string, value any, exp time.Duration) (bool, error)
	evalIntFn     func(script string, keys []string, args ...any) (int64, error)
	xinfoGroupsFn func(stream string) ([]redis.XInfoGroup, error)
	xreadGroupFn  func(args *redis.XReadGroupArgs) ([]redis.XStream, error)
	xautoClaimFn  func(args *redis.XAutoClaimArgs) ([]redis.XMessage, string, error)
}

func (f *fakeRedisStreamClient) Ping(context.Context) error { return nil }

func (f *fakeRedisStreamClient) XAdd(context.Context, *redis.XAddArgs) error { return nil }

func (f *fakeRedisStreamClient) XGroupCreateMkStream(context.Context, string, string, string) error {
	return nil
}

func (f *fakeRedisStreamClient) XInfoGroups(_ context.Context, stream string) ([]redis.XInfoGroup, error) {
	f.xinfoGroupCalls = append(f.xinfoGroupCalls, stream)
	if f.xinfoGroupsFn != nil {
		return f.xinfoGroupsFn(stream)
	}
	return nil, nil
}

func (f *fakeRedisStreamClient) XReadGroup(_ context.Context, args *redis.XReadGroupArgs) ([]redis.XStream, error) {
	f.xreadGroupCalls = append(f.xreadGroupCalls, cloneReadGroupArgs(args))
	if f.xreadGroupFn != nil {
		return f.xreadGroupFn(args)
	}
	return nil, redis.Nil
}

func (f *fakeRedisStreamClient) XAck(_ context.Context, stream, group string, ids ...string) error {
	f.xackCalls = append(f.xackCalls, xackCall{
		stream: stream,
		group:  group,
		ids:    append([]string(nil), ids...),
	})
	return nil
}

func (f *fakeRedisStreamClient) XAutoClaim(_ context.Context, args *redis.XAutoClaimArgs) ([]redis.XMessage, string, error) {
	f.xautoClaimCalls = append(f.xautoClaimCalls, cloneAutoClaimArgs(args))
	if f.xautoClaimFn != nil {
		return f.xautoClaimFn(args)
	}
	return nil, "0-0", nil
}

func (f *fakeRedisStreamClient) SetNX(_ context.Context, key string, value any, exp time.Duration) (bool, error) {
	f.setNXCalls = append(f.setNXCalls, setNXCall{key: key, value: value, exp: exp})
	if f.setNXFn != nil {
		return f.setNXFn(key, value, exp)
	}
	return false, nil
}

func (f *fakeRedisStreamClient) EvalInt(_ context.Context, script string, keys []string, args ...any) (int64, error) {
	f.evalCalls = append(f.evalCalls, evalCall{
		script: script,
		keys:   append([]string(nil), keys...),
		args:   append([]any(nil), args...),
	})
	if f.evalIntFn != nil {
		return f.evalIntFn(script, keys, args...)
	}
	return 0, nil
}

func TestRedisStreamsBusClaimOrRenewPartitionLease_AcquiresOwnership(t *testing.T) {
	client := &fakeRedisStreamClient{
		setNXFn: func(_ string, _ any, _ time.Duration) (bool, error) {
			return true, nil
		},
	}
	bus, err := newRedisStreamsBusWithClient(RedisStreamsConfig{
		Addr:           "unused",
		ConsumerPrefix: "worker",
		InstanceID:     "inst-1",
		LeaseTTL:       5 * time.Second,
	}, client, testLog())
	if err != nil {
		t.Fatalf("newRedisStreamsBusWithClient: %v", err)
	}

	owned, err := bus.claimOrRenewPartitionLease(context.Background(), 2)
	if err != nil {
		t.Fatalf("claimOrRenewPartitionLease: %v", err)
	}
	if !owned {
		t.Fatal("expected lease ownership")
	}
	if len(client.setNXCalls) != 1 {
		t.Fatalf("expected one SetNX call, got %d", len(client.setNXCalls))
	}
	call := client.setNXCalls[0]
	if call.key != "workflow.wakeup.p02.owner" {
		t.Fatalf("unexpected lease key: %q", call.key)
	}
	if call.value != "worker-inst-1-p02" {
		t.Fatalf("unexpected lease owner: %v", call.value)
	}
	if call.exp != 5*time.Second {
		t.Fatalf("unexpected lease ttl: %s", call.exp)
	}
}

func TestRedisStreamsBusClaimOrRenewPartitionLease_RenewsCurrentOwner(t *testing.T) {
	client := &fakeRedisStreamClient{
		setNXFn: func(_ string, _ any, _ time.Duration) (bool, error) {
			return false, nil
		},
		evalIntFn: func(script string, keys []string, args ...any) (int64, error) {
			if script != renewLeaseScript {
				t.Fatalf("unexpected renew script")
			}
			if len(keys) != 1 || keys[0] != "workflow.wakeup.p01.owner" {
				t.Fatalf("unexpected keys: %#v", keys)
			}
			if len(args) != 2 || args[0] != "worker-inst-2-p01" {
				t.Fatalf("unexpected args: %#v", args)
			}
			return 1, nil
		},
	}
	bus, err := newRedisStreamsBusWithClient(RedisStreamsConfig{
		Addr:           "unused",
		ConsumerPrefix: "worker",
		InstanceID:     "inst-2",
		LeaseTTL:       7 * time.Second,
	}, client, testLog())
	if err != nil {
		t.Fatalf("newRedisStreamsBusWithClient: %v", err)
	}

	owned, err := bus.claimOrRenewPartitionLease(context.Background(), 1)
	if err != nil {
		t.Fatalf("claimOrRenewPartitionLease: %v", err)
	}
	if !owned {
		t.Fatal("expected renew to keep ownership")
	}
}

func TestRedisStreamsBusProcessPendingForConsumer_AcksProcessedMessage(t *testing.T) {
	event := domain.Event{
		ID:         "tool-result:1",
		Kind:       domain.EventKindToolResult,
		DeviceID:   "dev-1",
		OccurredAt: time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
		Payload:    json.RawMessage(`{"ok":true}`),
	}
	client := &fakeRedisStreamClient{}
	readCount := 0
	client.xreadGroupFn = func(args *redis.XReadGroupArgs) ([]redis.XStream, error) {
		readCount++
		if args.Streams[1] != "0" {
			t.Fatalf("expected pending read with stream id 0, got %#v", args.Streams)
		}
		if readCount > 1 {
			return nil, redis.Nil
		}
		return []redis.XStream{{
			Stream: "workflow.wakeup.p00",
			Messages: []redis.XMessage{{
				ID:     "1710400000000-0",
				Values: map[string]any{"body": string(mustMarshal(t, newStreamEvent(event)))},
			}},
		}}, nil
	}
	bus, err := newRedisStreamsBusWithClient(RedisStreamsConfig{
		Addr:           "unused",
		ConsumerPrefix: "worker",
		InstanceID:     "inst-pending",
	}, client, testLog())
	if err != nil {
		t.Fatalf("newRedisStreamsBusWithClient: %v", err)
	}
	processor := &fakeAcceptedProcessor{}

	handled, err := bus.processPendingForConsumer(context.Background(), "workflow.wakeup.p00", bus.partitionConsumer(0), processor)
	if err != nil {
		t.Fatalf("processPendingForConsumer: %v", err)
	}
	if !handled {
		t.Fatal("expected pending message to be handled")
	}
	if len(processor.events) != 1 || processor.events[0].ID != event.ID {
		t.Fatalf("unexpected processed events: %#v", processor.events)
	}
	if len(client.xackCalls) != 1 {
		t.Fatalf("expected one ack call, got %d", len(client.xackCalls))
	}
	if client.xackCalls[0].ids[0] != "1710400000000-0" {
		t.Fatalf("unexpected ack id: %#v", client.xackCalls[0].ids)
	}
}

func TestRedisStreamsBusClaimIdlePending_ClaimsAndProcessesIdleMessage(t *testing.T) {
	event := domain.Event{
		ID:         "tool-result:2",
		Kind:       domain.EventKindToolResult,
		DeviceID:   "dev-2",
		OccurredAt: time.Date(2026, 3, 14, 11, 0, 0, 0, time.UTC),
		Payload:    json.RawMessage(`{"ok":true}`),
	}
	client := &fakeRedisStreamClient{}
	client.xautoClaimFn = func(args *redis.XAutoClaimArgs) ([]redis.XMessage, string, error) {
		if args.Start != "0-0" {
			t.Fatalf("expected initial claim cursor 0-0, got %q", args.Start)
		}
		return []redis.XMessage{{
			ID:     "1710403600000-0",
			Values: map[string]any{"body": string(mustMarshal(t, newStreamEvent(event)))},
		}}, "0-0", nil
	}
	bus, err := newRedisStreamsBusWithClient(RedisStreamsConfig{
		Addr:              "unused",
		ConsumerPrefix:    "worker",
		InstanceID:        "inst-claim",
		PendingIdle:       30 * time.Second,
		PendingClaimCount: 8,
	}, client, testLog())
	if err != nil {
		t.Fatalf("newRedisStreamsBusWithClient: %v", err)
	}
	processor := &fakeAcceptedProcessor{}

	handled, err := bus.claimIdlePending(context.Background(), "workflow.wakeup.p01", bus.partitionConsumer(1), processor)
	if err != nil {
		t.Fatalf("claimIdlePending: %v", err)
	}
	if !handled {
		t.Fatal("expected claimed idle pending message to be handled")
	}
	if len(client.xautoClaimCalls) != 1 {
		t.Fatalf("expected one XAutoClaim call, got %d", len(client.xautoClaimCalls))
	}
	call := client.xautoClaimCalls[0]
	if call.MinIdle != 30*time.Second {
		t.Fatalf("unexpected min idle: %s", call.MinIdle)
	}
	if call.Count != 8 {
		t.Fatalf("unexpected claim count: %d", call.Count)
	}
	if len(client.xackCalls) != 1 {
		t.Fatalf("expected one ack after claim, got %d", len(client.xackCalls))
	}
}

func TestRedisStreamsBusHandleMessage_LeavesPendingOnProcessorError(t *testing.T) {
	event := domain.Event{
		ID:         "tool-result:3",
		Kind:       domain.EventKindToolResult,
		DeviceID:   "dev-3",
		OccurredAt: time.Now().UTC(),
	}
	client := &fakeRedisStreamClient{}
	bus, err := newRedisStreamsBusWithClient(RedisStreamsConfig{
		Addr:           "unused",
		ConsumerPrefix: "worker",
		InstanceID:     "inst-error",
	}, client, testLog())
	if err != nil {
		t.Fatalf("newRedisStreamsBusWithClient: %v", err)
	}
	processor := &fakeAcceptedProcessor{err: errors.New("boom")}

	err = bus.handleMessage(context.Background(), "workflow.wakeup.p03", redis.XMessage{
		ID:     "1710407200000-0",
		Values: map[string]any{"body": string(mustMarshal(t, newStreamEvent(event)))},
	}, processor)
	if err == nil {
		t.Fatal("expected processor error")
	}
	if len(client.xackCalls) != 0 {
		t.Fatalf("expected no ack on processor error, got %d", len(client.xackCalls))
	}
}

func TestRedisStreamsBusMaintainPartitionLease_RecordsLeaseLost(t *testing.T) {
	client := &fakeRedisStreamClient{
		setNXFn: func(_ string, _ any, _ time.Duration) (bool, error) {
			return false, nil
		},
		evalIntFn: func(_ string, _ []string, _ ...any) (int64, error) {
			return 0, nil
		},
	}
	metrics := telemetry.NewRegistry()
	bus, err := newRedisStreamsBusWithClient(RedisStreamsConfig{
		Addr:           "unused",
		ConsumerPrefix: "worker",
		InstanceID:     "inst-lease-lost",
		LeaseTTL:       30 * time.Millisecond,
	}, client, testLog(), metrics)
	if err != nil {
		t.Fatalf("newRedisStreamsBusWithClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lostLease := make(chan struct{}, 1)
	go bus.maintainPartitionLease(ctx, 0, lostLease)

	select {
	case <-lostLease:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected lost lease signal")
	}

	if got := metrics.Snapshot().LeaseLost; got != 1 {
		t.Fatalf("expected lease lost metric 1, got %d", got)
	}
}

func TestRedisStreamsBusSampleWakeupQueueDepth_SumsLagAndPending(t *testing.T) {
	client := &fakeRedisStreamClient{
		xinfoGroupsFn: func(stream string) ([]redis.XInfoGroup, error) {
			switch stream {
			case "workflow.wakeup.p00":
				return []redis.XInfoGroup{
					{Name: "other-group", Lag: 999, Pending: 999},
					{Name: "server-agent", Lag: 4, Pending: 2},
				}, nil
			case "workflow.wakeup.p01":
				return []redis.XInfoGroup{
					{Name: "server-agent", Lag: -1, Pending: 3},
				}, nil
			default:
				return nil, nil
			}
		},
	}
	metrics := telemetry.NewRegistry()
	bus, err := newRedisStreamsBusWithClient(RedisStreamsConfig{
		Addr:       "unused",
		Partitions: 2,
		Group:      "server-agent",
	}, client, testLog(), metrics)
	if err != nil {
		t.Fatalf("newRedisStreamsBusWithClient: %v", err)
	}

	if err := bus.sampleWakeupQueueDepth(context.Background()); err != nil {
		t.Fatalf("sampleWakeupQueueDepth: %v", err)
	}
	snap := metrics.Snapshot()
	if got := snap.WorkflowWakeupQueueDepth; got != 9 {
		t.Fatalf("expected queue depth 9, got %d", got)
	}
	if snap.WakeupPartitionDepth["p00"] != 6 || snap.WakeupPartitionDepth["p01"] != 3 {
		t.Fatalf("unexpected per-partition depth snapshot: %#v", snap.WakeupPartitionDepth)
	}
	if len(client.xinfoGroupCalls) != 2 {
		t.Fatalf("expected 2 XInfoGroups calls, got %d", len(client.xinfoGroupCalls))
	}
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func cloneReadGroupArgs(args *redis.XReadGroupArgs) *redis.XReadGroupArgs {
	if args == nil {
		return nil
	}
	cp := *args
	cp.Streams = append([]string(nil), args.Streams...)
	return &cp
}

func cloneAutoClaimArgs(args *redis.XAutoClaimArgs) *redis.XAutoClaimArgs {
	if args == nil {
		return nil
	}
	cp := *args
	return &cp
}

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
