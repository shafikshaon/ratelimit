package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/shafikshaon/ratelimit/internal/model"
)

// ScyllaService handles all ScyllaDB operations for per-wallet overrides.
// Partition key: api_name — all overrides for one API land on the same shard,
// enabling efficient range scans and cursor-based pagination without cross-partition fanout.
type ScyllaService struct {
	session  *gocql.Session
	keyspace string
}

func NewScyllaService(session *gocql.Session, keyspace string) *ScyllaService {
	return &ScyllaService{session: session, keyspace: keyspace}
}

// GetOne does a point lookup for a single wallet override.
func (s *ScyllaService) GetOne(ctx context.Context, apiName, wallet string) (model.Override, bool, error) {
	var o model.Override
	err := s.session.Query(fmt.Sprintf(`
		SELECT wallet, t1_limit, t2_limit, t3_limit, reason
		FROM %s.api_overrides
		WHERE api_name = ? AND wallet = ?
	`, s.keyspace), apiName, wallet).
		WithContext(ctx).
		Scan(&o.Wallet, &o.T1, &o.T2, &o.T3, &o.Reason)

	if err == gocql.ErrNotFound {
		return model.Override{}, false, nil
	}
	if err != nil {
		return model.Override{}, false, err
	}
	return o, true, nil
}

// List returns a cursor-paginated page of overrides for apiName.
// pageToken is a base64-encoded ScyllaDB page state; empty = first page.
func (s *ScyllaService) List(ctx context.Context, apiName string, limit int, pageToken string) ([]model.Override, string, bool, error) {
	q := s.session.Query(fmt.Sprintf(`
		SELECT wallet, t1_limit, t2_limit, t3_limit, reason
		FROM %s.api_overrides
		WHERE api_name = ?
	`, s.keyspace), apiName).
		WithContext(ctx).
		PageSize(limit)

	if pageToken != "" {
		state, err := base64.StdEncoding.DecodeString(pageToken)
		if err != nil {
			return nil, "", false, fmt.Errorf("invalid page token")
		}
		q = q.PageState(state)
	}

	iter := q.Iter()
	var overrides []model.Override
	var wallet, t1, t2, t3, reason string
	for iter.Scan(&wallet, &t1, &t2, &t3, &reason) {
		overrides = append(overrides, model.Override{
			Wallet: wallet, T1: t1, T2: t2, T3: t3, Reason: reason,
		})
	}

	nextState := iter.PageState()
	if err := iter.Close(); err != nil {
		return nil, "", false, err
	}

	hasMore := len(nextState) > 0
	nextToken := ""
	if hasMore {
		nextToken = base64.StdEncoding.EncodeToString(nextState)
	}
	if overrides == nil {
		overrides = []model.Override{}
	}
	return overrides, nextToken, hasMore, nil
}

// Create inserts or updates a wallet override (upsert semantics).
func (s *ScyllaService) Create(ctx context.Context, apiName string, o model.Override) error {
	if o.T1 == "" {
		o.T1 = "global"
	}
	if o.T2 == "" {
		o.T2 = "global"
	}
	if o.T3 == "" {
		o.T3 = "global"
	}
	return s.session.Query(fmt.Sprintf(`
		INSERT INTO %s.api_overrides (api_name, wallet, t1_limit, t2_limit, t3_limit, reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, s.keyspace), apiName, o.Wallet, o.T1, o.T2, o.T3, o.Reason, time.Now()).
		WithContext(ctx).Exec()
}

// Delete removes a wallet override.
func (s *ScyllaService) Delete(ctx context.Context, apiName, wallet string) error {
	return s.session.Query(fmt.Sprintf(`
		DELETE FROM %s.api_overrides WHERE api_name = ? AND wallet = ?
	`, s.keyspace), apiName, wallet).WithContext(ctx).Exec()
}
