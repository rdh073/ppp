package main

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Duration wraps time.Duration so TOML can decode string values like "5s", "15s".
// It implements encoding.TextUnmarshaler which BurntSushi/toml honours.
type Duration time.Duration

func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", string(text), err)
	}
	*d = Duration(v)
	return nil
}

// D returns the underlying time.Duration value.
func (d Duration) D() time.Duration { return time.Duration(d) }

// Config holds all operational settings for the server.
//
// Override chain (lowest → highest priority):
//
//	DefaultConfig()  →  TOML file  →  env vars  →  CLI flags
//
// Secrets (Redis password, API keys) are read from env vars only and are
// intentionally absent from this struct.
type Config struct {
	Server       ServerConfig       `toml:"server"`
	Store        StoreConfig        `toml:"store"`
	Redis        RedisConfig        `toml:"redis"`
	EventRuntime EventRuntimeConfig `toml:"event_runtime"`
	EventBus     EventBusConfig     `toml:"event_bus"`
	Pruning      PruningConfig      `toml:"pruning"`
	ADB          ADBConfig          `toml:"adb"`
	Tools        ToolsConfig        `toml:"tools"`
}

type ServerConfig struct {
	Addr            string   `toml:"addr"`
	DataDir         string   `toml:"data_dir"`
	ToolDir         string   `toml:"tool_dir"`
	WorkflowDir     string   `toml:"workflow_dir"`
	WorkflowPoll    Duration `toml:"workflow_poll"`
	LogLevel        string   `toml:"log_level"`        // debug | info | warn | error
	ShutdownTimeout Duration `toml:"shutdown_timeout"` // graceful shutdown window
}

type StoreConfig struct {
	Driver   string   `toml:"driver"`    // "file" | "redis"
	StateTTL Duration `toml:"state_ttl"` // redis only; 0 = no expiry
}

type RedisConfig struct {
	Addr string `toml:"addr"`
	DB   int    `toml:"db"`
	// Password: AUTO_REDIS_PASSWORD env var only.
}

type EventRuntimeConfig struct {
	Mode string `toml:"mode"` // "inline" | "redis-streams"
}

type EventBusConfig struct {
	Partitions     int      `toml:"partitions"`
	Group          string   `toml:"group"`
	ConsumerPrefix string   `toml:"consumer_prefix"`
	InstanceID     string   `toml:"instance_id"` // empty = auto-generated
	LeaseTTL       Duration `toml:"lease_ttl"`
	PendingIdle    Duration `toml:"pending_idle"`
	ClaimCount     int      `toml:"claim_count"`
	OwnershipRetry Duration `toml:"ownership_retry"`
}

type PruningConfig struct {
	Interval          Duration `toml:"interval"`
	AcceptedEventsAge Duration `toml:"accepted_events_age"`
	DeadLettersAge    Duration `toml:"dead_letters_age"`
	CommandOutboxAge  Duration `toml:"command_outbox_age"`
}

type ADBConfig struct {
	Host                   string            `toml:"host"`
	Port                   int               `toml:"port"`
	AccessibilityComponent string            `toml:"accessibility_component"`
	SerialByDevice         map[string]string `toml:"serial_by_device"`
	ReconcileInterval      Duration          `toml:"reconcile_interval"`
}

type ToolsConfig struct {
	LLM LLMToolConfig `toml:"llm"`
}

type LLMToolConfig struct {
	APIURL string `toml:"api_url"`
	Model  string `toml:"model"`
	APIKey string `toml:"api_key"` // overridden by AUTO_TOOL_LLM_API_KEY env var
}

// DefaultConfig returns a Config populated with the same built-in defaults
// that were previously hardcoded in main.go.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Addr:            ":3000",
			DataDir:         "./var",
			ToolDir:         "./config/tools",
			WorkflowDir:     "",
			WorkflowPoll:    Duration(5 * time.Second),
			LogLevel:        "info",
			ShutdownTimeout: Duration(15 * time.Second),
		},
		Store: StoreConfig{
			Driver:   "file",
			StateTTL: 0,
		},
		Redis: RedisConfig{
			Addr: "localhost:6379",
			DB:   0,
		},
		EventRuntime: EventRuntimeConfig{
			Mode: "inline",
		},
		EventBus: EventBusConfig{
			Partitions:     8,
			Group:          "server-agent",
			ConsumerPrefix: "server-agent",
			InstanceID:     "",
			LeaseTTL:       Duration(15 * time.Second),
			PendingIdle:    Duration(45 * time.Second),
			ClaimCount:     16,
			OwnershipRetry: Duration(500 * time.Millisecond),
		},
		Pruning: PruningConfig{
			Interval:          Duration(time.Hour),
			AcceptedEventsAge: Duration(7 * 24 * time.Hour),
			DeadLettersAge:    Duration(7 * 24 * time.Hour),
			CommandOutboxAge:  Duration(24 * time.Hour),
		},
		ADB: ADBConfig{
			Host:                   "localhost",
			Port:                   5037,
			AccessibilityComponent: "com.autosdk.agent/com.autosdk.agent.service.AgentAccessibilityService",
			SerialByDevice:         make(map[string]string),
			ReconcileInterval:      Duration(15 * time.Second),
		},
		Tools: ToolsConfig{
			LLM: LLMToolConfig{
				APIURL: "",
				Model:  "",
			},
		},
	}
}

// SlogLevel maps cfg.Server.LogLevel to the corresponding slog.Level.
// Unknown or empty values fall back to slog.LevelInfo.
func (c *Config) SlogLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(c.Server.LogLevel)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// LoadConfig applies the override chain: DefaultConfig → TOML file → env vars.
// CLI flag overrides are applied separately via ApplyFlagOverrides after flag.Parse().
// path may be empty, in which case only defaults and env vars are applied.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	if strings.TrimSpace(path) != "" {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
	}
	if err := cfg.applyEnvOverrides(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ApplyFlagOverrides overlays explicitly-set CLI flags on top of the config.
// visitedFlags must contain the names of flags explicitly passed on the command
// line (populated by iterating flag.Visit after flag.Parse).
func (c *Config) ApplyFlagOverrides(
	addrFlag *string,
	dataDirFlag *string,
	toolDirFlag *string,
	workflowDirFlag *string,
	workflowPollFlag *time.Duration,
	visitedFlags map[string]bool,
) {
	if visitedFlags["addr"] {
		c.Server.Addr = *addrFlag
	}
	if visitedFlags["data-dir"] {
		c.Server.DataDir = *dataDirFlag
	}
	if visitedFlags["tool-dir"] {
		c.Server.ToolDir = *toolDirFlag
	}
	if visitedFlags["workflow-dir"] {
		c.Server.WorkflowDir = *workflowDirFlag
	}
	if visitedFlags["workflow-poll"] {
		c.Server.WorkflowPoll = Duration(*workflowPollFlag)
	}
}

// applyEnvOverrides overlays non-empty environment variables on top of the
// config file values. Returns an error for any env var with an invalid value.
func (c *Config) applyEnvOverrides() error {
	if v := envTrimmed("AUTO_STATE_STORE"); v != "" {
		c.Store.Driver = v
	}
	if v := envTrimmed("AUTO_REDIS_ADDR"); v != "" {
		c.Redis.Addr = v
	}
	if v := envTrimmed("AUTO_REDIS_DB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid AUTO_REDIS_DB %q: %w", v, err)
		}
		c.Redis.DB = n
	}
	if v := envTrimmed("AUTO_STATE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid AUTO_STATE_TTL %q: %w", v, err)
		}
		c.Store.StateTTL = Duration(d)
	}
	if v := envTrimmed("AUTO_EVENT_RUNTIME"); v != "" {
		c.EventRuntime.Mode = v
	}
	if v := envTrimmed("AUTO_EVENT_BUS_PARTITIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid AUTO_EVENT_BUS_PARTITIONS %q: %w", v, err)
		}
		c.EventBus.Partitions = n
	}
	if v := envTrimmed("AUTO_REDIS_GROUP"); v != "" {
		c.EventBus.Group = v
	}
	if v := envTrimmed("AUTO_REDIS_CONSUMER_PREFIX"); v != "" {
		c.EventBus.ConsumerPrefix = v
	}
	// InstanceID intentionally uses os.Getenv (no trimming) to allow spaces
	// in unusual instance identifiers, matching original main.go behaviour.
	if v := os.Getenv("AUTO_REDIS_INSTANCE_ID"); v != "" {
		c.EventBus.InstanceID = v
	}
	if v := envTrimmed("AUTO_EVENT_BUS_LEASE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid AUTO_EVENT_BUS_LEASE_TTL %q: %w", v, err)
		}
		c.EventBus.LeaseTTL = Duration(d)
	}
	if v := envTrimmed("AUTO_EVENT_BUS_PENDING_IDLE"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid AUTO_EVENT_BUS_PENDING_IDLE %q: %w", v, err)
		}
		c.EventBus.PendingIdle = Duration(d)
	}
	if v := envTrimmed("AUTO_EVENT_BUS_CLAIM_COUNT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid AUTO_EVENT_BUS_CLAIM_COUNT %q: %w", v, err)
		}
		c.EventBus.ClaimCount = n
	}
	if v := envTrimmed("AUTO_EVENT_BUS_OWNERSHIP_RETRY"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid AUTO_EVENT_BUS_OWNERSHIP_RETRY %q: %w", v, err)
		}
		c.EventBus.OwnershipRetry = Duration(d)
	}
	if v := envTrimmed("AUTO_ADB_SERVER_HOST"); v != "" {
		c.ADB.Host = v
	}
	if v := envTrimmed("AUTO_ADB_SERVER_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port <= 0 {
			return fmt.Errorf("invalid AUTO_ADB_SERVER_PORT %q: must be a positive integer", v)
		}
		c.ADB.Port = port
	}
	if v := envTrimmed("AUTO_AGENT_ACCESSIBILITY_COMPONENT"); v != "" {
		c.ADB.AccessibilityComponent = v
	}
	if v := os.Getenv("AUTO_ADB_SERIAL_BY_DEVICE"); v != "" {
		c.ADB.SerialByDevice = parseSerialByDevice(v)
	}
	if v := envTrimmed("AUTO_ADB_RECONCILE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid AUTO_ADB_RECONCILE_INTERVAL %q: %w", v, err)
		}
		c.ADB.ReconcileInterval = Duration(d)
	}
	if v := envTrimmed("AUTO_TOOL_LLM_API_URL"); v != "" {
		c.Tools.LLM.APIURL = v
	}
	if v := envTrimmed("AUTO_TOOL_LLM_MODEL"); v != "" {
		c.Tools.LLM.Model = v
	}
	if v := envTrimmed("AUTO_TOOL_LLM_API_KEY"); v != "" {
		c.Tools.LLM.APIKey = v
	}
	return nil
}

// formatSerialByDevice formats a deviceID→adbSerial map to the
// "deviceId1=serial1,deviceId2=serial2" string expected by
// devicectrl.NewAdbAccessibilityAutoEnabler. Output is sorted for determinism.
func formatSerialByDevice(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// parseSerialByDevice parses a "deviceId1=serial1,deviceId2=serial2" string
// into a map. Invalid entries are silently skipped, matching the behaviour
// of appport.parseADBSerialByDeviceMap.
func parseSerialByDevice(raw string) map[string]string {
	result := make(map[string]string)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		if k != "" && v != "" {
			result[k] = v
		}
	}
	return result
}

// envTrimmed returns the trimmed value of the environment variable key,
// or empty string if unset or blank.
func envTrimmed(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
