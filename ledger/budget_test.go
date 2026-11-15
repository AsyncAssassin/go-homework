package ledger

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
	"github.com/AsyncAssassin/go-homework/ledger/internal/repository/memory"
	"github.com/AsyncAssassin/go-homework/ledger/service"
)

// TestSetBudget checks how a budget is stored and that a budget rejected by
// Validate is not. The validation rules are tested in TestBudgetValidate.
func TestSetBudget(t *testing.T) {
	tests := []struct {
		name    string
		budget  domain.Budget
		wantErr error
	}{
		{name: "valid", budget: domain.Budget{Category: "еда", Limit: 5000}},
		{name: "category is normalized", budget: domain.Budget{Category: " Еда ", Limit: 5000}},
		{name: "limit is rounded to kopecks", budget: domain.Budget{Category: "еда", Limit: 5000.001}},
		{name: "blank category", budget: domain.Budget{Category: " ", Limit: 5000}, wantErr: domain.ErrEmptyCategory},
		{name: "invalid limit", budget: domain.Budget{Category: "еда", Limit: -1}, wantErr: domain.ErrInvalidLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewMemory()

			got, err := svc.SetBudget(context.Background(), tt.budget)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetBudget() error = %v, want %v", err, tt.wantErr)
			}

			var want []domain.Budget
			if tt.wantErr == nil {
				want = []domain.Budget{{Category: "еда", Limit: 5000}}
				if got != want[0] {
					t.Errorf("SetBudget() = %+v, want the stored budget %+v", got, want[0])
				}
			}
			if stored := mustListBudgets(t, svc); !slices.Equal(stored, want) {
				t.Errorf("budgets = %+v, want %+v", stored, want)
			}
		})
	}
}

func TestSetBudgetUpdatesLimit(t *testing.T) {
	svc := NewMemory()
	for _, limit := range []float64{5000, 7000} {
		if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: limit}); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
	}

	want := []domain.Budget{{Category: "еда", Limit: 7000}}
	if got := mustListBudgets(t, svc); !slices.Equal(got, want) {
		t.Errorf("budgets = %+v, want %+v", got, want)
	}
}

func TestSetBudgetPeriod(t *testing.T) {
	tests := []struct {
		name    string
		period  domain.Period
		want    domain.Period // period of the stored budget
		wantErr error
	}{
		{name: "no period", period: domain.PeriodNone, want: domain.PeriodNone},
		{name: "month", period: domain.PeriodMonth, want: domain.PeriodMonth},
		{name: "year", period: domain.PeriodYear, want: domain.PeriodYear},
		{name: "period is normalized", period: " Month ", want: domain.PeriodMonth},
		{name: "blank period", period: " ", want: domain.PeriodNone},
		{name: "unknown period", period: "week", wantErr: domain.ErrInvalidPeriod},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewMemory()

			_, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 5000, Period: tt.period})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetBudget() error = %v, want %v", err, tt.wantErr)
			}

			var want []domain.Budget
			if tt.wantErr == nil {
				want = []domain.Budget{{Category: "еда", Limit: 5000, Period: tt.want}}
			}
			if got := mustListBudgets(t, svc); !slices.Equal(got, want) {
				t.Errorf("budgets = %+v, want %+v", got, want)
			}
		})
	}
}

func TestListBudgetUsage(t *testing.T) {
	svc := NewMemory()
	for _, b := range []domain.Budget{
		{Category: "ремонт", Limit: 100000},
		{Category: "еда", Limit: 5000, Period: domain.PeriodMonth},
		{Category: "отпуск", Limit: 90000, Period: domain.PeriodYear},
	} {
		if _, err := svc.SetBudget(context.Background(), b); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
	}
	date := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.Local)
	for _, tx := range []domain.Transaction{
		{Amount: 1000, Category: "еда", Date: date},
		{Amount: 700, Category: "еда", Date: date.AddDate(0, -1, 0)},      // previous month
		{Amount: 30000, Category: "отпуск", Date: date.AddDate(0, -3, 0)}, // same year
		{Amount: 20000, Category: "ремонт", Date: date.AddDate(-1, 0, 0)}, // a year ago
		{Amount: 500, Category: "такси", Date: date},                      // no budget
	} {
		if _, err := svc.AddTransaction(context.Background(), tx); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}

	// Sorted by category, each with the spending of its period that contains date.
	want := []domain.BudgetUsage{
		{Budget: domain.Budget{Category: "еда", Limit: 5000, Period: domain.PeriodMonth}, PeriodLabel: "2026-10", Spent: 1000},
		{Budget: domain.Budget{Category: "отпуск", Limit: 90000, Period: domain.PeriodYear}, PeriodLabel: "2026", Spent: 30000},
		{Budget: domain.Budget{Category: "ремонт", Limit: 100000}, Spent: 20000},
	}
	got, err := svc.ListBudgetUsage(context.Background(), date)
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
			svc := NewMemory()
			if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: tt.limit}); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			for _, amount := range tt.spent {
				if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: amount, Category: "еда"}); err != nil {
					t.Fatalf("AddTransaction(%v) error = %v", amount, err)
				}
			}

			_, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: tt.amount, Category: tt.category})

			wantStored := len(tt.spent) + 1
			if tt.wantErr {
				if !errors.Is(err, domain.ErrBudgetExceeded) {
					t.Fatalf("AddTransaction() error = %v, want %v", err, domain.ErrBudgetExceeded)
				}
				wantStored--
			} else if err != nil {
				t.Fatalf("AddTransaction() error = %v, want nil", err)
			}
			if got := len(mustListTransactions(t, svc)); got != wantStored {
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
		period  domain.Period
		spentAt time.Time // date of the earlier transaction of 800
		date    time.Time // date of the checked transaction of 300
		wantErr bool      // whether 800 + 300 must exceed the limit of 1000
	}{
		{name: "month: same month", period: domain.PeriodMonth, spentAt: oct, date: oct.AddDate(0, 0, 30), wantErr: true},
		{name: "month: previous month", period: domain.PeriodMonth, spentAt: oct.Add(-time.Nanosecond), date: oct},
		{name: "month: same month of the next year", period: domain.PeriodMonth, spentAt: oct, date: oct.AddDate(1, 0, 0)},
		{name: "year: another month", period: domain.PeriodYear, spentAt: oct.AddDate(0, -9, 0), date: oct, wantErr: true},
		{name: "year: next year", period: domain.PeriodYear, spentAt: oct, date: oct.AddDate(0, 3, 0)},
		{name: "no period: another year", period: domain.PeriodNone, spentAt: oct.AddDate(-1, 0, 0), date: oct, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewMemory()
			if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 1000, Period: tt.period}); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 800, Category: "еда", Date: tt.spentAt}); err != nil {
				t.Fatalf("AddTransaction() error = %v", err)
			}

			_, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 300, Category: "еда", Date: tt.date})
			if tt.wantErr {
				if !errors.Is(err, domain.ErrBudgetExceeded) {
					t.Errorf("AddTransaction() error = %v, want %v", err, domain.ErrBudgetExceeded)
				}
			} else if err != nil {
				t.Errorf("AddTransaction() error = %v, want nil", err)
			}
		})
	}
}

func TestAddTransactionBudgetZeroDate(t *testing.T) {
	// Spending at the zero time, stored directly because AddTransaction would
	// date it today. In every time zone it is in the same month as a zero date.
	repo := memory.New()
	svc := service.New(repo, repo, repo, nil)
	if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 1000, Period: domain.PeriodMonth}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddTransaction(context.Background(), domain.Transaction{Amount: 800, Category: "еда"}); err != nil {
		t.Fatal(err)
	}

	// The zero date becomes today before the budget check, so the
	// spending at the zero time is in another month and does not count.
	if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 300, Category: "еда"}); err != nil {
		t.Fatalf("AddTransaction() error = %v, want nil", err)
	}
}

// TestSpentIsRounded checks that both storages report the amount spent in
// kopecks: float sums like 0.1+0.2 are not exactly 0.3.
func TestSpentIsRounded(t *testing.T) {
	date := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local)
	check := func(t *testing.T, svc service.Service) {
		if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 0.3}); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
		for _, amount := range []float64{0.1, 0.2} {
			if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: amount, Category: "еда", Date: date}); err != nil {
				t.Fatalf("AddTransaction(%v) error = %v", amount, err)
			}
		}
		usage, err := svc.ListBudgetUsage(context.Background(), date)
		if err != nil {
			t.Fatalf("ListBudgetUsage() error = %v", err)
		}
		if len(usage) != 1 || usage[0].Spent != 0.3 {
			t.Errorf("ListBudgetUsage() = %+v, want spent 0.3", usage)
		}
		_, err = svc.AddTransaction(context.Background(), domain.Transaction{Amount: 0.01, Category: "еда", Date: date})
		var exceeded *domain.BudgetExceededError
		if !errors.As(err, &exceeded) {
			t.Fatalf("AddTransaction() over the limit error = %v, want %v", err, domain.ErrBudgetExceeded)
		}
		if exceeded.Spent != 0.3 {
			t.Errorf("domain.BudgetExceededError.Spent = %v, want 0.3", exceeded.Spent)
		}
	}

	t.Run("memory", func(t *testing.T) {
		svc := NewMemory()

		check(t, svc)
	})
	t.Run("postgres", func(t *testing.T) {
		svc := connectTest(t).Service
		check(t, svc)
	})
}

func TestBudgetExceededError(t *testing.T) {
	date := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.Local)

	tests := []struct {
		name    string
		period  domain.Period
		want    domain.BudgetExceededError
		wantMsg string
	}{
		{
			name:    "no period",
			period:  domain.PeriodNone,
			want:    domain.BudgetExceededError{Category: "еда", Limit: 1000, Spent: 600, Amount: 500},
			wantMsg: `budget exceeded for "еда": spent 600.00 + new 500.00 > limit 1000.00`,
		},
		{
			name:    "month",
			period:  domain.PeriodMonth,
			want:    domain.BudgetExceededError{Category: "еда", PeriodLabel: "2026-10", Limit: 1000, Spent: 600, Amount: 500},
			wantMsg: `budget exceeded for "еда" in 2026-10: spent 600.00 + new 500.00 > limit 1000.00`,
		},
		{
			name:    "year",
			period:  domain.PeriodYear,
			want:    domain.BudgetExceededError{Category: "еда", PeriodLabel: "2026", Limit: 1000, Spent: 600, Amount: 500},
			wantMsg: `budget exceeded for "еда" in 2026: spent 600.00 + new 500.00 > limit 1000.00`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewMemory()
			if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 1000, Period: tt.period}); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 600, Category: "еда", Date: date}); err != nil {
				t.Fatalf("AddTransaction() error = %v", err)
			}

			_, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 500, Category: "еда", Date: date})

			var budgetErr *domain.BudgetExceededError
			if !errors.As(err, &budgetErr) {
				t.Fatalf("AddTransaction() error = %v, want *domain.BudgetExceededError", err)
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
	initial := []domain.Budget{{Category: "еда", Limit: 100}}

	tests := []struct {
		name    string
		input   string
		want    []domain.Budget // budgets after a successful load
		wantErr string          // part of the expected error message
	}{
		{
			name:  "adds and updates budgets",
			input: `[{"category": "еда", "limit": 5000, "period": "Month"}, {"category": " Транспорт ", "limit": 2000.5}]`,
			want: []domain.Budget{
				{Category: "еда", Limit: 5000, Period: domain.PeriodMonth},
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
			svc := NewMemory()
			if _, err := svc.SetBudget(context.Background(), initial[0]); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}

			err := svc.LoadBudgets(context.Background(), strings.NewReader(tt.input))

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
			if got := mustListBudgets(t, svc); !slices.Equal(got, want) {
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
			svc := NewMemory()

			err := svc.LoadBudgets(context.Background(), tt.r)
			if !errors.Is(err, readErr) {
				t.Fatalf("LoadBudgets() error = %v, want it to wrap %v", err, readErr)
			}
		})
	}
}

// mustListBudgets returns svc.ListBudgets(context.Background()) and stops the test if it fails.
func mustListBudgets(t *testing.T, svc service.Service) []domain.Budget {
	t.Helper()
	list, err := svc.ListBudgets(context.Background())
	if err != nil {
		t.Fatalf("ListBudgets() error = %v", err)
	}
	return list
}
