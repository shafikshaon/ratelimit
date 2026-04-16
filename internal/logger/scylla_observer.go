package logger

import (
	"context"

	"github.com/gocql/gocql"
	"go.uber.org/zap"
)

type scyllaObserver struct{}

// NewScyllaObserver returns a gocql.QueryObserver that logs every CQL statement with duration.
func NewScyllaObserver() gocql.QueryObserver { return &scyllaObserver{} }

func (o *scyllaObserver) ObserveQuery(_ context.Context, q gocql.ObservedQuery) {
	fields := []zap.Field{
		zap.String("keyspace", q.Keyspace),
		zap.String("stmt", q.Statement),
		zap.Any("values", q.Values),
		zap.Duration("duration", q.End.Sub(q.Start)),
		zap.Int("rows", q.Rows),
	}
	if q.Err != nil {
		fields = append(fields, zap.Error(q.Err))
		L.Error("[scylla] query", fields...)
	} else {
		L.Debug("[scylla] query", fields...)
	}
}
