CREATE TABLE catalog_activated_definitions (
    provider_slug VARCHAR(255) NOT NULL PRIMARY KEY,
    version VARCHAR(64) NOT NULL,
    content_hash VARCHAR(128) NOT NULL,
    bundle_json TEXT NOT NULL,
    activated_at TIMESTAMP NOT NULL
);
