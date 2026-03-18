package telemetry

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

type WakeupFallbackPath string

const (
	WakeupFallbackIngress        WakeupFallbackPath = "ingress"
	WakeupFallbackAcceptedReplay WakeupFallbackPath = "accepted_replay"
	WakeupFallbackEmittedBatch   WakeupFallbackPath = "emitted_batch"
)

type ReplayPath string

const (
	ReplayPathAccepted            ReplayPath = "accepted"
	ReplayPathDeadLetterIngestion ReplayPath = "dead_letter_ingestion"
	ReplayPathDeadLetterAccepted  ReplayPath = "dead_letter_accepted_event"
)

type ReplayOutcome string

const (
	ReplayOutcomeAttempted ReplayOutcome = "attempted"
	ReplayOutcomeSucceeded ReplayOutcome = "succeeded"
	ReplayOutcomeFailed    ReplayOutcome = "failed"
)

type IngestSource string

const (
	IngestSourceDevice   IngestSource = "device"
	IngestSourceInternal IngestSource = "internal"
)

type WorkflowNodeOutcome string

const (
	WorkflowNodeOutcomeSuccess WorkflowNodeOutcome = "success"
	WorkflowNodeOutcomeFailure WorkflowNodeOutcome = "failure"
	WorkflowNodeOutcomePending WorkflowNodeOutcome = "pending"
	WorkflowNodeOutcomeDone    WorkflowNodeOutcome = "done"
	WorkflowNodeOutcomeError   WorkflowNodeOutcome = "error"
)

type ToolCallOutcome string

const (
	ToolCallOutcomeSuccess       ToolCallOutcome = "success"
	ToolCallOutcomeUnsupported   ToolCallOutcome = "unsupported"
	ToolCallOutcomeDisabled      ToolCallOutcome = "disabled"
	ToolCallOutcomeInvalidParams ToolCallOutcome = "invalid_params"
	ToolCallOutcomeInvalidResult ToolCallOutcome = "invalid_result"
	ToolCallOutcomeTimeout       ToolCallOutcome = "timeout"
	ToolCallOutcomeCanceled      ToolCallOutcome = "canceled"
	ToolCallOutcomeRetryable     ToolCallOutcome = "retryable"
	ToolCallOutcomeFailure       ToolCallOutcome = "failure"
)

type CommandOutcome string

const (
	CommandOutcomeRespondedSuccess CommandOutcome = "responded_success"
	CommandOutcomeRespondedError   CommandOutcome = "responded_error"
	CommandOutcomeDispatchFailed   CommandOutcome = "dispatch_failed"
	CommandOutcomeTimedOut         CommandOutcome = "timed_out"
	CommandOutcomeCanceled         CommandOutcome = "canceled"
)

type Registry struct {
	wakeupFallbackIngress        atomic.Uint64
	wakeupFallbackAcceptedReplay atomic.Uint64
	wakeupFallbackEmittedBatch   atomic.Uint64
	leaseLost                    atomic.Uint64

	replayAcceptedAttempted atomic.Uint64
	replayAcceptedSucceeded atomic.Uint64
	replayAcceptedFailed    atomic.Uint64

	replayDeadLetterIngestionAttempted atomic.Uint64
	replayDeadLetterIngestionSucceeded atomic.Uint64
	replayDeadLetterIngestionFailed    atomic.Uint64

	replayDeadLetterAcceptedAttempted atomic.Uint64
	replayDeadLetterAcceptedSucceeded atomic.Uint64
	replayDeadLetterAcceptedFailed    atomic.Uint64

	workflowWakeupQueueDepth atomic.Int64
	deviceLaneActive         atomic.Int64
	commandInflight          atomic.Int64
	accessibilityPending     atomic.Int64
	accessibilityBlocked     atomic.Int64

	partitionDepthMu sync.Mutex
	partitionDepth   map[string]int64

	commandTimeoutMu sync.Mutex
	commandTimeout   map[string]uint64

	accessibilityRemediationMu sync.Mutex
	accessibilityRemediation   map[string]uint64

	ingestLag            histogram
	workflowNodeDuration histogram
	toolCallDuration     histogram
	commandDuration      histogram
}

type Snapshot struct {
	WakeupFallback           map[WakeupFallbackPath]uint64
	LeaseLost                uint64
	Replay                   map[ReplayPath]map[ReplayOutcome]uint64
	WorkflowWakeupQueueDepth int64
	DeviceLaneActive         int64
	CommandInflight          int64
	WakeupPartitionDepth     map[string]int64
	CommandTimeout           map[string]uint64
	AccessibilityRemediation map[string]uint64
	AccessibilityPending     int64
	AccessibilityBlocked     int64
}

type histogram struct {
	buckets   []float64
	labelKeys []string

	mu     sync.Mutex
	series map[string]*histogramSeries
}

type histogramSeries struct {
	labels       []string
	bucketCounts []uint64
	sum          float64
	count        uint64
}

type histogramSnapshot struct {
	buckets   []float64
	labelKeys []string
	series    []histogramSeriesSnapshot
}

type histogramSeriesSnapshot struct {
	key          string
	labels       []string
	bucketCounts []uint64
	sum          float64
	count        uint64
}

func NewRegistry() *Registry {
	return &Registry{
		partitionDepth:            make(map[string]int64),
		commandTimeout:            make(map[string]uint64),
		accessibilityRemediation:  make(map[string]uint64),
		ingestLag: newHistogram(
			[]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
			"source",
		),
		workflowNodeDuration: newHistogram(
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
			"node",
			"outcome",
		),
		toolCallDuration: newHistogram(
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
			"tool",
			"outcome",
		),
		commandDuration: newHistogram(
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
			"kind",
			"outcome",
		),
	}
}

func (r *Registry) RecordWakeupFallback(path WakeupFallbackPath) {
	switch path {
	case WakeupFallbackIngress:
		r.wakeupFallbackIngress.Add(1)
	case WakeupFallbackAcceptedReplay:
		r.wakeupFallbackAcceptedReplay.Add(1)
	case WakeupFallbackEmittedBatch:
		r.wakeupFallbackEmittedBatch.Add(1)
	}
}

func (r *Registry) RecordLeaseLost() {
	r.leaseLost.Add(1)
}

func (r *Registry) RecordReplay(path ReplayPath, outcome ReplayOutcome) {
	switch path {
	case ReplayPathAccepted:
		switch outcome {
		case ReplayOutcomeAttempted:
			r.replayAcceptedAttempted.Add(1)
		case ReplayOutcomeSucceeded:
			r.replayAcceptedSucceeded.Add(1)
		case ReplayOutcomeFailed:
			r.replayAcceptedFailed.Add(1)
		}
	case ReplayPathDeadLetterIngestion:
		switch outcome {
		case ReplayOutcomeAttempted:
			r.replayDeadLetterIngestionAttempted.Add(1)
		case ReplayOutcomeSucceeded:
			r.replayDeadLetterIngestionSucceeded.Add(1)
		case ReplayOutcomeFailed:
			r.replayDeadLetterIngestionFailed.Add(1)
		}
	case ReplayPathDeadLetterAccepted:
		switch outcome {
		case ReplayOutcomeAttempted:
			r.replayDeadLetterAcceptedAttempted.Add(1)
		case ReplayOutcomeSucceeded:
			r.replayDeadLetterAcceptedSucceeded.Add(1)
		case ReplayOutcomeFailed:
			r.replayDeadLetterAcceptedFailed.Add(1)
		}
	}
}

func (r *Registry) SetWakeupQueueDepth(depth int64) {
	if depth < 0 {
		depth = 0
	}
	r.workflowWakeupQueueDepth.Store(depth)
}

func (r *Registry) SetWakeupPartitionDepth(partition string, depth int64) {
	partition = normalizeMetricLabel(partition, "unknown")
	if depth < 0 {
		depth = 0
	}
	r.partitionDepthMu.Lock()
	defer r.partitionDepthMu.Unlock()
	r.partitionDepth[partition] = depth
}

func (r *Registry) EnterDeviceLane() {
	addGauge(&r.deviceLaneActive, 1)
}

func (r *Registry) LeaveDeviceLane() {
	addGauge(&r.deviceLaneActive, -1)
}

func (r *Registry) IncCommandInflight() {
	addGauge(&r.commandInflight, 1)
}

func (r *Registry) DecCommandInflight() {
	addGauge(&r.commandInflight, -1)
}

func (r *Registry) ObserveIngestLag(source IngestSource, d time.Duration) {
	r.ingestLag.observe(d.Seconds(), normalizeMetricLabel(string(source), string(IngestSourceInternal)))
}

func (r *Registry) RecordAccessibilityRemediation(outcome string) {
	outcome = normalizeMetricLabel(outcome, string(domain.AccessibilityRemediationStatusIdle))
	r.accessibilityRemediationMu.Lock()
	defer r.accessibilityRemediationMu.Unlock()
	r.accessibilityRemediation[outcome]++
}

func (r *Registry) SetAccessibilityPendingBindings(n int64) {
	if n < 0 {
		n = 0
	}
	r.accessibilityPending.Store(n)
}

func (r *Registry) SetAccessibilityBlockedBindings(n int64) {
	if n < 0 {
		n = 0
	}
	r.accessibilityBlocked.Store(n)
}

func (r *Registry) ObserveWorkflowNode(node string, outcome WorkflowNodeOutcome, d time.Duration) {
	node = normalizeMetricLabel(node, "unknown")
	r.workflowNodeDuration.observe(d.Seconds(), node, normalizeMetricLabel(string(outcome), string(WorkflowNodeOutcomeError)))
}

func (r *Registry) ObserveToolCall(tool string, outcome ToolCallOutcome, d time.Duration) {
	tool = normalizeMetricLabel(tool, "unknown")
	r.toolCallDuration.observe(d.Seconds(), tool, normalizeMetricLabel(string(outcome), string(ToolCallOutcomeFailure)))
}

func (r *Registry) ObserveCommand(kind string, outcome CommandOutcome, d time.Duration) {
	kind = normalizeMetricLabel(kind, "unknown")
	r.commandDuration.observe(d.Seconds(), kind, normalizeMetricLabel(string(outcome), string(CommandOutcomeDispatchFailed)))
}

func (r *Registry) RecordCommandTimeout(kind string) {
	kind = normalizeMetricLabel(kind, "unknown")
	r.commandTimeoutMu.Lock()
	defer r.commandTimeoutMu.Unlock()
	r.commandTimeout[kind]++
}

func (r *Registry) Snapshot() Snapshot {
	r.partitionDepthMu.Lock()
	partitionDepth := make(map[string]int64, len(r.partitionDepth))
	for partition, depth := range r.partitionDepth {
		partitionDepth[partition] = depth
	}
	r.partitionDepthMu.Unlock()

	r.commandTimeoutMu.Lock()
	commandTimeout := make(map[string]uint64, len(r.commandTimeout))
	for kind, count := range r.commandTimeout {
		commandTimeout[kind] = count
	}
	r.commandTimeoutMu.Unlock()

	r.accessibilityRemediationMu.Lock()
	accessibilityRemediation := make(map[string]uint64, len(r.accessibilityRemediation))
	for outcome, count := range r.accessibilityRemediation {
		accessibilityRemediation[outcome] = count
	}
	r.accessibilityRemediationMu.Unlock()

	return Snapshot{
		WakeupFallback: map[WakeupFallbackPath]uint64{
			WakeupFallbackIngress:        r.wakeupFallbackIngress.Load(),
			WakeupFallbackAcceptedReplay: r.wakeupFallbackAcceptedReplay.Load(),
			WakeupFallbackEmittedBatch:   r.wakeupFallbackEmittedBatch.Load(),
		},
		LeaseLost:                r.leaseLost.Load(),
		WorkflowWakeupQueueDepth: r.workflowWakeupQueueDepth.Load(),
		DeviceLaneActive:         r.deviceLaneActive.Load(),
		CommandInflight:          r.commandInflight.Load(),
		WakeupPartitionDepth:     partitionDepth,
		CommandTimeout:           commandTimeout,
		AccessibilityRemediation: accessibilityRemediation,
		AccessibilityPending:     r.accessibilityPending.Load(),
		AccessibilityBlocked:     r.accessibilityBlocked.Load(),
		Replay: map[ReplayPath]map[ReplayOutcome]uint64{
			ReplayPathAccepted: {
				ReplayOutcomeAttempted: r.replayAcceptedAttempted.Load(),
				ReplayOutcomeSucceeded: r.replayAcceptedSucceeded.Load(),
				ReplayOutcomeFailed:    r.replayAcceptedFailed.Load(),
			},
			ReplayPathDeadLetterIngestion: {
				ReplayOutcomeAttempted: r.replayDeadLetterIngestionAttempted.Load(),
				ReplayOutcomeSucceeded: r.replayDeadLetterIngestionSucceeded.Load(),
				ReplayOutcomeFailed:    r.replayDeadLetterIngestionFailed.Load(),
			},
			ReplayPathDeadLetterAccepted: {
				ReplayOutcomeAttempted: r.replayDeadLetterAcceptedAttempted.Load(),
				ReplayOutcomeSucceeded: r.replayDeadLetterAcceptedSucceeded.Load(),
				ReplayOutcomeFailed:    r.replayDeadLetterAcceptedFailed.Load(),
			},
		},
	}
}

func (r *Registry) RenderPrometheus() string {
	snap := r.Snapshot()
	var b strings.Builder

	b.WriteString("# HELP autosdk_server_wakeup_publish_fallback_total Number of wakeup publish fallbacks.\n")
	b.WriteString("# TYPE autosdk_server_wakeup_publish_fallback_total counter\n")
	fmt.Fprintf(&b, "autosdk_server_wakeup_publish_fallback_total{path=%q} %d\n", WakeupFallbackIngress, snap.WakeupFallback[WakeupFallbackIngress])
	fmt.Fprintf(&b, "autosdk_server_wakeup_publish_fallback_total{path=%q} %d\n", WakeupFallbackAcceptedReplay, snap.WakeupFallback[WakeupFallbackAcceptedReplay])
	fmt.Fprintf(&b, "autosdk_server_wakeup_publish_fallback_total{path=%q} %d\n", WakeupFallbackEmittedBatch, snap.WakeupFallback[WakeupFallbackEmittedBatch])

	b.WriteString("# HELP autosdk_server_redis_partition_lease_lost_total Number of Redis partition lease losses after ownership.\n")
	b.WriteString("# TYPE autosdk_server_redis_partition_lease_lost_total counter\n")
	fmt.Fprintf(&b, "autosdk_server_redis_partition_lease_lost_total %d\n", snap.LeaseLost)

	b.WriteString("# HELP autosdk_server_workflow_wakeup_queue_depth Current outstanding wakeups across Redis Streams partitions.\n")
	b.WriteString("# TYPE autosdk_server_workflow_wakeup_queue_depth gauge\n")
	fmt.Fprintf(&b, "autosdk_server_workflow_wakeup_queue_depth %d\n", snap.WorkflowWakeupQueueDepth)

	b.WriteString("# HELP autosdk_server_workflow_wakeup_partition_depth Current outstanding wakeups per Redis Streams partition for the configured consumer group.\n")
	b.WriteString("# TYPE autosdk_server_workflow_wakeup_partition_depth gauge\n")
	partitionLabels := make([]string, 0, len(snap.WakeupPartitionDepth))
	for partition := range snap.WakeupPartitionDepth {
		partitionLabels = append(partitionLabels, partition)
	}
	slices.Sort(partitionLabels)
	for _, partition := range partitionLabels {
		fmt.Fprintf(&b, "autosdk_server_workflow_wakeup_partition_depth{partition=%q} %d\n", partition, snap.WakeupPartitionDepth[partition])
	}

	b.WriteString("# HELP autosdk_server_device_lane_active Current number of device workflow lanes actively executing.\n")
	b.WriteString("# TYPE autosdk_server_device_lane_active gauge\n")
	fmt.Fprintf(&b, "autosdk_server_device_lane_active %d\n", snap.DeviceLaneActive)

	b.WriteString("# HELP autosdk_server_command_inflight Current number of device commands awaiting response.\n")
	b.WriteString("# TYPE autosdk_server_command_inflight gauge\n")
	fmt.Fprintf(&b, "autosdk_server_command_inflight %d\n", snap.CommandInflight)

	b.WriteString("# HELP autosdk_server_event_replay_total Number of operator-triggered replay attempts and outcomes.\n")
	b.WriteString("# TYPE autosdk_server_event_replay_total counter\n")
	replayPaths := []ReplayPath{
		ReplayPathAccepted,
		ReplayPathDeadLetterIngestion,
		ReplayPathDeadLetterAccepted,
	}
	replayOutcomes := []ReplayOutcome{
		ReplayOutcomeAttempted,
		ReplayOutcomeSucceeded,
		ReplayOutcomeFailed,
	}
	for _, path := range replayPaths {
		for _, outcome := range replayOutcomes {
			fmt.Fprintf(
				&b,
				"autosdk_server_event_replay_total{path=%q,outcome=%q} %d\n",
				path,
				outcome,
				snap.Replay[path][outcome],
			)
		}
	}

	b.WriteString("# HELP autosdk_server_command_timeout_total Number of dispatched device commands that timed out before a response arrived.\n")
	b.WriteString("# TYPE autosdk_server_command_timeout_total counter\n")
	timeoutKinds := make([]string, 0, len(snap.CommandTimeout))
	for kind := range snap.CommandTimeout {
		timeoutKinds = append(timeoutKinds, kind)
	}
	slices.Sort(timeoutKinds)
	for _, kind := range timeoutKinds {
		fmt.Fprintf(&b, "autosdk_server_command_timeout_total{kind=%q} %d\n", kind, snap.CommandTimeout[kind])
	}

	b.WriteString("# HELP autosdk_server_accessibility_remediation_total Number of accessibility remediation attempts by outcome.\n")
	b.WriteString("# TYPE autosdk_server_accessibility_remediation_total counter\n")
	remediationOutcomes := make([]string, 0, len(snap.AccessibilityRemediation))
	for outcome := range snap.AccessibilityRemediation {
		remediationOutcomes = append(remediationOutcomes, outcome)
	}
	slices.Sort(remediationOutcomes)
	for _, outcome := range remediationOutcomes {
		fmt.Fprintf(&b, "autosdk_server_accessibility_remediation_total{outcome=%q} %d\n", outcome, snap.AccessibilityRemediation[outcome])
	}

	b.WriteString("# HELP autosdk_server_accessibility_pending_bindings Current number of bindings waiting for accessibility remediation.\n")
	b.WriteString("# TYPE autosdk_server_accessibility_pending_bindings gauge\n")
	fmt.Fprintf(&b, "autosdk_server_accessibility_pending_bindings %d\n", snap.AccessibilityPending)

	b.WriteString("# HELP autosdk_server_accessibility_blocked_bindings Current number of bindings blocked by android_id mismatch.\n")
	b.WriteString("# TYPE autosdk_server_accessibility_blocked_bindings gauge\n")
	fmt.Fprintf(&b, "autosdk_server_accessibility_blocked_bindings %d\n", snap.AccessibilityBlocked)

	renderHistogram(
		&b,
		"autosdk_server_event_ingest_lag_seconds",
		"Time between event occurrence and accepted-event processing start.",
		r.ingestLag.snapshot(),
	)
	renderHistogram(
		&b,
		"autosdk_server_workflow_node_duration_seconds",
		"Time spent executing one workflow node run.",
		r.workflowNodeDuration.snapshot(),
	)
	renderHistogram(
		&b,
		"autosdk_server_tool_call_duration_seconds",
		"Time spent executing one workflow tool call.",
		r.toolCallDuration.snapshot(),
	)
	renderHistogram(
		&b,
		"autosdk_server_command_duration_seconds",
		"Time from command issue to dispatch failure or response delivery.",
		r.commandDuration.snapshot(),
	)

	return b.String()
}

func newHistogram(buckets []float64, labelKeys ...string) histogram {
	cpBuckets := append([]float64(nil), buckets...)
	slices.Sort(cpBuckets)
	return histogram{
		buckets:   cpBuckets,
		labelKeys: append([]string(nil), labelKeys...),
		series:    make(map[string]*histogramSeries),
	}
}

func (h *histogram) observe(value float64, labels ...string) {
	if len(labels) != len(h.labelKeys) {
		panic(fmt.Sprintf("telemetry histogram label mismatch: got %d want %d", len(labels), len(h.labelKeys)))
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	if value < 0 {
		value = 0
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	key := strings.Join(labels, "\x1f")
	series := h.series[key]
	if series == nil {
		series = &histogramSeries{
			labels:       append([]string(nil), labels...),
			bucketCounts: make([]uint64, len(h.buckets)),
		}
		h.series[key] = series
	}
	for idx, upperBound := range h.buckets {
		if value <= upperBound {
			series.bucketCounts[idx]++
			break
		}
	}
	series.sum += value
	series.count++
}

func (h *histogram) snapshot() histogramSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	series := make([]histogramSeriesSnapshot, 0, len(h.series))
	for key, sample := range h.series {
		series = append(series, histogramSeriesSnapshot{
			key:          key,
			labels:       append([]string(nil), sample.labels...),
			bucketCounts: append([]uint64(nil), sample.bucketCounts...),
			sum:          sample.sum,
			count:        sample.count,
		})
	}
	slices.SortFunc(series, func(a, b histogramSeriesSnapshot) int {
		return strings.Compare(a.key, b.key)
	})
	return histogramSnapshot{
		buckets:   append([]float64(nil), h.buckets...),
		labelKeys: append([]string(nil), h.labelKeys...),
		series:    series,
	}
}

func renderHistogram(b *strings.Builder, name, help string, snap histogramSnapshot) {
	b.WriteString("# HELP " + name + " " + help + "\n")
	b.WriteString("# TYPE " + name + " histogram\n")
	for _, series := range snap.series {
		cumulative := uint64(0)
		for idx, upperBound := range snap.buckets {
			cumulative += series.bucketCounts[idx]
			fmt.Fprintf(
				b,
				"%s_bucket{%s} %d\n",
				name,
				formatMetricLabels(snap.labelKeys, series.labels, "le", formatMetricFloat(upperBound)),
				cumulative,
			)
		}
		fmt.Fprintf(
			b,
			"%s_bucket{%s} %d\n",
			name,
			formatMetricLabels(snap.labelKeys, series.labels, "le", "+Inf"),
			series.count,
		)
		fmt.Fprintf(b, "%s_sum{%s} %s\n", name, formatMetricLabels(snap.labelKeys, series.labels), formatMetricFloat(series.sum))
		fmt.Fprintf(b, "%s_count{%s} %d\n", name, formatMetricLabels(snap.labelKeys, series.labels), series.count)
	}
}

func formatMetricLabels(labelKeys, labelValues []string, extra ...string) string {
	parts := make([]string, 0, len(labelKeys)+len(extra)/2)
	for idx, key := range labelKeys {
		parts = append(parts, fmt.Sprintf("%s=%q", key, labelValues[idx]))
	}
	for idx := 0; idx+1 < len(extra); idx += 2 {
		parts = append(parts, fmt.Sprintf("%s=%q", extra[idx], extra[idx+1]))
	}
	return strings.Join(parts, ",")
}

func formatMetricFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func normalizeMetricLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func addGauge(g *atomic.Int64, delta int64) {
	for {
		current := g.Load()
		next := current + delta
		if next < 0 {
			next = 0
		}
		if g.CompareAndSwap(current, next) {
			return
		}
	}
}
