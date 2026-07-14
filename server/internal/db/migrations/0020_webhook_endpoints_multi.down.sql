ALTER TABLE outbox_events DROP COLUMN endpoint_id;

ALTER TABLE webhook_signing_secrets DROP COLUMN endpoint_id;

DROP INDEX idx_webhook_endpoints_org;
CREATE UNIQUE INDEX idx_webhook_endpoints_org_id ON webhook_endpoints (organization_id);

ALTER TABLE webhook_endpoints DROP COLUMN consecutive_failures;
ALTER TABLE webhook_endpoints DROP COLUMN status;
ALTER TABLE webhook_endpoints DROP COLUMN event_types;
