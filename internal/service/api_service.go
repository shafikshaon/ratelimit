package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shafikshaon/ratelimit/internal/dto"
	"github.com/shafikshaon/ratelimit/internal/model"
)

// errNotFound is returned when an update targets a row that does not exist.
var errNotFound = errors.New("not found")

func IsNotFound(err error) bool { return errors.Is(err, errNotFound) }

// bdtLocation is Asia/Dhaka (UTC+6). Loaded once at startup.
var bdtLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Dhaka")
	if err != nil {
		loc = time.FixedZone("BDT", 6*3600)
	}
	return loc
}()

// APIService orchestrates business logic across the three storage layers.
// It owns the process-level tier-config memory cache to avoid Redis round trips
// on the hot path.
type APIService struct {
	postgres *PostgresService
	redis    *RedisService
	scylla   *ScyllaService

	configMu  sync.RWMutex
	configMem map[string][]model.Tier // api_name → tiers (process-level cache)
}

func NewAPIService(pg *PostgresService, rd *RedisService, sc *ScyllaService) *APIService {
	return &APIService{
		postgres:  pg,
		redis:     rd,
		scylla:    sc,
		configMem: make(map[string][]model.Tier),
	}
}

// ── Process-level cache helpers ───────────────────────────────────────────────

func (s *APIService) memGet(apiName string) []model.Tier {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.configMem[apiName]
}

func (s *APIService) memSet(apiName string, tiers []model.Tier) {
	s.configMu.Lock()
	s.configMem[apiName] = tiers
	s.configMu.Unlock()
}

func (s *APIService) memDel(apiName string) {
	s.configMu.Lock()
	delete(s.configMem, apiName)
	s.configMu.Unlock()
}

// ── Public API ────────────────────────────────────────────────────────────────

func (s *APIService) ListAPIs(ctx context.Context) ([]model.APIGroup, error) {
	return s.postgres.ListGrouped(ctx)
}

func (s *APIService) ListAllAPIs(ctx context.Context) ([]model.API, error) {
	return s.postgres.ListAll(ctx)
}

// GetTierConfig returns tiers for apiName. Read order: process memory → Redis → PostgreSQL.
func (s *APIService) GetTierConfig(ctx context.Context, apiName string) ([]model.Tier, error) {
	// 1. Process memory (zero network cost)
	if tiers := s.memGet(apiName); tiers != nil {
		return tiers, nil
	}
	// 2. Redis cache
	if tiers, ok, err := s.redis.GetTierConfig(ctx, apiName); ok && err == nil {
		s.memSet(apiName, tiers)
		return tiers, nil
	}
	// 3. PostgreSQL
	tiers, err := s.postgres.GetTiers(ctx, apiName)
	if err != nil {
		return nil, err
	}
	if tiers == nil {
		return nil, nil
	}
	s.memSet(apiName, tiers)
	s.redis.SetTierConfig(ctx, apiName, tiers)
	return tiers, nil
}

// UpdateTier writes to PostgreSQL then invalidates process memory and Redis cache.
func (s *APIService) UpdateTier(ctx context.Context, apiName string, tierNum int, t model.Tier) error {
	if err := s.postgres.UpdateTier(ctx, apiName, tierNum, t); err != nil {
		return err
	}
	s.memDel(apiName)
	s.redis.DeleteTierConfig(ctx, apiName)
	return nil
}

// GetWalletConfig resolves effective limits for a wallet, fetching config + override
// in a single Redis MGET round trip where possible.
func (s *APIService) GetWalletConfig(ctx context.Context, apiName, wallet string) (*model.ResolvedConfig, error) {
	tiers := s.memGet(apiName)

	var overrideRaw string

	if tiers != nil {
		// Hot path: config already in process memory — only fetch override from Redis.
		raw, err := s.redis.GetOverrideOnly(ctx, apiName, wallet)
		if err != nil {
			return nil, fmt.Errorf("redis override: %w", err)
		}
		overrideRaw = raw
	} else {
		// Cold path: fetch config + override together in one MGET round trip.
		configRaw, ovRaw, err := s.redis.GetConfigAndOverride(ctx, apiName, wallet)
		if err != nil {
			return nil, fmt.Errorf("redis mget: %w", err)
		}
		overrideRaw = ovRaw

		if configRaw != "" {
			var cached []model.Tier
			if err := json.Unmarshal([]byte(configRaw), &cached); err == nil {
				tiers = cached
				s.memSet(apiName, tiers)
			}
		}
		if tiers == nil {
			var err error
			tiers, err = s.postgres.GetTiers(ctx, apiName)
			if err != nil {
				return nil, err
			}
			if tiers == nil {
				return nil, nil
			}
			s.memSet(apiName, tiers)
			s.redis.SetTierConfig(ctx, apiName, tiers)
		}
	}

	override, err := s.resolveOverride(ctx, apiName, wallet, overrideRaw)
	if err != nil {
		return nil, err
	}
	return resolveConfig(apiName, wallet, tiers, override), nil
}

// Check evaluates tiers sequentially — T1 → T2 → T3.
// If a tier is blocked, subsequent tiers are skipped (not incremented).
func (s *APIService) Check(ctx context.Context, apiName, email, wallet string) (*dto.CheckResponse, error) {
	resolved, err := s.GetWalletConfig(ctx, apiName, wallet)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, nil
	}

	results := make([]dto.TierCheckResult, 0, len(resolved.Tiers))
	blocked := false

	for _, rt := range resolved.Tiers {
		scopeID := email
		if rt.Scope == "wallet" {
			scopeID = wallet
		}

		if blocked || scopeID == "" {
			results = append(results, dto.TierCheckResult{
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

		pass, count, err := s.redis.CheckAndIncrement(ctx, key, rt.EffectiveMax, ttlSecs, expireAt)
		if err != nil {
			return nil, fmt.Errorf("check tier %d: %w", rt.Tier, err)
		}

		status := "pass"
		if !pass {
			status = "blocked"
			blocked = true
		}
		results = append(results, dto.TierCheckResult{
			Tier: rt.Tier, Scope: rt.Scope, Status: status,
			Used: count, Limit: rt.EffectiveMax, ScopeID: scopeID,
		})
	}

	return &dto.CheckResponse{Allowed: !blocked, TierResults: results}, nil
}

// GetUsage reads current Redis counters and TTLs for all tiers in one pipeline round trip.
func (s *APIService) GetUsage(ctx context.Context, apiName, email, wallet string) ([]dto.TierUsage, error) {
	tiers, err := s.GetTierConfig(ctx, apiName)
	if err != nil {
		return nil, err
	}
	if tiers == nil {
		return nil, nil
	}

	resolved, err := s.GetWalletConfig(ctx, apiName, wallet)
	if err != nil {
		return nil, err
	}

	keys := make([]string, len(tiers))
	for i, t := range tiers {
		keys[i] = usageKey(t.RedisKey, email, wallet)
	}

	entries, err := s.redis.GetUsageWithTTL(ctx, keys)
	if err != nil {
		return nil, err
	}

	usage := make([]dto.TierUsage, len(tiers))
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
		usage[i] = dto.TierUsage{
			Tier:    t.Tier,
			Scope:   t.Scope,
			Used:    entries[i].Count,
			Limit:   effectiveMax,
			ScopeID: scopeID,
			Window:  windowLabel(t),
			ResetIn: entries[i].ResetIn,
		}
	}
	return usage, nil
}

// ── Override operations (ScyllaDB + Redis cache) ──────────────────────────────

func (s *APIService) ListOverrides(ctx context.Context, apiName string, limit int, pageToken string) ([]model.Override, string, bool, error) {
	return s.scylla.List(ctx, apiName, limit, pageToken)
}

func (s *APIService) CreateOverride(ctx context.Context, apiName string, o model.Override) error {
	if err := s.scylla.Create(ctx, apiName, o); err != nil {
		return err
	}
	s.redis.DeleteOverrideCache(ctx, apiName, o.Wallet)
	return nil
}

func (s *APIService) DeleteOverride(ctx context.Context, apiName, wallet string) error {
	if err := s.scylla.Delete(ctx, apiName, wallet); err != nil {
		return err
	}
	s.redis.DeleteOverrideCache(ctx, apiName, wallet)
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// resolveOverride decodes the raw cached override value and falls back to ScyllaDB on miss.
func (s *APIService) resolveOverride(ctx context.Context, apiName, wallet, raw string) (*model.Override, error) {
	if raw == "" {
		// Cache miss — point lookup in ScyllaDB
		o, found, err := s.scylla.GetOne(ctx, apiName, wallet)
		if err != nil {
			return nil, fmt.Errorf("scylla get override: %w", err)
		}
		if found {
			s.redis.SetOverride(ctx, apiName, wallet, o)
			return &o, nil
		}
		s.redis.SetOverrideNull(ctx, apiName, wallet)
		return nil, nil
	}
	if raw == overrideNullSentinel {
		return nil, nil // negatively cached
	}
	var o model.Override
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return nil, nil // corrupt cache → treat as no override
	}
	return &o, nil
}

// resolveConfig merges global tier limits with an optional wallet override.
func resolveConfig(apiName, wallet string, tiers []model.Tier, override *model.Override) *model.ResolvedConfig {
	resolved := &model.ResolvedConfig{API: apiName, Wallet: wallet}
	for _, t := range tiers {
		rt := model.ResolvedTier{
			Tier:         t.Tier,
			Scope:        t.Scope,
			RedisKey:     t.RedisKey,
			Window:       t.Window,
			Unit:         t.Unit,
			GlobalMax:    t.MaxRequests,
			EffectiveMax: t.MaxRequests,
			ResetHour:    t.ResetHour,
		}
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

// usageKey substitutes {e}/{w} placeholders in a redis_key template.
func usageKey(template, email, wallet string) string {
	k := strings.ReplaceAll(template, "{e}", email)
	k = strings.ReplaceAll(k, "{w}", wallet)
	return k
}

func windowSeconds(window *int, unit string) int64 {
	if window == nil {
		return 0
	}
	mult := map[string]int64{"seconds": 1, "minutes": 60, "hours": 3600}
	return int64(*window) * mult[unit]
}

func nextDailyReset(resetHour int) int64 {
	now := time.Now().In(bdtLocation)
	reset := time.Date(now.Year(), now.Month(), now.Day(), resetHour, 0, 0, 0, bdtLocation)
	if !now.Before(reset) {
		reset = reset.Add(24 * time.Hour)
	}
	return reset.Unix()
}

func windowLabel(t model.Tier) string {
	if t.Unit == "daily" {
		return fmt.Sprintf("daily (resets %02d:00 BDT)", t.ResetHour)
	}
	if t.Window != nil {
		return fmt.Sprintf("%d %s", *t.Window, t.Unit)
	}
	return t.Unit
}
