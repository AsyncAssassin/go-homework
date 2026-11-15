// Package db opens the pool of connections to the PostgreSQL database of
// the ledger.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver for database/sql
)

// Limits of the connection pool.
const (
	maxOpenConns    = 10
	maxIdleConns    = 5
	connMaxLifetime = 30 * time.Minute
)

// pingTimeout limits the connection check in Open.
const pingTimeout = 5 * time.Second

// DSN returns the connection string of the ledger database: DATABASE_URL if
// it is set, otherwise a URL built from DB_HOST, DB_PORT, DB_USER, DB_PASS
// and DB_NAME, which default to localhost, 5432, postgres, postgres and
// cashapp. The built URL turns TLS off, which suits a local database.
func DSN() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(getenv("DB_USER", "postgres"), getenv("DB_PASS", "postgres")),
		Host:     net.JoinHostPort(getenv("DB_HOST", "localhost"), getenv("DB_PORT", "5432")),
		Path:     "/" + getenv("DB_NAME", "cashapp"),
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

// errInvalidDSN is returned for a connection string that cannot be parsed.
var errInvalidDSN = errors.New("invalid PostgreSQL connection string: " +
	"check DATABASE_URL or the DB_* variables; special characters in a URL password must be percent-encoded")

// Target returns the server and the database of dsn for messages, such as
// "localhost:5432/cashapp", without the user and the password. If dsn
// cannot be parsed, it returns errInvalidDSN.
func Target(dsn string) (string, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		// The parse error may quote a part of the password, such as the rest
		// of an unescaped password taken for the port, so it is dropped.
		return "", errInvalidDSN
	}
	host := net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
	return host + "/" + cfg.Database, nil
}

// Open opens a pool of connections to the database at dsn, sets its limits
// and checks the connection with Ping. Errors name the server and the
// database, but not the password.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	target, err := Target(dsn)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL at %s: %w", target, err)
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)

	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to PostgreSQL at %s: %w", target, err)
	}
	return db, nil
}

// getenv returns the environment variable key, or def if it is empty.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
