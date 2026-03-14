package eventruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

const (
	acceptedStreamName   = "events.accepted"
	deadLetterStreamName = "events.deadletter"
	defaultRedisGroup    = "server-agent"
	defaultBlockTimeout  = 2 * time.Second
	pendingProbeTimeout  = 10 * time.Millisecond
)

// Bus is the external event-bus seam used by the queued runtime.
type Bus interface {
	PublishAccepted(ctx context.Context, record domain.AcceptedEventRecord) error
	PublishWakeup(ctx context.Context, event domain.Event) error
	PublishDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error
	Start(ctx context.Context, processor AcceptedEventProcessor) error
}

type RedisStreamsConfig struct {
	Addr           string
	Password       string
	DB             int
	Partitions     int
	Group          string
	ConsumerPrefix string
	BlockTimeout   time.Duration
}

type RedisStreamsBus struct {
	client         *redis.Client
	partitions     int
	group          string
	consumerPrefix string
	blockTimeout   time.Duration
	log            *slog.Logger
}

func NewRedisStreamsBus(cfg RedisStreamsConfig, log *slog.Logger) (*RedisStreamsBus, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, fmt.Errorf("redis addr is required")
	}
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
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	return &RedisStreamsBus{
		client:         client,
		partitions:     cfg.Partitions,
		group:          cfg.Group,
		consumerPrefix: cfg.ConsumerPrefix,
		blockTimeout:   cfg.BlockTimeout,
		log:            log,
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
	}).Err()
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
	}).Err()
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
	}).Err()
}

func (b *RedisStreamsBus) Start(ctx context.Context, processor AcceptedEventProcessor) error {
	if err := b.client.Ping(ctx).Err(); err != nil {
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

		handled, err := b.processOne(ctx, stream, consumer, "0", pendingProbeTimeout, processor)
		if err != nil {
			b.log.Warn("process pending wakeup failed",
				"stream", stream,
				"consumer", consumer,
				"err", err,
			)
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		if handled {
			continue
		}

		_, err = b.processOne(ctx, stream, consumer, ">", b.blockTimeout, processor)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.log.Warn("read wakeup failed", "stream", stream, "consumer", consumer, "err", err)
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
		}
	}
}

func (b *RedisStreamsBus) processOne(
	ctx context.Context,
	stream string,
	consumer string,
	streamID string,
	block time.Duration,
	processor AcceptedEventProcessor,
) (bool, error) {
	streams, err := b.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    b.group,
		Consumer: consumer,
		Streams:  []string{stream, streamID},
		Count:    1,
		Block:    block,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		return false, nil
	}

	msg := streams[0].Messages[0]
	event, err := decodeStreamEvent(msg.Values["body"])
	if err != nil {
		return true, fmt.Errorf("decode wakeup message %s: %w", msg.ID, err)
	}
	if err := processor.ProcessAcceptedEvent(ctx, event); err != nil {
		return true, err
	}
	if err := b.client.XAck(ctx, stream, b.group, msg.ID).Err(); err != nil {
		return true, fmt.Errorf("ack wakeup %s: %w", msg.ID, err)
	}
	return true, nil
}

func (b *RedisStreamsBus) ensureWakeupGroup(ctx context.Context, partition int) error {
	stream := b.partitionStream(partition)
	err := b.client.XGroupCreateMkStream(ctx, stream, b.group, "0").Err()
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

func (b *RedisStreamsBus) partitionConsumer(partition int) string {
	return fmt.Sprintf("%s-p%02d", b.consumerPrefix, partition)
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
