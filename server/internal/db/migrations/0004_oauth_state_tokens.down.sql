DROP TABLE oauth_states;
ALTER TABLE connections DROP COLUMN account_display_name;
ALTER TABLE connections DROP COLUMN account_email;
ALTER TABLE connections DROP COLUMN encrypted_refresh_token;
ALTER TABLE connections DROP COLUMN encrypted_access_token;
