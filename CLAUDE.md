# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run (hot-reload)
air                        # requires Air installed
go run ./cmd/server        # without hot-reload

# Build
go build ./...

# Docker (all services)
docker-compose up          # starts Postgres, Redis, ScyllaDB + viewers + app

# Environment
cp .env.example .env       # then edit as needed
```

There are no tests. Migrations run automatically at startup from `migrations/` in filename order (idempotent — `ON CONFLICT DO NOTHING`).

## Architecture

### Request hot path

`POST /api/v1/apis/:name/check` is the performance-critical path. It runs in **2 Redis RTTs** on a warm cache:

1. **Process memory** (`configMem` map, guarded by `sync.RWMutex`) → tier config, zero network
2. **Redis GET** → override raw value for the wallet
3. **Redis EVALSHA** → single atomic Lua script checks and increments all tier counters in sequence, stopping at the first blocked tier

On a cold cache, step 1 becomes an MGET (config + override together) then a PostgreSQL fallback.

### Three storage layers

| Store | Keys / Tables | Purpose |
|-------|--------------|---------|
| PostgreSQL | `apis`, `api_tiers` | Source of truth for tier config |
| Redis | `rl:config:{api}`, `rl:override:{api}:{wallet}`, `rl:usage:...` | Process cache (no TTL for config), override cache (30 min TTL), rate-limit counters |
| ScyllaDB | `{keyspace}.api_overrides` | Per-wallet override storage; partition key = `api_name`; cursor pagination via page-state tokens |

### Cache invalidation order

`UpdateTier` deletes the **Redis key first**, then the **process memory key**. This prevents a concurrent reader from repopulating stale memory from stale Redis between the two deletions.

Override writes/deletes call `redis.DeleteOverrideCache` immediately after ScyllaDB so the next read gets fresh data.

### Lua script (`checkAllScript` in `redis_service.go`)

Replaces 3 sequential EVALSHAs with one atomic execution. Accepts N keys + 3 ARGV per tier (limit, ttl, expireAt). Returns `[pass, count, ...]` pairs — `pass` is `1` (allowed), `0` (blocked), or `-1` (skipped because a prior tier blocked). Real atomicity: Lua runs single-threaded in Redis.

Tiers with an empty `scopeID` (e.g. email-scoped T1 when no email is provided) are **excluded** from the Lua call entirely and filled as "skipped" in memory afterward — they must not be sent as zero-limit keys or they would falsely block subsequent tiers.

### Three-tier rate limiting model

| Tier | Scope | Typical window | Reset |
|------|-------|---------------|-------|
| T1 | email | seconds/minutes | rolling |
| T2 | wallet | hours | rolling |
| T3 | wallet | daily | fixed at `reset_hour` in Asia/Dhaka (BDT, UTC+6) |

Daily reset uses `EXPIREAT` (Unix timestamp of next reset). Rolling windows use `EXPIRE` (TTL in seconds).

### Override resolution

```
raw == ""               → cache miss → ScyllaDB lookup → re-cache
raw == "null"           → negatively cached (no override exists)
raw == corrupt JSON     → evict from Redis → ScyllaDB fallback
override value == ""
  or "global"           → use tier's global default
override value == "N"   → use N as effective max (capped at 10_000_000)
```

### Key design decisions

- **`usageKey()`** strips `{` and `}` from email/wallet before substituting into Redis key templates — prevents template injection.
- **`WarmCache()`** uses a single JOIN query (`GetAllTiers`) to populate all APIs at startup with no N+1.
- **`GetTiers()`** returns `nil` for "API not found" and `[]model.Tier{}` (non-nil empty slice) for "API exists, no tiers" — callers distinguish these to avoid false 404s.
- **`overrideCacheTTL = 30 * time.Minute`** — overrides only change via explicit operator action.
- **ScyllaDB page tokens** are size-checked on the *encoded* string before decoding (`len(pageToken) > (maxPageTokenBytes*4/3)+10`) to prevent OOM from crafted large tokens.
- **`gin.New()` + `gin.Recovery()`** — default Gin logger is disabled; all structured logging goes through Zap. Redis/pgx/Scylla hooks gate `fmt.Sprintf` behind `L.Core().Enabled(zapcore.DebugLevel)` to avoid hot-path allocations in production.
- **Request body capped at 64 KB** via `http.MaxBytesReader` middleware.
- **10 s request timeout** via `requestTimeoutMiddleware`; HTTP server Read/Write timeouts are 15 s.

## Go Package Layout

```
cmd/server/
  main.go        — wires everything: DB connections, service layer, router, graceful shutdown
  migrate.go     — reads migrations/ sorted by filename, executes each in a transaction
  middleware.go  — requestTimeoutMiddleware, healthHandler (/health, /ready)

internal/
  config/        — env vars → Config struct; Config.DSN() builds postgres DSN
  database/      — connection factories (pgxpool, redis.Client, gocql.Session + schema init)
  handler/       — thin HTTP layer; all validation lives here, all business logic in service
  service/
    api_service.go     — orchestration: hot/cold path, cache hierarchy, Check(), GetUsage(), overrides
    redis_service.go   — Redis ops + Lua script; CheckAndIncrementAll(), GetUsageWithTTL()
    postgres_service.go— SQL: GetAllTiers (JOIN), GetTiers, UpdateTier, ListGrouped, ListAll
    scylla_service.go  — ScyllaDB CRUD + cursor pagination; keyspace validated by regex at startup
  logger/        — Zap setup + hooks for Redis, pgx, ScyllaDB (PII gated behind debug level)
  model/         — shared structs: API, Tier, APIGroup, Override, ResolvedConfig, ResolvedTier
  dto/           — request/response shapes; CheckResponse, TierUsage, OverridePageResponse

migrations/
  001_init.sql   — creates apis + api_tiers tables; seeds sample APIs idempotently
```

## Full API Reference

| Method | Path | Handler |
|--------|------|---------|
| GET | `/api/v1/apis` | ListAPIs — grouped by category |
| GET | `/api/v1/apis/:name` | GetAPI — tiers for one API |
| PATCH | `/api/v1/apis/:name/tiers/:tier` | UpdateTier — tier must be 1–3 |
| GET | `/api/v1/apis/:name/config/:wallet` | GetWalletConfig — resolved limits (override applied) |
| POST | `/api/v1/apis/:name/check` | CheckRequest — enforces rate limit, increments counters |
| GET | `/api/v1/apis/:name/usage` | GetUsage — current counters + TTLs (`?email=&wallet=`) |
| GET | `/api/v1/apis/:name/overrides` | ListOverrides — paginated (`?limit=&page_token=`) |
| POST | `/api/v1/apis/:name/overrides` | CreateOverride |
| DELETE | `/api/v1/apis/:name/overrides/:wallet` | DeleteOverride |
| GET | `/health`, `/ready` | healthHandler — probes Postgres + Redis |

## Frontend

`index.html` (configuration UI) and `tester.html` (request simulator) are served as static files. They call the backend API directly — no localStorage, no simulation. Both use **Inter** (body) and **JetBrains Mono** (code/keys) from Google Fonts, Tailwind CSS (CDN), and jQuery 3.7.1.

- `index.html` served at both `/` and `/index.html`
- `tester.html` served at `/tester.html`

## Tech Stack

**Backend:** Go 1.23 · Gin · pgx/v5 · go-redis/v9 · gocql · Air  
**Frontend:** Vanilla JS · jQuery 3.7.1 · Tailwind CSS (CDN)  
**Infra:** PostgreSQL 18 · Redis 7 · ScyllaDB 6.2 · Docker Compose  
**Viewers:** Adminer `:8082` · RedisInsight `:5540` · cassandra-web `:3000`
