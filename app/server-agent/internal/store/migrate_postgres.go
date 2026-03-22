package store

import (
	"context"
	"database/sql"
	"fmt"
)

// MigratePostgres runs all DDL migrations against the given database.
// Every statement uses IF NOT EXISTS, so this is safe to call on every startup.
func MigratePostgres(ctx context.Context, db *sql.DB) error {
	for i, ddl := range postgresMigrations {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("migration %d: %w", i, err)
		}
	}
	return nil
}

var postgresMigrations = []string{
	// 001: tasks, workflow states, event plane, task queue, command outbox
	`CREATE TABLE IF NOT EXISTS tasks (
		id VARCHAR(255) PRIMARY KEY,
		goal TEXT NOT NULL,
		input_artifacts JSONB,
		status VARCHAR(50) NOT NULL,
		assigned_device VARCHAR(255),
		workflow_name VARCHAR(255),
		created_at TIMESTAMP WITH TIME ZONE NOT NULL,
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS workflow_states (
		task_id VARCHAR(255) NOT NULL,
		device_id VARCHAR(255) NOT NULL,
		revision BIGINT NOT NULL,
		current_step VARCHAR(255) NOT NULL,
		retry_count INT NOT NULL,
		terminal_success BOOLEAN NOT NULL,
		waiting_expect JSONB,
		deadline_at TIMESTAMP WITH TIME ZONE,
		inputs JSONB,
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
		PRIMARY KEY (task_id, device_id)
	)`,
	`CREATE TABLE IF NOT EXISTS device_event_cursors (
		device_id VARCHAR(255) PRIMARY KEY,
		high_seq_no BIGINT NOT NULL,
		seen_ids JSONB NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS event_plane_accepted (
		id VARCHAR(255) PRIMARY KEY,
		event_id VARCHAR(255) NOT NULL,
		kind VARCHAR(255) NOT NULL,
		device_id VARCHAR(255) NOT NULL,
		seq_no BIGINT NOT NULL,
		occurred_at TIMESTAMP WITH TIME ZONE NOT NULL,
		payload JSONB,
		accepted_at TIMESTAMP WITH TIME ZONE NOT NULL,
		source VARCHAR(255) NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_event_plane_accepted_device ON event_plane_accepted(device_id, seq_no)`,
	`CREATE INDEX IF NOT EXISTS idx_event_plane_accepted_accepted_at ON event_plane_accepted(accepted_at)`,
	`CREATE TABLE IF NOT EXISTS event_plane_deadletters (
		id VARCHAR(255) PRIMARY KEY,
		event_id VARCHAR(255),
		kind VARCHAR(255),
		device_id VARCHAR(255),
		seq_no BIGINT,
		payload JSONB,
		reason TEXT NOT NULL,
		source VARCHAR(255) NOT NULL,
		recorded_at TIMESTAMP WITH TIME ZONE NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_event_plane_deadletters_recorded_at ON event_plane_deadletters(recorded_at)`,
	`CREATE TABLE IF NOT EXISTS task_queue (
		task_id VARCHAR(255) PRIMARY KEY,
		queued_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_task_queue_queued_at ON task_queue(queued_at)`,
	`CREATE TABLE IF NOT EXISTS command_outbox (
		command_id VARCHAR(255) PRIMARY KEY,
		kind VARCHAR(255) NOT NULL,
		device_id VARCHAR(255) NOT NULL,
		task_id VARCHAR(255) NOT NULL,
		params JSONB,
		issued_at TIMESTAMP WITH TIME ZONE NOT NULL,
		status VARCHAR(50) NOT NULL,
		last_error TEXT,
		last_result JSONB,
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL
	)`,

	// 002: accounts, personas
	`CREATE TABLE IF NOT EXISTS accounts (
		id                TEXT PRIMARY KEY,
		kind              TEXT NOT NULL,
		device_id         TEXT NOT NULL DEFAULT '',
		persona_id        TEXT NOT NULL DEFAULT '',
		email             TEXT NOT NULL DEFAULT '',
		username          TEXT NOT NULL DEFAULT '',
		password          TEXT NOT NULL DEFAULT '',
		linked_account_id TEXT NOT NULL DEFAULT '',
		status            TEXT NOT NULL DEFAULT 'deactive',
		created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_accounts_kind_device ON accounts(kind, device_id) WHERE device_id != ''`,
	`CREATE INDEX IF NOT EXISTS idx_accounts_status ON accounts(status)`,
	`CREATE TABLE IF NOT EXISTS personas (
		id          TEXT PRIMARY KEY,
		kind        TEXT NOT NULL DEFAULT '',
		first_name  TEXT NOT NULL DEFAULT '',
		last_name   TEXT NOT NULL DEFAULT '',
		gender      TEXT NOT NULL DEFAULT '',
		birth_date  TEXT NOT NULL DEFAULT '',
		email       TEXT NOT NULL DEFAULT '',
		username    TEXT NOT NULL DEFAULT '',
		password    TEXT NOT NULL DEFAULT '',
		status      TEXT NOT NULL DEFAULT 'available',
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_personas_status ON personas(status)`,
}
