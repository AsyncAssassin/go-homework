// Package ledger assembles a service and owns its external resources.
package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/internal/cache"
	"github.com/AsyncAssassin/go-homework/ledger/internal/db"
	"github.com/AsyncAssassin/go-homework/ledger/internal/repository/cached"
	"github.com/AsyncAssassin/go-homework/ledger/internal/repository/memory"
	"github.com/AsyncAssassin/go-homework/ledger/internal/repository/pg"
	"github.com/AsyncAssassin/go-homework/ledger/service"
)

// New connects to PostgreSQL and Redis and returns an independent service.
// It checks existing tables but never runs migrations. Close must be called
// after requests finish; it is safe to call it more than once.
func New(ctx context.Context) (service.Service, func() error, error) {
	dsn := db.DSN()
	target, err := db.Target(dsn)
	if err != nil {
		return nil, nil, err
	}
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	if err := checkTables(ctx, pool, target); err != nil {
		pool.Close()
		return nil, nil, err
	}
	log.Printf("connected to PostgreSQL at %s", target)

	cfg, err := cache.ConfigFromEnv()
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	c, err := cache.Open(ctx, cfg)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	log.Printf("connected to Redis at %s", cfg)

	repo := pg.New(pool)
	cachedRepo := cached.New(repo, repo, c)
	svc := service.New(cachedRepo, cachedRepo, repo, cachedRepo)
	var once sync.Once
	var closeErr error
	closeFn := func() error {
		once.Do(func() {
			closeErr = errors.Join(pool.Close(), c.Close())
		})
		return closeErr
	}
	return svc, closeFn, nil
}

// NewMemory returns an independent service for tests and the console demo.
func NewMemory() service.Service {
	repo := memory.New()
	return service.New(repo, repo, repo, nil)
}

// checkTables returns an error if the database has no table of the ledger,
// for example because the migrations were not applied. target names the
// database in the error.
func checkTables(ctx context.Context, pool *sql.DB, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, table := range []string{"budgets", "expenses"} {
		var exists bool
		if err := pool.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			return fmt.Errorf("check the tables of PostgreSQL at %s: %w", target, err)
		}
		if !exists {
			return fmt.Errorf("PostgreSQL at %s has no table %s: apply the migrations with goose up, see ledger/README.md", target, table)
		}
	}
	return nil
}
