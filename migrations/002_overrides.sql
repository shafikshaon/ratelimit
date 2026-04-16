CREATE TABLE IF NOT EXISTS api_overrides (
    id         SERIAL PRIMARY KEY,
    api_id     INTEGER      NOT NULL REFERENCES apis(id) ON DELETE CASCADE,
    wallet     VARCHAR(100) NOT NULL,
    t1_limit   VARCHAR(50)  NOT NULL DEFAULT 'global',
    t2_limit   VARCHAR(50)  NOT NULL DEFAULT 'global',
    t3_limit   VARCHAR(50)  NOT NULL DEFAULT 'global',
    reason     TEXT         NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (api_id, wallet)
);
