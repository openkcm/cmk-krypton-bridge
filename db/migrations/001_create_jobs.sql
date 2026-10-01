-- +goose Up
CREATE TABLE key_hierarchy_jobs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status       TEXT        NOT NULL DEFAULT 'pending',
    tenant_id    TEXT        NOT NULL,
    service_id   TEXT        NOT NULL DEFAULT '',
    level        INT         NOT NULL,
    metadata     JSONB,
    attempts     INT         NOT NULL DEFAULT 0,
    max_attempts INT         NOT NULL DEFAULT 10,
    last_error   TEXT        NOT NULL DEFAULT '',
    result_key_id TEXT       NOT NULL DEFAULT '',
    retry_after  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_jobs_claimable
    ON key_hierarchy_jobs (retry_after, status)
    WHERE status IN ('pending', 'retrying');

-- +goose Down
DROP TABLE key_hierarchy_jobs;
