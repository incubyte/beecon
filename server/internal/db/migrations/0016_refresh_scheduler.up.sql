ALTER TABLE connections ADD COLUMN refresh_lease_until TIMESTAMP NULL;
ALTER TABLE connections ADD COLUMN reconciled_at TIMESTAMP NULL;
ALTER TABLE connections ADD COLUMN reconcile_lease_until TIMESTAMP NULL;

CREATE INDEX idx_connections_status_token_expires ON connections (status, token_expires_at);
