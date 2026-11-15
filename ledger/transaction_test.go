package ledger

import (
	"context"
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
			if got := len(mustListTransactions(t)); got != wantStored {
				t.Errorf("stored %d transactions, want %d", got, wantStored)
			}
		})
	}
}

func TestAddTransactionFillsFields(t *testing.T) {
	Reset()
	date := time.Date(2026, 9, 1, 15, 30, 0, 0, time.Local)
	first, err := AddTransaction(Transaction{Amount: 10, Category: "еда", Date: date})
	if err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}
	today := startOfDay(time.Now())
	second, err := AddTransaction(Transaction{Amount: 20.004, Category: " Транспорт ", Description: "  проездной "})
	if err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	// Only the day of the date is kept.
	wantDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	if want := (Transaction{ID: 1, Amount: 10, Category: "еда", Date: wantDate}); first != want {
		t.Errorf("first AddTransaction() = %+v, want %+v", first, want)
	}
	// A zero date becomes today; the test may cross midnight.
	if d := second.Date; !d.Equal(today) && !d.Equal(today.AddDate(0, 0, 1)) {
		t.Errorf("zero date became %v, want today %v", d, today)
	}
	if want := (Transaction{ID: 2, Amount: 20, Category: "транспорт", Description: "проездной", Date: second.Date}); second != want {
		t.Errorf("second AddTransaction() = %+v, want %+v", second, want)
	}
	// AddTransaction returns exactly what it stored.
	if got, want := mustListTransactions(t), []Transaction{second, first}; !slices.Equal(got, want) {
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
		{Amount: 4, Category: "еда", Date: day.In(time.UTC)},      // ID 4, the day of ID 1 given in another zone
	} {
		if _, err := AddTransaction(tx); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}

	// Newest first, and the larger ID first for the same day.
	var ids []int
	for _, tx := range mustListTransactions(t) {
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
	list := mustListTransactions(t)
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

	list := mustListTransactions(t)
	list[0].Amount = 999

	if got := mustListTransactions(t)[0].Amount; got != 100 {
		t.Errorf("storage changed through the returned slice: amount = %v, want 100", got)
	}
}

func TestDay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		zone  string
		year  int
		month time.Month
		day   int
		want  string // RFC 3339
	}{
		{name: "ordinary day", zone: "Asia/Tbilisi", year: 2026, month: time.November, day: 15, want: "2026-11-15T00:00:00+04:00"},
		// The clocks go from 00:00 to 01:00, so the day starts at 01:00.
		{name: "no midnight in Santiago", zone: "America/Santiago", year: 2026, month: time.September, day: 6, want: "2026-09-06T01:00:00-03:00"},
		{name: "no midnight in Havana", zone: "America/Havana", year: 2026, month: time.March, day: 8, want: "2026-03-08T01:00:00-04:00"},
		{name: "first day of a month without midnight", zone: "America/Asuncion", year: 2023, month: time.October, day: 1, want: "2023-10-01T01:00:00-03:00"},
		// East of UTC time.Date already gives 01:00.
		{name: "no midnight in Cairo", zone: "Africa/Cairo", year: 2026, month: time.April, day: 24, want: "2026-04-24T01:00:00+03:00"},
		// Samoa skipped this day, so the next one starts instead.
		{name: "day that did not exist", zone: "Pacific/Apia", year: 2011, month: time.December, day: 30, want: "2011-12-31T00:00:00+14:00"},
		{name: "day 0 is the last day of the previous month", zone: "UTC", year: 2026, month: time.October, day: 0, want: "2026-09-30T00:00:00Z"},
		{name: "month 13 is January of the next year", zone: "UTC", year: 2026, month: 13, day: 1, want: "2027-01-01T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			loc, err := time.LoadLocation(tt.zone)
			if err != nil {
				t.Fatal(err)
			}
			if got := dayStart(tt.year, tt.month, tt.day, loc).Format(time.RFC3339); got != tt.want {
				t.Errorf("dayStart(%d, %d, %d, %s) = %s, want %s", tt.year, tt.month, tt.day, tt.zone, got, tt.want)
			}
		})
	}
}

// TestDayWithoutMidnight stores transactions on the day before and on the
// day when the clocks skip midnight in America/Santiago. The expectations
// hold in every time zone; run the tests with TZ=America/Santiago to check
// the day itself.
func TestDayWithoutMidnight(t *testing.T) {
	before := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.Local)
	noon := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.Local)
	check := func(t *testing.T) {
		for _, date := range []time.Time{before, noon} {
			if _, err := AddTransaction(Transaction{Amount: 1, Category: "еда", Date: date}); err != nil {
				t.Fatalf("AddTransaction() error = %v", err)
			}
		}
		var days []string
		for _, tx := range mustListTransactions(t) {
			days = append(days, formatDate(tx.Date))
		}
		if want := []string{"2026-09-06", "2026-09-05"}; !slices.Equal(days, want) {
			t.Errorf("days of the transactions = %q, want %q", days, want)
		}

		report, err := GetReportSummary(context.Background(), noon, noon)
		if err != nil {
			t.Fatalf("GetReportSummary() error = %v", err)
		}
		if from, to := formatDate(report.From), formatDate(report.To); from != "2026-09-06" || to != from || report.Total != 1 {
			t.Errorf("report from %s to %s total %v, want 2026-09-06 to 2026-09-06 total 1", from, to, report.Total)
		}
	}

	t.Run("memory", func(t *testing.T) {
		Reset()
		t.Cleanup(Reset)
		check(t)
	})
	t.Run("postgres", func(t *testing.T) {
		connectTest(t)
		deleteKeys(t, reportKey(noon, noon))
		check(t)
	})
}

// mustListTransactions returns ListTransactions() and stops the test if it
// fails.
func mustListTransactions(t *testing.T) []Transaction {
	t.Helper()
	list, err := ListTransactions()
	if err != nil {
		t.Fatalf("ListTransactions() error = %v", err)
	}
	return list
}
