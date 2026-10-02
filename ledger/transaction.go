package main

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// Transaction is a single expense recorded in the ledger.
type Transaction struct {
	ID          int
	Amount      float64
	Category    string
	Description string
	Date        time.Time
}

// minAmount is the smallest accepted amount: one kopeck.
const minAmount = 0.01

// Errors returned by AddTransaction for invalid transactions.
var (
	ErrInvalidAmount = errors.New("invalid amount")
	ErrEmptyCategory = errors.New("category must not be empty")
)

// transactions is the in-memory transaction storage. It starts empty and is
// not safe for concurrent use.
var transactions = []Transaction{}

// AddTransaction validates tx, assigns it the next ID and stores it.
// Category and description are trimmed, and a zero date is replaced with
// the current time.
func AddTransaction(tx Transaction) error {
	if math.IsNaN(tx.Amount) || math.IsInf(tx.Amount, 0) || tx.Amount < minAmount {
		return fmt.Errorf("%w %v: must be a finite number of at least %.2f", ErrInvalidAmount, tx.Amount, minAmount)
	}
	tx.Category = strings.TrimSpace(tx.Category)
	if tx.Category == "" {
		return ErrEmptyCategory
	}
	tx.Description = strings.TrimSpace(tx.Description)

	tx.ID = len(transactions) + 1
	if tx.Date.IsZero() {
		tx.Date = time.Now()
	}
	transactions = append(transactions, tx)
	return nil
}

// ListTransactions returns a copy of all stored transactions, so callers
// cannot change the storage through the returned slice.
func ListTransactions() []Transaction {
	return slices.Clone(transactions)
}
