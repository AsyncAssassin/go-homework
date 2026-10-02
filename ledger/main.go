// Command ledger is the business-logic service of the project. It keeps
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
)

func main() {
	budgetsPath := flag.String("budgets", "budgets.json", "path to the JSON file with budgets")
	flag.Parse()

	fmt.Println("Ledger service started")

	// Initial budgets are set in code, then budgets.json adds and updates some.
	for _, b := range []Budget{
		{Category: "еда", Limit: 5000, Period: PeriodMonth},
		{Category: "транспорт", Limit: 2000, Period: PeriodMonth},
	} {
		if err := SetBudget(b); err != nil {
			fmt.Fprintln(os.Stderr, "Cannot set budget:", err)
			os.Exit(1)
		}
	}
	if err := loadBudgetsFromFile(*budgetsPath); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot load budgets:", err)
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "Run the service from the ledger directory or pass -budgets <path>.")
		}
		os.Exit(1)
	}
	// One clock reading for the whole demo, so a run at midnight cannot split
	// it between two months.
	now := time.Now()
	lastMonth := now.AddDate(0, 0, -now.Day()) // the last day of the previous month

	fmt.Printf("Budgets loaded from %s\n\n", *budgetsPath)
	printBudgets(os.Stdout, now)

	fmt.Println("\nAdding transactions:")
	for _, tx := range []Transaction{
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
	printTransactions(os.Stdout, ListTransactions())
	fmt.Println()
	printBudgets(os.Stdout, now)
}

// loadBudgetsFromFile loads budgets from the JSON file at path.
func loadBudgetsFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("load budgets: %w", err)
	}
	defer f.Close()

	return LoadBudgets(bufio.NewReader(f))
}

// addAndReport adds tx and prints whether it was accepted.
func addAndReport(tx Transaction) {
	err := AddTransaction(tx)

	var budgetErr *BudgetExceededError
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
		fmt.Printf("  %s: %v\n", c.name, LoadBudgets(strings.NewReader(c.input)))
	}
	fmt.Printf("  missing file: %v\n", loadBudgetsFromFile("no-such-dir/budgets.json"))
}

// printTransactions writes transactions to w as a table. Dates are shown in
// the local time zone, the one budget periods are counted in.
func printTransactions(w io.Writer, txs []Transaction) {
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
	list := ListBudgets()
	fmt.Fprintf(w, "Budgets (%d):\n", len(list))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CATEGORY\tPERIOD\tLIMIT\tSPENT\tLEFT")
	for _, b := range list {
		period := b.Period.label(now)
		if period == "" {
			period = "all time"
		}
		spent := spentIn(b.Category, b.Period, now)
		fmt.Fprintf(tw, "%s\t%s\t%.2f\t%.2f\t%.2f\n",
			b.Category, period, b.Limit, spent, remaining(b.Limit, spent))
	}
	tw.Flush()
}

// remaining returns how much of the limit is still available. It is never
// negative, even if the limit was lowered below the amount already spent.
func remaining(limit, spent float64) float64 {
	return max(0, limit-spent)
}
