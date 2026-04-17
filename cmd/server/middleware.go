package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// requestTimeoutMiddleware cancels the request context after d, preventing slow
// storage operations from holding goroutines indefinitely.
func requestTimeoutMiddleware(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// healthHandler probes PostgreSQL and Redis. Returns 200 if both are reachable,
// 503 otherwise. Used by Kubernetes liveness/readiness probes.
func healthHandler(db *pgxpool.Pool, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "detail": "postgres unreachable"})
			return
		}
		if err := rdb.Ping(ctx).Err(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "detail": "redis unreachable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}
