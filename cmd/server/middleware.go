package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shafikshaon/ratelimit/internal/logger"
	"github.com/shafikshaon/ratelimit/internal/querycount"
	"go.uber.org/zap"
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

// queryCountLoggerMiddleware injects a per-request query counter into the context,
// then logs Postgres / Redis / ScyllaDB query counts after the handler returns.
// Applied only to /api/v1 routes — not to health checks or static files.
func queryCountLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := querycount.NewContext(c.Request.Context())
		c.Request = c.Request.WithContext(ctx)
		start := time.Now()

		c.Next()

		qc := querycount.FromContext(c.Request.Context())
		if qc == nil {
			return
		}
		logger.L.Info("db query counts",
			zap.String("method", c.Request.Method),
			zap.String("path", c.FullPath()),
			zap.String("api", c.Param("name")),
			zap.Int("status", c.Writer.Status()),
			zap.Int("postgres", qc.Postgres),
			zap.Int("redis", qc.Redis),
			zap.Int("scylla", qc.Scylla),
			zap.Duration("latency", time.Since(start).Round(time.Microsecond)),
		)
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
