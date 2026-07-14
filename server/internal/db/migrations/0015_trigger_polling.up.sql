ALTER TABLE trigger_instances ADD COLUMN watermark_at TIMESTAMP NULL;
ALTER TABLE trigger_instances ADD COLUMN seen_ids TEXT NOT NULL DEFAULT '[]';
ALTER TABLE trigger_instances ADD COLUMN paused_at TIMESTAMP NULL;
ALTER TABLE trigger_instances ADD COLUMN next_poll_at TIMESTAMP NULL;
ALTER TABLE trigger_instances ADD COLUMN poll_lease_until TIMESTAMP NULL;

CREATE INDEX idx_trigger_instances_status_next_poll ON trigger_instances (status, next_poll_at);

ALTER TABLE event_logs ADD COLUMN trigger_instance_id TEXT NULL;
