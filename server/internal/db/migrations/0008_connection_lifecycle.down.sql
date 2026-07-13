DROP INDEX idx_connections_org_user_created;
ALTER TABLE connections DROP COLUMN connect_token_used;
ALTER TABLE connections DROP COLUMN connect_token_expires_at;
ALTER TABLE connections DROP COLUMN token_expires_at;
