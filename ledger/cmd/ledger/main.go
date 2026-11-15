// Command ledger is the console client of the ledger package. By default it
// connects to PostgreSQL and Redis, as the gateway does, and prints the
// budgets, the transactions and the report of the current month stored
// there. With -demo it runs the console demo of homeworks 2-4 in memory and
// does not touch the database.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger"
	"github.com/AsyncAssassin/go-homework/ledger/domain"
	"github.com/AsyncAssassin/go-homework/ledger/service"
)

func main() {
	demo := flag.Bool("demo", false, "run the in-memory demo of homeworks 2-4 instead of reading the database")
	budgetsPath := flag.String("budgets", "budgets.json", "path to the JSON file with budgets for -demo")
	flag.Parse()

	if *demo {
		if err := runDemo(*budgetsPath); err != nil {
			fmt.Fprintln(os.Stderr, "Ledger demo failed:", err)
			os.Exit(1)
		}
		return
	}
	if err := showStoredData(context.Background(), os.Stdout); err != nil {
		log.Fatalf("Ledger service failed: %v", err)
	}
}

// showStoredData connects to PostgreSQL and Redis and writes the budgets,
// the transactions and the report of the current month to w.
func showStoredData(ctx context.Context, w io.Writer) error {
	svc, closeFn, err := ledger.New(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := closeFn(); err != nil {
			log.Printf("close the ledger: %v", err)
		}
	}()
	log.Print("Ledger service started")

	now := time.Now()
	if err := printBudgets(svc, w, now); err != nil {
		return err
	}
	fmt.Fprintln(w)
	if err := printTransactions(svc, w); err != nil {
		return err
	}

	y, m, _ := now.Date()
	from, to := domain.Day(y, m, 1), domain.Day(y, m+1, 0)
	// The first request fills the cache and the second one is answered from
	// it: the log shows "report cache miss" and then "report cache hit".
	var report domain.ReportSummary
	for range 2 {
		var err error
		if report, err = svc.GetReportSummary(ctx, from, to); err != nil {
			return err
		}
	}
	fmt.Fprintln(w)
	printReport(w, report)
	return nil
}

// runDemo runs the console demo of homeworks 2-4 in memory: it sets
// budgets, loads more from the JSON file at budgetsPath if it exists and
// shows how budgets limit new transactions.
func runDemo(budgetsPath string) error {
	svc := ledger.NewMemory()
	fmt.Println("Ledger service started")

	// Initial budgets are set in code, then budgets.json adds and updates some.
	for _, b := range []domain.Budget{
		{Category: "еда", Limit: 5000, Period: domain.PeriodMonth},
		{Category: "транспорт", Limit: 2000, Period: domain.PeriodMonth},
	} {
		if _, err := svc.SetBudget(context.Background(), b); err != nil {
			return fmt.Errorf("set budget: %w", err)
		}
	}
	loaded, err := loadBudgetsIfExists(svc, budgetsPath)
	if err != nil {
		return err
	}
	if loaded {
		fmt.Printf("Budgets loaded from %s\n", budgetsPath)
	} else {
		fmt.Fprintf(os.Stderr, "Budgets file %s not found, using the built-in budgets (pass -budgets <path> to load a file)\n", budgetsPath)
	}
	// One clock reading for the whole demo, so a run at midnight cannot split
	// it between two months.
	now := time.Now()
	lastMonth := now.AddDate(0, 0, -now.Day()) // the last day of the previous month

	fmt.Println()
	if err := printBudgets(svc, os.Stdout, now); err != nil {
		return err
	}

	fmt.Println("\nAdding transactions:")
	for _, tx := range []domain.Transaction{
		{Amount: 1250.50, Category: "еда", Description: "продукты на неделю", Date: now},
		{Amount: 2000, Category: "транспорт", Description: "проездной", Date: now},
		{Amount: 3500, Category: "еда", Description: "кафе", Date: now},
		{Amount: 500, Category: "еда", Description: "ресторан", Date: now},         // over the monthly budget
		{Amount: 249.50, Category: "еда", Description: "хлеб и молоко", Date: now}, // exactly up to the limit
		// The previous month has its own limit, so this one fits.
		{Amount: 500, Category: "еда", Description: "ресторан в прошлом месяце", Date: lastMonth},
		{Amount: 700, Category: "здоровье", Description: "лекарства", Date: now}, // category without a budget
		{Amount: 0, Category: "еда", Description: "пустой чек", Date: now},       // invalid amount
	} {
		addAndReport(svc, tx)
	}

	fmt.Println("\nBudget loading errors:")
	showLoadErrors(svc)

	fmt.Println()
	if err := printTransactions(svc, os.Stdout); err != nil {
		return err
	}
	fmt.Println()
	if err := printBudgets(svc, os.Stdout, now); err != nil {
		return err
	}

	fmt.Println("\nValidation through the Validatable interface:")
	showValidation(os.Stdout)
	return nil
}

// loadBudgetsIfExists loads budgets from the JSON file at path and reports
// whether it did. The file is optional: when it does not exist, the budgets
// set in code stay in effect and no error is returned. A file that exists
// but cannot be read or parsed is an error.
func loadBudgetsIfExists(svc service.Service, path string) (bool, error) {
	err := loadBudgetsFromFile(svc, path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// loadBudgetsFromFile loads budgets from the JSON file at path.
func loadBudgetsFromFile(svc service.Service, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("load budgets: %w", err)
	}
	defer f.Close()

	return svc.LoadBudgets(context.Background(), bufio.NewReader(f))
}

// addAndReport adds tx and prints whether it was accepted.
func addAndReport(svc service.Service, tx domain.Transaction) {
	_, err := svc.AddTransaction(context.Background(), tx)

	var budgetErr *domain.BudgetExceededError
	switch {
	case err == nil:
		fmt.Printf("  added    %q %.2f (%s)\n", tx.Description, tx.Amount, tx.Category)
	case errors.As(err, &budgetErr):
		fmt.Printf("  rejected %q: %v, only %.2f left\n",
			tx.Description, err, remaining(budgetErr.Limit, budgetErr.Spent))
	default:
		fmt.Printf("  rejected %q: %v\n", tx.Description, err)
	}
}

// showLoadErrors prints the errors returned for broken budget sources.
// None of them changes the stored budgets.
func showLoadErrors(svc service.Service) {
	for _, c := range []struct {
		name  string
		input string
	}{
		{name: "broken JSON", input: `[{"category": "кафе", "limit": 1000`},
		{name: "wrong type", input: `[{"category": "кафе", "limit": "много"}]`},
		{name: "invalid limit", input: `[{"category": "кафе", "limit": 1000}, {"category": "такси", "limit": -5}]`},
		{name: "invalid period", input: `[{"category": "кафе", "limit": 1000, "period": "week"}]`},
	} {
		fmt.Printf("  %s: %v\n", c.name, svc.LoadBudgets(context.Background(), strings.NewReader(c.input)))
	}
	fmt.Printf("  missing file: %v\n", loadBudgetsFromFile(svc, "no-such-dir/budgets.json"))
}

// showValidation checks transactions and budgets with CheckValid, which
// takes any Validatable, and writes the results to w. Validate itself only
// returns errors, so they are printed here.
func showValidation(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, v := range []domain.Validatable{
		domain.Transaction{Amount: 450, Category: "еда", Description: "ланч"},
		domain.Transaction{Amount: -100, Category: "еда", Description: "возврат"},
		domain.Transaction{Amount: 300, Category: " ", Description: "такси"},
		domain.Transaction{Amount: 50, Category: "еда", Description: "кофе", Date: time.Date(0, time.March, 1, 12, 0, 0, 0, time.Local)},
		domain.Budget{Category: "кафе", Limit: 3000, Period: domain.PeriodMonth},
		domain.Budget{Category: "кафе", Limit: 0},
		domain.Budget{Limit: 1000},
	} {
		result := "ok"
		if err := domain.CheckValid(v); err != nil {
			result = "invalid: " + err.Error()
		}
		fmt.Fprintf(tw, "  %T\t%s\t%s\n", v, describe(v), result)
	}
	tw.Flush()
}

// describe returns a short description of a transaction or a budget for
// the validation demo.
func describe(v domain.Validatable) string {
	switch v := v.(type) {
	case domain.Transaction:
		s := fmt.Sprintf("%.2f %q (%s)", v.Amount, v.Description, categoryOrNone(v.Category))
		if !v.Date.IsZero() {
			s += " on " + v.Date.Format(time.DateOnly)
		}
		return s
	case domain.Budget:
		s := fmt.Sprintf("%s %.2f", categoryOrNone(v.Category), v.Limit)
		if v.Period != domain.PeriodNone {
			s += " a " + string(v.Period)
		}
		return s
	default:
		return fmt.Sprint(v)
	}
}

// categoryOrNone returns the category, or "no category" if it is blank.
func categoryOrNone(category string) string {
	if strings.TrimSpace(category) == "" {
		return "no category"
	}
	return category
}

// printTransactions writes the stored transactions to w as a table, newest
// first. Dates are shown in the local time zone, the one budget periods are
// counted in.
func printTransactions(svc service.Service, w io.Writer) error {
	txs, err := svc.ListTransactions(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Transactions (%d):\n", len(txs))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tDATE\tCATEGORY\tAMOUNT\tDESCRIPTION")
	for _, tx := range txs {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%.2f\t%s\n",
			tx.ID, tx.Date.In(time.Local).Format(time.DateOnly), tx.Category, tx.Amount, tx.Description)
	}
	return tw.Flush()
}

// printBudgets writes every budget with the amounts spent and left to w.
// The amounts are for the budget period that contains now.
func printBudgets(svc service.Service, w io.Writer, now time.Time) error {
	list, err := svc.ListBudgetUsage(context.Background(), now)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Budgets (%d):\n", len(list))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CATEGORY\tPERIOD\tLIMIT\tSPENT\tLEFT")
	for _, u := range list {
		period := u.PeriodLabel
		if period == "" {
			period = "all time"
		}
		fmt.Fprintf(tw, "%s\t%s\t%.2f\t%.2f\t%.2f\n",
			u.Budget.Category, period, u.Budget.Limit, u.Spent, remaining(u.Budget.Limit, u.Spent))
	}
	return tw.Flush()
}

// printReport writes the amounts spent in every category of the report and
// their total to w.
func printReport(w io.Writer, r domain.ReportSummary) {
	fmt.Fprintf(w, "Report from %s to %s:\n", r.From.Format(time.DateOnly), r.To.Format(time.DateOnly))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CATEGORY\tSPENT")
	for _, c := range r.Categories {
		fmt.Fprintf(tw, "%s\t%.2f\n", c.Category, c.Amount)
	}
	fmt.Fprintf(tw, "total\t%.2f\n", r.Total)
	tw.Flush()
}

// remaining returns how much of the limit is still available. It is never
// negative, even if the limit was lowered below the amount already spent.
func remaining(limit, spent float64) float64 {
	return max(0, limit-spent)
}
