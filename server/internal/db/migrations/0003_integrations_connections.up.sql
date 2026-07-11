ALTER TABLE organizations ADD COLUMN allowed_redirect_uris TEXT NOT NULL DEFAULT '[]';

CREATE TABLE integrations (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    provider_slug VARCHAR(64) NOT NULL,
    client_id VARCHAR(255) NOT NULL,
    client_secret VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_integrations_provider_slug ON integrations (provider_slug);

CREATE TABLE connections (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    integration_id VARCHAR(64) NOT NULL,
    provider_slug VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    redirect_uri VARCHAR(2048) NOT NULL,
    connect_token VARCHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_connections_org_id ON connections (org_id);
CREATE UNIQUE INDEX idx_connections_connect_token ON connections (connect_token);
