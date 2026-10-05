-- +goose Up
CREATE TABLE managed_keys (
    proxy_key_id           TEXT PRIMARY KEY,
    job_id                 UUID NOT NULL REFERENCES key_hierarchy_jobs(id),
    krypton_key_id         TEXT NOT NULL,
    tenant_id              TEXT NOT NULL,
    service_id             TEXT NOT NULL DEFAULT '',
    level                  INT NOT NULL,
    parent_krypton_key_id  TEXT NOT NULL DEFAULT '',
    name                   TEXT NOT NULL DEFAULT '',
    kind                   TEXT NOT NULL DEFAULT '',
    status                 TEXT NOT NULL DEFAULT '',
    metadata               JSONB,
    last_seen_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_managed_keys_tenant_service
    ON managed_keys (tenant_id, service_id, level);

-- +goose Down
DROP TABLE managed_keys;
