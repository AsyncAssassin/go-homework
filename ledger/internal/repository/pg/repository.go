// Package pg implements the ledger repositories using PostgreSQL.
package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
)

type queryDB interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// queries runs the same repository operations on a pool or a transaction.
type queries struct{ db queryDB }
type Repository struct {
	*queries
	pool *sql.DB
}

func New(pool *sql.DB) *Repository {
	return &Repository{queries: &queries{db: pool}, pool: pool}
}

// WithinTransaction keeps a budget row locked across the service's budget
// check and insert. Rollback also runs during panic unwinding.
func (s *Repository) WithinTransaction(ctx context.Context, f func(domain.BudgetRepository, domain.ExpenseRepository) error) error {
	tx, err := s.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin a database transaction: %w", err)
	}
	defer tx.Rollback()
	repo := &queries{db: tx}
	if err := f(repo, repo); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit a database transaction: %w", err)
	}
	return nil
}

func (s *queries) AddTransaction(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO expenses (amount, category, description, date) VALUES ($1, $2, $3, $4) RETURNING id`,
		tx.Amount, tx.Category, tx.Description, formatDate(tx.Date)).Scan(&tx.ID)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("insert the transaction: %w", err)
	}
	return tx, nil
}

// SetBudgets sorts row locks so concurrent batch updates cannot deadlock.
func (s *Repository) SetBudgets(ctx context.Context, list ...domain.Budget) error {
	return s.WithinTransaction(ctx, func(b domain.BudgetRepository, _ domain.ExpenseRepository) error {
		return b.SetBudgets(ctx, list...)
	})
}

func (s *queries) SetBudgets(ctx context.Context, list ...domain.Budget) error {
	list = slices.Clone(list)
	slices.SortFunc(list, func(a, b domain.Budget) int { return strings.Compare(a.Category, b.Category) })
	for _, b := range list {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO budgets (category, limit_amount, period) VALUES ($1, $2, $3)
			ON CONFLICT (category) DO UPDATE SET limit_amount = EXCLUDED.limit_amount, period = EXCLUDED.period`,
			b.Category, b.Limit, string(b.Period))
		if err != nil {
			return fmt.Errorf("save the budget of %q: %w", b.Category, err)
		}
	}
	return nil
}

func (s *queries) ListTransactions(ctx context.Context) ([]domain.Transaction, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, amount, category, COALESCE(description, ''), date FROM expenses ORDER BY date DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	defer rows.Close()

	var list []domain.Transaction
	for rows.Next() {
		var tx domain.Transaction
		var date time.Time
		if err := rows.Scan(&tx.ID, &tx.Amount, &tx.Category, &tx.Description, &date); err != nil {
			return nil, fmt.Errorf("list transactions: %w", err)
		}
		tx.Date = localDay(date)
		list = append(list, tx)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	return list, nil
}

// ListBudgets sorts the budgets by the bytes of their categories, as
// the memory repository does: COLLATE "C" ignores the rules of the database locale.
func (s *queries) ListBudgets(ctx context.Context) ([]domain.Budget, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, limit_amount, period FROM budgets ORDER BY category COLLATE "C"`)
	if err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	defer rows.Close()

	// Not nil, as in memory: no budgets are cached as [], not null.
	list := []domain.Budget{}
	for rows.Next() {
		var b domain.Budget
		if err := rows.Scan(&b.Category, &b.Limit, &b.Period); err != nil {
			return nil, fmt.Errorf("list budgets: %w", err)
		}
		list = append(list, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	return list, nil
}

func (s *queries) ListCategories(ctx context.Context, from, to time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT category COLLATE "C" FROM expenses WHERE date >= $1 AND date < $2 ORDER BY 1`,
		formatDate(from), formatDate(to))
	if err != nil {
		return nil, fmt.Errorf("list expense categories: %w", err)
	}
	defer rows.Close()
	categories := []string{}
	for rows.Next() {
		var category string
		if err := rows.Scan(&category); err != nil {
			return nil, fmt.Errorf("list expense categories: %w", err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list expense categories: %w", err)
	}
	return categories, nil
}

// GetBudget reads the budget of category and locks its row until the end
// of the transaction. found is false if the category has no budget.
func (s *queries) GetBudget(ctx context.Context, category string) (b domain.Budget, found bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT category, limit_amount, period FROM budgets WHERE category = $1 FOR UPDATE`,
		category,
	).Scan(&b.Category, &b.Limit, &b.Period)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return domain.Budget{}, false, nil
	case err != nil:
		return domain.Budget{}, false, fmt.Errorf("read the budget of %q: %w", category, err)
	}
	return b, true, nil
}

// SumByCategory sums the half-open day range, or all time for zero bounds.
func (s *queries) SumByCategory(ctx context.Context, category string, from, to time.Time) (float64, error) {
	query := `SELECT COALESCE(SUM(amount), 0) FROM expenses WHERE category = $1`
	args := []any{category}
	if !from.IsZero() || !to.IsZero() {
		query += ` AND date >= $2 AND date < $3`
		args = append(args, formatDate(from), formatDate(to))
	}
	var spent float64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&spent); err != nil {
		return 0, fmt.Errorf("sum the expenses of %q: %w", category, err)
	}
	return spent, nil
}

func formatDate(t time.Time) string { return t.In(time.Local).Format(time.DateOnly) }

func localDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return domain.Day(y, m, d)
}

var (
	_ domain.BudgetRepository  = (*Repository)(nil)
	_ domain.ExpenseRepository = (*Repository)(nil)
	_ domain.TransactionRunner = (*Repository)(nil)
)
