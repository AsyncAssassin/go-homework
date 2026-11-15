package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/internal/cache"
	"github.com/AsyncAssassin/go-homework/ledger/internal/db"
)

// The tests in this file need PostgreSQL and Redis. They run only when
// LEDGER_TEST_DATABASE_URL and LEDGER_TEST_REDIS_ADDR are set, see
// ledger/README.md. They empty the tables of the test database and use
// Redis database testRedisDB. They change the storage of the package, so
// they do not run in parallel.

// testRedisDB is the Redis database of the tests.
const testRedisDB = 15

// connectTest connects the ledger to the test database and Redis, empties
// the tables, removes the cached list of budgets and returns the storage.
// Tests that read reports remove their cached reports with deleteKeys.
// Reset disconnects the ledger when the test ends.
func connectTest(t *testing.T) *cachedStore {
	t.Helper()
	dsn, addr := os.Getenv("LEDGER_TEST_DATABASE_URL"), os.Getenv("LEDGER_TEST_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("set LEDGER_TEST_DATABASE_URL and LEDGER_TEST_REDIS_ADDR to test with PostgreSQL and Redis")
	}
	target, err := db.Target(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(target, "_test") {
		t.Fatalf("LEDGER_TEST_DATABASE_URL points to %s: the tests delete all its data, so the name of the database must end with _test", target)
	}
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("REDIS_ADDR", addr)
	t.Setenv("REDIS_DB", strconv.Itoa(testRedisDB))
	t.Setenv("REDIS_PASSWORD", os.Getenv("LEDGER_TEST_REDIS_PASSWORD"))

	if err := Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(Reset)
	s := store.(*cachedStore)
	exec(t, s, `TRUNCATE budgets, expenses RESTART IDENTITY`)
	deleteKeys(t, budgetsKey)
	return s
}

// exec runs a statement in the test database, past the package functions.
func exec(t *testing.T, s *cachedStore, query string, args ...any) {
	t.Helper()
	if _, err := s.storage.(*postgresStore).db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// countExpenses returns the number of rows in the expenses table.
func countExpenses(t *testing.T, s *cachedStore) int {
	t.Helper()
	var n int
	if err := s.storage.(*postgresStore).db.QueryRow(`SELECT count(*) FROM expenses`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// deleteKeys removes the keys from the test database of Redis now and when
// the test ends, so no cached value passes from one test to another or
// stays in Redis. It has its own connection, which works even after a test
// has closed the cache of the ledger.
func deleteKeys(t *testing.T, keys ...string) {
	t.Helper()
	cfg := cache.Config{
		Addr:     os.Getenv("LEDGER_TEST_REDIS_ADDR"),
		DB:       testRedisDB,
		Password: os.Getenv("LEDGER_TEST_REDIS_PASSWORD"),
	}
	c, err := cache.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	remove := func() error {
		return c.Delete(context.Background(), keys...)
	}
	if err := remove(); err != nil {
		c.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := remove(); err != nil {
			t.Error(err)
		}
		c.Close()
	})
}

func TestPostgresBudgets(t *testing.T) {
	connectTest(t)

	for _, b := range []Budget{
		{Category: "яблоки", Limit: 300},
		{Category: "ёлка", Limit: 2500, Period: PeriodYear},
		{Category: " Еда ", Limit: 4000, Period: PeriodMonth},
		{Category: "еда", Limit: 5000.004, Period: " Month "}, // replaces the budget above
		{Category: "абв", Limit: 0.01},
	} {
		if _, err := SetBudget(b); err != nil {
			t.Fatalf("SetBudget(%+v) error = %v", b, err)
		}
	}

	// Sorted by bytes as in memory: "ё" comes after "я".
	want := []Budget{
		{Category: "абв", Limit: 0.01},
		{Category: "еда", Limit: 5000, Period: PeriodMonth},
		{Category: "яблоки", Limit: 300},
		{Category: "ёлка", Limit: 2500, Period: PeriodYear},
	}
	if got := mustListBudgets(t); !slices.Equal(got, want) {
		t.Errorf("ListBudgets() = %+v, want %+v", got, want)
	}
}

func TestPostgresAddTransaction(t *testing.T) {
	s := connectTest(t)
	if _, err := SetBudget(Budget{Category: "еда", Limit: 5000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	day := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local)

	first, err := AddTransaction(Transaction{Amount: 4500.004, Category: " Еда", Description: " продукты ", Date: day.Add(15 * time.Hour)})
	if err != nil {
		t.Fatalf("AddTransaction() within the limit error = %v", err)
	}
	if want := (Transaction{ID: 1, Amount: 4500, Category: "еда", Description: "продукты", Date: day}); first != want {
		t.Errorf("AddTransaction() = %+v, want %+v", first, want)
	}

	// 4500 + 600 is over the limit of 5000: an error and no new row.
	_, err = AddTransaction(Transaction{Amount: 600, Category: "еда", Date: day})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("AddTransaction() over the limit error = %v, want %v", err, ErrBudgetExceeded)
	}
	if got := countExpenses(t, s); got != 1 {
		t.Errorf("expenses has %d rows after the rejected transaction, want 1", got)
	}

	// The rest of the limit still fits, and the rejected one took no ID.
	second, err := AddTransaction(Transaction{Amount: 500, Category: "еда", Date: day})
	if err != nil {
		t.Fatalf("AddTransaction() up to the limit error = %v", err)
	}
	third, err := AddTransaction(Transaction{Amount: 99999, Category: "ремонт", Date: day.AddDate(0, 0, 1)})
	if err != nil {
		t.Fatalf("AddTransaction() without a budget error = %v", err)
	}
	// A row written by another program may have no description.
	exec(t, s, `INSERT INTO expenses (amount, category, description, date) VALUES (1, 'кофе', NULL, '2025-09-09')`)

	want := []Transaction{
		third,
		second,
		first,
		{ID: 4, Amount: 1, Category: "кофе", Date: day.AddDate(0, 0, -1)},
	}
	if second.ID != 2 || third.ID != 3 {
		t.Errorf("IDs = %d, %d; want 2, 3", second.ID, third.ID)
	}
	if got := mustListTransactions(t); !slices.Equal(got, want) {
		t.Errorf("ListTransactions() = %+v, want %+v", got, want)
	}
}

func TestPostgresBudgetPeriods(t *testing.T) {
	connectTest(t)
	for _, b := range []Budget{
		{Category: "еда", Limit: 1000, Period: PeriodMonth},
		{Category: "отпуск", Limit: 1000, Period: PeriodYear},
		{Category: "ремонт", Limit: 1000},
	} {
		if _, err := SetBudget(b); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
	}
	date := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}

	tests := []struct {
		category string
		amount   float64
		date     time.Time
		wantErr  bool
	}{
		{category: "еда", amount: 900, date: date(2026, time.October, 31)},
		{category: "еда", amount: 900, date: date(2026, time.November, 1)},                 // a new month
		{category: "еда", amount: 200, date: date(2026, time.November, 30), wantErr: true}, // 900 + 200 in November
		{category: "еда", amount: 100, date: date(2026, time.November, 30)},                // exactly 1000
		{category: "отпуск", amount: 900, date: date(2025, time.December, 31)},
		{category: "отпуск", amount: 900, date: date(2026, time.January, 1)}, // a new year
		{category: "отпуск", amount: 200, date: date(2026, time.December, 31), wantErr: true},
		{category: "ремонт", amount: 900, date: date(2020, time.January, 1)},
		{category: "ремонт", amount: 200, date: date(2030, time.January, 1), wantErr: true}, // no period
	}
	for _, tt := range tests {
		_, err := AddTransaction(Transaction{Amount: tt.amount, Category: tt.category, Date: tt.date})
		if tt.wantErr != errors.Is(err, ErrBudgetExceeded) || (!tt.wantErr && err != nil) {
			t.Errorf("AddTransaction(%v %s on %s) error = %v, want budget exceeded: %t",
				tt.amount, tt.category, formatDate(tt.date), err, tt.wantErr)
		}
	}

	usage, err := ListBudgetUsage(date(2026, time.November, 15))
	if err != nil {
		t.Fatalf("ListBudgetUsage() error = %v", err)
	}
	want := []BudgetUsage{
		{Budget: Budget{Category: "еда", Limit: 1000, Period: PeriodMonth}, PeriodLabel: "2026-11", Spent: 1000},
		{Budget: Budget{Category: "отпуск", Limit: 1000, Period: PeriodYear}, PeriodLabel: "2026", Spent: 900},
		{Budget: Budget{Category: "ремонт", Limit: 1000}, Spent: 900},
	}
	if !slices.Equal(usage, want) {
		t.Errorf("ListBudgetUsage() = %+v, want %+v", usage, want)
	}
}

// TestPostgresAddTransactionConcurrent sends more transactions at once than
// the budget holds: the row lock must let exactly as many in as fit.
func TestPostgresAddTransactionConcurrent(t *testing.T) {
	s := connectTest(t)
	if _, err := SetBudget(Budget{Category: "еда", Limit: 100, Period: PeriodMonth}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}

	const workers, fit = 30, 10
	date := time.Date(2026, time.October, 12, 0, 0, 0, 0, time.Local)
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := AddTransaction(Transaction{Amount: 10, Category: "еда", Date: date})
			errs <- err
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
	if got := countExpenses(t, s); got != fit {
		t.Errorf("expenses has %d rows, want %d", got, fit)
	}
}

// TestPostgresLoadBudgetsConcurrent loads the same budgets in opposite
// orders at once. The rows must be locked in one order, so no call fails
// with a deadlock.
func TestPostgresLoadBudgetsConcurrent(t *testing.T) {
	connectTest(t)
	budgets := []Budget{
		{Category: "аптека", Limit: 1000},
		{Category: "еда", Limit: 5000, Period: PeriodMonth},
		{Category: "кафе", Limit: 3000, Period: PeriodMonth},
		{Category: "кино", Limit: 800},
		{Category: "отпуск", Limit: 90000, Period: PeriodYear},
		{Category: "ремонт", Limit: 100000},
		{Category: "транспорт", Limit: 2000, Period: PeriodMonth},
		{Category: "ёлка", Limit: 2500, Period: PeriodYear},
	}
	reversed := slices.Clone(budgets)
	slices.Reverse(reversed)
	var inputs [2][]byte
	for i, list := range [][]Budget{budgets, reversed} {
		data, err := json.Marshal(list)
		if err != nil {
			t.Fatal(err)
		}
		inputs[i] = data
	}

	const workers, calls = 4, 6
	errs := make(chan error, workers*calls)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range calls {
				errs <- LoadBudgets(bytes.NewReader(inputs[(w+i)%2]))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("LoadBudgets() error = %v", err)
		}
	}
	if got := mustListBudgets(t); !slices.Equal(got, budgets) {
		t.Errorf("ListBudgets() = %+v, want %+v", got, budgets)
	}
}

// TestPostgresDataSurvivesReconnect checks that the data stays in the
// database when the ledger disconnects, as when the gateway restarts.
func TestPostgresDataSurvivesReconnect(t *testing.T) {
	connectTest(t)
	b, err := SetBudget(Budget{Category: "еда", Limit: 5000})
	if err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	tx, err := AddTransaction(Transaction{Amount: 450, Category: "еда", Description: "ланч", Date: time.Now()})
	if err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	if err := Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := ListTransactions(); !errors.Is(err, errClosed) {
		t.Errorf("ListTransactions() after Close() error = %v, want %v", err, errClosed)
	}
	if err := Close(); err != nil {
		t.Errorf("second Close() error = %v, want nil", err)
	}
	if err := Connect(context.Background()); err != nil {
		t.Fatalf("Connect() again error = %v", err)
	}

	if got, want := mustListTransactions(t), []Transaction{tx}; !slices.Equal(got, want) {
		t.Errorf("ListTransactions() after reconnecting = %+v, want %+v", got, want)
	}
	if got, want := mustListBudgets(t), []Budget{b}; !slices.Equal(got, want) {
		t.Errorf("ListBudgets() after reconnecting = %+v, want %+v", got, want)
	}
}

// TestReportCache checks that a repeated report comes from Redis until its
// TTL ends: an expense added in between appears only after that.
func TestReportCache(t *testing.T) {
	connectTest(t)
	logs := captureLog(t)
	ttl := reportTTL
	reportTTL = 500 * time.Millisecond
	t.Cleanup(func() { reportTTL = ttl })

	from := time.Date(2025, time.September, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2025, time.September, 30, 0, 0, 0, 0, time.Local)
	deleteKeys(t, reportKey(from, to))
	add := func(amount float64, category string) {
		t.Helper()
		if _, err := AddTransaction(Transaction{Amount: amount, Category: category, Date: from.AddDate(0, 0, 9)}); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}
	report := func() ReportSummary {
		t.Helper()
		// Only the days of from and to matter.
		r, err := GetReportSummary(context.Background(), from, to.Add(23*time.Hour))
		if err != nil {
			t.Fatalf("GetReportSummary() error = %v", err)
		}
		return r
	}
	add(450, "еда")
	add(0.1, "транспорт")
	add(0.2, "транспорт")

	first := report()
	want := ReportSummary{From: from, To: to, Categories: []CategoryTotal{{Category: "еда", Amount: 450}, {Category: "транспорт", Amount: 0.3}}, Total: 450.3}
	if !reportsEqual(first, want) {
		t.Errorf("first report = %+v, want %+v", first, want)
	}

	add(100, "еда")
	if second := report(); !reportsEqual(second, want) {
		t.Errorf("second report = %+v, want the cached %+v", second, want)
	}

	time.Sleep(reportTTL + 100*time.Millisecond)
	want.Categories[0].Amount, want.Total = 550, 550.3
	if third := report(); !reportsEqual(third, want) {
		t.Errorf("report after the TTL = %+v, want %+v", third, want)
	}

	key := reportKey(from, to)
	wantLog := []string{"report cache miss: " + key, "report cache hit: " + key, "report cache miss: " + key}
	if got := strings.Split(strings.TrimSuffix(logs.String(), "\n"), "\n"); !slices.Equal(got, wantLog) {
		t.Errorf("log = %q, want %q", got, wantLog)
	}
}

// TestBudgetsCache checks that the list of budgets comes from Redis for
// budgetsTTL or until SetBudget or LoadBudgets changes a budget. The
// changes past the package functions are seen only without the cache.
func TestBudgetsCache(t *testing.T) {
	s := connectTest(t)
	ttl := budgetsTTL
	budgetsTTL = 500 * time.Millisecond
	t.Cleanup(func() { budgetsTTL = ttl })

	if _, err := SetBudget(Budget{Category: "еда", Limit: 5000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	cached := []Budget{{Category: "еда", Limit: 5000}}
	if got := mustListBudgets(t); !slices.Equal(got, cached) {
		t.Fatalf("ListBudgets() = %+v, want %+v", got, cached)
	}

	exec(t, s, `UPDATE budgets SET limit_amount = 7000 WHERE category = 'еда'`)
	if got := mustListBudgets(t); !slices.Equal(got, cached) {
		t.Errorf("ListBudgets() = %+v, want the cached %+v", got, cached)
	}
	time.Sleep(budgetsTTL + 100*time.Millisecond)
	want := []Budget{{Category: "еда", Limit: 7000}}
	if got := mustListBudgets(t); !slices.Equal(got, want) {
		t.Errorf("ListBudgets() after the TTL = %+v, want %+v", got, want)
	}

	exec(t, s, `UPDATE budgets SET limit_amount = 8000 WHERE category = 'еда'`)
	if _, err := SetBudget(Budget{Category: "кафе", Limit: 3000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	want = []Budget{{Category: "еда", Limit: 8000}, {Category: "кафе", Limit: 3000}}
	if got := mustListBudgets(t); !slices.Equal(got, want) {
		t.Errorf("ListBudgets() after SetBudget() = %+v, want %+v", got, want)
	}

	exec(t, s, `UPDATE budgets SET limit_amount = 9000 WHERE category = 'еда'`)
	if err := LoadBudgets(strings.NewReader(`[{"category": "транспорт", "limit": 2000}]`)); err != nil {
		t.Fatalf("LoadBudgets() error = %v", err)
	}
	want = []Budget{{Category: "еда", Limit: 9000}, {Category: "кафе", Limit: 3000}, {Category: "транспорт", Limit: 2000}}
	if got := mustListBudgets(t); !slices.Equal(got, want) {
		t.Errorf("ListBudgets() after LoadBudgets() = %+v, want %+v", got, want)
	}
}

// TestPostgresReportSummary checks the query of the report: the first and
// the last day of the range count, the days around it do not, and the
// categories are sorted by bytes, as in memory.
func TestPostgresReportSummary(t *testing.T) {
	from, to := Day(2025, time.September, 1), Day(2025, time.September, 30)
	expenses := []Transaction{
		{Amount: 50, Category: "кафе", Date: Day(2025, time.August, 31)},
		{Amount: 30, Category: "ёлка", Date: from},
		{Amount: 450, Category: "еда", Date: Day(2025, time.September, 15)},
		{Amount: 120, Category: "яблоки", Date: to},
		{Amount: 0.1, Category: "еда", Date: to},
		{Amount: 0.2, Category: "еда", Date: to},
		{Amount: 999, Category: "кафе", Date: Day(2025, time.October, 1)},
	}
	// "ё" comes after "я" in bytes, unlike in the collation of most locales.
	want := ReportSummary{
		From: from,
		To:   to,
		Categories: []CategoryTotal{
			{Category: "еда", Amount: 450.3},
			{Category: "яблоки", Amount: 120},
			{Category: "ёлка", Amount: 30},
		},
		Total: 600.3,
	}
	check := func(t *testing.T) {
		for _, tx := range expenses {
			if _, err := AddTransaction(tx); err != nil {
				t.Fatalf("AddTransaction(%+v) error = %v", tx, err)
			}
		}
		got, err := GetReportSummary(context.Background(), from, to)
		if err != nil {
			t.Fatalf("GetReportSummary() error = %v", err)
		}
		if !reportsEqual(got, want) {
			t.Errorf("GetReportSummary() = %+v, want %+v", got, want)
		}
	}

	t.Run("memory", func(t *testing.T) {
		Reset()
		t.Cleanup(Reset)
		check(t)
	})
	t.Run("postgres", func(t *testing.T) {
		connectTest(t)
		deleteKeys(t, reportKey(from, to))
		check(t)
	})
}

// TestPostgresEmptyResults checks that no budgets and a report without
// expenses are empty lists, not nil, as in memory: both from PostgreSQL and
// then from the cache.
func TestPostgresEmptyResults(t *testing.T) {
	connectTest(t)
	day := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local)
	deleteKeys(t, reportKey(day, day))

	for _, source := range []string{"PostgreSQL", "Redis"} {
		if list := mustListBudgets(t); list == nil || len(list) != 0 {
			t.Errorf("ListBudgets() from %s = %#v, want an empty list", source, list)
		}
		report, err := GetReportSummary(context.Background(), day, day)
		if err != nil {
			t.Fatalf("GetReportSummary() error = %v", err)
		}
		if report.Categories == nil || len(report.Categories) != 0 {
			t.Errorf("GetReportSummary() from %s has categories %#v, want an empty list", source, report.Categories)
		}
	}
}

// TestRedisFailure checks that the ledger keeps working with PostgreSQL
// alone when Redis fails, and logs the errors of Redis.
func TestRedisFailure(t *testing.T) {
	s := connectTest(t)
	day := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local)
	deleteKeys(t, reportKey(day, day))
	if _, err := SetBudget(Budget{Category: "еда", Limit: 5000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	if _, err := AddTransaction(Transaction{Amount: 450, Category: "еда", Date: day}); err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}
	logs := captureLog(t)
	if err := s.cache.Close(); err != nil {
		t.Fatalf("close Redis: %v", err)
	}

	if _, err := SetBudget(Budget{Category: "кафе", Limit: 3000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	wantBudgets := []Budget{{Category: "еда", Limit: 5000}, {Category: "кафе", Limit: 3000}}
	if got := mustListBudgets(t); !slices.Equal(got, wantBudgets) {
		t.Errorf("ListBudgets() = %+v, want %+v", got, wantBudgets)
	}
	report, err := GetReportSummary(context.Background(), day, day)
	if err != nil {
		t.Fatalf("GetReportSummary() error = %v", err)
	}
	wantReport := ReportSummary{From: day, To: day, Categories: []CategoryTotal{{Category: "еда", Amount: 450}}, Total: 450}
	if !reportsEqual(report, wantReport) {
		t.Errorf("GetReportSummary() = %+v, want %+v", report, wantReport)
	}

	key := reportKey(day, day)
	wantLog := []string{
		// SetBudget removes the cached list of budgets.
		"cache: delete budgets:all from Redis: redis: client is closed",
		// ListBudgets reads the cached list and caches the list from PostgreSQL.
		"cache: read budgets:all from Redis: redis: client is closed",
		"cache: write budgets:all to Redis: redis: client is closed",
		// GetReportSummary does the same with the report.
		"cache: read " + key + " from Redis: redis: client is closed",
		"report cache miss: " + key,
		"cache: write " + key + " to Redis: redis: client is closed",
	}
	if got := strings.Split(strings.TrimSuffix(logs.String(), "\n"), "\n"); !slices.Equal(got, wantLog) {
		t.Errorf("log = %q, want %q", got, wantLog)
	}
}

// TestRedisHangs checks that a hung Redis delays the package functions by
// about a second at most and does not break them: the data comes from
// PostgreSQL. Redis hangs after the ledger has used it, so the first request
// goes over a connection that worked.
func TestRedisHangs(t *testing.T) {
	connectTest(t)
	day := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local)
	deleteKeys(t, reportKey(day, day))
	if _, err := SetBudget(Budget{Category: "еда", Limit: 5000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	if _, err := AddTransaction(Transaction{Amount: 450, Category: "еда", Date: day}); err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	proxy := startProxy(t, os.Getenv("LEDGER_TEST_REDIS_ADDR"))
	t.Setenv("REDIS_ADDR", proxy.addr())
	if err := Connect(context.Background()); err != nil {
		t.Fatalf("Connect() through the proxy error = %v", err)
	}
	mustListBudgets(t) // leaves a connection to Redis in the pool
	proxy.hang()
	logs := captureLog(t)

	// Every call waits for at most two requests to Redis.
	check := func(name string, call func() error) {
		t.Helper()
		start := time.Now()
		if err := call(); err != nil {
			t.Errorf("%s error = %v", name, err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("%s took %v with a hung Redis, want at most 2s", name, elapsed)
		}
	}
	check("GetReportSummary()", func() error {
		report, err := GetReportSummary(context.Background(), day, day)
		want := ReportSummary{From: day, To: day, Categories: []CategoryTotal{{Category: "еда", Amount: 450}}, Total: 450}
		if err == nil && !reportsEqual(report, want) {
			t.Errorf("GetReportSummary() = %+v, want %+v", report, want)
		}
		return err
	})
	check("SetBudget()", func() error {
		_, err := SetBudget(Budget{Category: "кафе", Limit: 3000})
		return err
	})
	check("ListBudgets()", func() error {
		list, err := ListBudgets()
		want := []Budget{{Category: "еда", Limit: 5000}, {Category: "кафе", Limit: 3000}}
		if err == nil && !slices.Equal(list, want) {
			t.Errorf("ListBudgets() = %+v, want %+v", list, want)
		}
		return err
	})
	check("AddTransaction()", func() error {
		_, err := AddTransaction(Transaction{Amount: 100, Category: "еда", Date: day})
		return err
	})

	for _, want := range []string{
		"cache: read " + reportKey(day, day) + " from Redis: ",
		"cache: delete budgets:all from Redis: ",
		"cache: read budgets:all from Redis: ",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log = %q, want a line with %q", logs, want)
		}
	}
}

// hangingProxy passes TCP connections through to a server until hang is
// called. Then it keeps the connections open but passes nothing, like
// a server that hangs.
type hangingProxy struct {
	ln     net.Listener
	target string
	hung   atomic.Bool

	mu     sync.Mutex
	closed bool
	conns  []net.Conn
}

// startProxy starts a hangingProxy to the server at target and stops it
// when the test ends.
func startProxy(t *testing.T, target string) *hangingProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &hangingProxy{ln: ln, target: target}
	go p.accept()
	t.Cleanup(p.close)
	return p
}

func (p *hangingProxy) addr() string {
	return p.ln.Addr().String()
}

// hang makes the proxy drop all data from now on.
func (p *hangingProxy) hang() {
	p.hung.Store(true)
}

func (p *hangingProxy) accept() {
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		server, err := net.Dial("tcp", p.target)
		if err != nil {
			client.Close()
			continue
		}
		if !p.track(client, server) {
			return
		}
		go p.pass(server, client)
		go p.pass(client, server)
	}
}

// track keeps the connections for close. If the proxy is already closed,
// it closes them and reports false.
func (p *hangingProxy) track(conns ...net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		for _, c := range conns {
			c.Close()
		}
		return false
	}
	p.conns = append(p.conns, conns...)
	return true
}

// pass copies data from src to dst, dropping it once the proxy hangs, and
// closes dst when src ends.
func (p *hangingProxy) pass(dst, src net.Conn) {
	defer dst.Close()
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if p.hung.Load() {
			continue
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}

func (p *hangingProxy) close() {
	p.ln.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for _, c := range p.conns {
		c.Close()
	}
}

func TestConnectRejectsInvalidRedisDB(t *testing.T) {
	connectTest(t)
	t.Setenv("REDIS_DB", "first")

	err := Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), `invalid REDIS_DB "first"`) {
		t.Errorf("Connect() error = %v, want invalid REDIS_DB", err)
	}
}

// TestConnectWithoutTables connects to a database where the tables of the
// ledger are not found, as in a database without the migrations: the
// search path is a schema that does not exist.
func TestConnectWithoutTables(t *testing.T) {
	connectTest(t)
	before := store
	t.Setenv("DATABASE_URL", withSearchPath(t, os.Getenv("LEDGER_TEST_DATABASE_URL"), "ledger_no_tables"))

	err := Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), " has no table budgets: apply the migrations with goose up") {
		t.Errorf("Connect() error = %v, want the advice to apply the migrations", err)
	}
	if store != before {
		t.Error("Connect() changed the storage after an error")
	}
}

// withSearchPath returns dsn, a URL or key=value pairs, with the search path
// set to schema.
func withSearchPath(t *testing.T, dsn, schema string) string {
	t.Helper()
	if !strings.Contains(dsn, "://") {
		return dsn + " search_path=" + schema
	}
	u, err := url.Parse(dsn)
	if err != nil {
		// The error would quote the password.
		t.Fatal("LEDGER_TEST_DATABASE_URL is not a valid URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// TestConnectError needs neither PostgreSQL nor Redis: nothing listens on
// port 1.
func TestConnectError(t *testing.T) {
	Reset()
	t.Setenv("DATABASE_URL", "postgres://ledger:secret-password@127.0.0.1:1/cashapp?sslmode=disable&connect_timeout=2")

	err := Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() succeeded, want an error")
	}
	if msg := err.Error(); !strings.HasPrefix(msg, "connect to PostgreSQL at 127.0.0.1:1/cashapp: ") || strings.Contains(msg, "secret-password") {
		t.Errorf("Connect() error = %q, want the server without the password", msg)
	}
	// The ledger still works in memory.
	if _, ok := store.(*memoryStore); !ok {
		t.Errorf("store is %T after a failed Connect(), want *memoryStore", store)
	}
}

// reportsEqual reports whether two report summaries are equal.
func reportsEqual(a, b ReportSummary) bool {
	return a.From.Equal(b.From) && a.To.Equal(b.To) && a.Total == b.Total && slices.Equal(a.Categories, b.Categories)
}

// captureLog sends the standard log to a buffer until the test ends.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	})
	return &buf
}
