package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shafikshaon/ratelimit/internal/service"
)

func runMigrations(db *pgxpool.Pool) error {
	entries, err := os.ReadDir("./migrations")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		sql, err := os.ReadFile(filepath.Join("./migrations", entry.Name()))
		if err != nil {
			return err
		}
		if _, err := db.Exec(context.Background(), string(sql)); err != nil {
			return err
		}
	}
	return nil
}

// warmCache pre-populates the process memory and Redis caches for every API
// in a single PostgreSQL query — no N+1.
func warmCache(ctx context.Context, svc *service.APIService) error {
	return svc.WarmCache(ctx)
}
