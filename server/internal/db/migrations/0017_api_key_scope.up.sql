ALTER TABLE server_api_keys ADD COLUMN scope VARCHAR(16) NOT NULL DEFAULT 'read-write';
