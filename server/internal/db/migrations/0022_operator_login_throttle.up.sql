ALTER TABLE operator_accounts ADD COLUMN failed_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE operator_accounts ADD COLUMN locked_until TIMESTAMP NULL;
