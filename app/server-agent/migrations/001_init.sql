CREATE TABLE IF NOT EXISTS tasks (
    id VARCHAR(255) PRIMARY KEY,
    goal TEXT NOT NULL,
    input_artifacts JSONB,
    status VARCHAR(50) NOT NULL,
    assigned_device VARCHAR(255),
    workflow_name VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE TABLE IF NOT EXISTS workflow_states (
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
);

CREATE TABLE IF NOT EXISTS device_event_cursors (
    device_id VARCHAR(255) PRIMARY KEY,
    high_seq_no BIGINT NOT NULL,
    seen_ids JSONB NOT NULL
);

CREATE TABLE IF NOT EXISTS event_plane_accepted (
    id VARCHAR(255) PRIMARY KEY,
    event_id VARCHAR(255) NOT NULL,
    kind VARCHAR(255) NOT NULL,
    device_id VARCHAR(255) NOT NULL,
    seq_no BIGINT NOT NULL,
    occurred_at TIMESTAMP WITH TIME ZONE NOT NULL,
    payload JSONB,
    accepted_at TIMESTAMP WITH TIME ZONE NOT NULL,
    source VARCHAR(255) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_event_plane_accepted_device ON event_plane_accepted(device_id, seq_no);
CREATE INDEX IF NOT EXISTS idx_event_plane_accepted_accepted_at ON event_plane_accepted(accepted_at);

CREATE TABLE IF NOT EXISTS event_plane_deadletters (
    id VARCHAR(255) PRIMARY KEY,
    event_id VARCHAR(255),
    kind VARCHAR(255),
    device_id VARCHAR(255),
    seq_no BIGINT,
    payload JSONB,
    reason TEXT NOT NULL,
    source VARCHAR(255) NOT NULL,
    recorded_at TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_event_plane_deadletters_recorded_at ON event_plane_deadletters(recorded_at);

CREATE TABLE IF NOT EXISTS task_queue (
    task_id VARCHAR(255) PRIMARY KEY,
    queued_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_task_queue_queued_at ON task_queue(queued_at);

CREATE TABLE IF NOT EXISTS command_outbox (
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
);
