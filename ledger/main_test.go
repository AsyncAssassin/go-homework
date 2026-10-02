package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemaining(t *testing.T) {
	tests := []struct {
		limit, spent, want float64
	}{
		{limit: 5000, spent: 4750.5, want: 249.5},
		{limit: 5000, spent: 5000, want: 0},
		{limit: 50, spent: 90, want: 0}, // the limit was lowered below the spending
	}
	for _, tt := range tests {
		if got := remaining(tt.limit, tt.spent); got != tt.want {
			t.Errorf("remaining(%v, %v) = %v, want %v", tt.limit, tt.spent, got, tt.want)
		}
	}
}

func TestPrintBudgets(t *testing.T) {
	resetStorage()
	for _, b := range []Budget{
		{Category: "еда", Limit: 5000, Period: PeriodMonth},
		{Category: "отпуск", Limit: 90000, Period: PeriodYear},
		{Category: "ремонт", Limit: 100000},
	} {
		if err := SetBudget(b); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
	}
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	for _, tx := range []Transaction{
		{Amount: 1000, Category: "еда", Date: now},
		{Amount: 700, Category: "еда", Date: now.AddDate(0, -1, 0)},      // previous month
		{Amount: 30000, Category: "отпуск", Date: now.AddDate(0, -3, 0)}, // same year
		{Amount: 20000, Category: "ремонт", Date: now.AddDate(-1, 0, 0)}, // a year ago
	} {
		if err := AddTransaction(tx); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}

	var out strings.Builder
	printBudgets(&out, now)

	want := "Budgets (3):\n" +
		"CATEGORY  PERIOD    LIMIT      SPENT     LEFT\n" +
		"еда       2026-10   5000.00    1000.00   4000.00\n" +
		"отпуск    2026      90000.00   30000.00  60000.00\n" +
		"ремонт    all time  100000.00  20000.00  80000.00\n"
	if got := out.String(); got != want {
		t.Errorf("printBudgets() wrote:\n%s\nwant:\n%s", got, want)
	}
}

func TestLoadBudgetsFromFile(t *testing.T) {
	t.Run("bundled budgets.json", func(t *testing.T) {
		resetStorage()
		if err := loadBudgetsFromFile("budgets.json"); err != nil {
			t.Fatalf("loadBudgetsFromFile() error = %v", err)
		}
		if len(ListBudgets()) == 0 {
			t.Error("no budgets loaded from budgets.json")
		}
	})

	t.Run("null instead of an array", func(t *testing.T) {
		resetStorage()
		path := filepath.Join(t.TempDir(), "budgets.json")
		if err := os.WriteFile(path, []byte("null"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := loadBudgetsFromFile(path); err == nil {
			t.Fatal("loadBudgetsFromFile() error = nil, want an error for null")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		resetStorage()
		err := loadBudgetsFromFile(filepath.Join(t.TempDir(), "missing.json"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("loadBudgetsFromFile() error = %v, want %v", err, fs.ErrNotExist)
		}
	})
}
