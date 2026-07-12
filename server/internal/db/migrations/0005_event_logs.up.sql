CREATE TABLE event_logs (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL DEFAULT '',
    connection_id VARCHAR(64) NOT NULL DEFAULT '',
    tool_slug VARCHAR(255) NOT NULL DEFAULT '',
    kind VARCHAR(32) NOT NULL,
    status INTEGER NOT NULL,
    duration_ms BIGINT NOT NULL,
    request_body TEXT NOT NULL DEFAULT '',
    response_body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_event_logs_org_created_at ON event_logs (org_id, created_at DESC, id DESC);
CREATE INDEX idx_event_logs_org_connection_id ON event_logs (org_id, connection_id);
CREATE INDEX idx_event_logs_org_user_id ON event_logs (org_id, user_id);
CREATE INDEX idx_event_logs_org_tool_slug ON event_logs (org_id, tool_slug);
