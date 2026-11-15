package ledger

import (
	"bytes"
	"context"
	"database/sql"
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

	"github.com/AsyncAssassin/go-homework/ledger/domain"
	"github.com/AsyncAssassin/go-homework/ledger/internal/cache"
	"github.com/AsyncAssassin/go-homework/ledger/internal/db"
	"github.com/AsyncAssassin/go-homework/ledger/internal/repository/cached"
	"github.com/AsyncAssassin/go-homework/ledger/internal/repository/pg"
	"github.com/AsyncAssassin/go-homework/ledger/service"
	"github.com/redis/go-redis/v9"
)

// The tests in this file need PostgreSQL and Redis. They run only when
// LEDGER_TEST_DATABASE_URL and LEDGER_TEST_REDIS_ADDR are set, see
// ledger/README.md. They empty the tables of the test database and use
// Redis database testRedisDB. They share these external test resources, so
// they do not run in parallel.

// testRedisDB is the Redis database of the tests.
const testRedisDB = 15
const budgetsKey = "budgets:all"

type testStore struct {
	service.Service
	db    *sql.DB
	cache *cache.Cache
}

func reportKey(from, to time.Time) string {
	return "report:summary:" + formatDate(from) + ":" + formatDate(to)
}
func formatDate(t time.Time) string { return t.In(time.Local).Format(time.DateOnly) }

// expireSoon verifies the production TTL before shortening it in Redis.
func expireSoon(t *testing.T, key string) {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: os.Getenv("LEDGER_TEST_REDIS_ADDR"), DB: testRedisDB, Password: os.Getenv("LEDGER_TEST_REDIS_PASSWORD")})
	defer c.Close()
	ttl, err := c.PTTL(context.Background(), key).Result()
	if err != nil || ttl <= 0 || ttl > 30*time.Second {
		t.Fatalf("PTTL(%q) = %v, %v", key, ttl, err)
	}
	if err := c.PExpire(context.Background(), key, 500*time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
}

// connectTest connects the ledger to the test database and Redis, empties
// the tables, removes the cached list of budgets and returns the storage.
// Tests that read reports remove their cached reports with deleteKeys.
// Cleanup closes all connections when the test ends.
func connectTest(t *testing.T) *testStore {
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

	pool, err := db.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	cfg, err := cache.ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	c, err := cache.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	repo := pg.New(pool)
	cachedRepo := cached.New(repo, repo, c)
	s := &testStore{Service: service.New(cachedRepo, cachedRepo, repo, cachedRepo), db: pool, cache: c}
	exec(t, s, `TRUNCATE budgets, expenses RESTART IDENTITY`)
	deleteKeys(t, budgetsKey)
	return s
}

// exec runs a statement in the test database, past the service.
func exec(t *testing.T, s *testStore, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// countExpenses returns the number of rows in the expenses table.
func countExpenses(t *testing.T, s *testStore) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM expenses`).Scan(&n); err != nil {
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
	svc := connectTest(t).Service

	for _, b := range []domain.Budget{
		{Category: "яблоки", Limit: 300},
		{Category: "ёлка", Limit: 2500, Period: domain.PeriodYear},
		{Category: " Еда ", Limit: 4000, Period: domain.PeriodMonth},
		{Category: "еда", Limit: 5000.004, Period: " Month "}, // replaces the budget above
		{Category: "абв", Limit: 0.01},
	} {
		if _, err := svc.SetBudget(context.Background(), b); err != nil {
			t.Fatalf("SetBudget(%+v) error = %v", b, err)
		}
	}

	// Sorted by bytes as in memory: "ё" comes after "я".
	want := []domain.Budget{
		{Category: "абв", Limit: 0.01},
		{Category: "еда", Limit: 5000, Period: domain.PeriodMonth},
		{Category: "яблоки", Limit: 300},
		{Category: "ёлка", Limit: 2500, Period: domain.PeriodYear},
	}
	if got := mustListBudgets(t, svc); !slices.Equal(got, want) {
		t.Errorf("ListBudgets() = %+v, want %+v", got, want)
	}
}

func TestPostgresAddTransaction(t *testing.T) {
	s := connectTest(t)
	svc := s.Service
	if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 5000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	day := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local)

	first, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 4500.004, Category: " Еда", Description: " продукты ", Date: day.Add(15 * time.Hour)})
	if err != nil {
		t.Fatalf("AddTransaction() within the limit error = %v", err)
	}
	if want := (domain.Transaction{ID: 1, Amount: 4500, Category: "еда", Description: "продукты", Date: day}); first != want {
		t.Errorf("AddTransaction() = %+v, want %+v", first, want)
	}

	// 4500 + 600 is over the limit of 5000: an error and no new row.
	_, err = svc.AddTransaction(context.Background(), domain.Transaction{Amount: 600, Category: "еда", Date: day})
	if !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Errorf("AddTransaction() over the limit error = %v, want %v", err, domain.ErrBudgetExceeded)
	}
	if got := countExpenses(t, s); got != 1 {
		t.Errorf("expenses has %d rows after the rejected transaction, want 1", got)
	}

	// The rest of the limit still fits, and the rejected one took no ID.
	second, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 500, Category: "еда", Date: day})
	if err != nil {
		t.Fatalf("AddTransaction() up to the limit error = %v", err)
	}
	third, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 99999, Category: "ремонт", Date: day.AddDate(0, 0, 1)})
	if err != nil {
		t.Fatalf("AddTransaction() without a budget error = %v", err)
	}
	// A row written by another program may have no description.
	exec(t, s, `INSERT INTO expenses (amount, category, description, date) VALUES (1, 'кофе', NULL, '2025-09-09')`)

	want := []domain.Transaction{
		third,
		second,
		first,
		{ID: 4, Amount: 1, Category: "кофе", Date: day.AddDate(0, 0, -1)},
	}
	if second.ID != 2 || third.ID != 3 {
		t.Errorf("IDs = %d, %d; want 2, 3", second.ID, third.ID)
	}
	if got := mustListTransactions(t, svc); !slices.Equal(got, want) {
		t.Errorf("ListTransactions() = %+v, want %+v", got, want)
	}
}

func TestPostgresBudgetPeriods(t *testing.T) {
	svc := connectTest(t).Service
	for _, b := range []domain.Budget{
		{Category: "еда", Limit: 1000, Period: domain.PeriodMonth},
		{Category: "отпуск", Limit: 1000, Period: domain.PeriodYear},
		{Category: "ремонт", Limit: 1000},
	} {
		if _, err := svc.SetBudget(context.Background(), b); err != nil {
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
		_, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: tt.amount, Category: tt.category, Date: tt.date})
		if tt.wantErr != errors.Is(err, domain.ErrBudgetExceeded) || (!tt.wantErr && err != nil) {
			t.Errorf("AddTransaction(%v %s on %s) error = %v, want budget exceeded: %t",
				tt.amount, tt.category, formatDate(tt.date), err, tt.wantErr)
		}
	}

	usage, err := svc.ListBudgetUsage(context.Background(), date(2026, time.November, 15))
	if err != nil {
		t.Fatalf("ListBudgetUsage() error = %v", err)
	}
	want := []domain.BudgetUsage{
		{Budget: domain.Budget{Category: "еда", Limit: 1000, Period: domain.PeriodMonth}, PeriodLabel: "2026-11", Spent: 1000},
		{Budget: domain.Budget{Category: "отпуск", Limit: 1000, Period: domain.PeriodYear}, PeriodLabel: "2026", Spent: 900},
		{Budget: domain.Budget{Category: "ремонт", Limit: 1000}, Spent: 900},
	}
	if !slices.Equal(usage, want) {
		t.Errorf("ListBudgetUsage() = %+v, want %+v", usage, want)
	}
}

// TestPostgresAddTransactionConcurrent sends more transactions at once than
// the budget holds: the row lock must let exactly as many in as fit.
func TestPostgresAddTransactionConcurrent(t *testing.T) {
	s := connectTest(t)
	svc := s.Service
	if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 100, Period: domain.PeriodMonth}); err != nil {
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
			_, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 10, Category: "еда", Date: date})
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
		case !errors.Is(err, domain.ErrBudgetExceeded):
			t.Errorf("AddTransaction() error = %v, want nil or %v", err, domain.ErrBudgetExceeded)
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
	svc := connectTest(t).Service
	budgets := []domain.Budget{
		{Category: "аптека", Limit: 1000},
		{Category: "еда", Limit: 5000, Period: domain.PeriodMonth},
		{Category: "кафе", Limit: 3000, Period: domain.PeriodMonth},
		{Category: "кино", Limit: 800},
		{Category: "отпуск", Limit: 90000, Period: domain.PeriodYear},
		{Category: "ремонт", Limit: 100000},
		{Category: "транспорт", Limit: 2000, Period: domain.PeriodMonth},
		{Category: "ёлка", Limit: 2500, Period: domain.PeriodYear},
	}
	reversed := slices.Clone(budgets)
	slices.Reverse(reversed)
	var inputs [2][]byte
	for i, list := range [][]domain.Budget{budgets, reversed} {
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
				errs <- svc.LoadBudgets(context.Background(), bytes.NewReader(inputs[(w+i)%2]))
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
	if got := mustListBudgets(t, svc); !slices.Equal(got, budgets) {
		t.Errorf("ListBudgets() = %+v, want %+v", got, budgets)
	}
}

// TestPostgresDataSurvivesReconnect checks that the data stays in the
// database when the ledger disconnects, as when the gateway restarts.
func TestPostgresDataSurvivesReconnect(t *testing.T) {
	svc := connectTest(t).Service
	b, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 5000})
	if err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	tx, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 450, Category: "еда", Description: "ланч", Date: time.Now()})
	if err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	first, closeFn, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := mustListTransactions(t, first); len(got) != 1 {
		t.Fatalf("factory service read %v", got)
	}
	if err := closeFn(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ListTransactions(context.Background()); err == nil {
		t.Fatal("closed service still reads the database")
	}
	if err := closeFn(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	svc, closeAgain, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeAgain(); err != nil {
			t.Error(err)
		}
	})

	if got, want := mustListTransactions(t, svc), []domain.Transaction{tx}; !slices.Equal(got, want) {
		t.Errorf("ListTransactions() after reconnecting = %+v, want %+v", got, want)
	}
	if got, want := mustListBudgets(t, svc), []domain.Budget{b}; !slices.Equal(got, want) {
		t.Errorf("ListBudgets() after reconnecting = %+v, want %+v", got, want)
	}
}

// TestReportCache checks that a repeated report comes from Redis until its
// TTL ends: an expense added in between appears only after that.
func TestReportCache(t *testing.T) {
	svc := connectTest(t).Service
	logs := captureLog(t)

	from := time.Date(2025, time.September, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2025, time.September, 30, 0, 0, 0, 0, time.Local)
	deleteKeys(t, reportKey(from, to))
	add := func(amount float64, category string) {
		t.Helper()
		if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: amount, Category: category, Date: from.AddDate(0, 0, 9)}); err != nil {
			t.Fatalf("AddTransaction() error = %v", err)
		}
	}
	report := func() domain.ReportSummary {
		t.Helper()
		// Only the days of from and to matter.
		r, err := svc.GetReportSummary(context.Background(), from, to.Add(23*time.Hour))
		if err != nil {
			t.Fatalf("GetReportSummary() error = %v", err)
		}
		return r
	}
	add(450, "еда")
	add(0.1, "транспорт")
	add(0.2, "транспорт")

	first := report()
	want := domain.ReportSummary{From: from, To: to, Categories: []domain.CategoryTotal{{Category: "еда", Amount: 450}, {Category: "транспорт", Amount: 0.3}}, Total: 450.3}
	if !reportsEqual(first, want) {
		t.Errorf("first report = %+v, want %+v", first, want)
	}

	add(100, "еда")
	if second := report(); !reportsEqual(second, want) {
		t.Errorf("second report = %+v, want the cached %+v", second, want)
	}

	expireSoon(t, reportKey(from, to))
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
// changes past the service are seen only without the cache.
func TestBudgetsCache(t *testing.T) {
	s := connectTest(t)
	svc := s.Service

	if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 5000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	cached := []domain.Budget{{Category: "еда", Limit: 5000}}
	if got := mustListBudgets(t, svc); !slices.Equal(got, cached) {
		t.Fatalf("ListBudgets() = %+v, want %+v", got, cached)
	}

	exec(t, s, `UPDATE budgets SET limit_amount = 7000 WHERE category = 'еда'`)
	if got := mustListBudgets(t, svc); !slices.Equal(got, cached) {
		t.Errorf("ListBudgets() = %+v, want the cached %+v", got, cached)
	}
	expireSoon(t, budgetsKey)
	want := []domain.Budget{{Category: "еда", Limit: 7000}}
	if got := mustListBudgets(t, svc); !slices.Equal(got, want) {
		t.Errorf("ListBudgets() after the TTL = %+v, want %+v", got, want)
	}

	exec(t, s, `UPDATE budgets SET limit_amount = 8000 WHERE category = 'еда'`)
	if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "кафе", Limit: 3000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	want = []domain.Budget{{Category: "еда", Limit: 8000}, {Category: "кафе", Limit: 3000}}
	if got := mustListBudgets(t, svc); !slices.Equal(got, want) {
		t.Errorf("ListBudgets() after SetBudget() = %+v, want %+v", got, want)
	}

	exec(t, s, `UPDATE budgets SET limit_amount = 9000 WHERE category = 'еда'`)
	if err := svc.LoadBudgets(context.Background(), strings.NewReader(`[{"category": "транспорт", "limit": 2000}]`)); err != nil {
		t.Fatalf("LoadBudgets() error = %v", err)
	}
	want = []domain.Budget{{Category: "еда", Limit: 9000}, {Category: "кафе", Limit: 3000}, {Category: "транспорт", Limit: 2000}}
	if got := mustListBudgets(t, svc); !slices.Equal(got, want) {
		t.Errorf("ListBudgets() after LoadBudgets() = %+v, want %+v", got, want)
	}
}

// TestPostgresReportSummary checks the query of the report: the first and
// the last day of the range count, the days around it do not, and the
// categories are sorted by bytes, as in memory.
func TestPostgresReportSummary(t *testing.T) {
	from, to := domain.Day(2025, time.September, 1), domain.Day(2025, time.September, 30)
	expenses := []domain.Transaction{
		{Amount: 50, Category: "кафе", Date: domain.Day(2025, time.August, 31)},
		{Amount: 30, Category: "ёлка", Date: from},
		{Amount: 450, Category: "еда", Date: domain.Day(2025, time.September, 15)},
		{Amount: 120, Category: "яблоки", Date: to},
		{Amount: 0.1, Category: "еда", Date: to},
		{Amount: 0.2, Category: "еда", Date: to},
		{Amount: 999, Category: "кафе", Date: domain.Day(2025, time.October, 1)},
	}
	// "ё" comes after "я" in bytes, unlike in the collation of most locales.
	want := domain.ReportSummary{
		From: from,
		To:   to,
		Categories: []domain.CategoryTotal{
			{Category: "еда", Amount: 450.3},
			{Category: "яблоки", Amount: 120},
			{Category: "ёлка", Amount: 30},
		},
		Total: 600.3,
	}
	check := func(t *testing.T, svc service.Service) {
		for _, tx := range expenses {
			if _, err := svc.AddTransaction(context.Background(), tx); err != nil {
				t.Fatalf("AddTransaction(%+v) error = %v", tx, err)
			}
		}
		got, err := svc.GetReportSummary(context.Background(), from, to)
		if err != nil {
			t.Fatalf("GetReportSummary() error = %v", err)
		}
		if !reportsEqual(got, want) {
			t.Errorf("GetReportSummary() = %+v, want %+v", got, want)
		}
	}

	t.Run("memory", func(t *testing.T) {
		svc := NewMemory()

		check(t, svc)
	})
	t.Run("postgres", func(t *testing.T) {
		svc := connectTest(t).Service
		deleteKeys(t, reportKey(from, to))
		check(t, svc)
	})
}

// TestPostgresEmptyResults checks that no budgets and a report without
// expenses are empty lists, not nil, as in memory: both from PostgreSQL and
// then from the cache.
func TestPostgresEmptyResults(t *testing.T) {
	svc := connectTest(t).Service
	day := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local)
	deleteKeys(t, reportKey(day, day))

	for _, source := range []string{"PostgreSQL", "Redis"} {
		if list := mustListBudgets(t, svc); list == nil || len(list) != 0 {
			t.Errorf("ListBudgets() from %s = %#v, want an empty list", source, list)
		}
		report, err := svc.GetReportSummary(context.Background(), day, day)
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
	svc := s.Service
	day := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local)
	deleteKeys(t, reportKey(day, day))
	if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 5000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 450, Category: "еда", Date: day}); err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}
	logs := captureLog(t)
	if err := s.cache.Close(); err != nil {
		t.Fatalf("close Redis: %v", err)
	}

	if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "кафе", Limit: 3000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	wantBudgets := []domain.Budget{{Category: "еда", Limit: 5000}, {Category: "кафе", Limit: 3000}}
	if got := mustListBudgets(t, svc); !slices.Equal(got, wantBudgets) {
		t.Errorf("ListBudgets() = %+v, want %+v", got, wantBudgets)
	}
	report, err := svc.GetReportSummary(context.Background(), day, day)
	if err != nil {
		t.Fatalf("GetReportSummary() error = %v", err)
	}
	wantReport := domain.ReportSummary{From: day, To: day, Categories: []domain.CategoryTotal{{Category: "еда", Amount: 450}}, Total: 450}
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
	svc := connectTest(t).Service
	day := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local)
	deleteKeys(t, reportKey(day, day))
	if _, err := svc.SetBudget(context.Background(), domain.Budget{Category: "еда", Limit: 5000}); err != nil {
		t.Fatalf("SetBudget() error = %v", err)
	}
	if _, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 450, Category: "еда", Date: day}); err != nil {
		t.Fatalf("AddTransaction() error = %v", err)
	}

	proxy := startProxy(t, os.Getenv("LEDGER_TEST_REDIS_ADDR"))
	t.Setenv("REDIS_ADDR", proxy.addr())
	svc, closeFn, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeFn() })
	mustListBudgets(t, svc) // leaves a connection to Redis in the pool
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
		report, err := svc.GetReportSummary(context.Background(), day, day)
		want := domain.ReportSummary{From: day, To: day, Categories: []domain.CategoryTotal{{Category: "еда", Amount: 450}}, Total: 450}
		if err == nil && !reportsEqual(report, want) {
			t.Errorf("GetReportSummary() = %+v, want %+v", report, want)
		}
		return err
	})
	check("SetBudget()", func() error {
		_, err := svc.SetBudget(context.Background(), domain.Budget{Category: "кафе", Limit: 3000})
		return err
	})
	check("ListBudgets()", func() error {
		list, err := svc.ListBudgets(context.Background())
		want := []domain.Budget{{Category: "еда", Limit: 5000}, {Category: "кафе", Limit: 3000}}
		if err == nil && !slices.Equal(list, want) {
			t.Errorf("ListBudgets() = %+v, want %+v", list, want)
		}
		return err
	})
	check("AddTransaction()", func() error {
		_, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 100, Category: "еда", Date: day})
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

	created, closeFn, err := New(context.Background())
	if err == nil || !strings.Contains(err.Error(), `invalid REDIS_DB "first"`) {
		t.Errorf("New() error = %v, want invalid REDIS_DB", err)
	}
	if created != nil || closeFn != nil {
		t.Fatal("failed factory returned resources")
	}
}

// TestConnectWithoutTables connects to a database where the tables of the
// ledger are not found, as in a database without the migrations: the
// search path is a schema that does not exist.
func TestConnectWithoutTables(t *testing.T) {
	svc := connectTest(t).Service
	before := svc
	t.Setenv("DATABASE_URL", withSearchPath(t, os.Getenv("LEDGER_TEST_DATABASE_URL"), "ledger_no_tables"))

	created, closeFn, err := New(context.Background())
	if err == nil || !strings.Contains(err.Error(), " has no table budgets: apply the migrations with goose up") {
		t.Errorf("New() error = %v, want the advice to apply the migrations", err)
	}
	if created != nil || closeFn != nil {
		t.Fatal("failed factory returned resources")
	}
	if _, err := before.ListTransactions(context.Background()); err != nil {
		t.Fatalf("existing service changed after factory error: %v", err)
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
	t.Setenv("DATABASE_URL", "postgres://ledger:secret-password@127.0.0.1:1/cashapp?sslmode=disable&connect_timeout=2")

	created, closeFn, err := New(context.Background())
	if err == nil {
		t.Fatal("New() succeeded, want an error")
	}
	if msg := err.Error(); !strings.HasPrefix(msg, "connect to PostgreSQL at 127.0.0.1:1/cashapp: ") || strings.Contains(msg, "secret-password") {
		t.Errorf("New() error = %q, want the server without the password", msg)
	}
	// A failed factory must not expose partially initialized resources.
	if created != nil || closeFn != nil {
		t.Fatal("failed factory returned resources")
	}
}

// reportsEqual reports whether two report summaries are equal.
func reportsEqual(a, b domain.ReportSummary) bool {
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

// A real lock forces QueryContext to wait past the deadline without adding
// artificial delays or test switches to the service or SQL repository.
func TestPostgresReportCancellation(t *testing.T) {
	s := connectTest(t)
	day := domain.Day(2026, 11, 15)
	key := reportKey(day, day)
	deleteKeys(t, key)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(context.Background(), "LOCK TABLE expenses IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = s.GetReportSummary(ctx, day, day)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancellation took %v", elapsed)
	}
	if inUse := s.db.Stats().InUse; inUse != 1 {
		t.Fatalf("%d connections in use; only the locking transaction should remain", inUse)
	}
	var totals []domain.CategoryTotal
	if found, err := s.cache.Get(context.Background(), key, &totals); err != nil || found {
		t.Fatalf("failed report cached: found=%v err=%v", found, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	report, err := s.GetReportSummary(context.Background(), day, day)
	if err != nil || report.Total != 0 || len(report.Categories) != 0 {
		t.Fatalf("report after cancellation = %+v, %v", report, err)
	}
}
