package main

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestTransactionValidate(t *testing.T) {
	t.Parallel()

	date := time.Date(2026, time.October, 12, 0, 0, 0, 0, time.Local)

	tests := []struct {
		name    string
		tx      Transaction
		wantErr error
	}{
		{name: "valid", tx: Transaction{Amount: 450, Category: "еда", Description: "ланч", Date: date}},
		{name: "one kopeck", tx: Transaction{Amount: 0.01, Category: "еда"}},
		{name: "upper limit", tx: Transaction{Amount: maxAmount, Category: "еда"}},
		{name: "zero date is filled in later", tx: Transaction{Amount: 450, Category: "еда"}},
		{name: "category with spaces and capitals", tx: Transaction{Amount: 450, Category: " Еда "}},
		{name: "zero amount", tx: Transaction{Amount: 0, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "negative amount", tx: Transaction{Amount: -450, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "less than a kopeck", tx: Transaction{Amount: 0.001, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "too large amount", tx: Transaction{Amount: 2e12, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "NaN amount", tx: Transaction{Amount: math.NaN(), Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "infinite amount", tx: Transaction{Amount: math.Inf(1), Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "empty category", tx: Transaction{Amount: 450}, wantErr: ErrEmptyCategory},
		{name: "blank category", tx: Transaction{Amount: 450, Category: " \t"}, wantErr: ErrEmptyCategory},
		{
			name:    "year 0",
			tx:      Transaction{Amount: 450, Category: "еда", Date: time.Date(0, time.June, 1, 12, 0, 0, 0, time.Local)},
			wantErr: ErrInvalidDate,
		},
		{
			name:    "year 10000",
			tx:      Transaction{Amount: 450, Category: "еда", Date: time.Date(10000, time.June, 1, 12, 0, 0, 0, time.Local)},
			wantErr: ErrInvalidDate,
		},
		{
			// The year is taken in the zone of the date, so the result does
			// not depend on the time zone of the machine.
			name: "first hour of the year 1 in UTC",
			tx:   Transaction{Amount: 450, Category: "еда", Date: time.Date(1, time.January, 1, 1, 0, 0, 0, time.UTC)},
		},
		{
			name:    "the amount is checked first",
			tx:      Transaction{Amount: 0, Category: " "},
			wantErr: ErrInvalidAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.tx.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestBudgetValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		budget  Budget
		wantErr error
	}{
		{name: "valid", budget: Budget{Category: "еда", Limit: 5000}},
		{name: "monthly", budget: Budget{Category: "еда", Limit: 5000, Period: PeriodMonth}},
		{name: "yearly with spaces and capitals", budget: Budget{Category: " Отпуск ", Limit: 90000, Period: " Year "}},
		{name: "one kopeck", budget: Budget{Category: "еда", Limit: 0.01}},
		{name: "zero limit", budget: Budget{Category: "еда", Limit: 0}, wantErr: ErrInvalidLimit},
		{name: "negative limit", budget: Budget{Category: "еда", Limit: -5000}, wantErr: ErrInvalidLimit},
		{name: "too large limit", budget: Budget{Category: "еда", Limit: 2e12}, wantErr: ErrInvalidLimit},
		{name: "NaN limit", budget: Budget{Category: "еда", Limit: math.NaN()}, wantErr: ErrInvalidLimit},
		{name: "empty category", budget: Budget{Limit: 5000}, wantErr: ErrEmptyCategory},
		{name: "blank category", budget: Budget{Category: "  ", Limit: 5000}, wantErr: ErrEmptyCategory},
		{name: "unknown period", budget: Budget{Category: "еда", Limit: 5000, Period: "week"}, wantErr: ErrInvalidPeriod},
		{name: "the category is checked first", budget: Budget{Limit: -1, Period: "week"}, wantErr: ErrEmptyCategory},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.budget.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckValid(t *testing.T) {
	t.Parallel()

	// One function checks values of different types through the interface.
	tests := []struct {
		name    string
		v       Validatable
		wantErr error
	}{
		{name: "valid transaction", v: Transaction{Amount: 450, Category: "еда"}},
		{name: "invalid transaction", v: Transaction{Amount: -1, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "pointer to a transaction", v: &Transaction{Amount: 450}, wantErr: ErrEmptyCategory},
		{name: "valid budget", v: Budget{Category: "еда", Limit: 5000}},
		{name: "invalid budget", v: Budget{Category: "еда"}, wantErr: ErrInvalidLimit},
		{name: "pointer to a budget", v: &Budget{Category: "еда", Limit: 5000, Period: PeriodYear}},
		{name: "nil", v: nil, wantErr: ErrNothingToValidate},
		{name: "nil pointer to a transaction", v: (*Transaction)(nil), wantErr: ErrNothingToValidate},
		{name: "nil pointer to a budget", v: (*Budget)(nil), wantErr: ErrNothingToValidate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := CheckValid(tt.v)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("CheckValid() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
