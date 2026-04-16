package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shafikshaon/ratelimit/internal/repository"
)

func runMigrations(db *pgxpool.Pool) error {
	entries, err := os.ReadDir("./migrations")
	if err != nil {
		return err
	}
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
		log.Printf("migration applied: %s", entry.Name())
	}
	return nil
}

// warmTierCache pre-populates Redis for every API in PostgreSQL at startup.
// On cache miss the TierRepository will populate on first request anyway,
// but warming up avoids cold-start latency.
func warmTierCache(ctx context.Context, apiRepo *repository.APIRepository, tierRepo *repository.TierRepository) error {
	apis, err := apiRepo.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, api := range apis {
		if _, err := tierRepo.Get(ctx, api.Name); err != nil {
			log.Printf("warn: warm cache for %s: %v", api.Name, err)
		}
	}
	return nil
}
