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

-- ── BALANCE ────────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('view_current_balance', 'BALANCE')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_current_balance:{e}:t1', 10,   'seconds', 7,    'transparent', TRUE),
               (aid, 2, 'wallet', 'rl:view_current_balance:{w}:t2', 3,    'hours',   100,  'transparent', TRUE),
               (aid, 3, 'wallet', 'rl:view_current_balance:{w}:t3', NULL, 'daily',   1500, 'transparent', TRUE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_mini_statement', 'BALANCE')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_mini_statement:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_mini_statement:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_mini_statement:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_account_summary', 'BALANCE')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_account_summary:{e}:t1', 10,   'seconds', 5,    'transparent', TRUE),
               (aid, 2, 'wallet', 'rl:view_account_summary:{w}:t2', 3,    'hours',   250,  'transparent', TRUE),
               (aid, 3, 'wallet', 'rl:view_account_summary:{w}:t3', NULL, 'daily',   1800, 'transparent', TRUE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── TRANSACTION ────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('view_recent_transaction', 'TRANSACTION')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_recent_transaction:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_recent_transaction:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_recent_transaction:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_detail_transaction', 'TRANSACTION')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_detail_transaction:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_detail_transaction:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_detail_transaction:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_transaction_history', 'TRANSACTION')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_transaction_history:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_transaction_history:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_transaction_history:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_transaction_receipt', 'TRANSACTION')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_transaction_receipt:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_transaction_receipt:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_transaction_receipt:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('export_transaction_csv', 'TRANSACTION')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:export_transaction_csv:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:export_transaction_csv:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:export_transaction_csv:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── PAYMENT ────────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('initiate_payment', 'PAYMENT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:initiate_payment:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:initiate_payment:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:initiate_payment:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('confirm_payment', 'PAYMENT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:confirm_payment:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:confirm_payment:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:confirm_payment:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('cancel_payment', 'PAYMENT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:cancel_payment:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:cancel_payment:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:cancel_payment:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('payment_status', 'PAYMENT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:payment_status:{e}:t1', 30,   'seconds', 3,   'enforce', TRUE),
               (aid, 2, 'wallet', 'rl:payment_status:{w}:t2', 1,    'minutes', 10,  'enforce', TRUE),
               (aid, 3, 'wallet', 'rl:payment_status:{w}:t3', NULL, 'daily',   200, 'enforce', TRUE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── MERCHANT ───────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('view_merchant_profile', 'MERCHANT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_merchant_profile:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_merchant_profile:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_merchant_profile:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_merchant_stats', 'MERCHANT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_merchant_stats:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_merchant_stats:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_merchant_stats:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_merchant_settlement', 'MERCHANT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_merchant_settlement:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_merchant_settlement:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_merchant_settlement:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('update_merchant_profile', 'MERCHANT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:update_merchant_profile:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:update_merchant_profile:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:update_merchant_profile:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── REPORT ─────────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('view_daily_report', 'REPORT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_daily_report:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_daily_report:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_daily_report:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_monthly_report', 'REPORT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_monthly_report:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_monthly_report:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_monthly_report:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('export_report_pdf', 'REPORT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:export_report_pdf:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:export_report_pdf:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:export_report_pdf:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_reconciliation', 'REPORT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_reconciliation:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_reconciliation:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_reconciliation:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── CUSTOMER ───────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('view_customer_profile', 'CUSTOMER')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_customer_profile:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_customer_profile:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_customer_profile:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_customer_kyc', 'CUSTOMER')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_customer_kyc:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_customer_kyc:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_customer_kyc:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('search_customer', 'CUSTOMER')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:search_customer:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:search_customer:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:search_customer:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── NOTIFICATION ───────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('view_notifications', 'NOTIFICATION')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_notifications:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_notifications:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_notifications:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('mark_notification_read', 'NOTIFICATION')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:mark_notification_read:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:mark_notification_read:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:mark_notification_read:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── AUTH ───────────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('refresh_token', 'AUTH')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:refresh_token:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:refresh_token:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:refresh_token:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('verify_pin', 'AUTH')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:verify_pin:{e}:t1', 60,   'seconds', 5,  'enforce', TRUE),
               (aid, 2, 'wallet', 'rl:verify_pin:{w}:t2', 5,    'minutes', 10, 'enforce', TRUE),
               (aid, 3, 'wallet', 'rl:verify_pin:{w}:t3', NULL, 'daily',   20, 'enforce', TRUE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('change_pin', 'AUTH')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:change_pin:{e}:t1', 60,   'seconds', 3,  'enforce', TRUE),
               (aid, 2, 'wallet', 'rl:change_pin:{w}:t2', 10,   'minutes', 5,  'enforce', TRUE),
               (aid, 3, 'wallet', 'rl:change_pin:{w}:t3', NULL, 'daily',   10, 'enforce', TRUE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── UTILITY ────────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('view_commission_rates', 'UTILITY')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_commission_rates:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_commission_rates:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_commission_rates:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_service_charges', 'UTILITY')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_service_charges:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_service_charges:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_service_charges:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_offers', 'UTILITY')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_offers:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_offers:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_offers:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_faq', 'UTILITY')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_faq:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_faq:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_faq:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── QR ─────────────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('generate_qr_code', 'QR')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:generate_qr_code:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:generate_qr_code:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:generate_qr_code:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('scan_qr_code', 'QR')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:scan_qr_code:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:scan_qr_code:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:scan_qr_code:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_qr_history', 'QR')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_qr_history:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_qr_history:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_qr_history:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── REFUND ─────────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('initiate_refund', 'REFUND')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:initiate_refund:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:initiate_refund:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:initiate_refund:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_refund_status', 'REFUND')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_refund_status:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_refund_status:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_refund_status:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('view_refund_history', 'REFUND')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_refund_history:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_refund_history:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_refund_history:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

-- ── SUPPORT ────────────────────────────────────────────────────────────────

INSERT INTO apis (name, group_name) VALUES ('view_support_tickets', 'SUPPORT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:view_support_tickets:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:view_support_tickets:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:view_support_tickets:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

INSERT INTO apis (name, group_name) VALUES ('create_support_ticket', 'SUPPORT')
    ON CONFLICT (name) DO NOTHING RETURNING id INTO aid;
IF aid IS NOT NULL THEN
    INSERT INTO api_tiers (api_id, tier, scope, redis_key, window_size, window_unit, max_requests, action_mode, enabled)
        VALUES (aid, 1, 'email',  'rl:create_support_ticket:{e}:t1', 10,   'seconds', 5,    'transparent', FALSE),
               (aid, 2, 'wallet', 'rl:create_support_ticket:{w}:t2', 3,    'hours',   100,  'transparent', FALSE),
               (aid, 3, 'wallet', 'rl:create_support_ticket:{w}:t3', NULL, 'daily',   1000, 'transparent', FALSE)
        ON CONFLICT (api_id, tier) DO NOTHING;
END IF;

END $$;
