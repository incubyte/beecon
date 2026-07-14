DROP INDEX idx_connections_status_token_expires;

ALTER TABLE connections DROP COLUMN reconcile_lease_until;
ALTER TABLE connections DROP COLUMN reconciled_at;
ALTER TABLE connections DROP COLUMN refresh_lease_until;
