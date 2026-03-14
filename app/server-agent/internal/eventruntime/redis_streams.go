package eventruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

const (
	acceptedStreamName       = "events.accepted"
	deadLetterStreamName     = "events.deadletter"
	defaultRedisGroup        = "server-agent"
	defaultBlockTimeout      = 2 * time.Second
	pendingProbeTimeout      = 10 * time.Millisecond
	defaultLeaseTTL          = 15 * time.Second
	defaultPendingIdle       = 45 * time.Second
	defaultPendingClaimCount = int64(16)
	defaultOwnershipRetry    = 500 * time.Millisecond
)

const (
	renewLeaseScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`
	releaseLeaseScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`
)

// Bus is the external event-bus seam used by the queued runtime.
type Bus interface {
	PublishAccepted(ctx context.Context, record domain.AcceptedEventRecord) error
	PublishWakeup(ctx context.Context, event domain.Event) error
	PublishDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error
	Start(ctx context.Context, processor AcceptedEventProcessor) error
}

type RedisStreamsConfig struct {
	Addr              string
	Password          string
	DB                int
	Partitions        int
	Group             string
	ConsumerPrefix    string
	BlockTimeout      time.Duration
	InstanceID        string
	LeaseTTL          time.Duration
	PendingIdle       time.Duration
	PendingClaimCount int64
	OwnershipRetry    time.Duration
}

type redisStreamClient interface {
	Ping(ctx context.Context) error
	XAdd(ctx context.Context, args *redis.XAddArgs) error
	XGroupCreateMkStream(ctx context.Context, stream, group, start string) error
	XReadGroup(ctx context.Context, args *redis.XReadGroupArgs) ([]redis.XStream, error)
	XAck(ctx context.Context, stream, group string, ids ...string) error
	XAutoClaim(ctx context.Context, args *redis.XAutoClaimArgs) ([]redis.XMessage, string, error)
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error)
	EvalInt(ctx context.Context, script string, keys []string, args ...any) (int64, error)
}

type goRedisClient struct {
	client *redis.Client
}

func (c *goRedisClient) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *goRedisClient) XAdd(ctx context.Context, args *redis.XAddArgs) error {
	return c.client.XAdd(ctx, args).Err()
}

func (c *goRedisClient) XGroupCreateMkStream(ctx context.Context, stream, group, start string) error {
	return c.client.XGroupCreateMkStream(ctx, stream, group, start).Err()
}

func (c *goRedisClient) XReadGroup(ctx context.Context, args *redis.XReadGroupArgs) ([]redis.XStream, error) {
	return c.client.XReadGroup(ctx, args).Result()
}

func (c *goRedisClient) XAck(ctx context.Context, stream, group string, ids ...string) error {
	return c.client.XAck(ctx, stream, group, ids...).Err()
}

func (c *goRedisClient) XAutoClaim(ctx context.Context, args *redis.XAutoClaimArgs) ([]redis.XMessage, string, error) {
	return c.client.XAutoClaim(ctx, args).Result()
}

func (c *goRedisClient) SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error) {
	return c.client.SetNX(ctx, key, value, expiration).Result()
}

func (c *goRedisClient) EvalInt(ctx context.Context, script string, keys []string, args ...any) (int64, error) {
	return c.client.Eval(ctx, script, keys, args...).Int64()
}

type RedisStreamsBus struct {
	client            redisStreamClient
	partitions        int
	group             string
	consumerPrefix    string
	instanceID        string
	blockTimeout      time.Duration
	leaseTTL          time.Duration
	pendingIdle       time.Duration
	pendingClaimCount int64
	ownershipRetry    time.Duration
	log               *slog.Logger
}

func NewRedisStreamsBus(cfg RedisStreamsConfig, log *slog.Logger) (*RedisStreamsBus, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, fmt.Errorf("redis addr is required")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	return newRedisStreamsBusWithClient(cfg, &goRedisClient{client: client}, log)
}

func newRedisStreamsBusWithClient(
	cfg RedisStreamsConfig,
	client redisStreamClient,
	log *slog.Logger,
) (*RedisStreamsBus, error) {
	if cfg.Partitions <= 0 {
		cfg.Partitions = 8
	}
	if strings.TrimSpace(cfg.Group) == "" {
		cfg.Group = defaultRedisGroup
	}
	if strings.TrimSpace(cfg.ConsumerPrefix) == "" {
		cfg.ConsumerPrefix = defaultRedisGroup
	}
	if cfg.BlockTimeout <= 0 {
		cfg.BlockTimeout = defaultBlockTimeout
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = defaultLeaseTTL
	}
	if cfg.PendingIdle <= 0 {
		cfg.PendingIdle = defaultPendingIdle
	}
	if cfg.PendingClaimCount <= 0 {
		cfg.PendingClaimCount = defaultPendingClaimCount
	}
	if cfg.OwnershipRetry <= 0 {
		cfg.OwnershipRetry = defaultOwnershipRetry
	}
	instanceID := sanitizeIdentifier(cfg.InstanceID)
	if instanceID == "" {
		instanceID = defaultInstanceID()
	}

	return &RedisStreamsBus{
		client:            client,
		partitions:        cfg.Partitions,
		group:             cfg.Group,
		consumerPrefix:    cfg.ConsumerPrefix,
		instanceID:        instanceID,
		blockTimeout:      cfg.BlockTimeout,
		leaseTTL:          cfg.LeaseTTL,
		pendingIdle:       cfg.PendingIdle,
		pendingClaimCount: cfg.PendingClaimCount,
		ownershipRetry:    cfg.OwnershipRetry,
		log:               log,
	}, nil
}

func (b *RedisStreamsBus) PublishAccepted(ctx context.Context, record domain.AcceptedEventRecord) error {
	body, err := json.Marshal(streamAcceptedEnvelope{
		Event:      newStreamEvent(record.Event),
		AcceptedAt: record.AcceptedAt,
		Source:     record.Source,
	})
	if err != nil {
		return fmt.Errorf("marshal accepted event: %w", err)
	}
	return b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: acceptedStreamName,
		Values: map[string]any{
			"body":      string(body),
			"event_id":  record.Event.ID,
			"device_id": string(record.Event.DeviceID),
			"kind":      string(record.Event.Kind),
			"seq_no":    record.Event.SeqNo,
			"source":    record.Source,
		},
	})
}

func (b *RedisStreamsBus) PublishWakeup(ctx context.Context, event domain.Event) error {
	body, err := json.Marshal(newStreamEvent(event))
	if err != nil {
		return fmt.Errorf("marshal wakeup event: %w", err)
	}
	return b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: b.wakeupStream(event.DeviceID),
		Values: map[string]any{
			"body":      string(body),
			"event_id":  event.ID,
			"device_id": string(event.DeviceID),
			"kind":      string(event.Kind),
			"seq_no":    event.SeqNo,
		},
	})
}

func (b *RedisStreamsBus) PublishDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error {
	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal dead letter: %w", err)
	}
	return b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: deadLetterStreamName,
		Values: map[string]any{
			"body":           string(body),
			"dead_letter_id": record.ID,
			"event_id":       record.EventID,
			"device_id":      string(record.DeviceID),
			"kind":           string(record.Kind),
			"seq_no":         record.SeqNo,
		},
	})
}

func (b *RedisStreamsBus) Start(ctx context.Context, processor AcceptedEventProcessor) error {
	if err := b.client.Ping(ctx); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	for partition := 0; partition < b.partitions; partition++ {
		if err := b.ensureWakeupGroup(ctx, partition); err != nil {
			return err
		}
	}
	for partition := 0; partition < b.partitions; partition++ {
		go b.runPartitionWorker(ctx, partition, processor)
	}
	return nil
}

func (b *RedisStreamsBus) runPartitionWorker(
	ctx context.Context,
	partition int,
	processor AcceptedEventProcessor,
) {
	stream := b.partitionStream(partition)
	consumer := b.partitionConsumer(partition)

	for {
		if ctx.Err() != nil {
			return
		}

		owned, err := b.claimOrRenewPartitionLease(ctx, partition)
		if err != nil {
			b.log.Warn("acquire partition lease failed",
				"stream", stream,
				"consumer", consumer,
				"err", err,
			)
			if !b.wait(ctx, b.ownershipRetry) {
				return
			}
			continue
		}
		if !owned {
			if !b.wait(ctx, b.ownershipRetry) {
				return
			}
			continue
		}

		leaseCtx, cancel := context.WithCancel(ctx)
		lostLease := make(chan struct{}, 1)
		go b.maintainPartitionLease(leaseCtx, partition, lostLease)

		err = b.consumePartition(leaseCtx, stream, consumer, processor, lostLease)
		cancel()
		b.releasePartitionLease(context.Background(), partition)

		if ctx.Err() != nil {
			return
		}
		if err != nil {
			b.log.Warn("consume partition failed",
				"stream", stream,
				"consumer", consumer,
				"err", err,
			)
		}
		if !b.wait(ctx, b.ownershipRetry) {
			return
		}
	}
}

func (b *RedisStreamsBus) consumePartition(
	ctx context.Context,
	stream string,
	consumer string,
	processor AcceptedEventProcessor,
	lostLease <-chan struct{},
) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		select {
		case <-lostLease:
			return nil
		default:
		}

		handled, err := b.processPendingForConsumer(ctx, stream, consumer, processor)
		if err != nil {
			return err
		}
		if handled {
			continue
		}

		handled, err = b.claimIdlePending(ctx, stream, consumer, processor)
		if err != nil {
			return err
		}
		if handled {
			continue
		}

		handled, err = b.readNewMessage(ctx, stream, consumer, processor)
		if err != nil {
			return err
		}
		if handled {
			continue
		}
	}
}

func (b *RedisStreamsBus) processPendingForConsumer(
	ctx context.Context,
	stream string,
	consumer string,
	processor AcceptedEventProcessor,
) (bool, error) {
	handledAny := false
	for {
		streams, err := b.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    b.group,
			Consumer: consumer,
			Streams:  []string{stream, "0"},
			Count:    1,
			Block:    pendingProbeTimeout,
		})
		if err != nil {
			if err == redis.Nil {
				return handledAny, nil
			}
			return handledAny, err
		}
		handled, err := b.processStreams(ctx, streams, processor)
		if err != nil {
			return handledAny, err
		}
		if !handled {
			return handledAny, nil
		}
		handledAny = true
	}
}

func (b *RedisStreamsBus) claimIdlePending(
	ctx context.Context,
	stream string,
	consumer string,
	processor AcceptedEventProcessor,
) (bool, error) {
	handledAny := false
	start := "0-0"
	for {
		messages, next, err := b.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   stream,
			Group:    b.group,
			Consumer: consumer,
			MinIdle:  b.pendingIdle,
			Start:    start,
			Count:    b.pendingClaimCount,
		})
		if err != nil {
			if err == redis.Nil {
				return handledAny, nil
			}
			return handledAny, err
		}
		if len(messages) == 0 {
			if next == "" || next == "0-0" || next == start {
				return handledAny, nil
			}
			start = next
			continue
		}
		for _, message := range messages {
			if err := b.handleMessage(ctx, stream, message, processor); err != nil {
				return handledAny, err
			}
		}
		handledAny = true
		if next == "" || next == "0-0" {
			return handledAny, nil
		}
		start = next
	}
}

func (b *RedisStreamsBus) readNewMessage(
	ctx context.Context,
	stream string,
	consumer string,
	processor AcceptedEventProcessor,
) (bool, error) {
	streams, err := b.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    b.group,
		Consumer: consumer,
		Streams:  []string{stream, ">"},
		Count:    1,
		Block:    b.blockTimeout,
	})
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	return b.processStreams(ctx, streams, processor)
}

func (b *RedisStreamsBus) processStreams(
	ctx context.Context,
	streams []redis.XStream,
	processor AcceptedEventProcessor,
) (bool, error) {
	handled := false
	for _, batch := range streams {
		for _, msg := range batch.Messages {
			handled = true
			if err := b.handleMessage(ctx, batch.Stream, msg, processor); err != nil {
				return true, err
			}
		}
	}
	return handled, nil
}

func (b *RedisStreamsBus) handleMessage(
	ctx context.Context,
	stream string,
	msg redis.XMessage,
	processor AcceptedEventProcessor,
) error {
	event, err := decodeStreamEvent(msg.Values["body"])
	if err != nil {
		return fmt.Errorf("decode wakeup message %s: %w", msg.ID, err)
	}
	if err := processor.ProcessAcceptedEvent(ctx, event); err != nil {
		return err
	}
	if err := b.client.XAck(ctx, stream, b.group, msg.ID); err != nil {
		return fmt.Errorf("ack wakeup %s: %w", msg.ID, err)
	}
	return nil
}

func (b *RedisStreamsBus) claimOrRenewPartitionLease(ctx context.Context, partition int) (bool, error) {
	key := b.partitionLeaseKey(partition)
	owner := b.partitionConsumer(partition)
	acquired, err := b.client.SetNX(ctx, key, owner, b.leaseTTL)
	if err != nil {
		return false, err
	}
	if acquired {
		return true, nil
	}
	renewed, err := b.client.EvalInt(ctx, renewLeaseScript, []string{key}, owner, int64(b.leaseTTL/time.Millisecond))
	if err != nil {
		return false, err
	}
	return renewed == 1, nil
}

func (b *RedisStreamsBus) maintainPartitionLease(
	ctx context.Context,
	partition int,
	lostLease chan<- struct{},
) {
	interval := b.leaseTTL / 3
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lastSuccess := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			owned, err := b.claimOrRenewPartitionLease(ctx, partition)
			if err != nil {
				b.log.Warn("renew partition lease failed",
					"stream", b.partitionStream(partition),
					"consumer", b.partitionConsumer(partition),
					"err", err,
				)
				if time.Since(lastSuccess) >= b.leaseTTL {
					signalLeaseLost(lostLease)
					return
				}
				continue
			}
			if !owned {
				signalLeaseLost(lostLease)
				return
			}
			lastSuccess = time.Now()
		}
	}
}

func (b *RedisStreamsBus) releasePartitionLease(ctx context.Context, partition int) {
	key := b.partitionLeaseKey(partition)
	owner := b.partitionConsumer(partition)
	if _, err := b.client.EvalInt(ctx, releaseLeaseScript, []string{key}, owner); err != nil {
		b.log.Warn("release partition lease failed",
			"stream", b.partitionStream(partition),
			"consumer", owner,
			"err", err,
		)
	}
}

func signalLeaseLost(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (b *RedisStreamsBus) wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		d = defaultOwnershipRetry
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (b *RedisStreamsBus) ensureWakeupGroup(ctx context.Context, partition int) error {
	stream := b.partitionStream(partition)
	err := b.client.XGroupCreateMkStream(ctx, stream, b.group, "0")
	if err == nil || strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return fmt.Errorf("create consumer group for %s: %w", stream, err)
}

func (b *RedisStreamsBus) wakeupStream(deviceID domain.DeviceID) string {
	return b.partitionStream(hashDevice(deviceID, b.partitions))
}

func (b *RedisStreamsBus) partitionStream(partition int) string {
	return fmt.Sprintf("workflow.wakeup.p%02d", partition)
}

func (b *RedisStreamsBus) partitionLeaseKey(partition int) string {
	return b.partitionStream(partition) + ".owner"
}

func (b *RedisStreamsBus) partitionConsumer(partition int) string {
	return fmt.Sprintf("%s-%s-p%02d", b.consumerPrefix, b.instanceID, partition)
}

func hashDevice(deviceID domain.DeviceID, partitions int) int {
	if partitions <= 1 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(deviceID))
	return int(h.Sum32() % uint32(partitions))
}

type streamEventEnvelope struct {
	ID         string           `json:"id"`
	Kind       domain.EventKind `json:"kind"`
	DeviceID   domain.DeviceID  `json:"deviceId"`
	SeqNo      uint64           `json:"seqNo"`
	OccurredAt time.Time        `json:"occurredAt"`
	Payload    json.RawMessage  `json:"payload,omitempty"`
}

type streamAcceptedEnvelope struct {
	Event      streamEventEnvelope `json:"event"`
	AcceptedAt time.Time           `json:"acceptedAt"`
	Source     string              `json:"source"`
}

func newStreamEvent(event domain.Event) streamEventEnvelope {
	return streamEventEnvelope{
		ID:         event.ID,
		Kind:       event.Kind,
		DeviceID:   event.DeviceID,
		SeqNo:      event.SeqNo,
		OccurredAt: event.OccurredAt,
		Payload:    domain.MarshalEventPayload(event),
	}
}

func decodeStreamEvent(raw any) (domain.Event, error) {
	body, err := valueAsBytes(raw)
	if err != nil {
		return domain.Event{}, err
	}
	var envelope streamEventEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return domain.Event{}, err
	}
	return domain.Event{
		ID:         envelope.ID,
		Kind:       envelope.Kind,
		DeviceID:   envelope.DeviceID,
		SeqNo:      envelope.SeqNo,
		OccurredAt: envelope.OccurredAt,
		Payload:    envelope.Payload,
	}, nil
}

func valueAsBytes(value any) ([]byte, error) {
	switch v := value.(type) {
	case string:
		return []byte(v), nil
	case []byte:
		return append([]byte(nil), v...), nil
	default:
		return nil, fmt.Errorf("unsupported stream value type %T", value)
	}
}

func sanitizeIdentifier(value string) string {
	replacer := strings.NewReplacer(" ", "_", ":", "_", "/", "_")
	return replacer.Replace(strings.TrimSpace(value))
}

func defaultInstanceID() string {
	host, err := os.Hostname()
	if err != nil {
		host = "server-agent"
	}
	host = sanitizeIdentifier(host)
	if host == "" {
		host = "server-agent"
	}
	return fmt.Sprintf("%s-%d-%d", host, os.Getpid(), time.Now().UnixNano())
}
