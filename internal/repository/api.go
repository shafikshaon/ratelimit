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

// ListGrouped returns APIs grouped by group_name — no tiers or overrides.
func (r *APIRepository) ListGrouped(ctx context.Context) ([]model.APIGroup, error) {
	rows, err := r.db.Query(ctx, `SELECT id, name, group_name FROM apis ORDER BY group_name, name`)
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

// ListAll returns every API — used for seeding Redis on startup.
func (r *APIRepository) ListAll(ctx context.Context) ([]model.API, error) {
	rows, err := r.db.Query(ctx, `SELECT id, name, group_name FROM apis ORDER BY id`)
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
