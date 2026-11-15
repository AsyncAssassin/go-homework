package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// The tests change environment variables, so they do not run in parallel.

func TestDSN(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "defaults",
			want: "postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable",
		},
		{
			name: "DATABASE_URL comes first",
			env:  map[string]string{"DATABASE_URL": "postgres://ledger:secret@db.local:6543/shop", "DB_HOST": "other"},
			want: "postgres://ledger:secret@db.local:6543/shop",
		},
		{
			name: "separate variables",
			env: map[string]string{
				"DB_HOST": "db.local", "DB_PORT": "6543", "DB_USER": "ledger", "DB_PASS": "p@ss:w/rd?", "DB_NAME": "shop",
			},
			want: "postgres://ledger:p%40ss%3Aw%2Frd%3F@db.local:6543/shop?sslmode=disable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range []string{"DATABASE_URL", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASS", "DB_NAME"} {
				t.Setenv(key, tt.env[key])
			}
			if got := DSN(); got != tt.want {
				t.Errorf("DSN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDSNEscapesPassword(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_PASS", "p@ss:w/rd?#")

	// The driver must read back the same password.
	cfg, err := pgx.ParseConfig(DSN())
	if err != nil {
		t.Fatalf("parse DSN(): %v", err)
	}
	if cfg.Password != "p@ss:w/rd?#" {
		t.Errorf("password = %q, want %q", cfg.Password, "p@ss:w/rd?#")
	}
}

func TestTarget(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{name: "URL", dsn: "postgres://ledger:secret@db.local:6543/shop?sslmode=disable", want: "db.local:6543/shop"},
		{name: "keywords", dsn: "host=db.local port=6543 user=ledger password=secret dbname=shop", want: "db.local:6543/shop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Target(tt.dsn)
			if err != nil {
				t.Fatalf("Target() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Target() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTargetError checks that no part of the password gets into the error.
// The parser of pgx quotes a part of these strings in its errors: the rest
// of an unescaped password taken for the port, or the whole string.
func TestTargetError(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
	}{
		{name: "# in the password", dsn: "postgres://postgres:secret#1@localhost:5432/cashapp"},
		{name: "/ in the password", dsn: "postgres://postgres:secret/1@localhost:5432/cashapp"},
		{name: "? in the password", dsn: "postgres://postgres:secret?1@localhost:5432/cashapp"},
		{name: "colons in the password and an invalid port", dsn: "postgres://postgres:secret:1:2@localhost:abc/cashapp"},
		{name: "/ in a longer password", dsn: "postgres://ledger:secret123/part2@localhost:5432/cashapp"},
		{name: "password in the query", dsn: "postgres://ledger@db.local:5432/shop?password=secret123&sslmode=invalid"},
		{name: "keywords", dsn: "host=db.local port=bad user=ledger PASSWORD=secret123"},
		{name: "invalid port", dsn: "postgres://postgres:secret1@localhost:abc/cashapp"},
		{name: "invalid escape in the password", dsn: "postgres://postgres:secret%ZZ1@localhost:5432/cashapp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Target(tt.dsn)
			if !errors.Is(err, errInvalidDSN) {
				t.Fatalf("Target() error = %v, want %v", err, errInvalidDSN)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("Target() error = %q, want it without the password", err)
			}
		})
	}
}

func TestOpenError(t *testing.T) {
	// Nothing listens on port 1.
	_, err := Open(context.Background(), "postgres://ledger:secret@127.0.0.1:1/shop?sslmode=disable&connect_timeout=2")
	if err == nil {
		t.Fatal("Open() succeeded, want an error")
	}
	if msg := err.Error(); !strings.HasPrefix(msg, "connect to PostgreSQL at 127.0.0.1:1/shop: ") || strings.Contains(msg, "secret") {
		t.Errorf("Open() error = %q, want the server and no password", msg)
	}
}
