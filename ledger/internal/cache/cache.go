// Package cache keeps values in Redis as JSON for a limited time.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// requestTimeout limits every request to Redis, connecting included. The
// cache only saves work, so a slow or hung Redis must not hold up the
// caller for longer, and a failed request is not retried.
const requestTimeout = 500 * time.Millisecond

// Config holds the connection settings of Redis.
type Config struct {
	Addr     string
	DB       int
	Password string
}

// ConfigFromEnv reads the settings from REDIS_ADDR, REDIS_DB and
// REDIS_PASSWORD. They default to localhost:6379, database 0 and no
// password.
func ConfigFromEnv() (Config, error) {
	cfg := Config{Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD")}
	if cfg.Addr == "" {
		cfg.Addr = "localhost:6379"
	}
	if s := os.Getenv("REDIS_DB"); s != "" {
		db, err := strconv.Atoi(s)
		if err != nil || db < 0 {
			return Config{}, fmt.Errorf("invalid REDIS_DB %q: must be a database number, such as 0", s)
		}
		cfg.DB = db
	}
	return cfg, nil
}

// String describes the settings for messages, without the password, such
// as "localhost:6379, db 0".
func (c Config) String() string {
	return fmt.Sprintf("%s, db %d", c.Addr, c.DB)
}

// Cache stores values in Redis as JSON. It is safe for concurrent use.
type Cache struct {
	client *redis.Client
}

// New returns a cache in the Redis of cfg without connecting to it: the
// first request connects. Every request takes at most requestTimeout.
func New(cfg Config) *Cache {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		DB:       cfg.DB,
		Password: cfg.Password,
		// The defaults of go-redis allow 5 seconds to connect with 5
		// attempts, 3 seconds to read and 3 retries of a command.
		DialTimeout:   requestTimeout,
		ReadTimeout:   requestTimeout,
		WriteTimeout:  requestTimeout,
		DialerRetries: 1,
		MaxRetries:    -1, // no retries
		// Let the deadline of the context end a read or a write in progress.
		ContextTimeoutEnabled: true,
	})
	return &Cache{client: client}
}

// Open returns a cache in the Redis of cfg and checks the connection with
// PING.
func Open(ctx context.Context, cfg Config) (*Cache, error) {
	c := New(cfg)
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := c.client.Ping(ctx).Err(); err != nil {
		c.Close()
		return nil, fmt.Errorf("connect to Redis at %s: %w", cfg, err)
	}
	return c, nil
}

// Get reads the value of key into v and reports whether the key exists.
func (c *Cache) Get(ctx context.Context, key string, v any) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	data, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s from Redis: %w", key, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("decode %s from Redis: %w", key, err)
	}
	return true, nil
}

// Set stores v under key for ttl.
func (c *Cache) Set(ctx context.Context, key string, v any, ttl time.Duration) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s for Redis: %w", key, err)
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("write %s to Redis: %w", key, err)
	}
	return nil
}

// Delete removes the keys. Missing keys are not an error.
func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := c.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("delete %s from Redis: %w", strings.Join(keys, ", "), err)
	}
	return nil
}

// Close closes the connections to Redis.
func (c *Cache) Close() error {
	return c.client.Close()
}
