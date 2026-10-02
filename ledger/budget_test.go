package main

import (
	"errors"
	"io"
	"math"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestSetBudget(t *testing.T) {
	tests := []struct {
		name    string
		budget  Budget
		wantErr error
	}{
		{name: "valid", budget: Budget{Category: "еда", Limit: 5000}},
		{name: "category is normalized", budget: Budget{Category: " Еда ", Limit: 5000}},
		{name: "limit is rounded to kopecks", budget: Budget{Category: "еда", Limit: 5000.001}},
		{name: "blank category", budget: Budget{Category: " ", Limit: 5000}, wantErr: ErrEmptyCategory},
		{name: "zero limit", budget: Budget{Category: "еда", Limit: 0}, wantErr: ErrInvalidLimit},
		{name: "negative limit", budget: Budget{Category: "еда", Limit: -1}, wantErr: ErrInvalidLimit},
		{name: "NaN limit", budget: Budget{Category: "еда", Limit: math.NaN()}, wantErr: ErrInvalidLimit},
		{name: "infinite limit", budget: Budget{Category: "еда", Limit: math.Inf(1)}, wantErr: ErrInvalidLimit},
		{name: "too large limit", budget: Budget{Category: "еда", Limit: 2e12}, wantErr: ErrInvalidLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetStorage()

			err := SetBudget(tt.budget)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetBudget() error = %v, want %v", err, tt.wantErr)
			}

			var want []Budget
			if tt.wantErr == nil {
				want = []Budget{{Category: "еда", Limit: 5000}}
			}
			if got := ListBudgets(); !slices.Equal(got, want) {
				t.Errorf("budgets = %+v, want %+v", got, want)
			}
		})
	}
}

func TestSetBudgetUpdatesLimit(t *testing.T) {
	resetStorage()
	for _, limit := range []float64{5000, 7000} {
		if err := SetBudget(Budget{Category: "еда", Limit: limit}); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
	}

	want := []Budget{{Category: "еда", Limit: 7000}}
	if got := ListBudgets(); !slices.Equal(got, want) {
		t.Errorf("budgets = %+v, want %+v", got, want)
	}
}

func TestSetBudgetPeriod(t *testing.T) {
	tests := []struct {
		name    string
		period  Period
		want    Period // period of the stored budget
		wantErr error
	}{
		{name: "no period", period: PeriodNone, want: PeriodNone},
		{name: "month", period: PeriodMonth, want: PeriodMonth},
		{name: "year", period: PeriodYear, want: PeriodYear},
		{name: "period is normalized", period: " Month ", want: PeriodMonth},
		{name: "blank period", period: " ", want: PeriodNone},
		{name: "unknown period", period: "week", wantErr: ErrInvalidPeriod},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetStorage()

			err := SetBudget(Budget{Category: "еда", Limit: 5000, Period: tt.period})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetBudget() error = %v, want %v", err, tt.wantErr)
			}

			var want []Budget
			if tt.wantErr == nil {
				want = []Budget{{Category: "еда", Limit: 5000, Period: tt.want}}
			}
			if got := ListBudgets(); !slices.Equal(got, want) {
				t.Errorf("budgets = %+v, want %+v", got, want)
			}
		})
	}
}

func TestPeriodLabel(t *testing.T) {
	// The first and the last half hour of October 2026 in the local time zone.
	// Shown in UTC-12 and UTC+14, at least one of them falls on another month,
	// whatever the local zone is, but the labels must still say October.
	first := time.Date(2026, time.October, 1, 0, 30, 0, 0, time.Local)
	last := time.Date(2026, time.October, 31, 23, 30, 0, 0, time.Local)
	west := time.FixedZone("UTC-12", -12*60*60)
	east := time.FixedZone("UTC+14", 14*60*60)

	tests := []struct {
		period Period
		want   string
	}{
		{period: PeriodNone, want: ""},
		{period: PeriodMonth, want: "2026-10"},
		{period: PeriodYear, want: "2026"},
	}
	for _, date := range []time.Time{first, first.In(west), last, last.In(east)} {
		for _, tt := range tests {
			if got := tt.period.label(date); got != tt.want {
				t.Errorf("Period(%q).label(%v) = %q, want %q", tt.period, date, got, tt.want)
			}
		}
	}
}

func TestAddTransactionBudget(t *testing.T) {
	tests := []struct {
		name     string
		limit    float64   // budget for "еда"
		spent    []float64 // amounts added to "еда" before the checked one
		category string
		amount   float64
		wantErr  bool // whether the budget must reject the transaction
	}{
		{name: "within the limit", limit: 1000, spent: []float64{600}, category: "еда", amount: 300},
		{name: "exactly the limit", limit: 1000, spent: []float64{600}, category: "еда", amount: 400},
		{name: "over the limit", limit: 1000, spent: []float64{600}, category: "еда", amount: 400.01, wantErr: true},
		{name: "first transaction over the limit", limit: 1000, category: "еда", amount: 1500, wantErr: true},
		{name: "category without a budget", limit: 1000, spent: []float64{600}, category: "здоровье", amount: 5000},
		{name: "category in another case", limit: 1000, spent: []float64{600}, category: " ЕДА", amount: 500, wantErr: true},
		{name: "float rounding", limit: 0.3, spent: []float64{0.1}, category: "еда", amount: 0.2},
		// 100.004 is stored as 100.00, so not even one more kopeck fits.
		{name: "fractions of a kopeck", limit: 100, spent: []float64{100.004}, category: "еда", amount: 0.01, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetStorage()
			if err := SetBudget(Budget{Category: "еда", Limit: tt.limit}); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			for _, amount := range tt.spent {
				if err := AddTransaction(Transaction{Amount: amount, Category: "еда"}); err != nil {
					t.Fatalf("AddTransaction(%v) error = %v", amount, err)
				}
			}

			err := AddTransaction(Transaction{Amount: tt.amount, Category: tt.category})

			wantStored := len(tt.spent) + 1
			if tt.wantErr {
				if !errors.Is(err, ErrBudgetExceeded) {
					t.Fatalf("AddTransaction() error = %v, want %v", err, ErrBudgetExceeded)
				}
				wantStored--
			} else if err != nil {
				t.Fatalf("AddTransaction() error = %v, want nil", err)
			}
			if got := len(ListTransactions()); got != wantStored {
				t.Errorf("stored %d transactions, want %d", got, wantStored)
			}
		})
	}
}

func TestAddTransactionBudgetPeriod(t *testing.T) {
	// Periods are counted in the local time zone, so the dates are local too.
	oct := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.Local)

	tests := []struct {
		name    string
		period  Period
		spentAt time.Time // date of the earlier transaction of 800
		date    time.Time // date of the checked transaction of 300
		wantErr bool      // whether 800 + 300 must exceed the limit of 1000
	}{
		{name: "month: same month", period: PeriodMonth, spentAt: oct, date: oct.AddDate(0, 0, 30), wantErr: true},
		{name: "month: previous month", period: PeriodMonth, spentAt: oct.Add(-time.Nanosecond), date: oct},
		{name: "month: same month of the next year", period: PeriodMonth, spentAt: oct, date: oct.AddDate(1, 0, 0)},
		{name: "year: another month", period: PeriodYear, spentAt: oct.AddDate(0, -9, 0), date: oct, wantErr: true},
		{name: "year: next year", period: PeriodYear, spentAt: oct, date: oct.AddDate(0, 3, 0)},
		{name: "no period: another year", period: PeriodNone, spentAt: oct.AddDate(-1, 0, 0), date: oct, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetStorage()
			if err := SetBudget(Budget{Category: "еда", Limit: 1000, Period: tt.period}); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			if err := AddTransaction(Transaction{Amount: 800, Category: "еда", Date: tt.spentAt}); err != nil {
				t.Fatalf("AddTransaction() error = %v", err)
			}

			err := AddTransaction(Transaction{Amount: 300, Category: "еда", Date: tt.date})
			if tt.wantErr {
				if !errors.Is(err, ErrBudgetExceeded) {
					t.Errorf("AddTransaction() error = %v, want %v", err, ErrBudgetExceeded)
				}
			} else if err != nil {
				t.Errorf("AddTransaction() error = %v, want nil", err)
			}
		})
	}
}

func TestAddTransactionBudgetZeroDate(t *testing.T) {
	resetStorage()
	if err := SetBudget(Budget{Category: "еда", Limit: 1000, Period: PeriodMonth}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	// Spending a second after the zero time.Time: in every time zone it falls
	// into the same month as a zero date would.
	early := time.Time{}.Add(time.Second)
	if err := AddTransaction(Transaction{Amount: 800, Category: "еда", Date: early}); err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	// The zero date becomes the current time before the budget check, so the
	// early spending is in another month and does not count.
	if err := AddTransaction(Transaction{Amount: 300, Category: "еда"}); err != nil {
		t.Fatalf("AddTransaction() error = %v, want nil", err)
	}
}

func TestBudgetExceededError(t *testing.T) {
	date := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.Local)

	tests := []struct {
		name    string
		period  Period
		want    BudgetExceededError
		wantMsg string
	}{
		{
			name:    "no period",
			period:  PeriodNone,
			want:    BudgetExceededError{Category: "еда", Limit: 1000, Spent: 600, Amount: 500},
			wantMsg: `budget exceeded for "еда": spent 600.00 + new 500.00 > limit 1000.00`,
		},
		{
			name:    "month",
			period:  PeriodMonth,
			want:    BudgetExceededError{Category: "еда", PeriodLabel: "2026-10", Limit: 1000, Spent: 600, Amount: 500},
			wantMsg: `budget exceeded for "еда" in 2026-10: spent 600.00 + new 500.00 > limit 1000.00`,
		},
		{
			name:    "year",
			period:  PeriodYear,
			want:    BudgetExceededError{Category: "еда", PeriodLabel: "2026", Limit: 1000, Spent: 600, Amount: 500},
			wantMsg: `budget exceeded for "еда" in 2026: spent 600.00 + new 500.00 > limit 1000.00`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetStorage()
			if err := SetBudget(Budget{Category: "еда", Limit: 1000, Period: tt.period}); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			if err := AddTransaction(Transaction{Amount: 600, Category: "еда", Date: date}); err != nil {
				t.Fatalf("AddTransaction() error = %v", err)
			}

			err := AddTransaction(Transaction{Amount: 500, Category: "еда", Date: date})

			var budgetErr *BudgetExceededError
			if !errors.As(err, &budgetErr) {
				t.Fatalf("AddTransaction() error = %v, want *BudgetExceededError", err)
			}
			if *budgetErr != tt.want {
				t.Errorf("error details = %+v, want %+v", *budgetErr, tt.want)
			}
			if got := err.Error(); got != tt.wantMsg {
				t.Errorf("error message = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestLoadBudgets(t *testing.T) {
	// Every case starts with this budget; on error it must stay unchanged.
	initial := []Budget{{Category: "еда", Limit: 100}}

	tests := []struct {
		name    string
		input   string
		want    []Budget // budgets after a successful load
		wantErr string   // part of the expected error message
	}{
		{
			name:  "adds and updates budgets",
			input: `[{"category": "еда", "limit": 5000, "period": "Month"}, {"category": " Транспорт ", "limit": 2000.5}]`,
			want: []Budget{
				{Category: "еда", Limit: 5000, Period: PeriodMonth},
				{Category: "транспорт", Limit: 2000.5},
			},
		},
		{name: "empty array", input: `[]`, want: initial},
		{name: "empty input", input: " \n", wantErr: "no data"},
		{name: "null", input: `null`, wantErr: "got null"},
		{name: "broken JSON", input: `[{"category": "еда"`, wantErr: "decode JSON"},
		{name: "not an array", input: `{"category": "еда", "limit": 1}`, wantErr: "decode JSON"},
		{name: "wrong type", input: `[{"category": "еда", "limit": "много"}]`, wantErr: "decode JSON"},
		{name: "unknown field", input: `[{"category": "еда", "limt": 1}]`, wantErr: "unknown field"},
		{
			name:    "invalid budget",
			input:   `[{"category": "такси", "limit": 300}, {"category": "кафе", "limit": -5}]`,
			wantErr: "budget #2: invalid limit",
		},
		{
			name:    "invalid period",
			input:   `[{"category": "такси", "limit": 300, "period": " Week "}]`,
			wantErr: `budget #1: invalid period " Week "`, // as written in the input
		},
		{name: "wrong period type", input: `[{"category": "такси", "limit": 300, "period": 1}]`, wantErr: "decode JSON"},
		{
			name:    "duplicate category",
			input:   `[{"category": "такси", "limit": 300}, {"category": " Такси", "limit": 400}]`,
			wantErr: "duplicate category",
		},
		{name: "data after the array", input: `[] []`, wantErr: "unexpected data"},
		{name: "broken data after the array", input: `[] ]`, wantErr: "decode JSON"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetStorage()
			if err := SetBudget(initial[0]); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}

			err := LoadBudgets(strings.NewReader(tt.input))

			want := tt.want
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("LoadBudgets() error = %v, want nil", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadBudgets() error = %v, want it to contain %q", err, tt.wantErr)
				}
				want = initial
			}
			if got := ListBudgets(); !slices.Equal(got, want) {
				t.Errorf("budgets = %+v, want %+v", got, want)
			}
		})
	}
}

func TestLoadBudgetsReadError(t *testing.T) {
	readErr := errors.New("disk failure")

	tests := []struct {
		name string
		r    io.Reader
	}{
		{name: "at the start", r: iotest.ErrReader(readErr)},
		{name: "after the array", r: io.MultiReader(strings.NewReader("[]"), iotest.ErrReader(readErr))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetStorage()

			err := LoadBudgets(tt.r)
			if !errors.Is(err, readErr) {
				t.Fatalf("LoadBudgets() error = %v, want it to wrap %v", err, readErr)
			}
		})
	}
}
