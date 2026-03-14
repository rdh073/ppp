package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// DefaultStateTTL is applied on every write and refreshed on each transition.
// States not written for this long (e.g. truly orphaned workflows) expire automatically.
const DefaultStateTTL = 7 * 24 * time.Hour

// wfStateSaveScript atomically checks the stored Revision against the caller's
// expected revision, then writes the new JSON and updates the device index.
//
// KEYS[1] = wf:state key
// KEYS[2] = wf:device:{deviceID} index (Set)
// ARGV[1] = index member (composite key taskID::deviceID)
// ARGV[2] = expected current revision (integer string)
// ARGV[3] = new JSON (already contains bumped Revision)
// ARGV[4] = TTL in milliseconds
//
// Returns 1 on success; returns CONFLICT:{currentRev} error on revision mismatch.
const wfStateSaveScript = `
local key       = KEYS[1]
local idx_key   = KEYS[2]
local idx_mbr   = ARGV[1]
local exp_rev   = tonumber(ARGV[2])
local new_json  = ARGV[3]
local ttl_ms    = tonumber(ARGV[4])

local raw = redis.call("GET", key)
local cur_rev = 0
if raw ~= false then
    local ok, obj = pcall(cjson.decode, raw)
    if ok and type(obj) == "table" and obj["Revision"] then
        cur_rev = tonumber(obj["Revision"]) or 0
    end
end

if cur_rev ~= exp_rev then
    return redis.error_reply("CONFLICT:" .. tostring(cur_rev))
end

redis.call("SET",     key,     new_json, "PX", ttl_ms)
redis.call("SADD",    idx_key, idx_mbr)
redis.call("PEXPIRE", idx_key, ttl_ms)
return 1
`

// RedisWorkflowStateStore persists workflow checkpoints in Redis.
// Every Save refreshes the TTL, so actively progressing workflows never expire.
// Abandoned states (no writes for TTL duration) are cleaned up automatically.
type RedisWorkflowStateStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisWorkflowStateStore creates a store using an existing Redis client.
// ttl is the expiry applied (and refreshed) on every Save; pass 0 to use DefaultStateTTL.
func NewRedisWorkflowStateStore(client *redis.Client, ttl time.Duration) *RedisWorkflowStateStore {
	if ttl <= 0 {
		ttl = DefaultStateTTL
	}
	return &RedisWorkflowStateStore{client: client, ttl: ttl}
}

func (s *RedisWorkflowStateStore) Save(ctx context.Context, ws *domain.WorkflowState) error {
	expectedRev := ws.Revision
	newRev := expectedRev + 1

	// Clone and bump revision before serialising so the stored JSON is authoritative.
	saved := cloneWorkflowState(ws)
	saved.Revision = newRev

	data, err := json.Marshal(saved)
	if err != nil {
		return fmt.Errorf("marshal workflow state: %w", err)
	}

	key := wfStateRedisKey(ws.TaskID, ws.DeviceID)
	idxKey := wfDeviceKey(ws.DeviceID)
	idxMember := stateCompositeKey(ws.TaskID, ws.DeviceID)

	err = s.client.Eval(ctx, wfStateSaveScript,
		[]string{key, idxKey},
		idxMember,
		expectedRev,
		string(data),
		s.ttl.Milliseconds(),
	).Err()
	if err != nil {
		if isConflictErr(err) {
			return fmt.Errorf("%w: task=%s device=%s", ErrCheckpointConflict, ws.TaskID, ws.DeviceID)
		}
		return fmt.Errorf("redis save workflow state: %w", err)
	}

	ws.Revision = newRev
	return nil
}

func (s *RedisWorkflowStateStore) Get(ctx context.Context, taskID domain.TaskID, deviceID domain.DeviceID) (*domain.WorkflowState, error) {
	key := wfStateRedisKey(taskID, deviceID)
	data, err := s.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("%w: workflow state task=%s device=%s", ErrNotFound, taskID, deviceID)
		}
		return nil, fmt.Errorf("redis get workflow state: %w", err)
	}

	var ws domain.WorkflowState
	if err := json.Unmarshal([]byte(data), &ws); err != nil {
		return nil, fmt.Errorf("unmarshal workflow state: %w", err)
	}
	return &ws, nil
}

func (s *RedisWorkflowStateStore) ListActiveByDevice(ctx context.Context, deviceID domain.DeviceID) ([]*domain.WorkflowState, error) {
	members, err := s.client.SMembers(ctx, wfDeviceKey(deviceID)).Result()
	if err != nil {
		return nil, fmt.Errorf("list device workflow state keys: %w", err)
	}
	if len(members) == 0 {
		return nil, nil
	}

	// Build Redis keys from composite members.
	keys := make([]string, len(members))
	for i, m := range members {
		taskID, devID, err := parseStateCompositeKey(m)
		if err != nil {
			continue
		}
		keys[i] = wfStateRedisKey(taskID, devID)
	}

	states, err := s.pipelineGetStates(ctx, keys)
	if err != nil {
		return nil, err
	}

	var out []*domain.WorkflowState
	for _, ws := range states {
		if !ws.IsTerminal() {
			out = append(out, ws)
		}
	}
	return out, nil
}

// pipelineGetStates bulk-fetches workflow state keys; skips nil (expired) entries.
func (s *RedisWorkflowStateStore) pipelineGetStates(ctx context.Context, keys []string) ([]*domain.WorkflowState, error) {
	pipe := s.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, k := range keys {
		if k == "" {
			continue
		}
		cmds[i] = pipe.Get(ctx, k)
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("pipeline get workflow states: %w", err)
	}

	var states []*domain.WorkflowState
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		data, err := cmd.Result()
		if err != nil {
			continue // redis.Nil = expired; any other error = skip
		}
		var ws domain.WorkflowState
		if err := json.Unmarshal([]byte(data), &ws); err != nil {
			continue
		}
		states = append(states, &ws)
	}
	return states, nil
}

// --- RedisTaskStore ---

// RedisTaskStore persists Task records in Redis.
// TTL is refreshed on every Save. Secondary indexes (tasks:all, tasks:device:{deviceID})
// are updated via pipeline; stale index members are filtered on read.
type RedisTaskStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisTaskStore creates a store using an existing Redis client.
// ttl is the expiry applied (and refreshed) on every Save; pass 0 to use DefaultStateTTL.
func NewRedisTaskStore(client *redis.Client, ttl time.Duration) *RedisTaskStore {
	if ttl <= 0 {
		ttl = DefaultStateTTL
	}
	return &RedisTaskStore{client: client, ttl: ttl}
}

func (s *RedisTaskStore) Save(ctx context.Context, task *domain.Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	pipe := s.client.Pipeline()
	pipe.Set(ctx, taskRedisKey(task.ID), string(data), s.ttl)
	pipe.SAdd(ctx, taskAllKey(), string(task.ID))
	pipe.Expire(ctx, taskAllKey(), s.ttl)
	if task.AssignedDevice != "" {
		pipe.SAdd(ctx, taskDeviceKey(task.AssignedDevice), string(task.ID))
		pipe.Expire(ctx, taskDeviceKey(task.AssignedDevice), s.ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis save task: %w", err)
	}
	return nil
}

func (s *RedisTaskStore) Get(ctx context.Context, id domain.TaskID) (*domain.Task, error) {
	data, err := s.client.Get(ctx, taskRedisKey(id)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("%w: task %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("redis get task: %w", err)
	}
	var task domain.Task
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		return nil, fmt.Errorf("unmarshal task: %w", err)
	}
	return &task, nil
}

func (s *RedisTaskStore) List(ctx context.Context) ([]*domain.Task, error) {
	ids, err := s.client.SMembers(ctx, taskAllKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("list task ids: %w", err)
	}
	return s.loadTasks(ctx, ids)
}

func (s *RedisTaskStore) ListByDevice(ctx context.Context, deviceID domain.DeviceID) ([]*domain.Task, error) {
	ids, err := s.client.SMembers(ctx, taskDeviceKey(deviceID)).Result()
	if err != nil {
		return nil, fmt.Errorf("list device task ids: %w", err)
	}
	tasks, err := s.loadTasks(ctx, ids)
	if err != nil {
		return nil, err
	}
	// Filter: only tasks currently assigned to this device.
	var out []*domain.Task
	for _, t := range tasks {
		if t.AssignedDevice == deviceID {
			out = append(out, t)
		}
	}
	return out, nil
}

// loadTasks bulk-fetches task keys by ID; skips nil (expired) entries.
func (s *RedisTaskStore) loadTasks(ctx context.Context, ids []string) ([]*domain.Task, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	pipe := s.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.Get(ctx, taskRedisKey(domain.TaskID(id)))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("pipeline load tasks: %w", err)
	}

	var tasks []*domain.Task
	for _, cmd := range cmds {
		data, err := cmd.Result()
		if err != nil {
			continue // redis.Nil = expired stale index member
		}
		var task domain.Task
		if err := json.Unmarshal([]byte(data), &task); err != nil {
			continue
		}
		tasks = append(tasks, &task)
	}
	return tasks, nil
}

// --- Redis key helpers ---

func taskRedisKey(id domain.TaskID) string {
	return "task:" + string(id)
}

func taskAllKey() string {
	return "tasks:all"
}

func taskDeviceKey(deviceID domain.DeviceID) string {
	return "tasks:device:" + string(deviceID)
}

func wfStateRedisKey(taskID domain.TaskID, deviceID domain.DeviceID) string {
	return "wf:state:" + string(taskID) + ":" + string(deviceID)
}

func wfDeviceKey(deviceID domain.DeviceID) string {
	return "wf:device:" + string(deviceID)
}

// isConflictErr reports whether the Redis error is a CONFLICT returned by the Lua script.
// go-redis prefixes Lua error replies with "ERR ", so we check by substring.
func isConflictErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Lua error_reply("CONFLICT:...") arrives as "ERR CONFLICT:..." from go-redis.
	for i := 0; i+8 <= len(msg); i++ {
		if msg[i:i+8] == "CONFLICT" {
			return true
		}
	}
	return false
}

// NewRedisClient constructs a go-redis client from the given connection options.
func NewRedisClient(addr, password string, db int) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
}


// --- RedisTaskQueue ---

// redisEnqueueScript atomically enqueues a task ID only if it is not already present.
// KEYS[1] = list key (task:queue)
// ARGV[1] = task ID
// Returns 1 if enqueued, 0 if already present.
const redisEnqueueScript = `
local pos = redis.call("LPOS", KEYS[1], ARGV[1])
if pos ~= false then return 0 end
redis.call("RPUSH", KEYS[1], ARGV[1])
return 1
`

// RedisTaskQueue is a durable FIFO TaskQueue backed by a Redis list.
// Enqueue uses a Lua script for atomic dedup.
type RedisTaskQueue struct {
	client *redis.Client
	key    string
}

func NewRedisTaskQueue(client *redis.Client) *RedisTaskQueue {
	return &RedisTaskQueue{client: client, key: "task:queue"}
}

func (q *RedisTaskQueue) Enqueue(ctx context.Context, taskID domain.TaskID) error {
	err := q.client.Eval(ctx, redisEnqueueScript, []string{q.key}, string(taskID)).Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("redis enqueue task %s: %w", taskID, err)
	}
	return nil
}

func (q *RedisTaskQueue) Dequeue(ctx context.Context) (domain.TaskID, bool, error) {
	val, err := q.client.LPop(ctx, q.key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("redis dequeue: %w", err)
	}
	return domain.TaskID(val), true, nil
}

func (q *RedisTaskQueue) Remove(ctx context.Context, taskID domain.TaskID) error {
	if err := q.client.LRem(ctx, q.key, 0, string(taskID)).Err(); err != nil {
		return fmt.Errorf("redis remove task %s from queue: %w", taskID, err)
	}
	return nil
}

func (q *RedisTaskQueue) Snapshot(ctx context.Context) ([]domain.TaskID, error) {
	vals, err := q.client.LRange(ctx, q.key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("redis queue snapshot: %w", err)
	}
	out := make([]domain.TaskID, len(vals))
	for i, v := range vals {
		out[i] = domain.TaskID(v)
	}
	return out, nil
}
