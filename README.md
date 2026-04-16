# Rate Limit

Multi-tier API rate limiting system for a financial platform.

## Quick Start

```bash
docker-compose up
```

App runs at **http://localhost:8080**

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

---

### Redis — RedisInsight

**URL:** http://localhost:5540

1. Click **Add Redis Database**
2. Fill in:

| Field | Value |
|-------|-------|
| Host | `redis` |
| Port | `6379` |
| Name | `ratelimit` (any label) |

3. Click **Add Redis Database**

---

### ScyllaDB — Cassandra Web

**URL:** http://localhost:3000

Pre-configured — connects to ScyllaDB automatically on startup.

Keyspace: `ratelimit`  
Tables: `api_overrides`

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/apis` | List all APIs grouped |
| GET | `/api/v1/apis/:name` | Get tier config for an API |
| PATCH | `/api/v1/apis/:name/tiers/:tier` | Update tier limits |
| GET | `/api/v1/apis/:name/config/:wallet` | Resolved config for a wallet (override-aware) |
| POST | `/api/v1/apis/:name/check` | Rate-limit check + increment counters |
| GET | `/api/v1/apis/:name/usage?email=&wallet=` | Current usage counters from Redis |
| GET | `/api/v1/apis/:name/overrides` | List per-wallet overrides (paginated) |
| POST | `/api/v1/apis/:name/overrides` | Create/update a wallet override |
| DELETE | `/api/v1/apis/:name/overrides/:wallet` | Delete a wallet override |

---

## Storage

| Store | Purpose | Port |
|-------|---------|------|
| PostgreSQL 18 | API registry + tier config (source of truth) | 5433 |
| Redis 7 | Usage counters + config read-through cache | 6379 |
| ScyllaDB 6.2 | Per-wallet overrides | 9042 |

## Timezone

All daily reset windows use **Asia/Dhaka (BDT, UTC+6)**.
