package domain

import (
	"context"
	"time"
)

// BudgetRepository stores normalized budgets. SetBudgets replaces all supplied
// budgets atomically. GetBudget reports found=false for an absent category.
// ListBudgets returns an independent slice sorted by category bytes.
type BudgetRepository interface {
	SetBudgets(context.Context, ...Budget) error
	GetBudget(context.Context, string) (Budget, bool, error)
	ListBudgets(context.Context) ([]Budget, error)
}

// ExpenseRepository stores normalized expenses and returns independent slices.
// Lists are ordered by date descending, then ID descending. Summary categories
// are sorted by their UTF-8 bytes. SumByCategory uses [from, to); two zero bounds
// mean all time. ListCategories returns sorted distinct categories with expenses
// in the half-open range [from, to). Calls must be safe for concurrent use.
type ExpenseRepository interface {
	AddTransaction(context.Context, Transaction) (Transaction, error)
	ListTransactions(context.Context) ([]Transaction, error)
	SumByCategory(ctx context.Context, category string, from, to time.Time) (float64, error)
	ListCategories(ctx context.Context, from, to time.Time) ([]string, error)
}

// TransactionRunner commits all callback writes together, or discards them on
// error or panic. Reading an existing budget inside the callback serializes
// competing writes of that category until completion. The repositories must not escape
// the callback; they bypass caches so budget checks always read stored data.
type TransactionRunner interface {
	WithinTransaction(context.Context, func(BudgetRepository, ExpenseRepository) error) error
}
