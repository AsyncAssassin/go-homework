package ledger

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestClose checks that after Close every function of the package fails
// with errClosed until Reset. It changes the storage of the package, so it
// does not run in parallel.
func TestClose(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	if err := Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := Close(); err != nil {
		t.Errorf("second Close() error = %v", err)
	}

	tx := Transaction{Amount: 100, Category: "еда"}
	b := Budget{Category: "еда", Limit: 5000}
	tests := []struct {
		name string
		call func() error
	}{
		{"AddTransaction", func() error { _, err := AddTransaction(tx); return err }},
		{"ListTransactions", func() error { _, err := ListTransactions(); return err }},
		{"SetBudget", func() error { _, err := SetBudget(b); return err }},
		{"ListBudgets", func() error { _, err := ListBudgets(); return err }},
		{"ListBudgetUsage", func() error { _, err := ListBudgetUsage(time.Now()); return err }},
		{"LoadBudgets", func() error { return LoadBudgets(strings.NewReader(`[{"category":"еда","limit":5000}]`)) }},
		{"GetReportSummary", func() error {
			_, err := GetReportSummary(context.Background(), time.Now(), time.Now())
			return err
		}},
	}
	for _, tt := range tests {
		if err := tt.call(); !errors.Is(err, errClosed) {
			t.Errorf("%s() after Close() error = %v, want errClosed", tt.name, err)
		}
	}

	Reset()
	if _, err := AddTransaction(tx); err != nil {
		t.Errorf("AddTransaction() after Reset() error = %v", err)
	}
}
