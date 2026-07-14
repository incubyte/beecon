CREATE TABLE org_governance (
    organization_id VARCHAR(64) NOT NULL PRIMARY KEY,
    allow_list TEXT NULL,
    hidden TEXT NOT NULL DEFAULT '[]',
    featured TEXT NOT NULL DEFAULT '[]',
    featured_cap INTEGER NOT NULL DEFAULT 8
);
