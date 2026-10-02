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
		{Category: "еда", Limit: 5000},
		{Category: "транспорт", Limit: 2000},
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
	fmt.Printf("Budgets loaded from %s\n\n", *budgetsPath)
	printBudgets(os.Stdout)

	fmt.Println("\nAdding transactions:")
	for _, tx := range []Transaction{
		{Amount: 1250.50, Category: "еда", Description: "продукты на неделю"},
		{Amount: 2000, Category: "транспорт", Description: "проездной"},
		{Amount: 3500, Category: "еда", Description: "кафе"},
		{Amount: 500, Category: "еда", Description: "ресторан"},         // over the budget
		{Amount: 249.50, Category: "еда", Description: "хлеб и молоко"}, // exactly up to the limit
		{Amount: 700, Category: "здоровье", Description: "лекарства"},   // category without a budget
		{Amount: 0, Category: "еда", Description: "пустой чек"},         // invalid amount
	} {
		addAndReport(tx)
	}

	fmt.Println("\nBudget loading errors:")
	showLoadErrors()

	fmt.Println()
	printTransactions(os.Stdout, ListTransactions())
	fmt.Println()
	printBudgets(os.Stdout)
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
	} {
		fmt.Printf("  %s: %v\n", c.name, LoadBudgets(strings.NewReader(c.input)))
	}
	fmt.Printf("  missing file: %v\n", loadBudgetsFromFile("no-such-dir/budgets.json"))
}

// printTransactions writes transactions to w as a table.
func printTransactions(w io.Writer, txs []Transaction) {
	fmt.Fprintf(w, "Transactions (%d):\n", len(txs))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tDATE\tCATEGORY\tAMOUNT\tDESCRIPTION")
	for _, tx := range txs {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%.2f\t%s\n",
			tx.ID, tx.Date.Format(time.DateOnly), tx.Category, tx.Amount, tx.Description)
	}
	tw.Flush()
}

// printBudgets writes every budget with the amounts spent and left to w.
func printBudgets(w io.Writer) {
	list := ListBudgets()
	fmt.Fprintf(w, "Budgets (%d):\n", len(list))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CATEGORY\tLIMIT\tSPENT\tLEFT")
	for _, b := range list {
		spent := spentIn(b.Category)
		fmt.Fprintf(tw, "%s\t%.2f\t%.2f\t%.2f\n", b.Category, b.Limit, spent, remaining(b.Limit, spent))
	}
	tw.Flush()
}

// remaining returns how much of the limit is still available. It is never
// negative, even if the limit was lowered below the amount already spent.
func remaining(limit, spent float64) float64 {
	return max(0, limit-spent)
}
