package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
)

// Embedded interfaces make unexpected repository calls fail the test.
type spendingRepository struct {
	domain.BudgetRepository
	domain.ExpenseRepository
	budget   domain.Budget
	found    bool
	spent    float64
	failAt   string
	failure  error
	calls    []string
	stored   domain.Transaction
	from, to time.Time
	ctx      context.Context
}

func (r *spendingRepository) GetBudget(ctx context.Context, category string) (domain.Budget, bool, error) {
	r.calls = append(r.calls, "budget")
	r.ctx = ctx
	if category != r.budget.Category {
		return domain.Budget{}, false, errors.New("category was not normalized")
	}
	if r.failAt == "budget" {
		return domain.Budget{}, false, r.failure
	}
	return r.budget, r.found, nil
}
func (r *spendingRepository) SumByCategory(ctx context.Context, category string, from, to time.Time) (float64, error) {
	r.calls = append(r.calls, "sum")
	r.from, r.to = from, to
	if ctx != r.ctx || category != r.budget.Category {
		return 0, errors.New("sum lost context or category")
	}
	if r.failAt == "sum" {
		return 0, r.failure
	}
	return r.spent, nil
}
func (r *spendingRepository) AddTransaction(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	r.calls = append(r.calls, "insert")
	if ctx != r.ctx {
		return domain.Transaction{}, errors.New("insert lost context")
	}
	if r.failAt == "insert" {
		return domain.Transaction{}, r.failure
	}
	tx.ID = 17
	r.stored = tx
	return tx, nil
}

type transactionRunner struct {
	repo                *spendingRepository
	beginErr, commitErr error
	calls               int
}

func (r *transactionRunner) WithinTransaction(ctx context.Context, f func(domain.BudgetRepository, domain.ExpenseRepository) error) error {
	r.calls++
	if r.beginErr != nil {
		return r.beginErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f(r.repo, r.repo); err != nil {
		return err
	}
	return r.commitErr
}

func TestAddTransactionUsesTransactionalRepositories(t *testing.T) {
	failure := errors.New("repository failed")
	day := domain.Day(2026, time.November, 15)
	tests := []struct {
		name      string
		found     bool
		spent     float64
		failAt    string
		amount    float64
		wantErr   error
		wantCalls []string
	}{
		{name: "exact limit", found: true, spent: 60, amount: 40.004, wantCalls: []string{"budget", "sum", "insert"}},
		{name: "no budget", amount: 40, wantCalls: []string{"budget", "insert"}},
		{name: "over budget", found: true, spent: 61, amount: 40, wantErr: domain.ErrBudgetExceeded, wantCalls: []string{"budget", "sum"}},
		{name: "invalid input", amount: -1, wantErr: domain.ErrInvalidAmount},
		{name: "begin failure", amount: 40, failAt: "begin", wantErr: failure},
		{name: "budget failure", amount: 40, failAt: "budget", wantErr: failure, wantCalls: []string{"budget"}},
		{name: "sum failure", found: true, amount: 40, failAt: "sum", wantErr: failure, wantCalls: []string{"budget", "sum"}},
		{name: "insert failure", amount: 40, failAt: "insert", wantErr: failure, wantCalls: []string{"budget", "insert"}},
		{name: "commit failure", amount: 40, failAt: "commit", wantErr: failure, wantCalls: []string{"budget", "insert"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &spendingRepository{budget: domain.Budget{Category: "еда", Limit: 100, Period: domain.PeriodMonth}, found: tt.found, spent: tt.spent, failAt: tt.failAt, failure: failure}
			runner := &transactionRunner{repo: repo}
			if tt.failAt == "begin" {
				runner.beginErr = failure
			}
			if tt.failAt == "commit" {
				runner.commitErr = failure
			}
			// The regular repositories are nil: a budget decision must use only
			// the repositories supplied inside the transaction.
			svc := New(nil, nil, runner)
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
			defer cancel()
			got, err := svc.AddTransaction(ctx, domain.Transaction{Amount: tt.amount, Category: " Еда ", Description: " lunch ", Date: day.Add(12 * time.Hour)})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(repo.calls, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", repo.calls, tt.wantCalls)
			}
			if tt.wantErr != nil && got != (domain.Transaction{}) {
				t.Errorf("failed operation returned %+v", got)
			}
			if tt.amount < 0 && runner.calls != 0 {
				t.Error("invalid input reached transaction")
			}
			if err == nil {
				want := domain.Transaction{ID: 17, Amount: 40, Category: "еда", Description: "lunch", Date: day}
				if got != want {
					t.Errorf("stored = %+v, want %+v", got, want)
				}
				deadline, _ := repo.ctx.Deadline()
				parentDeadline, _ := ctx.Deadline()
				if deadline != parentDeadline {
					t.Error("service extended the parent deadline")
				}
				if tt.found && (!repo.from.Equal(domain.Day(2026, time.November, 1)) || !repo.to.Equal(domain.Day(2026, time.December, 1))) {
					t.Errorf("budget range = %v to %v", repo.from, repo.to)
				}
			}
		})
	}
}
