package main

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
)

func TestSetBudget(t *testing.T) {
	tests := []struct {
		name    string
		budget  Budget
		wantErr error
	}{
		{name: "valid", budget: Budget{Category: "еда", Limit: 5000}},
		{name: "category is trimmed", budget: Budget{Category: " еда ", Limit: 5000}},
		{name: "blank category", budget: Budget{Category: " ", Limit: 5000}, wantErr: ErrEmptyCategory},
		{name: "zero limit", budget: Budget{Category: "еда", Limit: 0}, wantErr: ErrInvalidLimit},
		{name: "negative limit", budget: Budget{Category: "еда", Limit: -1}, wantErr: ErrInvalidLimit},
		{name: "NaN limit", budget: Budget{Category: "еда", Limit: math.NaN()}, wantErr: ErrInvalidLimit},
		{name: "infinite limit", budget: Budget{Category: "еда", Limit: math.Inf(1)}, wantErr: ErrInvalidLimit},
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
		{name: "float rounding", limit: 0.3, spent: []float64{0.1}, category: "еда", amount: 0.2},
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

func TestBudgetExceededErrorDetails(t *testing.T) {
	resetStorage()
	if err := SetBudget(Budget{Category: "еда", Limit: 1000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	if err := AddTransaction(Transaction{Amount: 600, Category: "еда"}); err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	err := AddTransaction(Transaction{Amount: 500, Category: "еда"})

	var budgetErr *BudgetExceededError
	if !errors.As(err, &budgetErr) {
		t.Fatalf("AddTransaction() error = %v, want *BudgetExceededError", err)
	}
	want := BudgetExceededError{Category: "еда", Limit: 1000, Spent: 600, Amount: 500}
	if *budgetErr != want {
		t.Errorf("error details = %+v, want %+v", *budgetErr, want)
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
			input: `[{"category": "еда", "limit": 5000}, {"category": " транспорт ", "limit": 2000.5}]`,
			want:  []Budget{{Category: "еда", Limit: 5000}, {Category: "транспорт", Limit: 2000.5}},
		},
		{name: "empty array", input: `[]`, want: initial},
		{name: "empty input", input: " \n", wantErr: "no data"},
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
			name:    "duplicate category",
			input:   `[{"category": "такси", "limit": 300}, {"category": " такси", "limit": 400}]`,
			wantErr: "duplicate category",
		},
		{name: "data after the array", input: `[] []`, wantErr: "unexpected data"},
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
	resetStorage()
	readErr := errors.New("disk failure")

	err := LoadBudgets(iotest.ErrReader(readErr))
	if !errors.Is(err, readErr) {
		t.Fatalf("LoadBudgets() error = %v, want it to wrap %v", err, readErr)
	}
}
