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

// Errors returned by AddTransaction for invalid transactions.
var (
	ErrInvalidAmount = errors.New("amount must be a positive number")
	ErrEmptyCategory = errors.New("category must not be empty")
)

// transactions is the in-memory transaction storage. It starts empty.
var transactions = []Transaction{}

// AddTransaction validates tx, assigns it the next ID and stores it.
// The category is trimmed, and a zero date is replaced with the current time.
func AddTransaction(tx Transaction) error {
	if math.IsNaN(tx.Amount) || math.IsInf(tx.Amount, 0) || tx.Amount <= 0 {
		return fmt.Errorf("%w, got %v", ErrInvalidAmount, tx.Amount)
	}
	tx.Category = strings.TrimSpace(tx.Category)
	if tx.Category == "" {
		return ErrEmptyCategory
	}

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
