package ledger

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// TestAddTransaction checks that a transaction rejected by Validate is not
// stored. The validation rules are tested in TestTransactionValidate.
func TestAddTransaction(t *testing.T) {
	tests := []struct {
		name    string
		tx      Transaction
		wantErr error
	}{
		{name: "valid", tx: Transaction{Amount: 100, Category: "еда"}},
		{name: "invalid amount", tx: Transaction{Amount: -50, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "blank category", tx: Transaction{Amount: 100, Category: "  "}, wantErr: ErrEmptyCategory},
		{
			name:    "year out of range",
			tx:      Transaction{Amount: 100, Category: "еда", Date: time.Date(10000, time.January, 1, 12, 0, 0, 0, time.Local)},
			wantErr: ErrInvalidDate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Reset()

			_, err := AddTransaction(tt.tx)
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
	Reset()
	date := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	first, err := AddTransaction(Transaction{Amount: 10, Category: "еда", Date: date})
	if err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}
	before := time.Now()
	second, err := AddTransaction(Transaction{Amount: 20.004, Category: " Транспорт ", Description: "  проездной "})
	if err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	if want := (Transaction{ID: 1, Amount: 10, Category: "еда", Date: date}); first != want {
		t.Errorf("first AddTransaction() = %+v, want %+v", first, want)
	}
	if second.Date.Before(before) {
		t.Errorf("zero date was not replaced with the current time: %v", second.Date)
	}
	if want := (Transaction{ID: 2, Amount: 20, Category: "транспорт", Description: "проездной", Date: second.Date}); second != want {
		t.Errorf("second AddTransaction() = %+v, want %+v", second, want)
	}
	// AddTransaction returns exactly what it stored.
	if got, want := ListTransactions(), []Transaction{second, first}; !slices.Equal(got, want) {
		t.Errorf("stored %+v, want %+v", got, want)
	}
}

func TestListTransactionsOrder(t *testing.T) {
	Reset()
	day := time.Date(2026, time.October, 12, 12, 0, 0, 0, time.Local)
	for _, tx := range []Transaction{
		{Amount: 1, Category: "еда", Date: day},                   // ID 1
		{Amount: 2, Category: "еда", Date: day.AddDate(0, 0, -1)}, // ID 2, the day before
		{Amount: 3, Category: "еда", Date: day.AddDate(0, 0, 1)},  // ID 3, the day after
		{Amount: 4, Category: "еда", Date: day.In(time.UTC)},      // ID 4, the moment of ID 1 in another zone
	} {
		if _, err := AddTransaction(tx); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}

	// Newest first, and the larger ID first for the same moment.
	var ids []int
	for _, tx := range ListTransactions() {
		ids = append(ids, tx.ID)
	}
	if want := []int{3, 4, 1, 2}; !slices.Equal(ids, want) {
		t.Errorf("IDs in ListTransactions() order = %v, want %v", ids, want)
	}
}

// TestAddTransactionConcurrent adds transactions from many goroutines at
// once. Each of them fits into the budget alone, but not all of them fit
// together, so exactly as many as fit must be stored: the budget check and
// the insert happen under one lock. With -race it also checks reads and
// writes of the storage for data races.
func TestAddTransactionConcurrent(t *testing.T) {
	Reset()
	if _, err := SetBudget(Budget{Category: "еда", Limit: 50, Period: PeriodMonth}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}

	// 600 transactions of 0.10 start together, 500 of them fit into 50.00.
	// A fixed date keeps them in one month even if the test runs at midnight.
	const workers, fit = 600, 500
	date := time.Date(2026, time.October, 12, 12, 0, 0, 0, time.Local)
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := AddTransaction(Transaction{Amount: 0.1, Category: "еда", Date: date})
			errs <- err
			// Readers and another writer at the same time.
			ListTransactions()
			ListBudgetUsage(date)
			if _, err := SetBudget(Budget{Category: "такси", Limit: float64(i + 1)}); err != nil {
				t.Errorf("SetBudget() error = %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	var added int
	for err := range errs {
		switch {
		case err == nil:
			added++
		case !errors.Is(err, ErrBudgetExceeded):
			t.Errorf("AddTransaction() error = %v, want nil or %v", err, ErrBudgetExceeded)
		}
	}
	if added != fit {
		t.Errorf("added %d transactions, want %d", added, fit)
	}
	// All dates are equal, so the list goes from the last ID down to 1.
	list := ListTransactions()
	if len(list) != fit {
		t.Fatalf("stored %d transactions, want %d", len(list), fit)
	}
	for i, tx := range list {
		if want := fit - i; tx.ID != want {
			t.Fatalf("transaction #%d in the list has ID %d, want %d", i+1, tx.ID, want)
		}
	}
}

func TestListTransactionsReturnsCopy(t *testing.T) {
	Reset()
	if _, err := AddTransaction(Transaction{Amount: 100, Category: "еда"}); err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	list := ListTransactions()
	list[0].Amount = 999

	if got := ListTransactions()[0].Amount; got != 100 {
		t.Errorf("storage changed through the returned slice: amount = %v, want 100", got)
	}
}
