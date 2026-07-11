ALTER TABLE connections ADD COLUMN encrypted_access_token TEXT NOT NULL DEFAULT '';
ALTER TABLE connections ADD COLUMN encrypted_refresh_token TEXT NOT NULL DEFAULT '';
ALTER TABLE connections ADD COLUMN account_email VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE connections ADD COLUMN account_display_name VARCHAR(255) NOT NULL DEFAULT '';

CREATE TABLE oauth_states (
    state VARCHAR(128) NOT NULL PRIMARY KEY,
    connection_id VARCHAR(64) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    consumed_at TIMESTAMP NULL
);

CREATE INDEX idx_oauth_states_connection_id ON oauth_states (connection_id);
