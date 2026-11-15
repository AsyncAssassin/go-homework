package ledger

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
	"github.com/AsyncAssassin/go-homework/ledger/service"
)

// TestAddTransaction checks that a transaction rejected by Validate is not
// stored. The validation rules are tested in TestTransactionValidate.
func TestAddTransaction(t *testing.T) {
	tests := []struct {
		name    string
		tx      domain.Transaction
		wantErr error
	}{
		{name: "valid", tx: domain.Transaction{Amount: 100, Category: "еда"}},
		{name: "invalid amount", tx: domain.Transaction{Amount: -50, Category: "еда"}, wantErr: domain.ErrInvalidAmount},
		{name: "blank category", tx: domain.Transaction{Amount: 100, Category: "  "}, wantErr: domain.ErrEmptyCategory},
		{
			name:    "year out of range",
			tx:      domain.Transaction{Amount: 100, Category: "еда", Date: time.Date(10000, time.January, 1, 12, 0, 0, 0, time.Local)},
			wantErr: domain.ErrInvalidDate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewMemory()

			_, err := svc.AddTransaction(context.Background(), tt.tx)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AddTransaction() error = %v, want %v", err, tt.wantErr)
			}

			wantStored := 1
			if tt.wantErr != nil {
				wantStored = 0
			}
			if got := len(mustListTransactions(t, svc)); got != wantStored {
				t.Errorf("stored %d transactions, want %d", got, wantStored)
			}
		})
	}
}

func TestAddTransactionFillsFields(t *testing.T) {
	svc := NewMemory()
	date := time.Date(2026, 9, 1, 15, 30, 0, 0, time.Local)
	first, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 10, Category: "еда", Date: date})
	if err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}
	today := domain.StartOfDay(time.Now())
	second, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 20.004, Category: " Транспорт ", Description: "  проездной "})
	if err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	// Only the day of the date is kept.
	wantDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	if want := (domain.Transaction{ID: 1, Amount: 10, Category: "еда", Date: wantDate}); first != want {
		t.Errorf("first AddTransaction() = %+v, want %+v", first, want)
	}
	// A zero date becomes today; the test may cross midnight.
	if d := second.Date; !d.Equal(today) && !d.Equal(today.AddDate(0, 0, 1)) {
		t.Errorf("zero date became %v, want today %v", d, today)
	}
	if want := (domain.Transaction{ID: 2, Amount: 20, Category: "транспорт", Description: "проездной", Date: second.Date}); second != want {
		t.Errorf("second AddTransaction() = %+v, want %+v", second, want)
	}
	// AddTransaction returns exactly what it stored.
	if got, want := mustListTransactions(t, svc), []domain.Transaction{second, first}; !slices.Equal(got, want) {
		t.Errorf("stored %+v, want %+v", got, want)
	}
}

func TestListTransactionsOrder(t *testing.T) {
	svc := NewMemory()
	day := time.Date(2026, time.October, 12, 12, 0, 0, 0, time.Local)
	for _, tx := range []domain.Transaction{
		{Amount: 1, Category: "еда", Date: day},                   // ID 1
		{Amount: 2, Category: "еда", Date: day.AddDate(0, 0, -1)}, // ID 2, the day before
		{Amount: 3, Category: "еда", Date: day.AddDate(0, 0, 1)},  // ID 3, the day after
		{Amount: 4, Category: "еда", Date: day.In(time.UTC)},      // ID 4, the day of ID 1 given in another zone
	} {
		if _, err := svc.AddTransaction(context.Background(), tx); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}

	// Newest first, and the larger ID first for the same day.
	var ids []int
	for _, tx := range mustListTransactions(t, svc) {
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
	svc := NewMemory()
	if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 50, Period: domain.PeriodMonth}); err != nil {
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
			_, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 0.1, Category: "еда", Date: date})
			errs <- err
			// Readers and another writer at the same time.
			svc.ListTransactions(context.Background())
			svc.ListBudgetUsage(context.Background(), date)
			if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "такси", Limit: float64(i + 1)}); err != nil {
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
		case !errors.Is(err, domain.ErrBudgetExceeded):
			t.Errorf("AddTransaction() error = %v, want nil or %v", err, domain.ErrBudgetExceeded)
		}
	}
	if added != fit {
		t.Errorf("added %d transactions, want %d", added, fit)
	}
	// All dates are equal, so the list goes from the last ID down to 1.
	list := mustListTransactions(t, svc)
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
	svc := NewMemory()
	if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 100, Category: "еда"}); err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	list := mustListTransactions(t, svc)
	list[0].Amount = 999

	if got := mustListTransactions(t, svc)[0].Amount; got != 100 {
		t.Errorf("storage changed through the returned slice: amount = %v, want 100", got)
	}
}

// TestDayWithoutMidnight stores transactions on the day before and on the
// day when the clocks skip midnight in America/Santiago. The expectations
// hold in every time zone; run the tests with TZ=America/Santiago to check
// the day itself.
func TestDayWithoutMidnight(t *testing.T) {
	before := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.Local)
	noon := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.Local)
	check := func(t *testing.T, svc service.Service) {
		for _, date := range []time.Time{before, noon} {
			if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 1, Category: "еда", Date: date}); err != nil {
				t.Fatalf("AddTransaction() error = %v", err)
			}
		}
		var days []string
		for _, tx := range mustListTransactions(t, svc) {
			days = append(days, formatDate(tx.Date))
		}
		if want := []string{"2026-09-06", "2026-09-05"}; !slices.Equal(days, want) {
			t.Errorf("days of the transactions = %q, want %q", days, want)
		}

		report, err := svc.GetReportSummary(context.Background(), noon, noon)
		if err != nil {
			t.Fatalf("GetReportSummary() error = %v", err)
		}
		if from, to := formatDate(report.From), formatDate(report.To); from != "2026-09-06" || to != from || report.Total != 1 {
			t.Errorf("report from %s to %s total %v, want 2026-09-06 to 2026-09-06 total 1", from, to, report.Total)
		}
	}

	t.Run("memory", func(t *testing.T) {
		svc := NewMemory()

		check(t, svc)
	})
	t.Run("postgres", func(t *testing.T) {
		svc := connectTest(t).Service
		deleteKeys(t, reportKey(noon, noon))
		check(t, svc)
	})
}

// mustListTransactions returns svc.ListTransactions(context.Background()) and stops the test if it
// fails.
func mustListTransactions(t *testing.T, svc service.Service) []domain.Transaction {
	t.Helper()
	list, err := svc.ListTransactions(context.Background())
	if err != nil {
		t.Fatalf("ListTransactions() error = %v", err)
	}
	return list
}
