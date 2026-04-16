package database

import (
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/shafikshaon/ratelimit/internal/config"
)

func NewScyllaSession(cfg *config.Config) (*gocql.Session, error) {
	cluster := gocql.NewCluster(cfg.ScyllaHosts...)
	cluster.Consistency = gocql.LocalQuorum
	cluster.Timeout = 30 * time.Second
	cluster.ConnectTimeout = 30 * time.Second
	cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{NumRetries: 3}

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("create scylla session: %w", err)
	}
	return session, nil
}

func InitScyllaSchema(session *gocql.Session, keyspace string) error {
	if err := session.Query(fmt.Sprintf(`
		CREATE KEYSPACE IF NOT EXISTS %s
		WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}
		AND durable_writes = true
	`, keyspace)).Exec(); err != nil {
		return fmt.Errorf("create keyspace: %w", err)
	}

	return session.Query(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.api_overrides (
			api_name   text,
			wallet     text,
			t1_limit   text,
			t2_limit   text,
			t3_limit   text,
			reason     text,
			created_at timestamp,
			PRIMARY KEY (api_name, wallet)
		) WITH CLUSTERING ORDER BY (wallet ASC)
	`, keyspace)).Exec()
}
