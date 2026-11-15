package ledger

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// TestSetBudget checks how a budget is stored and that a budget rejected by
// Validate is not. The validation rules are tested in TestBudgetValidate.
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
		{name: "invalid limit", budget: Budget{Category: "еда", Limit: -1}, wantErr: ErrInvalidLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Reset()

			got, err := SetBudget(tt.budget)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetBudget() error = %v, want %v", err, tt.wantErr)
			}

			var want []Budget
			if tt.wantErr == nil {
				want = []Budget{{Category: "еда", Limit: 5000}}
				if got != want[0] {
					t.Errorf("SetBudget() = %+v, want the stored budget %+v", got, want[0])
				}
			}
			if stored := mustListBudgets(t); !slices.Equal(stored, want) {
				t.Errorf("budgets = %+v, want %+v", stored, want)
			}
		})
	}
}

func TestSetBudgetUpdatesLimit(t *testing.T) {
	Reset()
	for _, limit := range []float64{5000, 7000} {
		if _, err := SetBudget(Budget{Category: "еда", Limit: limit}); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
	}

	want := []Budget{{Category: "еда", Limit: 7000}}
	if got := mustListBudgets(t); !slices.Equal(got, want) {
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
			Reset()

			_, err := SetBudget(Budget{Category: "еда", Limit: 5000, Period: tt.period})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetBudget() error = %v, want %v", err, tt.wantErr)
			}

			var want []Budget
			if tt.wantErr == nil {
				want = []Budget{{Category: "еда", Limit: 5000, Period: tt.want}}
			}
			if got := mustListBudgets(t); !slices.Equal(got, want) {
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

func TestPeriodBounds(t *testing.T) {
	t.Parallel()

	day := Day
	tests := []struct {
		name      string
		period    Period
		t         time.Time
		wantStart time.Time
		wantEnd   time.Time
		wantOK    bool
	}{
		{name: "last hour of a month", period: PeriodMonth, t: day(2026, time.October, 31).Add(23 * time.Hour), wantStart: day(2026, time.October, 1), wantEnd: day(2026, time.November, 1), wantOK: true},
		// In America/Asuncion this month starts at 01:00: the clocks skipped
		// midnight of its first day.
		{name: "month that starts after midnight", period: PeriodMonth, t: day(2023, time.October, 15), wantStart: day(2023, time.October, 1), wantEnd: day(2023, time.November, 1), wantOK: true},
		{name: "December", period: PeriodMonth, t: day(2026, time.December, 15), wantStart: day(2026, time.December, 1), wantEnd: day(2027, time.January, 1), wantOK: true},
		{name: "local day given in another zone", period: PeriodMonth, t: day(2026, time.November, 1).In(time.FixedZone("UTC-12", -12*60*60)), wantStart: day(2026, time.November, 1), wantEnd: day(2026, time.December, 1), wantOK: true},
		{name: "first day of a year", period: PeriodYear, t: day(2026, time.January, 1), wantStart: day(2026, time.January, 1), wantEnd: day(2027, time.January, 1), wantOK: true},
		{name: "no period", period: PeriodNone, t: day(2026, time.May, 5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			start, end, ok := tt.period.bounds(tt.t)
			if !start.Equal(tt.wantStart) || !end.Equal(tt.wantEnd) || ok != tt.wantOK {
				t.Fatalf("bounds() = %v, %v, %v; want %v, %v, %v", start, end, ok, tt.wantStart, tt.wantEnd, tt.wantOK)
			}
			// The bounds and the labels split time the same way.
			if ok && (tt.period.label(start) != tt.period.label(tt.t) ||
				tt.period.label(end.Add(-time.Nanosecond)) != tt.period.label(tt.t) ||
				tt.period.label(end) == tt.period.label(tt.t)) {
				t.Errorf("bounds() = %v, %v do not match the label %q", start, end, tt.period.label(tt.t))
			}
		})
	}
}

func TestListBudgetUsage(t *testing.T) {
	Reset()
	for _, b := range []Budget{
		{Category: "ремонт", Limit: 100000},
		{Category: "еда", Limit: 5000, Period: PeriodMonth},
		{Category: "отпуск", Limit: 90000, Period: PeriodYear},
	} {
		if _, err := SetBudget(b); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
	}
	date := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.Local)
	for _, tx := range []Transaction{
		{Amount: 1000, Category: "еда", Date: date},
		{Amount: 700, Category: "еда", Date: date.AddDate(0, -1, 0)},      // previous month
		{Amount: 30000, Category: "отпуск", Date: date.AddDate(0, -3, 0)}, // same year
		{Amount: 20000, Category: "ремонт", Date: date.AddDate(-1, 0, 0)}, // a year ago
		{Amount: 500, Category: "такси", Date: date},                      // no budget
	} {
		if _, err := AddTransaction(tx); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}

	// Sorted by category, each with the spending of its period that contains date.
	want := []BudgetUsage{
		{Budget: Budget{Category: "еда", Limit: 5000, Period: PeriodMonth}, PeriodLabel: "2026-10", Spent: 1000},
		{Budget: Budget{Category: "отпуск", Limit: 90000, Period: PeriodYear}, PeriodLabel: "2026", Spent: 30000},
		{Budget: Budget{Category: "ремонт", Limit: 100000}, Spent: 20000},
	}
	got, err := ListBudgetUsage(date)
	if err != nil {
		t.Fatalf("ListBudgetUsage() error = %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("ListBudgetUsage() = %+v, want %+v", got, want)
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
			Reset()
			if _, err := SetBudget(Budget{Category: "еда", Limit: tt.limit}); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			for _, amount := range tt.spent {
				if _, err := AddTransaction(Transaction{Amount: amount, Category: "еда"}); err != nil {
					t.Fatalf("AddTransaction(%v) error = %v", amount, err)
				}
			}

			_, err := AddTransaction(Transaction{Amount: tt.amount, Category: tt.category})

			wantStored := len(tt.spent) + 1
			if tt.wantErr {
				if !errors.Is(err, ErrBudgetExceeded) {
					t.Fatalf("AddTransaction() error = %v, want %v", err, ErrBudgetExceeded)
				}
				wantStored--
			} else if err != nil {
				t.Fatalf("AddTransaction() error = %v, want nil", err)
			}
			if got := len(mustListTransactions(t)); got != wantStored {
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
			Reset()
			if _, err := SetBudget(Budget{Category: "еда", Limit: 1000, Period: tt.period}); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			if _, err := AddTransaction(Transaction{Amount: 800, Category: "еда", Date: tt.spentAt}); err != nil {
				t.Fatalf("AddTransaction() error = %v", err)
			}

			_, err := AddTransaction(Transaction{Amount: 300, Category: "еда", Date: tt.date})
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
	Reset()
	if _, err := SetBudget(Budget{Category: "еда", Limit: 1000, Period: PeriodMonth}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	// Spending at the zero time, stored directly because AddTransaction would
	// date it today. In every time zone it is in the same month as a zero date.
	mem := store.(*memoryStore)
	mem.transactions = append(mem.transactions, Transaction{ID: 1, Amount: 800, Category: "еда"})

	// The zero date becomes today before the budget check, so the
	// spending at the zero time is in another month and does not count.
	if _, err := AddTransaction(Transaction{Amount: 300, Category: "еда"}); err != nil {
		t.Fatalf("AddTransaction() error = %v, want nil", err)
	}
}

// TestSpentIsRounded checks that both storages report the amount spent in
// kopecks: float sums like 0.1+0.2 are not exactly 0.3.
func TestSpentIsRounded(t *testing.T) {
	date := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local)
	check := func(t *testing.T) {
		if _, err := SetBudget(Budget{Category: "еда", Limit: 0.3}); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
		for _, amount := range []float64{0.1, 0.2} {
			if _, err := AddTransaction(Transaction{Amount: amount, Category: "еда", Date: date}); err != nil {
				t.Fatalf("AddTransaction(%v) error = %v", amount, err)
			}
		}
		usage, err := ListBudgetUsage(date)
		if err != nil {
			t.Fatalf("ListBudgetUsage() error = %v", err)
		}
		if len(usage) != 1 || usage[0].Spent != 0.3 {
			t.Errorf("ListBudgetUsage() = %+v, want spent 0.3", usage)
		}
		_, err = AddTransaction(Transaction{Amount: 0.01, Category: "еда", Date: date})
		var exceeded *BudgetExceededError
		if !errors.As(err, &exceeded) {
			t.Fatalf("AddTransaction() over the limit error = %v, want %v", err, ErrBudgetExceeded)
		}
		if exceeded.Spent != 0.3 {
			t.Errorf("BudgetExceededError.Spent = %v, want 0.3", exceeded.Spent)
		}
	}

	t.Run("memory", func(t *testing.T) {
		Reset()
		t.Cleanup(Reset)
		check(t)
	})
	t.Run("postgres", func(t *testing.T) {
		connectTest(t)
		check(t)
	})
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
			Reset()
			if _, err := SetBudget(Budget{Category: "еда", Limit: 1000, Period: tt.period}); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			if _, err := AddTransaction(Transaction{Amount: 600, Category: "еда", Date: date}); err != nil {
				t.Fatalf("AddTransaction() error = %v", err)
			}

			_, err := AddTransaction(Transaction{Amount: 500, Category: "еда", Date: date})

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
			Reset()
			if _, err := SetBudget(initial[0]); err != nil {
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
			if got := mustListBudgets(t); !slices.Equal(got, want) {
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
			Reset()

			err := LoadBudgets(tt.r)
			if !errors.Is(err, readErr) {
				t.Fatalf("LoadBudgets() error = %v, want it to wrap %v", err, readErr)
			}
		})
	}
}

// mustListBudgets returns ListBudgets() and stops the test if it fails.
func mustListBudgets(t *testing.T) []Budget {
	t.Helper()
	list, err := ListBudgets()
	if err != nil {
		t.Fatalf("ListBudgets() error = %v", err)
	}
	return list
}
