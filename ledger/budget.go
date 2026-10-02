package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
)

// Budget limits the total amount of expenses in a category.
type Budget struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}

// Errors returned for invalid budgets and for transactions over budget.
var (
	ErrInvalidLimit   = errors.New("invalid limit")
	ErrBudgetExceeded = errors.New("budget exceeded")
)

// BudgetExceededError reports a transaction that does not fit into the
// budget of its category. errors.Is(err, ErrBudgetExceeded) matches it.
type BudgetExceededError struct {
	Category string
	Limit    float64 // budget limit of the category
	Spent    float64 // amount already spent in the category
	Amount   float64 // amount of the rejected transaction
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("budget exceeded for %q: spent %.2f + new %.2f > limit %.2f",
		e.Category, e.Spent, e.Amount, e.Limit)
}

// Is makes errors.Is(err, ErrBudgetExceeded) report true.
func (e *BudgetExceededError) Is(target error) bool {
	return target == ErrBudgetExceeded
}

// budgets is the in-memory budget storage keyed by category. It starts empty
// and is not safe for concurrent use.
var budgets = map[string]Budget{}

// SetBudget adds a budget for b.Category or replaces the existing one.
func SetBudget(b Budget) error {
	b, err := normalizeBudget(b)
	if err != nil {
		return err
	}
	budgets[b.Category] = b
	return nil
}

// ListBudgets returns all budgets sorted by category.
func ListBudgets() []Budget {
	list := make([]Budget, 0, len(budgets))
	for _, b := range budgets {
		list = append(list, b)
	}
	slices.SortFunc(list, func(a, b Budget) int {
		return strings.Compare(a.Category, b.Category)
	})
	return list
}

// LoadBudgets reads a JSON array of budgets from r, for example
// [{"category": "еда", "limit": 5000}], and sets each of them with SetBudget.
// Nothing is changed unless the whole input is valid.
func LoadBudgets(r io.Reader) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var list []Budget
	if err := dec.Decode(&list); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("load budgets: no data, expected a JSON array of budgets")
		}
		return fmt.Errorf("load budgets: decode JSON: %w", err)
	}
	if list == nil {
		return errors.New("load budgets: expected a JSON array of budgets, got null")
	}
	// The array must be the only value in the input.
	tok, err := dec.Token()
	switch {
	case err == nil:
		return fmt.Errorf("load budgets: unexpected data after the JSON array: %v", tok)
	case !errors.Is(err, io.EOF):
		return fmt.Errorf("load budgets: decode JSON: %w", err)
	}

	// Check every budget before changing the storage.
	seen := make(map[string]bool, len(list))
	for i, b := range list {
		b, err := normalizeBudget(b)
		if err != nil {
			return fmt.Errorf("load budgets: budget #%d: %w", i+1, err)
		}
		if seen[b.Category] {
			return fmt.Errorf("load budgets: budget #%d: duplicate category %q", i+1, b.Category)
		}
		seen[b.Category] = true
	}

	for _, b := range list {
		if err := SetBudget(b); err != nil {
			return fmt.Errorf("load budgets: %w", err)
		}
	}
	return nil
}

// normalizeBudget checks that the budget is valid, normalizes its category
// and rounds the limit to kopecks.
func normalizeBudget(b Budget) (Budget, error) {
	b.Category = normalizeCategory(b.Category)
	if b.Category == "" {
		return Budget{}, ErrEmptyCategory
	}
	if !inAmountRange(b.Limit) {
		return Budget{}, fmt.Errorf("%w %v for %q: must be from %g to %g",
			ErrInvalidLimit, b.Limit, b.Category, minAmount, maxAmount)
	}
	b.Limit = roundToKopecks(b.Limit)
	return b, nil
}

// checkBudget returns a *BudgetExceededError if tx does not fit into the
// budget of its category. Categories without a budget are not limited.
func checkBudget(tx Transaction) error {
	b, ok := budgets[tx.Category]
	if !ok {
		return nil
	}
	spent := spentIn(tx.Category)
	// Amounts and limits are whole kopecks, so compare kopecks: float sums
	// like 0.1+0.2 are not exactly 0.3. maxAmount keeps the product finite.
	if math.Round((spent+tx.Amount)*100) > math.Round(b.Limit*100) {
		return &BudgetExceededError{Category: tx.Category, Limit: b.Limit, Spent: spent, Amount: tx.Amount}
	}
	return nil
}

// spentIn returns the total amount of the stored transactions in category.
func spentIn(category string) float64 {
	var total float64
	for _, tx := range transactions {
		if tx.Category == category {
			total += tx.Amount
		}
	}
	return total
}
