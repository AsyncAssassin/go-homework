package ledger

import (
	"context"
	"errors"
	"testing"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
)

// TestBudgetRule follows a budget through a transaction that fits into it
// and one that does not. It changes the package storage, so it does not run
// in parallel.
func TestBudgetRule(t *testing.T) {
	svc := NewMemory()

	if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 5000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 4500, Category: "еда", Description: "продукты"}); err != nil {
		t.Fatalf("AddTransaction() within the limit error = %v, want nil", err)
	}
	if got := len(mustListTransactions(t, svc)); got != 1 {
		t.Fatalf("ListTransactions() has %d transactions, want 1", got)
	}

	// 4500 + 600 is over the limit of 5000.
	_, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 600, Category: "еда", Description: "ресторан"})
	if !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Errorf("AddTransaction() over the limit error = %v, want %v", err, domain.ErrBudgetExceeded)
	}
	if got := len(mustListTransactions(t, svc)); got != 1 {
		t.Errorf("ListTransactions() has %d transactions after the rejected one, want 1", got)
	}
}
