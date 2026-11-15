package cache

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests change environment variables, so they do not run in parallel.

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{name: "defaults", want: Config{Addr: "localhost:6379"}},
		{
			name: "all variables",
			env:  map[string]string{"REDIS_ADDR": "cache.local:6380", "REDIS_DB": "15", "REDIS_PASSWORD": "secret"},
			want: Config{Addr: "cache.local:6380", DB: 15, Password: "secret"},
		},
		{name: "database is not a number", env: map[string]string{"REDIS_DB": "first"}, wantErr: true},
		{name: "negative database", env: map[string]string{"REDIS_DB": "-1"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range []string{"REDIS_ADDR", "REDIS_DB", "REDIS_PASSWORD"} {
				t.Setenv(key, tt.env[key])
			}
			got, err := ConfigFromEnv()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ConfigFromEnv() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ConfigFromEnv() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConfigString(t *testing.T) {
	cfg := Config{Addr: "localhost:6379", DB: 2, Password: "secret"}
	if got, want := cfg.String(), "localhost:6379, db 2"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestOpenError(t *testing.T) {
	// Nothing listens on port 1.
	_, err := Open(context.Background(), Config{Addr: "127.0.0.1:1", Password: "secret"})
	if err == nil {
		t.Fatal("Open() succeeded, want an error")
	}
	if msg := err.Error(); !strings.HasPrefix(msg, "connect to Redis at 127.0.0.1:1, db 0: ") || strings.Contains(msg, "secret") {
		t.Errorf("Open() error = %q, want the server and no password", msg)
	}
}

// TestHungServer checks that a Redis that accepts connections but never
// answers holds up every request for about requestTimeout, not longer.
func TestHungServer(t *testing.T) {
	cfg := Config{Addr: hungServer(t)}
	ctx := context.Background()
	c := New(cfg)
	defer c.Close()

	tests := []struct {
		name string
		call func() error
	}{
		{name: "Open", call: func() error {
			opened, err := Open(ctx, cfg)
			if err == nil {
				opened.Close()
			}
			return err
		}},
		{name: "Get", call: func() error {
			var v string
			_, err := c.Get(ctx, "test:hung", &v)
			return err
		}},
		{name: "Set", call: func() error { return c.Set(ctx, "test:hung", "value", time.Minute) }},
		{name: "Delete", call: func() error { return c.Delete(ctx, "test:hung") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			err := tt.call()
			if elapsed := time.Since(start); elapsed > 2*requestTimeout {
				t.Errorf("%s() took %v, want at most %v", tt.name, elapsed, 2*requestTimeout)
			}
			if err == nil {
				t.Errorf("%s() succeeded, want an error", tt.name)
			}
		})
	}
}

// hungServer starts a TCP server that accepts connections and reads from
// them, but never answers, like a hung Redis. It returns the address of the
// server and stops it when the test ends.
func hungServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			go io.Copy(io.Discard, conn)
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			conn.Close()
		}
	})
	return ln.Addr().String()
}

// TestCache needs Redis at LEDGER_TEST_REDIS_ADDR. It uses database 15 and
// removes its key.
func TestCache(t *testing.T) {
	addr := os.Getenv("LEDGER_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set LEDGER_TEST_REDIS_ADDR to test with Redis")
	}
	ctx := context.Background()
	c, err := Open(ctx, Config{Addr: addr, DB: 15, Password: os.Getenv("LEDGER_TEST_REDIS_PASSWORD")})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer c.Close()
	const key = "test:cache"
	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Delete(ctx, key) })

	var got []string
	if found, err := c.Get(ctx, key, &got); found || err != nil {
		t.Fatalf("Get() of a missing key = %v, %v; want false, nil", found, err)
	}
	want := []string{"еда", "транспорт"}
	if err := c.Set(ctx, key, want, time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if found, err := c.Get(ctx, key, &got); !found || err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Get() = %v, %v, %q; want true, nil, %q", found, err, got, want)
	}
	var wrong int
	if _, err := c.Get(ctx, key, &wrong); err == nil {
		t.Error("Get() into a wrong type succeeded, want a decoding error")
	}
	if err := c.Set(ctx, key, func() {}, time.Minute); err == nil {
		t.Error("Set() of a function succeeded, want an encoding error")
	}
	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if found, err := c.Get(ctx, key, &got); found || err != nil {
		t.Errorf("Get() after Delete() = %v, %v; want false, nil", found, err)
	}
}
