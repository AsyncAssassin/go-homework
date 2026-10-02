package main

import (
	"errors"
	"math"
	"testing"
	"time"
)

// resetStorage empties the package-level storages before a test.
func resetStorage() {
	transactions = []Transaction{}
	budgets = map[string]Budget{}
}

func TestAddTransaction(t *testing.T) {
	tests := []struct {
		name    string
		tx      Transaction
		wantErr error
	}{
		{name: "valid", tx: Transaction{Amount: 100, Category: "еда"}},
		{name: "one kopeck", tx: Transaction{Amount: 0.01, Category: "еда"}},
		{name: "zero amount", tx: Transaction{Amount: 0, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "less than a kopeck", tx: Transaction{Amount: 0.001, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "negative amount", tx: Transaction{Amount: -50, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "NaN amount", tx: Transaction{Amount: math.NaN(), Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "infinite amount", tx: Transaction{Amount: math.Inf(1), Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "too large amount", tx: Transaction{Amount: 2e12, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "blank category", tx: Transaction{Amount: 100, Category: "  "}, wantErr: ErrEmptyCategory},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetStorage()

			err := AddTransaction(tt.tx)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AddTransaction() error = %v, want %v", err, tt.wantErr)
			}

			wantStored := 1
			if tt.wantErr != nil {
				wantStored = 0
			}
			if got := len(ListTransactions()); got != wantStored {
				t.Errorf("stored %d transactions, want %d", got, wantStored)
			}
		})
	}
}

func TestAddTransactionFillsFields(t *testing.T) {
	resetStorage()
	date := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, tx := range []Transaction{
		{Amount: 10, Category: "еда", Date: date},
		{Amount: 20.004, Category: " Транспорт ", Description: "  проездной "},
	} {
		if err := AddTransaction(tx); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}

	got := ListTransactions()
	if got[0].ID != 1 || got[1].ID != 2 {
		t.Errorf("IDs = %d, %d; want 1, 2", got[0].ID, got[1].ID)
	}
	if !got[0].Date.Equal(date) {
		t.Errorf("explicit date changed: got %v, want %v", got[0].Date, date)
	}
	if got[1].Date.IsZero() {
		t.Error("zero date was not replaced with the current time")
	}
	if got[1].Category != "транспорт" || got[1].Description != "проездной" {
		t.Errorf("category, description = %q, %q; want normalized %q, %q",
			got[1].Category, got[1].Description, "транспорт", "проездной")
	}
	if got[1].Amount != 20 {
		t.Errorf("amount = %v, want it rounded to kopecks: 20", got[1].Amount)
	}
}

func TestListTransactionsReturnsCopy(t *testing.T) {
	resetStorage()
	if err := AddTransaction(Transaction{Amount: 100, Category: "еда"}); err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	list := ListTransactions()
	list[0].Amount = 999

	if got := ListTransactions()[0].Amount; got != 100 {
		t.Errorf("storage changed through the returned slice: amount = %v, want 100", got)
	}
}
