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

// Accepted range for transaction amounts and budget limits, in rubles.
const (
	minAmount = 0.01 // one kopeck
	maxAmount = 1e12
)

// Errors returned by AddTransaction for invalid transactions.
var (
	ErrInvalidAmount = errors.New("invalid amount")
	ErrEmptyCategory = errors.New("category must not be empty")
)

// transactions is the in-memory transaction storage. It starts empty and is
// not safe for concurrent use.
var transactions = []Transaction{}

// AddTransaction validates tx, assigns it the next ID and stores it.
// The amount is rounded to kopecks, the category is normalized with
// normalizeCategory, the description is trimmed, and a zero date is replaced
// with the current time. If the category has a budget and tx does not fit
// into it within the budget period of tx.Date, AddTransaction returns
// a *BudgetExceededError and stores nothing.
func AddTransaction(tx Transaction) error {
	if !inAmountRange(tx.Amount) {
		return fmt.Errorf("%w %v: must be from %g to %g", ErrInvalidAmount, tx.Amount, minAmount, maxAmount)
	}
	tx.Amount = roundToKopecks(tx.Amount)
	tx.Category = normalizeCategory(tx.Category)
	if tx.Category == "" {
		return ErrEmptyCategory
	}
	tx.Description = strings.TrimSpace(tx.Description)
	// The budget period depends on the date, so set it before the check.
	if tx.Date.IsZero() {
		tx.Date = time.Now()
	}
	if err := checkBudget(tx); err != nil {
		return err
	}

	tx.ID = len(transactions) + 1
	transactions = append(transactions, tx)
	return nil
}

// ListTransactions returns a copy of all stored transactions, so callers
// cannot change the storage through the returned slice.
func ListTransactions() []Transaction {
	return slices.Clone(transactions)
}

// inAmountRange reports whether x is an acceptable amount or limit.
// NaN fails every comparison, so it is rejected as well as infinities.
func inAmountRange(x float64) bool {
	return x >= minAmount && x <= maxAmount
}

// roundToKopecks rounds an amount in rubles to whole kopecks.
func roundToKopecks(x float64) float64 {
	return math.Round(x*100) / 100
}

// normalizeCategory trims spaces and lowercases the category, so "Еда " and
// "еда" are the same category.
func normalizeCategory(category string) string {
	return strings.ToLower(strings.TrimSpace(category))
}
