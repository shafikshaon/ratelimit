# Rate Limit

Multi-tier API rate limiting system for a financial platform.

## Quick Start

```bash
cp .env.example .env   # edit as needed
docker-compose up
```

App runs at **http://localhost:8080**

---

## Architecture

### Three storage layers

| Store | Purpose | Port |
|-------|---------|------|
| PostgreSQL 18 | API registry + tier config (source of truth) | 5433 |
| Redis 7 | Usage counters + config/override read-through cache | 6379 |
| ScyllaDB 6.2 | Per-wallet overrides | 9042 |

### Three-tier rate limiting model

| Tier | Scope | Typical window | Reset strategy |
|------|-------|---------------|---------------|
| T1 | email | seconds / minutes | Rolling (`EXPIRE`) |
| T2 | wallet | hours | Rolling (`EXPIRE`) |
| T3 | wallet | daily | Fixed at `reset_hour` BDT (`EXPIREAT`) |

### Check endpoint hot path (`POST /api/v1/apis/:name/check`)

Runs in **2 Redis round trips** on a warm cache:

1. **Process memory** — tier config served from an in-process `sync.RWMutex`-guarded map (zero network)
2. **Redis GET** — override raw value for the wallet
3. **Redis EVALSHA** — single atomic Lua script checks and increments all tier counters in sequence, stopping at the first blocked tier

| Cache state | Redis RTTs | ScyllaDB | PostgreSQL |
|-------------|-----------|----------|-----------|
| Hot (config in process memory, override cached) | **2** | 0 | 0 |
| Warm (config in Redis, not in memory) | **2** | 0 | 0 |
| Cold (config not cached anywhere) | **3** | 0–1 | **1** |
| Override cache miss | +1 | **1** | 0 |

### Override vs global config

The **global tier config** (PostgreSQL → Redis → process memory) defines structure for every wallet:
- Redis key template, scope (email/wallet), window, unit, reset hour, global max requests

A **per-wallet override** (ScyllaDB → Redis 30 min cache) stores only the limit override per tier (`t1`, `t2`, `t3`). The override replaces `max_requests` for that wallet; all structural fields (key, scope, window, TTL) always come from the global config.

### Cache invalidation

- `UpdateTier` deletes the Redis key **before** the process memory key — prevents a concurrent reader from repopulating stale memory from stale Redis.
- Override writes/deletes call `redis.DeleteOverrideCache` immediately after ScyllaDB so the next read gets fresh data.

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/apis` | List all APIs grouped by category |
| GET | `/api/v1/apis/:name` | Get tier config for an API |
| PATCH | `/api/v1/apis/:name/tiers/:tier` | Update tier limits (tier must be 1–3) |
| GET | `/api/v1/apis/:name/config/:wallet` | Resolved config for a wallet (override-aware) |
| POST | `/api/v1/apis/:name/check` | Rate-limit check + increment counters |
| GET | `/api/v1/apis/:name/usage?email=&wallet=` | Current usage counters + TTLs from Redis |
| GET | `/api/v1/apis/:name/overrides?limit=&page_token=` | List per-wallet overrides (cursor-paginated) |
| POST | `/api/v1/apis/:name/overrides` | Create a wallet override |
| DELETE | `/api/v1/apis/:name/overrides/:wallet` | Delete a wallet override |
| GET | `/health`, `/ready` | Health probe (checks Postgres + Redis) |

---

## Database Viewers

### PostgreSQL — Adminer

**URL:** http://localhost:8082

| Field | Value |
|-------|-------|
| System | PostgreSQL |
| Server | `postgres` |
| Username | `postgres` |
| Password | `postgres` |
| Database | `ratelimit` |

### Redis — RedisInsight

**URL:** http://localhost:5540

1. Click **Add Redis Database**
2. Host: `redis` · Port: `6379` · Name: `ratelimit`
3. Click **Add Redis Database**

### ScyllaDB — Cassandra Web

**URL:** http://localhost:3000

Pre-configured — connects to ScyllaDB automatically on startup.

Keyspace: `ratelimit` · Table: `api_overrides`

---

## Logging

Single structured log line per request via **Uber Zap** — query counts only.

```
{"level":"info","msg":"db query counts","method":"POST","path":"/api/v1/apis/:name/check",
 "api":"view_current_balance","status":200,"postgres":0,"redis":2,"scylla":0,"latency":"312µs"}
```

| Env var | Default | Description |
|---------|---------|-------------|
| `LOG_FILE` | `logs/app.log` | Path to the rotating JSON log file |
| `GIN_MODE` | _(unset)_ | `release` → JSON console; otherwise coloured console |

Log rotation (lumberjack): 100 MB max · 7 backups · 30 days · gzip compressed.

---

## Timezone

All daily reset windows use **Asia/Dhaka (BDT, UTC+6)**.

---

## Diagrams

See [`diagram/check_db_query_flow.md`](diagram/check_db_query_flow.md) for the full sequence diagram of the check endpoint DB query flow.
