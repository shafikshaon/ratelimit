// Package querycount tracks per-request database query counts for Postgres,
// Redis, and ScyllaDB. A Counter is injected into the request context by
// middleware and retrieved by the service layer to increment counts.
// The log entry is written after the handler returns.
package querycount

import "context"

type ctxKey struct{}

// Counter holds the per-request query counts for all three storage backends.
// It is not goroutine-safe — it is owned by a single request goroutine.
type Counter struct {
	Postgres int
	Redis    int
	Scylla   int
}

// NewContext returns a child context with a fresh, zero-value Counter attached.
func NewContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, &Counter{})
}

// FromContext retrieves the Counter from ctx. Returns nil if not present
// (e.g. background tasks, health checks, or tests that don't set it up).
func FromContext(ctx context.Context) *Counter {
	c, _ := ctx.Value(ctxKey{}).(*Counter)
	return c
}

// IncPostgres increments the Postgres query counter by 1.
// No-op if ctx carries no counter (safe to call unconditionally).
func IncPostgres(ctx context.Context) {
	if c := FromContext(ctx); c != nil {
		c.Postgres++
	}
}

// IncRedis increments the Redis query counter by 1.
func IncRedis(ctx context.Context) {
	if c := FromContext(ctx); c != nil {
		c.Redis++
	}
}

// IncScylla increments the ScyllaDB query counter by 1.
func IncScylla(ctx context.Context) {
	if c := FromContext(ctx); c != nil {
		c.Scylla++
	}
}
