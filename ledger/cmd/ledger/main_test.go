package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger"
	"github.com/AsyncAssassin/go-homework/ledger/domain"
	"github.com/AsyncAssassin/go-homework/ledger/service"
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
	svc := ledger.NewMemory()
	// The first and the last half hour of October 2026 in the local time zone,
	// given in UTC-12 and UTC+14. The table must show the local dates.
	first := time.Date(2026, time.October, 1, 0, 30, 0, 0, time.Local)
	last := time.Date(2026, time.October, 31, 23, 30, 0, 0, time.Local)
	for _, tx := range []domain.Transaction{
		{Amount: 10, Category: "еда", Description: "завтрак", Date: first.In(time.FixedZone("UTC-12", -12*60*60))},
		{Amount: 20, Category: "еда", Description: "ужин", Date: last.In(time.FixedZone("UTC+14", 14*60*60))},
	} {
		if _, err := svc.AddTransaction(context.Background(), tx); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}

	var out strings.Builder
	if err := printTransactions(svc, &out); err != nil {
		t.Fatalf("printTransactions() error = %v", err)
	}

	// Newest first, as ListTransactions returns them.
	want := "Transactions (2):\n" +
		"ID  DATE        CATEGORY  AMOUNT  DESCRIPTION\n" +
		"2   2026-10-31  еда       20.00   ужин\n" +
		"1   2026-10-01  еда       10.00   завтрак\n"
	if got := out.String(); got != want {
		t.Errorf("printTransactions() wrote:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrintBudgets(t *testing.T) {
	svc := ledger.NewMemory()
	for _, b := range []domain.Budget{
		{Category: "еда", Limit: 5000, Period: domain.PeriodMonth},
		{Category: "отпуск", Limit: 90000, Period: domain.PeriodYear},
		{Category: "ремонт", Limit: 100000},
	} {
		if _, err := svc.SetBudget(context.Background(), b); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
	}
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.Local)
	for _, tx := range []domain.Transaction{
		{Amount: 1000, Category: "еда", Date: now},
		{Amount: 700, Category: "еда", Date: now.AddDate(0, -1, 0)},      // previous month
		{Amount: 30000, Category: "отпуск", Date: now.AddDate(0, -3, 0)}, // same year
		{Amount: 20000, Category: "ремонт", Date: now.AddDate(-1, 0, 0)}, // a year ago
	} {
		if _, err := svc.AddTransaction(context.Background(), tx); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}

	var out strings.Builder
	if err := printBudgets(svc, &out, now); err != nil {
		t.Fatalf("printBudgets() error = %v", err)
	}

	want := "Budgets (3):\n" +
		"CATEGORY  PERIOD    LIMIT      SPENT     LEFT\n" +
		"еда       2026-10   5000.00    1000.00   4000.00\n" +
		"отпуск    2026      90000.00   30000.00  60000.00\n" +
		"ремонт    all time  100000.00  20000.00  80000.00\n"
	if got := out.String(); got != want {
		t.Errorf("printBudgets() wrote:\n%s\nwant:\n%s", got, want)
	}
}

func TestShowValidation(t *testing.T) {
	var out strings.Builder
	showValidation(&out)

	// Each line shows the type the interface held, the value and the result
	// of CheckValid; the columns are separated by two or more spaces.
	want := [][3]string{
		{"domain.Transaction", `450.00 "ланч" (еда)`, "ok"},
		{"domain.Transaction", `-100.00 "возврат" (еда)`, "invalid: invalid amount -100: must be from 0.01 to 999999999999.99"},
		{"domain.Transaction", `300.00 "такси" (no category)`, "invalid: category must not be empty"},
		{"domain.Transaction", `50.00 "кофе" (еда) on 0000-03-01`, "invalid: invalid date 0000-03-01: must be from 0001-01-02 to 9999-12-30 UTC"},
		{"domain.Budget", "кафе 3000.00 a month", "ok"},
		{"domain.Budget", "кафе 0.00", `invalid: invalid limit 0 for "кафе": must be from 0.01 to 999999999999.99`},
		{"domain.Budget", "no category 1000.00", "invalid: category must not be empty"},
	}
	columns := regexp.MustCompile(` {2,}`)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("showValidation() wrote %d lines, want %d:\n%s", len(lines), len(want), out.String())
	}
	for i, w := range want {
		got := columns.Split(strings.TrimPrefix(lines[i], "  "), -1)
		if len(got) != 3 || [3]string(got) != w {
			t.Errorf("line %d = %q, want columns %q", i+1, lines[i], w)
		}
	}
}

func TestLoadBudgetsFromFile(t *testing.T) {
	t.Run("bundled budgets.json", func(t *testing.T) {
		svc := ledger.NewMemory()
		if err := loadBudgetsFromFile(svc, filepath.Join("..", "..", "budgets.json")); err != nil {
			t.Fatalf("loadBudgetsFromFile() error = %v", err)
		}
		// The README shows this file and the demo output that depends on it.
		want := []domain.Budget{
			{Category: "еда", Limit: 5000, Period: domain.PeriodMonth},
			{Category: "развлечения", Limit: 30000, Period: domain.PeriodYear},
			{Category: "ремонт", Limit: 100000},
			{Category: "транспорт", Limit: 2500, Period: domain.PeriodMonth},
		}
		if got := mustListBudgets(t, svc); !slices.Equal(got, want) {
			t.Errorf("budgets = %+v, want %+v", got, want)
		}
	})

	t.Run("broken JSON", func(t *testing.T) {
		svc := ledger.NewMemory()
		path := filepath.Join(t.TempDir(), "budgets.json")
		if err := os.WriteFile(path, []byte(`[{"category": "еда", "limit": 1`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := loadBudgetsFromFile(svc, path); err == nil {
			t.Fatal("loadBudgetsFromFile() error = nil, want a decode error")
		}
	})

	t.Run("null instead of an array", func(t *testing.T) {
		svc := ledger.NewMemory()
		path := filepath.Join(t.TempDir(), "budgets.json")
		if err := os.WriteFile(path, []byte("null"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := loadBudgetsFromFile(svc, path); err == nil {
			t.Fatal("loadBudgetsFromFile() error = nil, want an error for null")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		svc := ledger.NewMemory()
		err := loadBudgetsFromFile(svc, filepath.Join(t.TempDir(), "missing.json"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("loadBudgetsFromFile() error = %v, want %v", err, fs.ErrNotExist)
		}
	})
}

func TestLoadBudgetsIfExists(t *testing.T) {
	// Every case starts with this budget; it must survive a missing file.
	initial := domain.Budget{Category: "еда", Limit: 100}

	tests := []struct {
		name       string
		content    string // written to the file; "" means no file at all
		wantLoaded bool
		wantErr    bool
		want       []domain.Budget // budgets after the call
	}{
		{
			name:       "file exists",
			content:    `[{"category": "еда", "limit": 5000, "period": "month"}]`,
			wantLoaded: true,
			want:       []domain.Budget{{Category: "еда", Limit: 5000, Period: domain.PeriodMonth}},
		},
		{name: "file is missing", want: []domain.Budget{initial}},
		{name: "file is broken", content: `[{"category": "еда"`, wantErr: true, want: []domain.Budget{initial}},
		{name: "file is empty", content: "", wantErr: true, want: []domain.Budget{initial}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := ledger.NewMemory()
			if _, err := svc.SetBudget(context.Background(), initial); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}
			path := filepath.Join(t.TempDir(), "budgets.json")
			if tt.name != "file is missing" {
				if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			loaded, err := loadBudgetsIfExists(svc, path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadBudgetsIfExists() error = %v, wantErr %v", err, tt.wantErr)
			}
			if loaded != tt.wantLoaded {
				t.Errorf("loadBudgetsIfExists() loaded = %v, want %v", loaded, tt.wantLoaded)
			}
			if got := mustListBudgets(t, svc); !slices.Equal(got, tt.want) {
				t.Errorf("budgets = %+v, want %+v", got, tt.want)
			}
		})
	}

	t.Run("directory instead of a file", func(t *testing.T) {
		svc := ledger.NewMemory()
		loaded, err := loadBudgetsIfExists(svc, t.TempDir())
		if err == nil || loaded {
			t.Fatalf("loadBudgetsIfExists() = %v, %v; want false and an error", loaded, err)
		}
	})
}

func TestPrintReport(t *testing.T) {
	var out strings.Builder
	printReport(&out, domain.ReportSummary{
		From:       time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local),
		To:         time.Date(2026, time.November, 30, 0, 0, 0, 0, time.Local),
		Categories: []domain.CategoryTotal{{Category: "еда", Amount: 1250.5}, {Category: "транспорт", Amount: 2000}},
		Total:      3250.5,
	})

	want := "Report from 2026-11-01 to 2026-11-30:\n" +
		"CATEGORY   SPENT\n" +
		"еда        1250.50\n" +
		"транспорт  2000.00\n" +
		"total      3250.50\n"
	if got := out.String(); got != want {
		t.Errorf("printReport() wrote:\n%s\nwant:\n%s", got, want)
	}
}

// mustListBudgets returns svc.ListBudgets(context.Background()) and stops the test if it
// fails.
func mustListBudgets(t *testing.T, svc service.Service) []domain.Budget {
	t.Helper()
	list, err := svc.ListBudgets(context.Background())
	if err != nil {
		t.Fatalf("ListBudgets() error = %v", err)
	}
	return list
}
