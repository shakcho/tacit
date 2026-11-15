package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	URL               string
	ApplicationName   string
	MaxConnections    int32
	MinConnections    int32
	ConnectTimeout    time.Duration
	HealthCheckPeriod time.Duration
	PingTimeout       time.Duration
}

type Store struct {
	pool      *pgxpool.Pool
	closeOnce sync.Once
}

func Open(ctx context.Context, config Config) (*Store, error) {
	if config.URL == "" {
		return nil, errors.New("database URL is required")
	}
	if config.ApplicationName == "" {
		return nil, errors.New("database application name is required")
	}
	if config.MaxConnections < 1 || config.MinConnections < 0 || config.MinConnections > config.MaxConnections {
		return nil, errors.New("invalid database connection limits")
	}
	if config.ConnectTimeout <= 0 || config.HealthCheckPeriod <= 0 || config.PingTimeout <= 0 {
		return nil, errors.New("database timeouts must be positive")
	}
	poolConfig, err := pgxpool.ParseConfig(config.URL)
	if err != nil {
		return nil, errors.New("invalid database URL")
	}
	poolConfig.ConnConfig.ConnectTimeout = config.ConnectTimeout
	poolConfig.ConnConfig.RuntimeParams["application_name"] = config.ApplicationName
	poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	poolConfig.ConnConfig.StatementCacheCapacity = 0
	poolConfig.ConnConfig.DescriptionCacheCapacity = 0
	poolConfig.MaxConns = config.MaxConnections
	poolConfig.MinConns = config.MinConnections
	poolConfig.HealthCheckPeriod = config.HealthCheckPeriod
	poolConfig.PingTimeout = config.PingTimeout
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	store := &Store{pool: pool}
	pingContext, cancel := context.WithTimeout(ctx, config.PingTimeout)
	defer cancel()
	if err := pool.Ping(pingContext); err != nil {
		store.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := rejectFutureSchema(pingContext, pool); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) Close() {
	if store == nil {
		return
	}
	store.closeOnce.Do(func() {
		if store.pool != nil {
			store.pool.Close()
		}
	})
}
