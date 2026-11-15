package ledger

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/internal/cache"
)

// cachedStore adds a Redis cache to another storage. Report summaries stay
// in the cache for reportTTL, so a report may miss the transactions of the
// last reportTTL. The list of budgets stays there for budgetsTTL or until
// a budget changes. The budget check of a new transaction never uses the
// cache. If Redis fails, the error is logged and the data comes from the
// storage.
type cachedStore struct {
	storage
	cache *cache.Cache
}

// budgetsKey is the cache key of the list of budgets.
const budgetsKey = "budgets:all"

// Lifetimes of the cached values. They are variables, so tests can make
// them shorter.
var (
	reportTTL  = 30 * time.Second
	budgetsTTL = 30 * time.Second
)

// reportKey returns the cache key of the report summary from the day from
// to the day to, such as "report:summary:2026-11-01:2026-11-30".
func reportKey(from, to time.Time) string {
	return "report:summary:" + formatDate(from) + ":" + formatDate(to)
}

// reportSummary logs whether the report came from the cache, so the effect
// of the cache can be seen in the log.
func (s *cachedStore) reportSummary(ctx context.Context, from, to time.Time) ([]CategoryTotal, error) {
	key := reportKey(from, to)
	var totals []CategoryTotal
	if s.get(ctx, key, &totals) {
		log.Printf("report cache hit: %s", key)
		return totals, nil
	}
	log.Printf("report cache miss: %s", key)

	totals, err := s.storage.reportSummary(ctx, from, to)
	if err != nil {
		return nil, err
	}
	s.set(ctx, key, totals, reportTTL)
	return totals, nil
}

func (s *cachedStore) listBudgets(ctx context.Context) ([]Budget, error) {
	var list []Budget
	if s.get(ctx, budgetsKey, &list) {
		return list, nil
	}
	list, err := s.storage.listBudgets(ctx)
	if err != nil {
		return nil, err
	}
	s.set(ctx, budgetsKey, list, budgetsTTL)
	return list, nil
}

// setBudgets removes the cached list of budgets after the change, so the
// next list comes from the storage.
func (s *cachedStore) setBudgets(ctx context.Context, list ...Budget) error {
	if err := s.storage.setBudgets(ctx, list...); err != nil {
		return err
	}
	if err := s.cache.Delete(ctx, budgetsKey); err != nil {
		log.Printf("cache: %v", err)
	}
	return nil
}

func (s *cachedStore) close() error {
	return errors.Join(s.storage.close(), s.cache.Close())
}

// get reads the cached value of key into v and reports whether it was
// found. A failure of Redis counts as a miss.
func (s *cachedStore) get(ctx context.Context, key string, v any) bool {
	found, err := s.cache.Get(ctx, key, v)
	if err != nil {
		log.Printf("cache: %v", err)
		return false
	}
	return found
}

// set caches v under key for ttl. A failure of Redis is only logged: the
// value is already in the storage.
func (s *cachedStore) set(ctx context.Context, key string, v any, ttl time.Duration) {
	if err := s.cache.Set(ctx, key, v, ttl); err != nil {
		log.Printf("cache: %v", err)
	}
}
