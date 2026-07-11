CREATE TABLE server_api_keys (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    lookup_prefix VARCHAR(20) NOT NULL,
    secret_hash VARCHAR(128) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP NULL
);

CREATE INDEX idx_server_api_keys_org_id ON server_api_keys (org_id);
CREATE INDEX idx_server_api_keys_lookup_prefix ON server_api_keys (lookup_prefix);

CREATE TABLE users (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    external_id VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_users_org_id ON users (org_id);
