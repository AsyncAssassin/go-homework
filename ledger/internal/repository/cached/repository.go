package cached

import (
	"context"
	"log"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
	"github.com/AsyncAssassin/go-homework/ledger/internal/cache"
)

// Repository adds a Redis cache to the domain repositories. Report summaries stay
// in the cache for reportTTL, so a report may miss the transactions of the
// last reportTTL. The list of budgets stays there for budgetsTTL or until
// a budget changes. The budget check of a new transaction never uses the
// cache. If Redis fails, the error is logged and the data comes from the
// storage.
type Repository struct {
	domain.BudgetRepository
	domain.ExpenseRepository
	cache *cache.Cache
}

func New(budgets domain.BudgetRepository, expenses domain.ExpenseRepository, cache *cache.Cache) *Repository {
	return &Repository{BudgetRepository: budgets, ExpenseRepository: expenses, cache: cache}
}

// budgetsKey is the cache key of the list of budgets.
const budgetsKey = "budgets:all"

// Lifetimes of the cached values.
const (
	reportTTL  = 30 * time.Second
	budgetsTTL = 30 * time.Second
)

// reportKey returns the cache key of the report summary from the day from
// to the day to, such as "report:summary:2026-11-01:2026-11-30".
func reportKey(from, to time.Time) string {
	return "report:summary:" + formatDate(from) + ":" + formatDate(to)
}

// ReportSummary logs whether the report came from the cache, so the effect
// of the cache can be seen in the log.
func (s *Repository) ReportSummary(ctx context.Context, from, to time.Time) ([]domain.CategoryTotal, error) {
	key := reportKey(from, to)
	var totals []domain.CategoryTotal
	if s.get(ctx, key, &totals) {
		log.Printf("report cache hit: %s", key)
		return totals, nil
	}
	log.Printf("report cache miss: %s", key)

	totals, err := s.ExpenseRepository.ReportSummary(ctx, from, to)
	if err != nil {
		return nil, err
	}
	s.set(ctx, key, totals, reportTTL)
	return totals, nil
}

func (s *Repository) ListBudgets(ctx context.Context) ([]domain.Budget, error) {
	var list []domain.Budget
	if s.get(ctx, budgetsKey, &list) {
		return list, nil
	}
	list, err := s.BudgetRepository.ListBudgets(ctx)
	if err != nil {
		return nil, err
	}
	s.set(ctx, budgetsKey, list, budgetsTTL)
	return list, nil
}

// SetBudgets removes the cached list of budgets after the change, so the
// next list comes from the storage.
func (s *Repository) SetBudgets(ctx context.Context, list ...domain.Budget) error {
	if err := s.BudgetRepository.SetBudgets(ctx, list...); err != nil {
		return err
	}
	if err := s.cache.Delete(ctx, budgetsKey); err != nil {
		log.Printf("cache: %v", err)
	}
	return nil
}

// get reads the cached value of key into v and reports whether it was
// found. A failure of Redis counts as a miss.
func (s *Repository) get(ctx context.Context, key string, v any) bool {
	found, err := s.cache.Get(ctx, key, v)
	if err != nil {
		log.Printf("cache: %v", err)
		return false
	}
	return found
}

// set caches v under key for ttl. A failure of Redis is only logged: the
// value is already in the storage.
func (s *Repository) set(ctx context.Context, key string, v any, ttl time.Duration) {
	if err := s.cache.Set(ctx, key, v, ttl); err != nil {
		log.Printf("cache: %v", err)
	}
}

func formatDate(t time.Time) string { return t.In(time.Local).Format(time.DateOnly) }
