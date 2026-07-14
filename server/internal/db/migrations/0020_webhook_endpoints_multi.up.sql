ALTER TABLE webhook_endpoints ADD COLUMN event_types TEXT NULL;
ALTER TABLE webhook_endpoints ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'ENABLED';
ALTER TABLE webhook_endpoints ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0;

DROP INDEX idx_webhook_endpoints_org_id;
CREATE INDEX idx_webhook_endpoints_org ON webhook_endpoints (organization_id);

ALTER TABLE webhook_signing_secrets ADD COLUMN endpoint_id VARCHAR(64) NULL;

UPDATE webhook_signing_secrets
SET endpoint_id = (
    SELECT id FROM webhook_endpoints
    WHERE webhook_endpoints.organization_id = webhook_signing_secrets.organization_id
)
WHERE endpoint_id IS NULL;

ALTER TABLE outbox_events ADD COLUMN endpoint_id VARCHAR(64) NULL;
