package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shafikshaon/ratelimit/internal/dto"
	"github.com/shafikshaon/ratelimit/internal/logger"
	"github.com/shafikshaon/ratelimit/internal/model"
	"go.uber.org/zap"
)

// errNotFound is returned when an update targets a row that does not exist.
var errNotFound = errors.New("not found")

const (
	// maxOverrideLimit caps the per-wallet override to prevent runaway limits.
	maxOverrideLimit = 10_000_000
)

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

// WarmCache fetches all API tiers in a single SQL query and populates the
// process-level memory cache and Redis in bulk — no N+1 queries.
func (s *APIService) WarmCache(ctx context.Context) error {
	allTiers, err := s.postgres.GetAllTiers(ctx)
	if err != nil {
		return err
	}
	for apiName, tiers := range allTiers {
		s.memSet(apiName, tiers)
		s.redis.SetTierConfig(ctx, apiName, tiers)
	}
	return nil
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

// UpdateTier writes to PostgreSQL then invalidates caches.
// Redis is invalidated before process memory so a concurrent reader can't repopulate
// memory from stale Redis data in the window between the two deletions.
func (s *APIService) UpdateTier(ctx context.Context, apiName string, tierNum int, t model.Tier) error {
	if err := s.postgres.UpdateTier(ctx, apiName, tierNum, t); err != nil {
		return err
	}
	if err := s.redis.DeleteTierConfig(ctx, apiName); err != nil {
		logger.L.Warn("failed to invalidate redis tier cache", zap.String("api", apiName), zap.Error(err))
	}
	s.memDel(apiName)
	return nil
}

// GetWalletConfig resolves effective limits for a wallet.
func (s *APIService) GetWalletConfig(ctx context.Context, apiName, wallet string) (*model.ResolvedConfig, error) {
	tiers, overrideRaw, err := s.loadTiersAndOverride(ctx, apiName, wallet)
	if err != nil {
		return nil, err
	}
	if tiers == nil {
		return nil, nil
	}
	override, err := s.resolveOverride(ctx, apiName, wallet, overrideRaw)
	if err != nil {
		return nil, err
	}
	return resolveConfig(apiName, wallet, tiers, override), nil
}

// Check evaluates tiers sequentially — T1 → T2 → T3.
// Hot path: 2 Redis round trips total (1 GET for override + 1 atomic Lua EVALSHA for all tiers).
// Tiers whose scope has no identifier (e.g. email-scoped tier when email is empty) are marked
// "skipped" in-memory and never sent to Redis, so they do not block subsequent tiers.
func (s *APIService) Check(ctx context.Context, apiName, email, wallet string) (*dto.CheckResponse, error) {
	// ── Step 1: resolve config + override ────────────────────────────────────
	tiers, overrideRaw, err := s.loadTiersAndOverride(ctx, apiName, wallet)
	if err != nil {
		return nil, err
	}
	if tiers == nil {
		return nil, nil // API not found
	}

	override, err := s.resolveOverride(ctx, apiName, wallet, overrideRaw)
	if err != nil {
		return nil, err
	}
	resolved := resolveConfig(apiName, wallet, tiers, override)

	// ── Step 2: separate real tiers (have a scopeID) from skipped ones ────────
	n := len(resolved.Tiers)
	scopeIDs := make([]string, n)
	type realEntry struct {
		origIdx  int
		key      string
		limit    int
		ttl      int64
		expireAt int64
	}
	var real []realEntry

	for i, rt := range resolved.Tiers {
		scopeID := email
		if rt.Scope == "wallet" {
			scopeID = wallet
		}
		scopeIDs[i] = scopeID
		if scopeID == "" {
			continue // skip — no identifier for this scope
		}
		key := usageKey(rt.RedisKey, email, wallet)
		var ttl, exp int64
		if rt.Unit == "daily" {
			exp = nextDailyReset(rt.ResetHour)
		} else {
			ttl = windowSeconds(rt.Window, rt.Unit)
		}
		real = append(real, realEntry{i, key, rt.EffectiveMax, ttl, exp})
	}

	// ── Step 3: single atomic Lua EVALSHA for real tiers only ────────────────
	results := make([]dto.TierCheckResult, n)
	allowed := true

	if len(real) > 0 {
		keys := make([]string, len(real))
		limits := make([]int, len(real))
		ttlSecs := make([]int64, len(real))
		expiresAt := make([]int64, len(real))
		for j, e := range real {
			keys[j] = e.key
			limits[j] = e.limit
			ttlSecs[j] = e.ttl
			expiresAt[j] = e.expireAt
		}

		tierResults, err := s.redis.CheckAndIncrementAll(ctx, keys, limits, ttlSecs, expiresAt)
		if err != nil {
			return nil, fmt.Errorf("check tiers: %w", err)
		}

		// Map real results back by original index.
		for j, e := range real {
			tr := tierResults[j]
			rt := resolved.Tiers[e.origIdx]
			var status string
			switch tr.Pass {
			case 1:
				status = "pass"
			case -1:
				status = "skipped" // blocked by a prior real tier in Lua
			default:
				status = "blocked"
				allowed = false
			}
			results[e.origIdx] = dto.TierCheckResult{
				Tier: rt.Tier, Scope: rt.Scope, Status: status,
				Used: tr.Count, Limit: rt.EffectiveMax, ScopeID: scopeIDs[e.origIdx],
			}
		}
	}

	// Fill skipped positions (tiers with no scopeID).
	for i, rt := range resolved.Tiers {
		if scopeIDs[i] == "" {
			results[i] = dto.TierCheckResult{
				Tier: rt.Tier, Scope: rt.Scope, Status: "skipped",
				Limit: rt.EffectiveMax, ScopeID: "",
			}
		}
	}

	return &dto.CheckResponse{Allowed: allowed, TierResults: results}, nil
}

// GetUsage reads current Redis counters and TTLs for all tiers in one pipeline round trip.
func (s *APIService) GetUsage(ctx context.Context, apiName, email, wallet string) ([]dto.TierUsage, error) {
	tiers, overrideRaw, err := s.loadTiersAndOverride(ctx, apiName, wallet)
	if err != nil {
		return nil, err
	}
	if tiers == nil {
		return nil, nil
	}

	override, err := s.resolveOverride(ctx, apiName, wallet, overrideRaw)
	if err != nil {
		return nil, err
	}
	resolved := resolveConfig(apiName, wallet, tiers, override)

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
		usage[i] = dto.TierUsage{
			Tier:    t.Tier,
			Scope:   t.Scope,
			Used:    entries[i].Count,
			Limit:   resolved.Tiers[i].EffectiveMax,
			ScopeID: scopeID,
			Window:  windowLabel(t),
			ResetIn: entries[i].ResetIn,
		}
	}
	return usage, nil
}

// loadTiersAndOverride is the shared hot/cold path for loading tier config and the
// wallet override raw value from process memory → Redis → PostgreSQL.
func (s *APIService) loadTiersAndOverride(ctx context.Context, apiName, wallet string) ([]model.Tier, string, error) {
	tiers := s.memGet(apiName)
	if tiers != nil {
		// Hot path: config in memory — one GET for override only.
		raw, err := s.redis.GetOverrideOnly(ctx, apiName, wallet)
		if err != nil {
			return nil, "", fmt.Errorf("redis override: %w", err)
		}
		return tiers, raw, nil
	}
	// Cold path: MGET config + override together.
	configRaw, overrideRaw, err := s.redis.GetConfigAndOverride(ctx, apiName, wallet)
	if err != nil {
		return nil, "", fmt.Errorf("redis mget: %w", err)
	}
	if configRaw != "" {
		var cached []model.Tier
		if err := json.Unmarshal([]byte(configRaw), &cached); err == nil {
			tiers = cached
			s.memSet(apiName, tiers)
		}
	}
	if tiers == nil {
		tiers, err = s.postgres.GetTiers(ctx, apiName)
		if err != nil {
			return nil, "", err
		}
		if tiers == nil {
			return nil, "", nil // API not found
		}
		s.memSet(apiName, tiers)
		s.redis.SetTierConfig(ctx, apiName, tiers)
	}
	return tiers, overrideRaw, nil
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
		// Cache miss — point lookup in ScyllaDB.
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
		// Corrupt cache entry — evict it and fall back to ScyllaDB.
		logger.L.Warn("corrupt override cache; evicting and refreshing",
			zap.String("api", apiName), zap.String("wallet", wallet), zap.Error(err))
		s.redis.DeleteOverrideCache(ctx, apiName, wallet)
		fresh, found, scyllaErr := s.scylla.GetOne(ctx, apiName, wallet)
		if scyllaErr != nil {
			return nil, fmt.Errorf("scylla get override: %w", scyllaErr)
		}
		if found {
			s.redis.SetOverride(ctx, apiName, wallet, fresh)
			return &fresh, nil
		}
		s.redis.SetOverrideNull(ctx, apiName, wallet)
		return nil, nil
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
				if v, err := strconv.Atoi(overrideStr); err == nil && v > 0 && v <= maxOverrideLimit {
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
// Curly braces are stripped from email and wallet first to prevent template injection
// (e.g. an email of "x{w}y" must not cause a double substitution).
func usageKey(template, email, wallet string) string {
	safeEmail := strings.NewReplacer("{", "", "}", "").Replace(email)
	safeWallet := strings.NewReplacer("{", "", "}", "").Replace(wallet)
	k := strings.ReplaceAll(template, "{e}", safeEmail)
	k = strings.ReplaceAll(k, "{w}", safeWallet)
	return k
}

var windowMultipliers = map[string]int64{"seconds": 1, "minutes": 60, "hours": 3600}

func windowSeconds(window *int, unit string) int64 {
	if window == nil {
		return 0
	}
	return int64(*window) * windowMultipliers[unit]
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
