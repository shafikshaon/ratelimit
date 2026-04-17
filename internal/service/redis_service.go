package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shafikshaon/ratelimit/internal/model"
)

const (
	tierCachePrefix      = "rl:config:"
	overrideCachePrefix  = "rl:override:"
	overrideNullSentinel = "null"
	overrideCacheTTL     = 5 * time.Minute
)

// checkIncrScript atomically checks a counter against a limit and increments if allowed.
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
	if raw, err := json.Marshal(tiers); err == nil {
		s.client.Set(ctx, tierCachePrefix+apiName, raw, 0) // best-effort, no TTL
	}
}

func (s *RedisService) DeleteTierConfig(ctx context.Context, apiName string) {
	s.client.Del(ctx, tierCachePrefix+apiName)
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
	if raw, err := json.Marshal(o); err == nil {
		s.client.Set(ctx, overrideCacheKey(apiName, wallet), raw, overrideCacheTTL)
	}
}

func (s *RedisService) SetOverrideNull(ctx context.Context, apiName, wallet string) {
	s.client.Set(ctx, overrideCacheKey(apiName, wallet), overrideNullSentinel, overrideCacheTTL)
}

func (s *RedisService) DeleteOverrideCache(ctx context.Context, apiName, wallet string) {
	s.client.Del(ctx, overrideCacheKey(apiName, wallet))
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

// CheckAndIncrement atomically checks the counter and increments it if under the limit.
// Returns (pass, currentCount, error).
func (s *RedisService) CheckAndIncrement(ctx context.Context, key string, limit int, ttlSecs, expireAt int64) (bool, int64, error) {
	raw, err := checkIncrScript.Run(ctx, s.client, []string{key}, limit, ttlSecs, expireAt).Result()
	if err != nil {
		return false, 0, err
	}
	arr, _ := raw.([]interface{})
	pass := toInt64(arr, 0) == 1
	count := toInt64(arr, 1)
	return pass, count, nil
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

	vals, _ := mgetCmd.Result()
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
