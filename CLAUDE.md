# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A browser-based prototype for configuring and testing a multi-tier API rate limiting system. Designed for a financial API platform (wallets, payments, merchants) where different APIs have independent rate limits scoped by email and wallet.

## Running the App

**Docker (recommended):**
```bash
docker-compose up
```
Starts PostgreSQL and the Go API server with Air hot-reload. Migrations run automatically on startup.

**Local Go server:**
```bash
cp .env.example .env
go mod download
air                          # hot-reload via air
# or: go run ./cmd/server
```

**Frontend only:** Open `index.html` or `tester.html` directly in a browser (no server needed).

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/apis` | List all APIs grouped by category |

Response shape:
```json
{
  "data": [
    {
      "name": "BALANCE",
      "count": 3,
      "apis": [
        {
          "id": 1,
          "name": "view_current_balance",
          "group": "BALANCE",
          "tiers": [{ "tier": 1, "scope": "email", "unit": "seconds", ... }]
        }
      ]
    }
  ]
}
```

## Architecture

### Two-Page Frontend Application

**`index.html`** — Configuration interface (~1000 lines)
- Define per-API rate limit tiers (windows, limits, action modes)
- Manage per-wallet overrides with audit trail (reason field)
- Live usage visualization with usage bars per tier

**`tester.html`** — Testing and simulation tool (~670 lines)
- Fire N requests against a selected API with configurable email/wallet scope
- Real-time tier status cards (pass / throttled / blocked)
- Detailed per-request log showing tier-by-tier enforcement results

### Data Flow

Both pages share state via `localStorage`:
- Config key: `ratelimit_config_v2` — API definitions and overrides
- Usage key: `ratelimit_usage_v2` — Request timestamps per scope/tier
- Cross-tab sync via `window.addEventListener('storage', ...)`

### Three-Tier Rate Limiting Model

Each API has up to 3 independent tiers evaluated in sequence:

| Tier | Scope | Typical Window |
|------|-------|---------------|
| T1 | Email | Seconds |
| T2 | Wallet | Hours |
| T3 | Wallet | Daily |

**Action modes**: `transparent` (count but allow) vs `enforce` (block when exceeded)

**Window types**:
- `seconds` / `minutes` / `hours`: Rolling window using stored timestamp arrays
- `daily`: Fixed window resetting at a configurable hour (0–23)

### Override Resolution

```
Per-wallet override for this API exists?
  → value != "global": use override limit
  → value == "global": use tier default
No override: use tier default
```

### Key Functions (tester.html)

- `fireRequest(apiName, email, wallet)` — Core enforcement logic; returns `{ allowed, tiers[] }`
- `getCurrentCount(key, window, unit, resetHour)` — Rolling/daily window count
- `recordRequest(key)` — Appends timestamp to usage log in localStorage
- `runTest()` — Fires N requests with delay, updates log and tier cards

### Key Functions (index.html)

- `loadConfig()` / `persist()` — Read/write config to localStorage
- `buildTierCard(t, idx)` — Renders a tier control card with all fields
- `buildOverridesCard(apiName)` — Override management: lookup, add, edit, delete
- `refreshUsageOnCards(apiName)` — Updates live usage bars

## Go Project Structure

```
cmd/server/
  main.go       — server bootstrap, graceful shutdown
  migrate.go    — runs SQL files from migrations/ at startup
internal/
  config/       — env-based config (DB_HOST, SERVER_PORT, etc.)
  database/     — pgxpool connection setup
  handler/      — Gin HTTP handlers
  model/        — shared structs (API, Tier, APIGroup)
  repository/   — SQL queries against postgres
migrations/
  001_init.sql  — schema creation + idempotent seed data
```

Migrations are idempotent (`ON CONFLICT DO NOTHING`) and run on every startup from `migrations/` in filename order.

## Storage Architecture

| Store | What lives there | Why |
|-------|-----------------|-----|
| **PostgreSQL** | `apis` (registry) + `api_tiers` (tier config source of truth) | Durable, transactional |
| **Redis** | Read-through cache for tier configs (`rl:config:{api_name}`) | Fast reads; invalidated on write |
| **ScyllaDB** | Per-wallet overrides (`ratelimit.api_overrides`) | Partition key = `api_name`; supports millions of rows + native cursor pagination |

On startup the app warms the Redis cache for every API in PostgreSQL. A tier `PATCH` writes to PostgreSQL then deletes the Redis key so the next read is fresh.

Override pagination uses ScyllaDB page-state tokens (base64-encoded), not offset. Requests: `GET /api/v1/apis/:name/overrides?limit=20&page_token=<token>`.

## Tech Stack

**Backend:** Go 1.23 · Gin · pgx/v5 · go-redis/v9 · gocql · Air (hot-reload)  
**Frontend:** Vanilla JavaScript (ES6) + jQuery 3.7.1 · Tailwind CSS (CDN)  
**Infrastructure:** PostgreSQL 16 · Redis 7 · ScyllaDB 5.4 · Docker Compose

## Backend Integration (Not Yet Implemented)

The UI header references "Redis + DynamoDB" — this prototype simulates what would be:
- **Redis**: Rolling window timestamp storage
- **DynamoDB**: Persistent API config and override storage
