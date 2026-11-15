package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
)

// AddTransaction checks tx with Validate, assigns it the next ID, stores it
// and returns the stored transaction. The amount is rounded to kopecks, the
// category is normalized with normalizeCategory, the description is trimmed,
// and the date is reduced to its day, today for a zero date. If the category
// has a budget and tx does not fit into it within the budget period of
// tx.Date, AddTransaction returns a *domain.BudgetExceededError and stores nothing.
// It is safe for concurrent use.
func (s *ledgerService) AddTransaction(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	if err := tx.Validate(); err != nil {
		return domain.Transaction{}, err
	}
	tx.Amount = roundToKopecks(tx.Amount)
	tx.Category = normalizeCategory(tx.Category)
	tx.Description = strings.TrimSpace(tx.Description)
	// The budget period depends on the date, so set it before the check.
	if tx.Date.IsZero() {
		tx.Date = time.Now()
	}
	tx.Date = domain.StartOfDay(tx.Date)

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var stored domain.Transaction
	err := s.transactions.WithinTransaction(ctx, func(budgets domain.BudgetRepository, expenses domain.ExpenseRepository) error {
		b, found, err := budgets.GetBudget(ctx, tx.Category)
		if err != nil {
			return err
		}
		if found {
			from, to, _ := b.Period.Bounds(tx.Date)
			spent, err := expenses.SumByCategory(ctx, tx.Category, from, to)
			if err != nil {
				return err
			}
			if err := checkBudget(b, spent, tx); err != nil {
				return err
			}
		}
		stored, err = expenses.AddTransaction(ctx, tx)
		return err
	})
	if err != nil {
		return domain.Transaction{}, err
	}
	return stored, nil
}

// ListTransactions returns all stored transactions, newest first: by date,
// and by ID for equal dates. Callers cannot change the storage through the
// returned slice.
func (s *ledgerService) ListTransactions(ctx context.Context) ([]domain.Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	return s.expenses.ListTransactions(ctx)
}

// SetBudget checks b with Validate, adds a budget for b.Category or replaces
// the existing one as a whole, period included, and returns the stored
// budget. A budget without a period replaces a monthly one with a limit for
// all time. The category and the period are stored trimmed and lowercased,
// the limit is rounded to kopecks. It is safe for concurrent use.
func (s *ledgerService) SetBudget(ctx context.Context, b domain.Budget) (domain.Budget, error) {
	if err := b.Validate(); err != nil {
		return domain.Budget{}, err
	}
	b = normalizeBudget(b)

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	if err := s.budgets.SetBudgets(ctx, b); err != nil {
		return domain.Budget{}, err
	}
	return b, nil
}

// ListBudgets returns all budgets sorted by category.
func (s *ledgerService) ListBudgets(ctx context.Context) ([]domain.Budget, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	return s.budgets.ListBudgets(ctx)
}

// LoadBudgets reads a JSON array of budgets from r, for example
// [{"category": "еда", "limit": 5000, "period": "month"}], checks each of
// them with Validate and sets them as SetBudget does, all at once. The period
// is optional. Nothing is changed unless the whole input is valid.
func (s *ledgerService) LoadBudgets(ctx context.Context, r io.Reader) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var list []domain.Budget
	if err := dec.Decode(&list); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("load budgets: no data, expected a JSON array of budgets")
		}
		return fmt.Errorf("load budgets: decode JSON: %w", err)
	}
	if list == nil {
		return errors.New("load budgets: expected a JSON array of budgets, got null")
	}
	// The array must be the only value in the input.
	tok, err := dec.Token()
	switch {
	case err == nil:
		return fmt.Errorf("load budgets: unexpected data after the JSON array: %v", tok)
	case !errors.Is(err, io.EOF):
		return fmt.Errorf("load budgets: decode JSON: %w", err)
	}

	// Check every budget before changing the storage.
	seen := make(map[string]bool, len(list))
	for i, b := range list {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("load budgets: budget #%d: %w", i+1, err)
		}
		b = normalizeBudget(b)
		if seen[b.Category] {
			return fmt.Errorf("load budgets: budget #%d: duplicate category %q", i+1, b.Category)
		}
		seen[b.Category] = true
		list[i] = b
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	if err := s.budgets.SetBudgets(ctx, list...); err != nil {
		return fmt.Errorf("load budgets: %w", err)
	}
	return nil
}

// normalizeBudget trims and lowercases the category and the period of
// a valid budget and rounds its limit to kopecks.
func normalizeBudget(b domain.Budget) domain.Budget {
	b.Category = normalizeCategory(b.Category)
	b.Period = normalizePeriod(b.Period)
	b.Limit = roundToKopecks(b.Limit)
	return b
}

// normalizePeriod trims and lowercases the period, so " Month" and "month"
// are the same period.
func normalizePeriod(p domain.Period) domain.Period {
	return domain.Period(strings.ToLower(strings.TrimSpace(string(p))))
}

// checkBudget returns a *domain.BudgetExceededError if tx does not fit into
// budget b of its category, given the amount already spent within the period
// of b that contains tx.Date.
func checkBudget(b domain.Budget, spent float64, tx domain.Transaction) error {
	// Amounts and limits are whole kopecks, so compare kopecks: float sums
	// like 0.1+0.2 are not exactly 0.3. Validation keeps the product finite.
	if math.Round((spent+tx.Amount)*100) > math.Round(b.Limit*100) {
		return &domain.BudgetExceededError{
			Category:    tx.Category,
			PeriodLabel: b.Period.Label(tx.Date),
			Limit:       b.Limit,
			Spent:       spent,
			Amount:      tx.Amount,
		}
	}
	return nil
}

// GetReportSummary returns the amount spent in every category from the day
// of from to the day of to, both included, in the local time zone. Both
// days must be from 0001-01-02 to 9999-12-30, otherwise the error wraps
// domain.ErrInvalidDate. The injected repository may cache category totals.
func (s *ledgerService) GetReportSummary(ctx context.Context, from, to time.Time) (domain.ReportSummary, error) {
	from, to = domain.StartOfDay(from), domain.StartOfDay(to)
	// PostgreSQL has no year 0, so such a day must not reach the query.
	if err := domain.CheckDay(from); err != nil {
		return domain.ReportSummary{}, err
	}
	if err := domain.CheckDay(to); err != nil {
		return domain.ReportSummary{}, err
	}
	if to.Before(from) {
		return domain.ReportSummary{}, fmt.Errorf("%w: from %s is after to %s", domain.ErrInvalidRange, formatDate(from), formatDate(to))
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	totals, err := s.expenses.ReportSummary(ctx, from, to)
	if err != nil {
		return domain.ReportSummary{}, err
	}

	report := domain.ReportSummary{From: from, To: to, Categories: totals}
	for _, t := range totals {
		report.Total += t.Amount
	}
	// Float sums like 0.1+0.2 are not exactly 0.3.
	report.Total = roundToKopecks(report.Total)
	return report, nil
}

// ListBudgetUsage returns budgets with the amount spent in their current period.
func (s *ledgerService) ListBudgetUsage(ctx context.Context, date time.Time) ([]domain.BudgetUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	// Use the transaction repositories directly, bypassing the list cache.
	var usage []domain.BudgetUsage
	err := s.transactions.WithinTransaction(ctx, func(budgets domain.BudgetRepository, expenses domain.ExpenseRepository) error {
		list, err := budgets.ListBudgets(ctx)
		if err != nil {
			return err
		}
		usage = make([]domain.BudgetUsage, len(list))
		for i, b := range list {
			from, to, _ := b.Period.Bounds(date)
			spent, err := expenses.SumByCategory(ctx, b.Category, from, to)
			if err != nil {
				return err
			}
			usage[i] = domain.BudgetUsage{Budget: b, PeriodLabel: b.Period.Label(date), Spent: spent}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return usage, nil
}

func roundToKopecks(x float64) float64         { return math.Round(x*100) / 100 }
func normalizeCategory(category string) string { return strings.ToLower(strings.TrimSpace(category)) }
func formatDate(t time.Time) string            { return t.In(time.Local).Format(time.DateOnly) }
