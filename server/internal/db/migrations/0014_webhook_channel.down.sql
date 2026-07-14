ALTER TABLE event_logs DROP COLUMN attempt;
ALTER TABLE event_logs DROP COLUMN event_id;

DROP TABLE outbox_events;
DROP TABLE webhook_signing_secrets;
DROP TABLE webhook_endpoints;
