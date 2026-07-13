ALTER TABLE server_api_keys ADD COLUMN lookup_prefix VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE server_api_keys ADD COLUMN secret_hash VARCHAR(128) NOT NULL DEFAULT '';

CREATE INDEX idx_server_api_keys_lookup_prefix ON server_api_keys (lookup_prefix);

DROP TABLE server_api_key_secrets;
