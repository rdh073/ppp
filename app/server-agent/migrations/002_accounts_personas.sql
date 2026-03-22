CREATE TABLE IF NOT EXISTS accounts (
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
);

CREATE INDEX IF NOT EXISTS idx_accounts_kind_device ON accounts(kind, device_id) WHERE device_id != '';
CREATE INDEX IF NOT EXISTS idx_accounts_status ON accounts(status);

CREATE TABLE IF NOT EXISTS personas (
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
);

CREATE INDEX IF NOT EXISTS idx_personas_status ON personas(status);
