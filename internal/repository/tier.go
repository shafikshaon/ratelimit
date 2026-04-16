package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shafikshaon/ratelimit/internal/model"
)

const overrideCacheTTL = 5 * time.Minute

// TierRepository reads from PostgreSQL (source of truth) with a Redis read-through cache.
// Tier configs are also kept in a process-level memory map (configMem) so the hot path
// never needs a Redis round trip just to know key patterns — only the override and usage
// counters are fetched from Redis, combined into a single MGET.
type TierRepository struct {
	db           *pgxpool.Pool
	cache        *redis.Client
	overrideRepo *OverrideRepository

	configMu  sync.RWMutex
	configMem map[string][]model.Tier // api_name → tiers (process-level cache)
}

func NewTierRepository(db *pgxpool.Pool, cache *redis.Client, overrideRepo *OverrideRepository) *TierRepository {
	return &TierRepository{
		db:           db,
		cache:        cache,
		overrideRepo: overrideRepo,
		configMem:    make(map[string][]model.Tier),
	}
}

// memGet returns tiers from the process-level cache, or nil if not present.
func (r *TierRepository) memGet(apiName string) []model.Tier {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	return r.configMem[apiName]
}

// memSet stores tiers in the process-level cache.
func (r *TierRepository) memSet(apiName string, tiers []model.Tier) {
	r.configMu.Lock()
	r.configMem[apiName] = tiers
	r.configMu.Unlock()
}

// memDel removes an entry from the process-level cache (called on tier update).
func (r *TierRepository) memDel(apiName string) {
	r.configMu.Lock()
	delete(r.configMem, apiName)
	r.configMu.Unlock()
}

// Get returns tiers for apiName. Read order: process memory → Redis → PostgreSQL.
func (r *TierRepository) Get(ctx context.Context, apiName string) ([]model.Tier, error) {
	// 1. Process memory (zero network cost)
	if tiers := r.memGet(apiName); tiers != nil {
		return tiers, nil
	}

	// 2. Redis cache
	key := tierCachePrefix + apiName
	if data, err := r.cache.Get(ctx, key).Result(); err == nil {
		var tiers []model.Tier
		if err := json.Unmarshal([]byte(data), &tiers); err == nil {
			r.memSet(apiName, tiers)
			return tiers, nil
		}
	}

	// 3. PostgreSQL
	tiers, err := r.fetchFromDB(ctx, apiName)
	if err != nil {
		return nil, err
	}
	if tiers == nil {
		return nil, nil
	}
	r.memSet(apiName, tiers)
	if raw, err := json.Marshal(tiers); err == nil {
		r.cache.Set(ctx, key, raw, 0) // best-effort
	}
	return tiers, nil
}

func (r *TierRepository) fetchFromDB(ctx context.Context, apiName string) ([]model.Tier, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.id, t.tier, t.scope, t.redis_key, t.window_size, t.window_unit,
		       t.max_requests, t.reset_hour
		FROM api_tiers t
		JOIN apis a ON t.api_id = a.id
		WHERE a.name = $1
		ORDER BY t.tier
	`, apiName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tiers []model.Tier
	for rows.Next() {
		var t model.Tier
		if err := rows.Scan(&t.ID, &t.Tier, &t.Scope, &t.RedisKey,
			&t.Window, &t.Unit, &t.MaxRequests, &t.ResetHour); err != nil {
			return nil, err
		}
		tiers = append(tiers, t)
	}
	return tiers, rows.Err()
}

func (r *TierRepository) UpdateTier(ctx context.Context, apiName string, tierNum int, t model.Tier) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE api_tiers
		SET window_size = $1, window_unit = $2, max_requests = $3, reset_hour = $4
		FROM apis
		WHERE api_tiers.api_id = apis.id
		  AND apis.name = $5
		  AND api_tiers.tier = $6
	`, t.Window, t.Unit, t.MaxRequests, t.ResetHour, apiName, tierNum)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tier not found")
	}

	// Invalidate both process memory and Redis cache
	r.memDel(apiName)
	r.cache.Del(ctx, tierCachePrefix+apiName)
	return nil
}

// usageKey derives the actual Redis key for a tier's usage counter by substituting
// the scope placeholder in the redis_key template with the real email or wallet value.
//
// Template convention (set in migrations):
//
//	{e} → email scope
//	{w} → wallet scope
func usageKey(redisKeyTemplate, email, wallet string) string {
	k := strings.ReplaceAll(redisKeyTemplate, "{e}", email)
	k = strings.ReplaceAll(k, "{w}", wallet)
	return k
}

// GetWithOverride fetches tier config AND wallet override in a single Redis MGET round trip.
//
// Hot path (config already in process memory):
//
//	MGET rl:override:{api}:{wallet}
//	→ 1 Redis key, 1 round trip
//
// Cold path (config not in memory):
//
//	MGET rl:config:{api}  rl:override:{api}:{wallet}
//	→ 2 Redis keys, still 1 round trip
//
// Override absence is cached as the "null" sentinel (5-minute TTL) to avoid
// ScyllaDB on every request for wallets with no override.
func (r *TierRepository) GetWithOverride(ctx context.Context, apiName, wallet string) (*model.ResolvedConfig, error) {
	overrideKey := overrideCacheKey(apiName, wallet)

	// If config is already in process memory we only need the override key → 1 key MGET.
	// Otherwise include the config key so we still pay only 1 round trip.
	tiers := r.memGet(apiName)
	var configVal interface{}

	if tiers != nil {
		// Process memory hit — only fetch override from Redis
		vals, err := r.cache.MGet(ctx, overrideKey).Result()
		if err != nil {
			return nil, fmt.Errorf("redis mget: %w", err)
		}
		configVal = nil // already have tiers
		return r.resolveFromVals(ctx, apiName, wallet, tiers, vals[0])
	}

	// Process memory miss — fetch config + override together
	configKey := tierCachePrefix + apiName
	vals, err := r.cache.MGet(ctx, configKey, overrideKey).Result()
	if err != nil {
		return nil, fmt.Errorf("redis mget: %w", err)
	}
	configVal = vals[0]

	if configVal != nil {
		if s, ok := configVal.(string); ok {
			if err := json.Unmarshal([]byte(s), &tiers); err == nil {
				r.memSet(apiName, tiers)
			}
		}
	}
	if tiers == nil {
		var err error
		tiers, err = r.fetchFromDB(ctx, apiName)
		if err != nil {
			return nil, err
		}
		if tiers == nil {
			return nil, nil // API not found
		}
		r.memSet(apiName, tiers)
		if raw, err := json.Marshal(tiers); err == nil {
			r.cache.Set(ctx, configKey, raw, 0)
		}
	}

	return r.resolveFromVals(ctx, apiName, wallet, tiers, vals[1])
}

// GetWithOverrideAndUsage combines override + usage counter reads into a single MGET.
//
// Since tier configs are in process memory, the full hot-path Redis call is:
//
//	MGET rl:override:{api}:{wallet}
//	     rl:view_{api}:{email}:t1
//	     rl:view_{api}:{wallet}:t2
//	     rl:view_{api}:{wallet}:t3
//	→ (1 + numTiers) keys, 1 round trip
//
// Returns the resolved config and a map of usageKey → current raw value (may be nil on miss).
func (r *TierRepository) GetWithOverrideAndUsage(ctx context.Context, apiName, email, wallet string) (*model.ResolvedConfig, map[string]string, error) {
	tiers, err := r.Get(ctx, apiName)
	if err != nil {
		return nil, nil, err
	}
	if tiers == nil {
		return nil, nil, nil
	}

	// Build the full key list in one slice: override first, then per-tier usage keys.
	overrideKey := overrideCacheKey(apiName, wallet)
	keys := make([]string, 0, 1+len(tiers))
	keys = append(keys, overrideKey)
	for _, t := range tiers {
		keys = append(keys, usageKey(t.RedisKey, email, wallet))
	}

	// Single MGET — one Redis round trip regardless of number of tiers.
	vals, err := r.cache.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, nil, fmt.Errorf("redis mget: %w", err)
	}

	// vals[0] = override, vals[1..] = usage counters
	resolved, err := r.resolveFromVals(ctx, apiName, wallet, tiers, vals[0])
	if err != nil {
		return nil, nil, err
	}

	usage := make(map[string]string, len(tiers))
	for i, t := range tiers {
		if vals[i+1] != nil {
			if s, ok := vals[i+1].(string); ok {
				usage[usageKey(t.RedisKey, email, wallet)] = s
			}
		}
	}

	return resolved, usage, nil
}

// resolveFromVals handles the override cache value (or nil) and returns a ResolvedConfig.
func (r *TierRepository) resolveFromVals(ctx context.Context, apiName, wallet string, tiers []model.Tier, overrideVal interface{}) (*model.ResolvedConfig, error) {
	overrideKey := overrideCacheKey(apiName, wallet)
	var override *model.Override

	if overrideVal != nil {
		if s, ok := overrideVal.(string); ok && s != overrideNullSentinel {
			var o model.Override
			if err := json.Unmarshal([]byte(s), &o); err == nil {
				override = &o
			}
		}
		// s == "null" → negatively cached, skip ScyllaDB
	} else {
		// Cache miss — point lookup in ScyllaDB
		o, found, err := r.overrideRepo.GetOne(ctx, apiName, wallet)
		if err != nil {
			return nil, fmt.Errorf("scylla get override: %w", err)
		}
		if found {
			override = &o
			if raw, err := json.Marshal(o); err == nil {
				r.cache.Set(ctx, overrideKey, raw, overrideCacheTTL)
			}
		} else {
			r.cache.Set(ctx, overrideKey, overrideNullSentinel, overrideCacheTTL)
		}
	}

	return resolveConfig(apiName, wallet, tiers, override), nil
}

// checkIncrScript atomically checks a counter against a limit and increments it if allowed.
// Returns {1, new_count} on pass, {0, current_count} on block.
// ARGV[1]=limit  ARGV[2]=ttl_seconds (rolling; 0=skip)  ARGV[3]=expireat_unix (daily; 0=skip)
var checkIncrScript = redis.NewScript(`
local cur = tonumber(redis.call('GET', KEYS[1])) or 0
if cur >= tonumber(ARGV[1]) then return {0, cur} end
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  local ttl = tonumber(ARGV[2])
  local exp = tonumber(ARGV[3])
  if ttl > 0 then redis.call('EXPIRE', KEYS[1], ttl)
  elseif exp > 0 then redis.call('EXPIREAT', KEYS[1], exp) end
end
return {1, n}
`)

// windowSeconds converts a tier window to a TTL in seconds.
func windowSeconds(window *int, unit string) int64 {
	if window == nil {
		return 0
	}
	mult := map[string]int64{"seconds": 1, "minutes": 60, "hours": 3600}
	return int64(*window) * mult[unit]
}

// nextDailyReset returns the Unix timestamp of the next reset_hour in UTC.
func nextDailyReset(resetHour int) int64 {
	now := time.Now().UTC()
	reset := time.Date(now.Year(), now.Month(), now.Day(), resetHour, 0, 0, 0, time.UTC)
	if !now.Before(reset) {
		reset = reset.Add(24 * time.Hour)
	}
	return reset.Unix()
}

// windowLabel returns a human-readable window description for a tier.
func windowLabel(t model.Tier) string {
	if t.Unit == "daily" {
		return fmt.Sprintf("daily (resets %02d:00 UTC)", t.ResetHour)
	}
	if t.Window != nil {
		return fmt.Sprintf("%d %s", *t.Window, t.Unit)
	}
	return t.Unit
}

// Check evaluates tiers sequentially for an incoming request.
//
// Evaluation order: T1 → T2 → T3.
// If a tier is blocked, all subsequent tiers are skipped (not incremented).
// Only tiers that pass have their Redis counters incremented.
func (r *TierRepository) Check(ctx context.Context, apiName, email, wallet string) (*model.CheckResponse, error) {
	resolved, err := r.GetWithOverride(ctx, apiName, wallet)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, nil
	}

	results := make([]model.TierCheckResult, 0, len(resolved.Tiers))
	blocked := false // once true, remaining tiers are skipped

	for _, rt := range resolved.Tiers {
		scopeID := email
		if rt.Scope == "wallet" {
			scopeID = wallet
		}

		// Skip if a previous tier was blocked OR if scope ID is missing.
		if blocked || scopeID == "" {
			results = append(results, model.TierCheckResult{
				Tier: rt.Tier, Scope: rt.Scope, Status: "skipped",
				Used: 0, Limit: rt.EffectiveMax, ScopeID: scopeID,
			})
			continue
		}

		key := usageKey(rt.RedisKey, email, wallet)

		var ttlSecs, expireAt int64
		if rt.Unit == "daily" {
			expireAt = nextDailyReset(rt.ResetHour)
		} else {
			ttlSecs = windowSeconds(rt.Window, rt.Unit)
		}

		raw, err := checkIncrScript.Run(ctx, r.cache, []string{key},
			rt.EffectiveMax, ttlSecs, expireAt).Result()
		if err != nil {
			return nil, fmt.Errorf("check script tier %d: %w", rt.Tier, err)
		}

		arr, _ := raw.([]interface{})
		pass := toInt64(arr, 0) == 1
		count := toInt64(arr, 1)

		status := "pass"
		if !pass {
			status = "blocked"
			blocked = true // subsequent tiers will be skipped
		}
		results = append(results, model.TierCheckResult{
			Tier: rt.Tier, Scope: rt.Scope, Status: status,
			Used: count, Limit: rt.EffectiveMax, ScopeID: scopeID,
		})
	}

	return &model.CheckResponse{Allowed: !blocked, TierResults: results}, nil
}

// GetUsage reads current usage counters and TTLs for all tiers in one pipeline round trip.
func (r *TierRepository) GetUsage(ctx context.Context, apiName, email, wallet string) ([]model.TierUsage, error) {
	tiers, err := r.Get(ctx, apiName)
	if err != nil {
		return nil, err
	}
	if tiers == nil {
		return nil, nil
	}

	// Resolved config for effective limits (override-aware)
	resolved, err := r.GetWithOverride(ctx, apiName, wallet)
	if err != nil {
		return nil, err
	}

	keys := make([]string, len(tiers))
	for i, t := range tiers {
		keys[i] = usageKey(t.RedisKey, email, wallet)
	}

	// Single pipeline: MGET (counts) + TTL per key — one round trip.
	pipe := r.cache.Pipeline()
	mgetCmd := pipe.MGet(ctx, keys...)
	ttlCmds := make([]*redis.DurationCmd, len(keys))
	for i, k := range keys {
		ttlCmds[i] = pipe.TTL(ctx, k)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("redis pipeline usage: %w", err)
	}

	vals, _ := mgetCmd.Result()

	usage := make([]model.TierUsage, len(tiers))
	for i, t := range tiers {
		scopeID := email
		if t.Scope == "wallet" {
			scopeID = wallet
		}

		effectiveMax := t.MaxRequests
		if resolved != nil {
			for _, rt := range resolved.Tiers {
				if rt.Tier == t.Tier {
					effectiveMax = rt.EffectiveMax
					break
				}
			}
		}

		var used int64
		if i < len(vals) && vals[i] != nil {
			if s, ok := vals[i].(string); ok {
				used, _ = strconv.ParseInt(s, 10, 64)
			}
		}

		// TTL: >0 = seconds remaining, -1 = no expiry, -2 = key missing
		resetIn := int64(-1)
		if ttl, err := ttlCmds[i].Result(); err == nil && ttl > 0 {
			resetIn = int64(ttl.Seconds())
		}

		usage[i] = model.TierUsage{
			Tier:    t.Tier,
			Scope:   t.Scope,
			Used:    used,
			Limit:   effectiveMax,
			ScopeID: scopeID,
			Window:  windowLabel(t),
			ResetIn: resetIn,
		}
	}
	return usage, nil
}

// toInt64 safely extracts an int64 from a []interface{} at index i.
func toInt64(arr []interface{}, i int) int64 {
	if arr == nil || i >= len(arr) {
		return 0
	}
	if v, ok := arr[i].(int64); ok {
		return v
	}
	return 0
}

// resolveConfig merges global tier limits with an optional wallet override.
func resolveConfig(apiName, wallet string, tiers []model.Tier, override *model.Override) *model.ResolvedConfig {
	resolved := &model.ResolvedConfig{API: apiName, Wallet: wallet}
	for _, t := range tiers {
		rt := model.ResolvedTier{
			Tier:      t.Tier,
			Scope:     t.Scope,
			RedisKey:  t.RedisKey,
			Window:    t.Window,
			Unit:      t.Unit,
			GlobalMax: t.MaxRequests,
			ResetHour: t.ResetHour,
		}
		rt.EffectiveMax = t.MaxRequests

		if override != nil {
			var overrideStr string
			switch t.Tier {
			case 1:
				overrideStr = override.T1
			case 2:
				overrideStr = override.T2
			case 3:
				overrideStr = override.T3
			}
			if overrideStr != "" && overrideStr != "global" {
				var v int
				if _, err := fmt.Sscanf(overrideStr, "%d", &v); err == nil && v > 0 {
					rt.EffectiveMax = v
					rt.Overridden = true
				}
			}
		}
		resolved.Tiers = append(resolved.Tiers, rt)
	}
	return resolved
}
