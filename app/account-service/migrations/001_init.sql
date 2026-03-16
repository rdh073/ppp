CREATE TABLE IF NOT EXISTS google_accounts (
    id         TEXT PRIMARY KEY,
    email      TEXT NOT NULL UNIQUE,
    password   TEXT NOT NULL,
    device_id  TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS instagram_accounts (
    id                TEXT PRIMARY KEY,
    username          TEXT NOT NULL UNIQUE,
    contact           TEXT NOT NULL DEFAULT '',
    password          TEXT NOT NULL,
    device_id         TEXT NOT NULL DEFAULT '',
    google_account_id TEXT REFERENCES google_accounts(id) ON DELETE SET NULL,
    status            TEXT NOT NULL DEFAULT 'active',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_goog_device ON google_accounts(device_id) WHERE device_id != '';
CREATE INDEX IF NOT EXISTS idx_goog_status ON google_accounts(status);
CREATE INDEX IF NOT EXISTS idx_ig_device   ON instagram_accounts(device_id) WHERE device_id != '';
CREATE INDEX IF NOT EXISTS idx_ig_google   ON instagram_accounts(google_account_id) WHERE google_account_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ig_status   ON instagram_accounts(status);
