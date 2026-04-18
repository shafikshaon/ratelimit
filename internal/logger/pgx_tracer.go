package logger

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type pgxTracer struct{}

// NewPgxTracer returns a pgx.QueryTracer (no-op: logging removed).
func NewPgxTracer() pgx.QueryTracer { return &pgxTracer{} }

func (t *pgxTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (t *pgxTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {}
