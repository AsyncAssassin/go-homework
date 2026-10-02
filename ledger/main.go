// Command ledger is the business-logic service of the project. For now it
// keeps transactions in memory and shows how they are added and listed.
package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"
)

func main() {
	fmt.Println("Ledger service started")

	demo := []Transaction{
		{Amount: 1250.50, Category: "еда", Description: "продукты на неделю"},
		{Amount: 300, Category: "транспорт", Description: "проездной"},
		{Amount: 2000, Category: "развлечения", Description: "кино"},
		{Amount: 0, Category: "еда", Description: "пустой чек"},
	}
	for _, tx := range demo {
		if err := AddTransaction(tx); err != nil {
			fmt.Printf("Rejected %q: %v\n", tx.Description, err)
			continue
		}
		fmt.Printf("Added %q: %.2f (%s)\n", tx.Description, tx.Amount, tx.Category)
	}

	fmt.Println()
	printTransactions(os.Stdout, ListTransactions())
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
