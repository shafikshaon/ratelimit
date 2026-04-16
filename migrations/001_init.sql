-- Schema

CREATE TABLE IF NOT EXISTS apis (
    id         SERIAL PRIMARY KEY,
    name       VARCHAR(200) NOT NULL UNIQUE,
    group_name VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_tiers (
    id           SERIAL PRIMARY KEY,
    api_id       INTEGER      NOT NULL REFERENCES apis(id) ON DELETE CASCADE,
    tier         INTEGER      NOT NULL CHECK (tier IN (1, 2, 3)),
    scope        VARCHAR(20)  NOT NULL CHECK (scope IN ('email', 'wallet')),
    redis_key    VARCHAR(300) NOT NULL,
    window_size  INTEGER,
    window_unit  VARCHAR(20)  NOT NULL CHECK (window_unit IN ('seconds', 'minutes', 'hours', 'daily')),
    max_requests INTEGER      NOT NULL,
    action_mode  VARCHAR(20)  NOT NULL DEFAULT 'transparent' CHECK (action_mode IN ('transparent', 'enforce')),
    enabled      BOOLEAN      NOT NULL DEFAULT FALSE,
    reset_hour   INTEGER               DEFAULT 0,
    UNIQUE (api_id, tier)
);

-- Seed data (idempotent)

DO $$
DECLARE
    aid INTEGER;
BEGIN

INSERT INTO apis (name, group_name) VALUES ('view_current_balance', 'BALANCE')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_current_balance:{e}:t1', 10,   'seconds', 7,    'transparent', TRUE),
               (aid, 2, 'wallet', 'rl:view_current_balance:{w}:t2', 3,    'hours',   100,  'transparent', TRUE),
               (aid, 3, 'wallet', 'rl:view_current_balance:{w}:t3', NULL, 'daily',   1500, 'transparent', TRUE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_recent_transactions', 'TRANSACTION')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_recent_transactions:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_recent_transactions:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_recent_transactions:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_detailed_transactions', 'TRANSACTION')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_detailed_transactions:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_detailed_transactions:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_detailed_transactions:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

END $$;
