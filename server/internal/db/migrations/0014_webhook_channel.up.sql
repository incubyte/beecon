CREATE TABLE webhook_endpoints (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    organization_id VARCHAR(64) NOT NULL,
    url TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_webhook_endpoints_org_id ON webhook_endpoints (organization_id);

CREATE TABLE webhook_signing_secrets (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    organization_id VARCHAR(64) NOT NULL,
    display_prefix VARCHAR(20) NOT NULL,
    encrypted_secret TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NULL
);

CREATE INDEX idx_webhook_signing_secrets_org_id ON webhook_signing_secrets (organization_id);

CREATE TABLE outbox_events (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    organization_id VARCHAR(64) NOT NULL,
    type VARCHAR(64) NOT NULL,
    body TEXT NOT NULL,
    status VARCHAR(32) NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMP NOT NULL,
    last_attempt_at TIMESTAMP NULL,
    lease_until TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_outbox_events_status_next_attempt ON outbox_events (status, next_attempt_at);
CREATE INDEX idx_outbox_events_org_created_at ON outbox_events (organization_id, created_at);

ALTER TABLE event_logs ADD COLUMN event_id TEXT NULL;
ALTER TABLE event_logs ADD COLUMN attempt INTEGER NOT NULL DEFAULT 0;
