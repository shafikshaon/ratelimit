package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shafikshaon/ratelimit/internal/model"
)

const overrideCacheTTL = 5 * time.Minute

// TierRepository reads from PostgreSQL (source of truth) with a Redis read-through cache.
// Writes always go to PostgreSQL first; the cache key is invalidated so the next read re-populates it.
type TierRepository struct {
	db           *pgxpool.Pool
	cache        *redis.Client
	overrideRepo *OverrideRepository
}

func NewTierRepository(db *pgxpool.Pool, cache *redis.Client, overrideRepo *OverrideRepository) *TierRepository {
	return &TierRepository{db: db, cache: cache, overrideRepo: overrideRepo}
}

func (r *TierRepository) Get(ctx context.Context, apiName string) ([]model.Tier, error) {
	key := tierCachePrefix + apiName

	// Cache hit
	if data, err := r.cache.Get(ctx, key).Result(); err == nil {
		var tiers []model.Tier
		if err := json.Unmarshal([]byte(data), &tiers); err == nil {
			return tiers, nil
		}
	}

	// Cache miss — fetch from PostgreSQL
	tiers, err := r.fetchFromDB(ctx, apiName)
	if err != nil {
		return nil, err
	}
	if tiers == nil {
		return nil, nil
	}

	// Populate cache (best-effort — don't fail the request on cache error)
	if raw, err := json.Marshal(tiers); err == nil {
		r.cache.Set(ctx, key, raw, 0)
	}

	return tiers, nil
}

func (r *TierRepository) fetchFromDB(ctx context.Context, apiName string) ([]model.Tier, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.id, t.tier, t.scope, t.redis_key, t.window_size, t.window_unit,
		       t.max_requests, t.action_mode, t.enabled, t.reset_hour
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
			&t.Window, &t.Unit, &t.MaxRequests, &t.ActionMode, &t.Enabled, &t.ResetHour); err != nil {
			return nil, err
		}
		tiers = append(tiers, t)
	}
	return tiers, rows.Err()
}

func (r *TierRepository) UpdateTier(ctx context.Context, apiName string, tierNum int, t model.Tier) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE api_tiers
		SET window_size = $1, window_unit = $2, max_requests = $3,
		    action_mode = $4, enabled = $5, reset_hour = $6
		FROM apis
		WHERE api_tiers.api_id = apis.id
		  AND apis.name = $7
		  AND api_tiers.tier = $8
	`, t.Window, t.Unit, t.MaxRequests, t.ActionMode, t.Enabled, t.ResetHour, apiName, tierNum)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tier not found")
	}

	// Invalidate cache so next read re-fetches fresh data
	r.cache.Del(ctx, tierCachePrefix+apiName)
	return nil
}

// GetWithOverride fetches tier config AND wallet override in a single Redis MGET round trip.
// On cache miss for either key, it falls back to PostgreSQL/ScyllaDB and caches the result.
// Override absence is cached as the "null" sentinel (5-minute TTL) to avoid ScyllaDB on every miss.
func (r *TierRepository) GetWithOverride(ctx context.Context, apiName, wallet string) (*model.ResolvedConfig, error) {
	configKey := tierCachePrefix + apiName
	overrideKey := overrideCacheKey(apiName, wallet)

	// Single round trip: fetch both keys at once
	vals, err := r.cache.MGet(ctx, configKey, overrideKey).Result()
	if err != nil {
		return nil, fmt.Errorf("redis mget: %w", err)
	}

	// --- Tier config ---
	var tiers []model.Tier
	if vals[0] != nil {
		if s, ok := vals[0].(string); ok {
			_ = json.Unmarshal([]byte(s), &tiers)
		}
	}
	if tiers == nil {
		tiers, err = r.fetchFromDB(ctx, apiName)
		if err != nil {
			return nil, err
		}
		if tiers == nil {
			return nil, nil // API not found
		}
		if raw, err := json.Marshal(tiers); err == nil {
			r.cache.Set(ctx, configKey, raw, 0)
		}
	}

	// --- Override ---
	var override *model.Override
	if vals[1] != nil {
		if s, ok := vals[1].(string); ok && s != overrideNullSentinel {
			var o model.Override
			if err := json.Unmarshal([]byte(s), &o); err == nil {
				override = &o
			}
		}
		// s == "null" → cached absence, skip ScyllaDB
	} else {
		// Cache miss — check ScyllaDB
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
			// Cache the negative result to avoid ScyllaDB on future requests
			r.cache.Set(ctx, overrideKey, overrideNullSentinel, overrideCacheTTL)
		}
	}

	return resolveConfig(apiName, wallet, tiers, override), nil
}

// resolveConfig merges global tier limits with an optional wallet override.
func resolveConfig(apiName, wallet string, tiers []model.Tier, override *model.Override) *model.ResolvedConfig {
	resolved := &model.ResolvedConfig{API: apiName, Wallet: wallet}
	for _, t := range tiers {
		rt := model.ResolvedTier{
			Tier:       t.Tier,
			Scope:      t.Scope,
			RedisKey:   t.RedisKey,
			Window:     t.Window,
			Unit:       t.Unit,
			GlobalMax:  t.MaxRequests,
			ActionMode: t.ActionMode,
			Enabled:    t.Enabled,
			ResetHour:  t.ResetHour,
		}
		rt.EffectiveMax = t.MaxRequests
		rt.Overridden = false

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
