package ledger

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
)

func TestMemoryServicesAreIndependent(t *testing.T) {
	first, second := NewMemory(), NewMemory()
	ctx := context.Background()
	if _, err := first.SetBudget(ctx, domain.Budget{Category: "еда", Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.AddTransaction(ctx, domain.Transaction{Category: "еда", Amount: 1}); err != nil {
		t.Fatal(err)
	}
	if list, err := second.ListBudgets(ctx); err != nil || len(list) != 0 {
		t.Fatalf("second budgets = %v, %v", list, err)
	}
	if tx, err := second.AddTransaction(ctx, domain.Transaction{Category: "еда", Amount: 2}); err != nil || tx.ID != 1 {
		t.Fatalf("second transaction = %v, %v", tx, err)
	}
	if _, err := first.AddTransaction(ctx, domain.Transaction{Category: "еда", Amount: 1}); !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Fatalf("first budget check = %v", err)
	}
}

func TestCanceledOperations(t *testing.T) {
	svc := NewMemory()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := []struct {
		name string
		call func() error
	}{
		{"add", func() error {
			_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 1, Category: "еда"})
			return err
		}},
		{"set", func() error { _, err := svc.SetBudget(ctx, domain.Budget{Limit: 1, Category: "еда"}); return err }},
		{"transactions", func() error { _, err := svc.ListTransactions(ctx); return err }},
		{"budgets", func() error { _, err := svc.ListBudgets(ctx); return err }},
		{"usage", func() error { _, err := svc.ListBudgetUsage(ctx, time.Now()); return err }},
		{"load", func() error { return svc.LoadBudgets(ctx, strings.NewReader(`[{"category":"еда","limit":1}]`)) }},
		{"report", func() error { _, err := svc.GetReportSummary(ctx, time.Now(), time.Now()); return err }},
	}
	for _, tt := range calls {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if got := mustListTransactions(t, svc); len(got) != 0 {
		t.Errorf("canceled add stored %v", got)
	}
	if got := mustListBudgets(t, svc); len(got) != 0 {
		t.Errorf("canceled writes stored %v", got)
	}
}

func TestNewCanceledContext(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:1/cashapp_test?sslmode=disable")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc, closeFn, err := New(ctx)
	if !errors.Is(err, context.Canceled) || svc != nil || closeFn != nil {
		t.Fatalf("New canceled: service=%v close=%t err=%v", svc, closeFn != nil, err)
	}
}
