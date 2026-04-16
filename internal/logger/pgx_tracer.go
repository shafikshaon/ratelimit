package logger

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

type pgxTracer struct{}

// NewPgxTracer returns a pgx.QueryTracer that logs every SQL query with args and duration.
func NewPgxTracer() pgx.QueryTracer { return &pgxTracer{} }

type pgxCtxKey struct{}

type pgxSpan struct {
	sql   string
	args  []any
	start time.Time
}

func (t *pgxTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, pgxCtxKey{}, &pgxSpan{
		sql:   d.SQL,
		args:  d.Args,
		start: time.Now(),
	})
}

func (t *pgxTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	span, _ := ctx.Value(pgxCtxKey{}).(*pgxSpan)
	if span == nil {
		return
	}
	fields := []zap.Field{
		zap.String("sql", span.sql),
		zap.Any("args", span.args),
		zap.Duration("duration", time.Since(span.start)),
		zap.Int64("rows_affected", d.CommandTag.RowsAffected()),
	}
	if d.Err != nil {
		fields = append(fields, zap.Error(d.Err))
		L.Error("[postgres] query", fields...)
	} else {
		L.Debug("[postgres] query", fields...)
	}
}
