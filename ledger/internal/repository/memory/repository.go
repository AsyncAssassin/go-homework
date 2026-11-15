// Package memory implements repositories for the demo and isolated tests.
package memory

import (
	"cmp"
	"context"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
)

type data struct {
	transactions []domain.Transaction
	budgets      map[string]domain.Budget
}

type Repository struct {
	mu   sync.RWMutex
	data data
}

func New() *Repository {
	return &Repository{data: data{budgets: map[string]domain.Budget{}}}
}

// WithinTransaction keeps the check and write under one lock and publishes
// changes only after success. An error, cancellation or panic discards the copy.
func (r *Repository) WithinTransaction(ctx context.Context, f func(domain.BudgetRepository, domain.ExpenseRepository) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	next := &data{transactions: slices.Clone(r.data.transactions), budgets: maps.Clone(r.data.budgets)}
	if err := f(next, next); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.data = *next
	return nil
}

func (r *Repository) AddTransaction(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data.AddTransaction(ctx, tx)
}

func (r *Repository) ListTransactions(ctx context.Context) ([]domain.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data.ListTransactions(ctx)
}

func (r *Repository) SetBudgets(ctx context.Context, list ...domain.Budget) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data.SetBudgets(ctx, list...)
}

func (r *Repository) GetBudget(ctx context.Context, category string) (domain.Budget, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data.GetBudget(ctx, category)
}

func (r *Repository) ListBudgets(ctx context.Context) ([]domain.Budget, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data.ListBudgets(ctx)
}

func (r *Repository) SumByCategory(ctx context.Context, category string, from, to time.Time) (float64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data.SumByCategory(ctx, category, from, to)
}

func (r *Repository) ListCategories(ctx context.Context, from, to time.Time) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data.ListCategories(ctx, from, to)
}

func (d *data) AddTransaction(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	if err := ctx.Err(); err != nil {
		return domain.Transaction{}, err
	}
	tx.ID = len(d.transactions) + 1
	d.transactions = append(d.transactions, tx)
	return tx, nil
}

func (d *data) ListTransactions(ctx context.Context) ([]domain.Transaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	list := slices.Clone(d.transactions)
	slices.SortFunc(list, func(a, b domain.Transaction) int {
		if c := b.Date.Compare(a.Date); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return list, nil
}

func (d *data) SetBudgets(ctx context.Context, list ...domain.Budget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, b := range list {
		d.budgets[b.Category] = b
	}
	return nil
}

func (d *data) GetBudget(ctx context.Context, category string) (domain.Budget, bool, error) {
	if err := ctx.Err(); err != nil {
		return domain.Budget{}, false, err
	}
	b, found := d.budgets[category]
	return b, found, nil
}

func (d *data) ListBudgets(ctx context.Context) ([]domain.Budget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	list := make([]domain.Budget, 0, len(d.budgets))
	for _, b := range d.budgets {
		list = append(list, b)
	}
	slices.SortFunc(list, func(a, b domain.Budget) int { return strings.Compare(a.Category, b.Category) })
	return list, nil
}

func (d *data) SumByCategory(ctx context.Context, category string, from, to time.Time) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var spent float64
	for _, tx := range d.transactions {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if tx.Category == category && (from.IsZero() && to.IsZero() || !tx.Date.Before(from) && tx.Date.Before(to)) {
			spent += tx.Amount
		}
	}
	return math.Round(spent*100) / 100, nil
}

func (d *data) ListCategories(ctx context.Context, from, to time.Time) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, tx := range d.transactions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !tx.Date.Before(from) && tx.Date.Before(to) {
			seen[tx.Category] = true
		}
	}
	categories := make([]string, 0, len(seen))
	for category := range seen {
		categories = append(categories, category)
	}
	slices.Sort(categories)
	return categories, nil
}
