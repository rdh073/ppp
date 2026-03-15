package main

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

// ── DefaultConfig ─────────────────────────────────────────────────────────────

func TestDefaultConfig_ServerDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Addr != ":3000" {
		t.Fatalf("addr: expected :3000, got %q", cfg.Server.Addr)
	}
	if cfg.Server.DataDir != "./var" {
		t.Fatalf("data_dir: expected ./var, got %q", cfg.Server.DataDir)
	}
	if cfg.Server.ToolDir != "./config/tools" {
		t.Fatalf("tool_dir: expected ./config/tools, got %q", cfg.Server.ToolDir)
	}
	if cfg.Server.WorkflowDir != "" {
		t.Fatalf("workflow_dir: expected empty, got %q", cfg.Server.WorkflowDir)
	}
	if cfg.Server.WorkflowPoll.D() != 5*time.Second {
		t.Fatalf("workflow_poll: expected 5s, got %s", cfg.Server.WorkflowPoll.D())
	}
	if cfg.Server.ShutdownTimeout.D() != 15*time.Second {
		t.Fatalf("shutdown_timeout: expected 15s, got %s", cfg.Server.ShutdownTimeout.D())
	}
}

func TestDefaultConfig_StoreDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Store.Driver != "file" {
		t.Fatalf("store.driver: expected file, got %q", cfg.Store.Driver)
	}
	if cfg.Store.StateTTL.D() != 0 {
		t.Fatalf("store.state_ttl: expected 0, got %s", cfg.Store.StateTTL.D())
	}
}

func TestDefaultConfig_EventRuntimeDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.EventRuntime.Mode != "inline" {
		t.Fatalf("event_runtime.mode: expected inline, got %q", cfg.EventRuntime.Mode)
	}
	if cfg.EventBus.Partitions != 8 {
		t.Fatalf("event_bus.partitions: expected 8, got %d", cfg.EventBus.Partitions)
	}
	if cfg.EventBus.LeaseTTL.D() != 15*time.Second {
		t.Fatalf("event_bus.lease_ttl: expected 15s, got %s", cfg.EventBus.LeaseTTL.D())
	}
	if cfg.EventBus.PendingIdle.D() != 45*time.Second {
		t.Fatalf("event_bus.pending_idle: expected 45s, got %s", cfg.EventBus.PendingIdle.D())
	}
	if cfg.EventBus.ClaimCount != 16 {
		t.Fatalf("event_bus.claim_count: expected 16, got %d", cfg.EventBus.ClaimCount)
	}
	if cfg.EventBus.OwnershipRetry.D() != 500*time.Millisecond {
		t.Fatalf("event_bus.ownership_retry: expected 500ms, got %s", cfg.EventBus.OwnershipRetry.D())
	}
}

func TestDefaultConfig_PruningDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Pruning.Interval.D() != time.Hour {
		t.Fatalf("pruning.interval: expected 1h, got %s", cfg.Pruning.Interval.D())
	}
	if cfg.Pruning.AcceptedEventsAge.D() != 7*24*time.Hour {
		t.Fatalf("pruning.accepted_events_age: expected 168h, got %s", cfg.Pruning.AcceptedEventsAge.D())
	}
}

func TestDefaultConfig_ADBDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ADB.Host != "localhost" {
		t.Fatalf("adb.host: expected localhost, got %q", cfg.ADB.Host)
	}
	if cfg.ADB.Port != 5037 {
		t.Fatalf("adb.port: expected 5037, got %d", cfg.ADB.Port)
	}
	if cfg.ADB.AccessibilityComponent != "com.autosdk.agent/com.autosdk.agent.service.AgentAccessibilityService" {
		t.Fatalf("adb.accessibility_component: unexpected value %q", cfg.ADB.AccessibilityComponent)
	}
}

// ── LoadConfig ────────────────────────────────────────────────────────────────

func TestLoadConfig_EmptyPath_UsesDefaults(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":3000" {
		t.Fatalf("expected :3000, got %q", cfg.Server.Addr)
	}
	if cfg.Store.Driver != "file" {
		t.Fatalf("expected file driver, got %q", cfg.Store.Driver)
	}
}

func TestLoadConfig_FromFile_OverridesDefaults(t *testing.T) {
	f := writeTempConfig(t, `
[server]
addr      = ":9000"
log_level = "debug"

[store]
driver = "redis"

[redis]
addr = "redis.internal:6379"
db   = 2

[event_bus]
partitions = 4
lease_ttl  = "30s"

[pruning]
interval             = "30m"
accepted_events_age  = "24h"
`)

	cfg, err := LoadConfig(f)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Addr != ":9000" {
		t.Fatalf("addr: expected :9000, got %q", cfg.Server.Addr)
	}
	if cfg.Server.LogLevel != "debug" {
		t.Fatalf("log_level: expected debug, got %q", cfg.Server.LogLevel)
	}
	if cfg.Store.Driver != "redis" {
		t.Fatalf("store.driver: expected redis, got %q", cfg.Store.Driver)
	}
	if cfg.Redis.Addr != "redis.internal:6379" {
		t.Fatalf("redis.addr: expected redis.internal:6379, got %q", cfg.Redis.Addr)
	}
	if cfg.Redis.DB != 2 {
		t.Fatalf("redis.db: expected 2, got %d", cfg.Redis.DB)
	}
	if cfg.EventBus.Partitions != 4 {
		t.Fatalf("event_bus.partitions: expected 4, got %d", cfg.EventBus.Partitions)
	}
	if cfg.EventBus.LeaseTTL.D() != 30*time.Second {
		t.Fatalf("event_bus.lease_ttl: expected 30s, got %s", cfg.EventBus.LeaseTTL.D())
	}
	if cfg.Pruning.Interval.D() != 30*time.Minute {
		t.Fatalf("pruning.interval: expected 30m, got %s", cfg.Pruning.Interval.D())
	}
	if cfg.Pruning.AcceptedEventsAge.D() != 24*time.Hour {
		t.Fatalf("pruning.accepted_events_age: expected 24h, got %s", cfg.Pruning.AcceptedEventsAge.D())
	}
	// Unspecified fields keep their defaults.
	if cfg.EventRuntime.Mode != "inline" {
		t.Fatalf("event_runtime.mode should keep default inline, got %q", cfg.EventRuntime.Mode)
	}
	if cfg.ADB.Port != 5037 {
		t.Fatalf("adb.port should keep default 5037, got %d", cfg.ADB.Port)
	}
}

func TestLoadConfig_ADBSerialByDevice_FromFile(t *testing.T) {
	f := writeTempConfig(t, `
[adb.serial_by_device]
"device-abc" = "emulator-5554"
"device-def" = "emulator-5556"
`)

	cfg, err := LoadConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ADB.SerialByDevice["device-abc"] != "emulator-5554" {
		t.Fatalf("expected emulator-5554, got %q", cfg.ADB.SerialByDevice["device-abc"])
	}
	if cfg.ADB.SerialByDevice["device-def"] != "emulator-5556" {
		t.Fatalf("expected emulator-5556, got %q", cfg.ADB.SerialByDevice["device-def"])
	}
}

func TestLoadConfig_InvalidFile(t *testing.T) {
	f := writeTempConfig(t, "this is ][ not valid toml")
	_, err := LoadConfig(f)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/server.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// ── Env overrides ─────────────────────────────────────────────────────────────

func TestLoadConfig_EnvOverridesFile(t *testing.T) {
	f := writeTempConfig(t, `[store]
driver = "file"
`)
	t.Setenv("AUTO_STATE_STORE", "redis")
	t.Setenv("AUTO_REDIS_ADDR", "redis-env:6380")

	cfg, err := LoadConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.Driver != "redis" {
		t.Fatalf("env should override file: expected redis, got %q", cfg.Store.Driver)
	}
	if cfg.Redis.Addr != "redis-env:6380" {
		t.Fatalf("env should override default: expected redis-env:6380, got %q", cfg.Redis.Addr)
	}
}

func TestLoadConfig_EnvOverrides_EventBus(t *testing.T) {
	t.Setenv("AUTO_EVENT_RUNTIME", "redis-streams")
	t.Setenv("AUTO_EVENT_BUS_PARTITIONS", "16")
	t.Setenv("AUTO_EVENT_BUS_LEASE_TTL", "30s")
	t.Setenv("AUTO_REDIS_GROUP", "my-group")
	t.Setenv("AUTO_REDIS_CONSUMER_PREFIX", "worker")
	t.Setenv("AUTO_REDIS_INSTANCE_ID", "node-1")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EventRuntime.Mode != "redis-streams" {
		t.Fatalf("expected redis-streams, got %q", cfg.EventRuntime.Mode)
	}
	if cfg.EventBus.Partitions != 16 {
		t.Fatalf("expected 16, got %d", cfg.EventBus.Partitions)
	}
	if cfg.EventBus.LeaseTTL.D() != 30*time.Second {
		t.Fatalf("expected 30s, got %s", cfg.EventBus.LeaseTTL.D())
	}
	if cfg.EventBus.Group != "my-group" {
		t.Fatalf("expected my-group, got %q", cfg.EventBus.Group)
	}
	if cfg.EventBus.ConsumerPrefix != "worker" {
		t.Fatalf("expected worker, got %q", cfg.EventBus.ConsumerPrefix)
	}
	if cfg.EventBus.InstanceID != "node-1" {
		t.Fatalf("expected node-1, got %q", cfg.EventBus.InstanceID)
	}
}

func TestLoadConfig_EnvOverrides_ADB(t *testing.T) {
	t.Setenv("AUTO_ADB_SERVER_HOST", "adb.internal")
	t.Setenv("AUTO_ADB_SERVER_PORT", "5038")
	t.Setenv("AUTO_ADB_SERIAL_BY_DEVICE", "dev1=emulator-5554,dev2=emulator-5556")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ADB.Host != "adb.internal" {
		t.Fatalf("expected adb.internal, got %q", cfg.ADB.Host)
	}
	if cfg.ADB.Port != 5038 {
		t.Fatalf("expected 5038, got %d", cfg.ADB.Port)
	}
	if cfg.ADB.SerialByDevice["dev1"] != "emulator-5554" {
		t.Fatalf("expected emulator-5554, got %q", cfg.ADB.SerialByDevice["dev1"])
	}
}

func TestLoadConfig_InvalidEnvVar_RedisDB(t *testing.T) {
	t.Setenv("AUTO_REDIS_DB", "not-a-number")
	_, err := LoadConfig("")
	if err == nil {
		t.Fatal("expected error for invalid AUTO_REDIS_DB")
	}
}

func TestLoadConfig_InvalidEnvVar_Partitions(t *testing.T) {
	t.Setenv("AUTO_EVENT_BUS_PARTITIONS", "abc")
	_, err := LoadConfig("")
	if err == nil {
		t.Fatal("expected error for invalid AUTO_EVENT_BUS_PARTITIONS")
	}
}

func TestLoadConfig_InvalidEnvVar_LeaseTTL(t *testing.T) {
	t.Setenv("AUTO_EVENT_BUS_LEASE_TTL", "bad-duration")
	_, err := LoadConfig("")
	if err == nil {
		t.Fatal("expected error for invalid AUTO_EVENT_BUS_LEASE_TTL")
	}
}

func TestLoadConfig_InvalidEnvVar_ADBPort(t *testing.T) {
	t.Setenv("AUTO_ADB_SERVER_PORT", "not-a-port")
	_, err := LoadConfig("")
	if err == nil {
		t.Fatal("expected error for invalid AUTO_ADB_SERVER_PORT")
	}
}

func TestLoadConfig_InvalidEnvVar_ADBPortZero(t *testing.T) {
	t.Setenv("AUTO_ADB_SERVER_PORT", "0")
	_, err := LoadConfig("")
	if err == nil {
		t.Fatal("expected error for zero AUTO_ADB_SERVER_PORT")
	}
}

// ── SlogLevel ─────────────────────────────────────────────────────────────────

func TestSlogLevel_Mapping(t *testing.T) {
	cases := []struct {
		level string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}
	for _, tc := range cases {
		cfg := DefaultConfig()
		cfg.Server.LogLevel = tc.level
		if got := cfg.SlogLevel(); got != tc.want {
			t.Errorf("LogLevel %q: expected %v, got %v", tc.level, tc.want, got)
		}
	}
}

// ── formatSerialByDevice / parseSerialByDevice ────────────────────────────────

func TestFormatSerialByDevice_Empty(t *testing.T) {
	if got := formatSerialByDevice(nil); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
	if got := formatSerialByDevice(map[string]string{}); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestFormatSerialByDevice_Sorted(t *testing.T) {
	m := map[string]string{
		"device-zzz": "serial-1",
		"device-aaa": "serial-2",
	}
	got := formatSerialByDevice(m)
	want := "device-aaa=serial-2,device-zzz=serial-1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestParseSerialByDevice_Valid(t *testing.T) {
	got := parseSerialByDevice("dev1=emulator-5554,dev2=emulator-5556")
	if got["dev1"] != "emulator-5554" {
		t.Fatalf("dev1: expected emulator-5554, got %q", got["dev1"])
	}
	if got["dev2"] != "emulator-5556" {
		t.Fatalf("dev2: expected emulator-5556, got %q", got["dev2"])
	}
}

func TestParseSerialByDevice_SkipsInvalidEntries(t *testing.T) {
	got := parseSerialByDevice(",noequals,dev1=serial-1,,")
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(got), got)
	}
	if got["dev1"] != "serial-1" {
		t.Fatalf("expected serial-1, got %q", got["dev1"])
	}
}

func TestFormatParseRoundTrip(t *testing.T) {
	original := map[string]string{
		"dev-a": "emulator-5554",
		"dev-b": "emulator-5556",
	}
	parsed := parseSerialByDevice(formatSerialByDevice(original))
	for k, v := range original {
		if parsed[k] != v {
			t.Fatalf("round-trip mismatch for %q: expected %q, got %q", k, v, parsed[k])
		}
	}
}

// ── Duration ──────────────────────────────────────────────────────────────────

func TestDuration_UnmarshalText(t *testing.T) {
	cases := []struct {
		text string
		want time.Duration
	}{
		{"5s", 5 * time.Second},
		{"15s", 15 * time.Second},
		{"1h", time.Hour},
		{"500ms", 500 * time.Millisecond},
		{"45s", 45 * time.Second},
	}
	for _, tc := range cases {
		var d Duration
		if err := d.UnmarshalText([]byte(tc.text)); err != nil {
			t.Fatalf("%q: unexpected error: %v", tc.text, err)
		}
		if d.D() != tc.want {
			t.Fatalf("%q: expected %s, got %s", tc.text, tc.want, d.D())
		}
	}
}

func TestDuration_UnmarshalText_Invalid(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("not-a-duration")); err == nil {
		t.Fatal("expected error for invalid duration string")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}
