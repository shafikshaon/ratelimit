package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shafikshaon/ratelimit/internal/logger"
	"github.com/shafikshaon/ratelimit/internal/repository"
	"go.uber.org/zap"
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
		logger.L.Info("migration applied", zap.String("file", entry.Name()))
	}
	return nil
}

// warmTierCache pre-populates Redis for every API in PostgreSQL at startup.
func warmTierCache(ctx context.Context, apiRepo *repository.APIRepository, tierRepo *repository.TierRepository) error {
	apis, err := apiRepo.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, api := range apis {
		if _, err := tierRepo.Get(ctx, api.Name); err != nil {
			logger.L.Warn("warm cache", zap.String("api", api.Name), zap.Error(err))
		}
	}
	return nil
}
