package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shafikshaon/ratelimit/internal/model"
	"github.com/shafikshaon/ratelimit/internal/querycount"
)

// PostgresService handles all PostgreSQL queries.
type PostgresService struct {
	db *pgxpool.Pool
}

func NewPostgresService(db *pgxpool.Pool) *PostgresService {
	return &PostgresService{db: db}
}

// ListGrouped returns all APIs grouped by group_name (no tiers, cheap list query).
func (s *PostgresService) ListGrouped(ctx context.Context) ([]model.APIGroup, error) {
	querycount.IncPostgres(ctx)
	rows, err := s.db.Query(ctx, `SELECT id, name, group_name FROM apis ORDER BY group_name, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groupMap := make(map[string]*model.APIGroup)
	groupOrder := []string{}

	for rows.Next() {
		var api model.API
		if err := rows.Scan(&api.ID, &api.Name, &api.GroupName); err != nil {
			return nil, err
		}
		if _, exists := groupMap[api.GroupName]; !exists {
			groupMap[api.GroupName] = &model.APIGroup{Name: api.GroupName}
			groupOrder = append(groupOrder, api.GroupName)
		}
		groupMap[api.GroupName].APIs = append(groupMap[api.GroupName].APIs, api)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	groups := make([]model.APIGroup, 0, len(groupOrder))
	for _, name := range groupOrder {
		g := groupMap[name]
		g.Count = len(g.APIs)
		groups = append(groups, *g)
	}
	return groups, nil
}

// ListAll returns every API — used for warming the Redis cache on startup.
func (s *PostgresService) ListAll(ctx context.Context) ([]model.API, error) {
	querycount.IncPostgres(ctx)
	rows, err := s.db.Query(ctx, `SELECT id, name, group_name FROM apis ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apis []model.API
	for rows.Next() {
		var a model.API
		if err := rows.Scan(&a.ID, &a.Name, &a.GroupName); err != nil {
			return nil, err
		}
		apis = append(apis, a)
	}
	return apis, rows.Err()
}

// GetAllTiers fetches every tier for every API in a single query, returning a map of api_name → tiers.
// Used at startup to warm caches without an N+1 per API.
func (s *PostgresService) GetAllTiers(ctx context.Context) (map[string][]model.Tier, error) {
	querycount.IncPostgres(ctx)
	rows, err := s.db.Query(ctx, `
		SELECT a.name, t.id, t.tier, t.scope, t.redis_key, t.window_size, t.window_unit,
		       t.max_requests, t.reset_hour
		FROM api_tiers t
		JOIN apis a ON t.api_id = a.id
		ORDER BY a.name, t.tier
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]model.Tier)
	for rows.Next() {
		var apiName string
		var t model.Tier
		if err := rows.Scan(&apiName, &t.ID, &t.Tier, &t.Scope, &t.RedisKey,
			&t.Window, &t.Unit, &t.MaxRequests, &t.ResetHour); err != nil {
			return nil, err
		}
		result[apiName] = append(result[apiName], t)
	}
	return result, rows.Err()
}

// GetTiers fetches the full tier configuration for a single API from PostgreSQL.
// Returns (nil, nil) when the API name does not exist.
// Returns ([]model.Tier{}, nil) when the API exists but has no tiers configured.
func (s *PostgresService) GetTiers(ctx context.Context, apiName string) ([]model.Tier, error) {
	querycount.IncPostgres(ctx)
	rows, err := s.db.Query(ctx, `
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if tiers == nil {
		// Distinguish "API not found" from "API has no tiers".
		var exists bool
		querycount.IncPostgres(ctx)
		if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM apis WHERE name = $1)`, apiName).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			return []model.Tier{}, nil
		}
		return nil, nil
	}
	return tiers, nil
}

// UpdateTier writes new limits for a specific tier and returns an error if not found.
func (s *PostgresService) UpdateTier(ctx context.Context, apiName string, tierNum int, t model.Tier) error {
	querycount.IncPostgres(ctx)
	tag, err := s.db.Exec(ctx, `
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
		return errNotFound
	}
	return nil
}
