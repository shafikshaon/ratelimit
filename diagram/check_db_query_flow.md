# Check Endpoint — DB Query Flow

`POST /api/v1/apis/:name/check`

```mermaid
sequenceDiagram
    actor Client
    participant H as APIHandler<br/>/check
    participant S as APIService
    participant Mem as Process Memory<br/>(configMem map)
    participant R as Redis
    participant SC as ScyllaDB
    participant PG as PostgreSQL

    Client->>H: POST /api/v1/apis/:name/check<br/>{email, wallet}

    %% ── Step 1: loadTiersAndOverride ─────────────────────────────
    H->>S: Check(ctx, apiName, email, wallet)
    S->>Mem: memGet(apiName)

    alt HOT PATH — config in process memory
        Mem-->>S: []Tier (hit)
        S->>R: GET rl:override:{api}:{wallet}
        R-->>S: overrideRaw ("null" / JSON / "")
    else WARM PATH — config in Redis, not in memory
        Mem-->>S: nil (miss)
        S->>R: MGET rl:config:{api}, rl:override:{api}:{wallet}
        R-->>S: configRaw + overrideRaw
        S->>Mem: memSet(apiName, tiers)
    else COLD PATH — config not in Redis either
        Mem-->>S: nil (miss)
        S->>R: MGET rl:config:{api}, rl:override:{api}:{wallet}
        R-->>S: "" + overrideRaw
        S->>PG: GetTiers(apiName)
        PG-->>S: []Tier
        S->>Mem: memSet(apiName, tiers)
        S->>R: SET rl:config:{api} (no TTL)
    end

    %% ── Step 2: resolveOverride ──────────────────────────────────
    alt overrideRaw == "" (cache miss)
        S->>SC: GetOne(apiName, wallet)
        SC-->>S: Override / not found
        alt Override found
            S->>R: SET rl:override:{api}:{wallet} JSON (30 min TTL)
        else Not found
            S->>R: SET rl:override:{api}:{wallet} "null" (30 min TTL)
        end
    else overrideRaw == "null" (negatively cached)
        Note over S: no override — use global defaults
    else overrideRaw is corrupt JSON
        S->>R: DEL rl:override:{api}:{wallet}
        S->>SC: GetOne(apiName, wallet)
        SC-->>S: Override / not found
        S->>R: SET rl:override:{api}:{wallet} JSON or "null"
    end

    %% ── Step 3: CheckAndIncrementAll ─────────────────────────────
    Note over S: resolveConfig() — merge tier limits<br/>with override (pure in-memory)
    Note over S: Build keys/limits/ttls for tiers<br/>that have a scopeID (skip email-scoped<br/>tiers if no email provided)
    S->>R: EVALSHA checkAllScript<br/>KEYS[T1,T2,T3] ARGV[limit,ttl,exp × N]
    Note over R: Lua runs atomically —<br/>stops at first blocked tier,<br/>marks rest as -1 (skipped)
    R-->>S: [pass1,count1, pass2,count2, ...]

    S-->>H: CheckResponse{allowed, tiers[]}
    H-->>Client: 200 OK / 429 Too Many Requests
```

## Redis RTT Summary

| Path | Redis RTTs | ScyllaDB | PostgreSQL |
|------|-----------|----------|-----------|
| Hot (memory hit) | **2** — GET override + EVALSHA | 0 | 0 |
| Warm (Redis hit) | **2** — MGET + EVALSHA | 0 | 0 |
| Cold (full miss) | **3** — MGET + SET config + EVALSHA | 0–1 | **1** |
| Override miss | +1 SET after ScyllaDB lookup | **1** | 0 |
