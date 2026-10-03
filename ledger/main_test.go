package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

func TestPrintTransactions(t *testing.T) {
	resetStorage()
	// The first and the last half hour of October 2026 in the local time zone,
	// given in UTC-12 and UTC+14. The table must show the local dates.
	first := time.Date(2026, time.October, 1, 0, 30, 0, 0, time.Local)
	last := time.Date(2026, time.October, 31, 23, 30, 0, 0, time.Local)
	for _, tx := range []Transaction{
		{Amount: 10, Category: "еда", Description: "завтрак", Date: first.In(time.FixedZone("UTC-12", -12*60*60))},
		{Amount: 20, Category: "еда", Description: "ужин", Date: last.In(time.FixedZone("UTC+14", 14*60*60))},
	} {
		if err := AddTransaction(tx); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}

	var out strings.Builder
	printTransactions(&out, ListTransactions())

	want := "Transactions (2):\n" +
		"ID  DATE        CATEGORY  AMOUNT  DESCRIPTION\n" +
		"1   2026-10-01  еда       10.00   завтрак\n" +
		"2   2026-10-31  еда       20.00   ужин\n"
	if got := out.String(); got != want {
		t.Errorf("printTransactions() wrote:\n%s\nwant:\n%s", got, want)
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
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.Local)
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
		// The README shows this file and the demo output that depends on it.
		want := []Budget{
			{Category: "еда", Limit: 5000, Period: PeriodMonth},
			{Category: "развлечения", Limit: 30000, Period: PeriodYear},
			{Category: "ремонт", Limit: 100000},
			{Category: "транспорт", Limit: 2500, Period: PeriodMonth},
		}
		if got := ListBudgets(); !slices.Equal(got, want) {
			t.Errorf("budgets = %+v, want %+v", got, want)
		}
	})

	t.Run("broken JSON", func(t *testing.T) {
		resetStorage()
		path := filepath.Join(t.TempDir(), "budgets.json")
		if err := os.WriteFile(path, []byte(`[{"category": "еда", "limit": 1`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := loadBudgetsFromFile(path); err == nil {
			t.Fatal("loadBudgetsFromFile() error = nil, want a decode error")
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

func TestLoadBudgetsIfExists(t *testing.T) {
	// Every case starts with this budget; it must survive a missing file.
	initial := Budget{Category: "еда", Limit: 100}

	tests := []struct {
		name       string
		content    string // written to the file; "" means no file at all
		wantLoaded bool
		wantErr    bool
		want       []Budget // budgets after the call
	}{
		{
			name:       "file exists",
			content:    `[{"category": "еда", "limit": 5000, "period": "month"}]`,
			wantLoaded: true,
			want:       []Budget{{Category: "еда", Limit: 5000, Period: PeriodMonth}},
		},
		{name: "file is missing", want: []Budget{initial}},
		{name: "file is broken", content: `[{"category": "еда"`, wantErr: true, want: []Budget{initial}},
		{name: "file is empty", content: "", wantErr: true, want: []Budget{initial}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetStorage()
			if err := SetBudget(initial); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			path := filepath.Join(t.TempDir(), "budgets.json")
			if tt.name != "file is missing" {
				if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			loaded, err := loadBudgetsIfExists(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadBudgetsIfExists() error = %v, wantErr %v", err, tt.wantErr)
			}
			if loaded != tt.wantLoaded {
				t.Errorf("loadBudgetsIfExists() loaded = %v, want %v", loaded, tt.wantLoaded)
			}
			if got := ListBudgets(); !slices.Equal(got, tt.want) {
				t.Errorf("budgets = %+v, want %+v", got, tt.want)
			}
		})
	}

	t.Run("directory instead of a file", func(t *testing.T) {
		resetStorage()
		loaded, err := loadBudgetsIfExists(t.TempDir())
		if err == nil || loaded {
			t.Fatalf("loadBudgetsIfExists() = %v, %v; want false and an error", loaded, err)
		}
	})
}
