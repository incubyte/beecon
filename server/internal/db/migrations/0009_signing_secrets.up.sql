CREATE TABLE signing_secrets (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    display_prefix VARCHAR(20) NOT NULL,
    encrypted_secret TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_signing_secrets_org_id ON signing_secrets (org_id);
