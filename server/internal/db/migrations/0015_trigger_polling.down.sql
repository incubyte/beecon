ALTER TABLE event_logs DROP COLUMN trigger_instance_id;

DROP INDEX idx_trigger_instances_status_next_poll;

ALTER TABLE trigger_instances DROP COLUMN poll_lease_until;
ALTER TABLE trigger_instances DROP COLUMN next_poll_at;
ALTER TABLE trigger_instances DROP COLUMN paused_at;
ALTER TABLE trigger_instances DROP COLUMN seen_ids;
ALTER TABLE trigger_instances DROP COLUMN watermark_at;
