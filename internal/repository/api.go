package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shafikshaon/ratelimit/internal/model"
)

type APIRepository struct {
	db *pgxpool.Pool
}

func NewAPIRepository(db *pgxpool.Pool) *APIRepository {
	return &APIRepository{db: db}
}

func (r *APIRepository) ListGrouped(ctx context.Context) ([]model.APIGroup, error) {
	// Fetch all apis ordered by group and name
	apiRows, err := r.db.Query(ctx, `
		SELECT id, name, group_name
		FROM apis
		ORDER BY group_name, name
	`)
	if err != nil {
		return nil, err
	}
	defer apiRows.Close()

	type apiRow struct {
		id        int
		name      string
		groupName string
	}

	var apis []apiRow
	for apiRows.Next() {
		var a apiRow
		if err := apiRows.Scan(&a.id, &a.name, &a.groupName); err != nil {
			return nil, err
		}
		apis = append(apis, a)
	}
	if err := apiRows.Err(); err != nil {
		return nil, err
	}

	// Build api id list for tier query
	if len(apis) == 0 {
		return []model.APIGroup{}, nil
	}

	// Fetch all tiers
	tierRows, err := r.db.Query(ctx, `
		SELECT id, api_id, tier, scope, redis_key, window_size, window_unit,
		       max_requests, action_mode, enabled, reset_hour
		FROM api_tiers
		ORDER BY api_id, tier
	`)
	if err != nil {
		return nil, err
	}
	defer tierRows.Close()

	tiersByAPI := make(map[int][]model.Tier)
	for tierRows.Next() {
		var t model.Tier
		var apiID int
		if err := tierRows.Scan(
			&t.ID, &apiID, &t.Tier, &t.Scope, &t.RedisKey,
			&t.Window, &t.Unit, &t.MaxRequests, &t.ActionMode, &t.Enabled, &t.ResetHour,
		); err != nil {
			return nil, err
		}
		tiersByAPI[apiID] = append(tiersByAPI[apiID], t)
	}
	if err := tierRows.Err(); err != nil {
		return nil, err
	}

	// Group apis
	groupMap := make(map[string]*model.APIGroup)
	groupOrder := []string{}

	for _, a := range apis {
		if _, exists := groupMap[a.groupName]; !exists {
			groupMap[a.groupName] = &model.APIGroup{Name: a.groupName}
			groupOrder = append(groupOrder, a.groupName)
		}
		api := model.API{
			ID:        a.id,
			Name:      a.name,
			GroupName: a.groupName,
			Tiers:     tiersByAPI[a.id],
		}
		if api.Tiers == nil {
			api.Tiers = []model.Tier{}
		}
		groupMap[a.groupName].APIs = append(groupMap[a.groupName].APIs, api)
	}

	groups := make([]model.APIGroup, 0, len(groupOrder))
	for _, name := range groupOrder {
		g := groupMap[name]
		g.Count = len(g.APIs)
		groups = append(groups, *g)
	}

	return groups, nil
}
