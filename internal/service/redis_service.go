package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shafikshaon/ratelimit/internal/logger"
	"github.com/shafikshaon/ratelimit/internal/model"
	"go.uber.org/zap"
)

const (
	tierCachePrefix      = "rl:config:"
	overrideCachePrefix  = "rl:override:"
	overrideNullSentinel = "null"
	overrideCacheTTL     = 30 * time.Minute
)

// checkAllScript atomically checks and increments N tier counters in sequence.
// Evaluation stops at the first blocked tier (subsequent tiers are skipped, not incremented).
// This replaces 3 sequential EVALSHAs with a single atomic Lua execution.
//
// KEYS: [key1, key2, ..., keyN]   — one Redis key per tier counter
// ARGV: [limit1, ttl1, exp1, limit2, ttl2, exp2, ...]  — 3 values per tier
//
// Returns flat array: [pass1, count1, pass2, count2, ...]
//
//	pass = 1 (allowed), 0 (blocked), -1 (skipped because prior tier blocked)
var checkAllScript = redis.NewScript(`
local n = #KEYS
local results = {}
local blocked = false
for i = 1, n do
  if blocked then
    results[#results+1] = -1
    results[#results+1] = 0
  else
    local limit = tonumber(ARGV[(i-1)*3 + 1])
    local ttl   = tonumber(ARGV[(i-1)*3 + 2])
    local exp   = tonumber(ARGV[(i-1)*3 + 3])
    local cur = tonumber(redis.call('GET', KEYS[i])) or 0
    if cur >= limit then
      results[#results+1] = 0
      results[#results+1] = cur
      blocked = true
    else
      local newval = redis.call('INCR', KEYS[i])
      if newval == 1 then
        if ttl > 0 then redis.call('EXPIRE', KEYS[i], ttl)
        elseif exp > 0 then redis.call('EXPIREAT', KEYS[i], exp) end
      end
      results[#results+1] = 1
      results[#results+1] = newval
    end
  end
end
return results
`)

// TierResult holds the outcome of a single tier check from CheckAndIncrementAll.
type TierResult struct {
	Pass  int8 // 1=allowed, 0=blocked, -1=skipped
	Count int64
}

// RedisService handles all Redis operations: config caching, override caching,
// rate-limit counters, and usage reads.
type RedisService struct {
	client *redis.Client
}

func NewRedisService(client *redis.Client) *RedisService {
	return &RedisService{client: client}
}

// ── Tier config cache ─────────────────────────────────────────────────────────

func (s *RedisService) GetTierConfig(ctx context.Context, apiName string) ([]model.Tier, bool, error) {
	data, err := s.client.Get(ctx, tierCachePrefix+apiName).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var tiers []model.Tier
	if err := json.Unmarshal([]byte(data), &tiers); err != nil {
		return nil, false, nil // treat corrupt cache as a miss
	}
	return tiers, true, nil
}

func (s *RedisService) SetTierConfig(ctx context.Context, apiName string, tiers []model.Tier) {
	raw, err := json.Marshal(tiers)
	if err != nil {
		return
	}
	if err := s.client.Set(ctx, tierCachePrefix+apiName, raw, 0).Err(); err != nil {
		logger.L.Warn("redis: set tier config failed", zap.String("api", apiName), zap.Error(err))
	}
}

func (s *RedisService) DeleteTierConfig(ctx context.Context, apiName string) error {
	return s.client.Del(ctx, tierCachePrefix+apiName).Err()
}

// ── Override cache ────────────────────────────────────────────────────────────

func overrideCacheKey(apiName, wallet string) string {
	return fmt.Sprintf("%s%s:%s", overrideCachePrefix, apiName, wallet)
}

// GetOverrideRaw returns the raw cached value for a wallet override.
// ("null" sentinel = negatively cached; "" = cache miss).
func (s *RedisService) GetOverrideRaw(ctx context.Context, apiName, wallet string) (string, error) {
	val, err := s.client.Get(ctx, overrideCacheKey(apiName, wallet)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (s *RedisService) SetOverride(ctx context.Context, apiName, wallet string, o model.Override) {
	raw, err := json.Marshal(o)
	if err != nil {
		return
	}
	if err := s.client.Set(ctx, overrideCacheKey(apiName, wallet), raw, overrideCacheTTL).Err(); err != nil {
		logger.L.Warn("redis: set override failed", zap.String("api", apiName), zap.String("wallet", wallet), zap.Error(err))
	}
}

func (s *RedisService) SetOverrideNull(ctx context.Context, apiName, wallet string) {
	if err := s.client.Set(ctx, overrideCacheKey(apiName, wallet), overrideNullSentinel, overrideCacheTTL).Err(); err != nil {
		logger.L.Warn("redis: set override null failed", zap.String("api", apiName), zap.String("wallet", wallet), zap.Error(err))
	}
}

func (s *RedisService) DeleteOverrideCache(ctx context.Context, apiName, wallet string) {
	if err := s.client.Del(ctx, overrideCacheKey(apiName, wallet)).Err(); err != nil {
		logger.L.Warn("redis: delete override cache failed", zap.String("api", apiName), zap.String("wallet", wallet), zap.Error(err))
	}
}

// ── Bulk reads ────────────────────────────────────────────────────────────────

// MGet fetches multiple keys in a single round trip.
func (s *RedisService) MGet(ctx context.Context, keys ...string) ([]interface{}, error) {
	return s.client.MGet(ctx, keys...).Result()
}

// GetConfigAndOverride fetches tier config + override in one MGET round trip.
func (s *RedisService) GetConfigAndOverride(ctx context.Context, apiName, wallet string) (configRaw string, overrideRaw string, err error) {
	vals, err := s.client.MGet(ctx, tierCachePrefix+apiName, overrideCacheKey(apiName, wallet)).Result()
	if err != nil {
		return "", "", err
	}
	if vals[0] != nil {
		configRaw, _ = vals[0].(string)
	}
	if vals[1] != nil {
		overrideRaw, _ = vals[1].(string)
	}
	return configRaw, overrideRaw, nil
}

// GetOverrideOnly fetches only the override key (when config is already in process memory).
func (s *RedisService) GetOverrideOnly(ctx context.Context, apiName, wallet string) (string, error) {
	val, err := s.client.Get(ctx, overrideCacheKey(apiName, wallet)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

// ── Rate-limit counter ────────────────────────────────────────────────────────

// CheckAndIncrementAll atomically checks and increments all tier counters in one
// Redis round trip. Evaluation stops at the first blocked tier — subsequent tiers
// are not incremented (pass=-1, count=0 returned for them).
//
// keys, limits, ttlSecs, expiresAt must all have the same length.
func (s *RedisService) CheckAndIncrementAll(ctx context.Context, keys []string, limits []int, ttlSecs, expiresAt []int64) ([]TierResult, error) {
	n := len(keys)
	argv := make([]interface{}, 0, n*3)
	for i := 0; i < n; i++ {
		argv = append(argv, limits[i], ttlSecs[i], expiresAt[i])
	}

	raw, err := checkAllScript.Run(ctx, s.client, keys, argv...).Result()
	if err != nil {
		return nil, err
	}
	arr, ok := raw.([]interface{})
	if !ok || len(arr) < n*2 {
		return nil, fmt.Errorf("redis: unexpected Lua response type %T (len=%d, want>=%d)", raw, len(arr), n*2)
	}
	results := make([]TierResult, n)
	for i := 0; i < n; i++ {
		results[i].Pass = int8(toInt64(arr, i*2))
		results[i].Count = toInt64(arr, i*2+1)
	}
	return results, nil
}

// ── Usage read (counts + TTLs in one pipeline) ────────────────────────────────

type UsageEntry struct {
	Count   int64
	ResetIn int64 // seconds; -1 = no expiry / key missing
}

// GetUsageWithTTL fetches usage counts and TTLs for the given keys in a single pipeline.
func (s *RedisService) GetUsageWithTTL(ctx context.Context, keys []string) ([]UsageEntry, error) {
	pipe := s.client.Pipeline()
	mgetCmd := pipe.MGet(ctx, keys...)
	ttlCmds := make([]*redis.DurationCmd, len(keys))
	for i, k := range keys {
		ttlCmds[i] = pipe.TTL(ctx, k)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("redis pipeline: %w", err)
	}

	vals, err := mgetCmd.Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("redis pipeline mget: %w", err)
	}
	entries := make([]UsageEntry, len(keys))
	for i := range keys {
		if i < len(vals) && vals[i] != nil {
			if s, ok := vals[i].(string); ok {
				entries[i].Count, _ = strconv.ParseInt(s, 10, 64)
			}
		}
		entries[i].ResetIn = -1
		if ttl, err := ttlCmds[i].Result(); err == nil && ttl > 0 {
			entries[i].ResetIn = int64(ttl.Seconds())
		}
	}
	return entries, nil
}

func toInt64(arr []interface{}, i int) int64 {
	if arr == nil || i >= len(arr) {
		return 0
	}
	if v, ok := arr[i].(int64); ok {
		return v
	}
	return 0
}
