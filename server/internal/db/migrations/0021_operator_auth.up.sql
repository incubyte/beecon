CREATE TABLE operator_accounts (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    email VARCHAR(320) NOT NULL,
    password_hash VARCHAR(256) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_operator_accounts_email ON operator_accounts (email);

CREATE TABLE operator_sessions (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    operator_id VARCHAR(64) NOT NULL,
    token_hash VARCHAR(128) NOT NULL,
    csrf_token VARCHAR(128) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP NULL
);

CREATE UNIQUE INDEX idx_operator_sessions_token_hash ON operator_sessions (token_hash);
CREATE INDEX idx_operator_sessions_operator_id ON operator_sessions (operator_id);
