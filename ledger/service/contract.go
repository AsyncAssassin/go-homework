// Package service implements ledger use cases through domain repositories.
package service

import (
	"context"
	"io"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
)

// Service is the application interface used by the HTTP and console clients.
type Service interface {
	AddTransaction(context.Context, domain.Transaction) (domain.Transaction, error)
	ListTransactions(context.Context) ([]domain.Transaction, error)
	SetBudget(context.Context, domain.Budget) (domain.Budget, error)
	ListBudgets(context.Context) ([]domain.Budget, error)
	ListBudgetUsage(context.Context, time.Time) ([]domain.BudgetUsage, error)
	LoadBudgets(context.Context, io.Reader) error
	GetReportSummary(context.Context, time.Time, time.Time) (domain.ReportSummary, error)
}

const queryTimeout = 5 * time.Second

type ledgerService struct {
	budgets      domain.BudgetRepository
	expenses     domain.ExpenseRepository
	transactions domain.TransactionRunner
	reports      ReportCache
}

// New assembles a service. All repositories must refer to the same data;
// transactions supplies uncached repositories for atomic budget checks.
func New(budgets domain.BudgetRepository, expenses domain.ExpenseRepository, transactions domain.TransactionRunner, reports ReportCache) Service {
	return &ledgerService{budgets: budgets, expenses: expenses, transactions: transactions, reports: reports}
}

// ReportCache is a best-effort cache of complete category totals. Implementations
// log cache failures and treat failed reads as misses. Nil disables report caching.
type ReportCache interface {
	GetReport(context.Context, time.Time, time.Time) ([]domain.CategoryTotal, bool)
	SetReport(context.Context, time.Time, time.Time, []domain.CategoryTotal)
}
