ALTER TABLE connections ADD COLUMN token_expires_at TIMESTAMP NULL;
ALTER TABLE connections ADD COLUMN connect_token_expires_at TIMESTAMP NOT NULL DEFAULT '1970-01-01 00:00:00';
ALTER TABLE connections ADD COLUMN connect_token_used BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE connections SET connect_token_used = TRUE WHERE status <> 'INITIATED';

CREATE INDEX idx_connections_org_user_created ON connections (org_id, user_id, created_at);
