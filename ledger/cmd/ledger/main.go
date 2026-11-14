// Command ledger is the console demo of the ledger package. It keeps
// transactions and category budgets in memory and shows how budgets limit
// new transactions.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger"
)

func main() {
	budgetsPath := flag.String("budgets", "budgets.json", "path to the JSON file with budgets")
	flag.Parse()

	fmt.Println("Ledger service started")

	// Initial budgets are set in code, then budgets.json adds and updates some.
	for _, b := range []ledger.Budget{
		{Category: "еда", Limit: 5000, Period: ledger.PeriodMonth},
		{Category: "транспорт", Limit: 2000, Period: ledger.PeriodMonth},
	} {
		if _, err := ledger.SetBudget(b); err != nil {
			fmt.Fprintln(os.Stderr, "Cannot set budget:", err)
			os.Exit(1)
		}
	}
	loaded, err := loadBudgetsIfExists(*budgetsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot load budgets:", err)
		os.Exit(1)
	}
	if loaded {
		fmt.Printf("Budgets loaded from %s\n", *budgetsPath)
	} else {
		fmt.Fprintf(os.Stderr, "Budgets file %s not found, using the built-in budgets (pass -budgets <path> to load a file)\n", *budgetsPath)
	}
	// One clock reading for the whole demo, so a run at midnight cannot split
	// it between two months.
	now := time.Now()
	lastMonth := now.AddDate(0, 0, -now.Day()) // the last day of the previous month

	fmt.Println()
	printBudgets(os.Stdout, now)

	fmt.Println("\nAdding transactions:")
	for _, tx := range []ledger.Transaction{
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
		addAndReport(tx)
	}

	fmt.Println("\nBudget loading errors:")
	showLoadErrors()

	fmt.Println()
	printTransactions(os.Stdout, ledger.ListTransactions())
	fmt.Println()
	printBudgets(os.Stdout, now)

	fmt.Println("\nValidation through the Validatable interface:")
	showValidation(os.Stdout)
}

// loadBudgetsIfExists loads budgets from the JSON file at path and reports
// whether it did. The file is optional: when it does not exist, the budgets
// set in code stay in effect and no error is returned. A file that exists
// but cannot be read or parsed is an error.
func loadBudgetsIfExists(path string) (bool, error) {
	err := loadBudgetsFromFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// loadBudgetsFromFile loads budgets from the JSON file at path.
func loadBudgetsFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("load budgets: %w", err)
	}
	defer f.Close()

	return ledger.LoadBudgets(bufio.NewReader(f))
}

// addAndReport adds tx and prints whether it was accepted.
func addAndReport(tx ledger.Transaction) {
	_, err := ledger.AddTransaction(tx)

	var budgetErr *ledger.BudgetExceededError
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
func showLoadErrors() {
	for _, c := range []struct {
		name  string
		input string
	}{
		{name: "broken JSON", input: `[{"category": "кафе", "limit": 1000`},
		{name: "wrong type", input: `[{"category": "кафе", "limit": "много"}]`},
		{name: "invalid limit", input: `[{"category": "кафе", "limit": 1000}, {"category": "такси", "limit": -5}]`},
		{name: "invalid period", input: `[{"category": "кафе", "limit": 1000, "period": "week"}]`},
	} {
		fmt.Printf("  %s: %v\n", c.name, ledger.LoadBudgets(strings.NewReader(c.input)))
	}
	fmt.Printf("  missing file: %v\n", loadBudgetsFromFile("no-such-dir/budgets.json"))
}

// showValidation checks transactions and budgets with CheckValid, which
// takes any Validatable, and writes the results to w. Validate itself only
// returns errors, so they are printed here.
func showValidation(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, v := range []ledger.Validatable{
		ledger.Transaction{Amount: 450, Category: "еда", Description: "ланч"},
		ledger.Transaction{Amount: -100, Category: "еда", Description: "возврат"},
		ledger.Transaction{Amount: 300, Category: " ", Description: "такси"},
		ledger.Transaction{Amount: 50, Category: "еда", Description: "кофе", Date: time.Date(0, time.March, 1, 12, 0, 0, 0, time.Local)},
		ledger.Budget{Category: "кафе", Limit: 3000, Period: ledger.PeriodMonth},
		ledger.Budget{Category: "кафе", Limit: 0},
		ledger.Budget{Limit: 1000},
	} {
		result := "ok"
		if err := ledger.CheckValid(v); err != nil {
			result = "invalid: " + err.Error()
		}
		fmt.Fprintf(tw, "  %T\t%s\t%s\n", v, describe(v), result)
	}
	tw.Flush()
}

// describe returns a short description of a transaction or a budget for
// the validation demo.
func describe(v ledger.Validatable) string {
	switch v := v.(type) {
	case ledger.Transaction:
		s := fmt.Sprintf("%.2f %q (%s)", v.Amount, v.Description, categoryOrNone(v.Category))
		if !v.Date.IsZero() {
			s += " on " + v.Date.Format(time.DateOnly)
		}
		return s
	case ledger.Budget:
		s := fmt.Sprintf("%s %.2f", categoryOrNone(v.Category), v.Limit)
		if v.Period != ledger.PeriodNone {
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

// printTransactions writes transactions to w as a table. Dates are shown in
// the local time zone, the one budget periods are counted in.
func printTransactions(w io.Writer, txs []ledger.Transaction) {
	fmt.Fprintf(w, "Transactions (%d):\n", len(txs))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tDATE\tCATEGORY\tAMOUNT\tDESCRIPTION")
	for _, tx := range txs {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%.2f\t%s\n",
			tx.ID, tx.Date.In(time.Local).Format(time.DateOnly), tx.Category, tx.Amount, tx.Description)
	}
	tw.Flush()
}

// printBudgets writes every budget with the amounts spent and left to w.
// The amounts are for the budget period that contains now.
func printBudgets(w io.Writer, now time.Time) {
	list := ledger.ListBudgetUsage(now)
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
	tw.Flush()
}

// remaining returns how much of the limit is still available. It is never
// negative, even if the limit was lowered below the amount already spent.
func remaining(limit, spent float64) float64 {
	return max(0, limit-spent)
}
