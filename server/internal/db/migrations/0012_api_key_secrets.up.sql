CREATE TABLE server_api_key_secrets (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    key_id VARCHAR(64) NOT NULL,
    lookup_prefix VARCHAR(20) NOT NULL,
    secret_hash VARCHAR(128) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NULL
);

CREATE INDEX idx_server_api_key_secrets_key_id ON server_api_key_secrets (key_id);
CREATE INDEX idx_server_api_key_secrets_lookup_prefix ON server_api_key_secrets (lookup_prefix);

INSERT INTO server_api_key_secrets (id, key_id, lookup_prefix, secret_hash, created_at, expires_at)
SELECT id, id, lookup_prefix, secret_hash, created_at, NULL
FROM server_api_keys;

DROP INDEX idx_server_api_keys_lookup_prefix;
ALTER TABLE server_api_keys DROP COLUMN lookup_prefix;
ALTER TABLE server_api_keys DROP COLUMN secret_hash;
