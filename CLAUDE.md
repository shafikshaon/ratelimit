# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A browser-based prototype for configuring and testing a multi-tier API rate limiting system. Designed for a financial API platform (wallets, payments, merchants) where different APIs have independent rate limits scoped by email and wallet.

## Running the App

No build system. Open files directly in a browser:
- `index.html` — Rate limit configuration UI
- `tester.html` — Request simulation and testing tool

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

## Tech Stack

- Vanilla JavaScript (ES6) + jQuery 3.7.1
- Tailwind CSS (CDN)
- No bundler, no npm, no backend

## Backend Integration (Not Yet Implemented)

The UI header references "Redis + DynamoDB" — this prototype simulates what would be:
- **Redis**: Rolling window timestamp storage
- **DynamoDB**: Persistent API config and override storage
