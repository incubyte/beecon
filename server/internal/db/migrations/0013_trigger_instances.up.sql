CREATE TABLE trigger_instances (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    organization_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    connection_id VARCHAR(64) NOT NULL,
    trigger_slug VARCHAR(255) NOT NULL,
    config TEXT NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_trigger_instances_org_connection ON trigger_instances (organization_id, connection_id);
CREATE INDEX idx_trigger_instances_org_user_created ON trigger_instances (organization_id, user_id, created_at);
