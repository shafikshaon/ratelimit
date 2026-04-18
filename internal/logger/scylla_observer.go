package logger

import (
	"context"

	"github.com/gocql/gocql"
)

type scyllaObserver struct{}

// NewScyllaObserver returns a gocql.QueryObserver (no-op: logging removed).
func NewScyllaObserver() gocql.QueryObserver { return &scyllaObserver{} }

func (o *scyllaObserver) ObserveQuery(_ context.Context, _ gocql.ObservedQuery) {}
