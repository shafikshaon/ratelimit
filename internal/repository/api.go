package repository

import (
	"context"
	"fmt"

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
	apiRows, err := r.db.Query(ctx, `
		SELECT id, name, group_name FROM apis ORDER BY group_name, name
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
	if len(apis) == 0 {
		return []model.APIGroup{}, nil
	}

	// Fetch tiers
	tierRows, err := r.db.Query(ctx, `
		SELECT id, api_id, tier, scope, redis_key, window_size, window_unit,
		       max_requests, action_mode, enabled, reset_hour
		FROM api_tiers ORDER BY api_id, tier
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

	// Fetch overrides
	ovrRows, err := r.db.Query(ctx, `
		SELECT a.id, o.wallet, o.t1_limit, o.t2_limit, o.t3_limit, o.reason
		FROM api_overrides o JOIN apis a ON o.api_id = a.id
		ORDER BY o.created_at
	`)
	if err != nil {
		return nil, err
	}
	defer ovrRows.Close()

	overridesByAPI := make(map[int][]model.Override)
	for ovrRows.Next() {
		var apiID int
		var o model.Override
		if err := ovrRows.Scan(&apiID, &o.Wallet, &o.T1, &o.T2, &o.T3, &o.Reason); err != nil {
			return nil, err
		}
		overridesByAPI[apiID] = append(overridesByAPI[apiID], o)
	}
	if err := ovrRows.Err(); err != nil {
		return nil, err
	}

	// Group
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
			Overrides: overridesByAPI[a.id],
		}
		if api.Tiers == nil {
			api.Tiers = []model.Tier{}
		}
		if api.Overrides == nil {
			api.Overrides = []model.Override{}
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

func (r *APIRepository) UpdateTier(ctx context.Context, apiName string, tierNum int, t model.Tier) error {
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
	return nil
}

func (r *APIRepository) CreateOverride(ctx context.Context, apiName string, o model.Override) error {
	if o.T1 == "" {
		o.T1 = "global"
	}
	if o.T2 == "" {
		o.T2 = "global"
	}
	if o.T3 == "" {
		o.T3 = "global"
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO api_overrides (api_id, wallet, t1_limit, t2_limit, t3_limit, reason)
		SELECT id, $2, $3, $4, $5, $6 FROM apis WHERE name = $1
	`, apiName, o.Wallet, o.T1, o.T2, o.T3, o.Reason)
	return err
}

func (r *APIRepository) DeleteOverride(ctx context.Context, apiName, wallet string) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM api_overrides
		USING apis
		WHERE api_overrides.api_id = apis.id
		  AND apis.name = $1
		  AND api_overrides.wallet = $2
	`, apiName, wallet)
	return err
}
