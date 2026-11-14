package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"
)

// Budget limits the total amount of expenses in a category. With a Period
// the limit starts over every calendar month or year.
type Budget struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
	Period   Period  `json:"period,omitempty"`
}

// Period sets which transactions of a category share one budget limit.
type Period string

// Supported budget periods.
const (
	PeriodNone  Period = ""      // all transactions, whatever their dates
	PeriodMonth Period = "month" // transactions of the same calendar month
	PeriodYear  Period = "year"  // transactions of the same calendar year
)

// periodLayouts maps every supported period to the time layout of its
// labels. The empty layout gives all dates the same empty label.
var periodLayouts = map[Period]string{
	PeriodNone:  "",
	PeriodMonth: "2006-01",
	PeriodYear:  "2006",
}

// label returns the name of the period of p that contains t, such as
// "2026-10" for PeriodMonth, "2026" for PeriodYear and "" for PeriodNone.
// Transactions with equal labels share one limit. Months and years are
// counted in the local time zone, so a moment falls into the same period
// whatever the location of t.
func (p Period) label(t time.Time) string {
	return t.In(time.Local).Format(periodLayouts[p])
}

// valid reports whether p is one of the supported periods.
func (p Period) valid() bool {
	_, ok := periodLayouts[p]
	return ok
}

// Errors returned for invalid budgets and for transactions over budget.
var (
	ErrInvalidLimit   = errors.New("invalid limit")
	ErrInvalidPeriod  = errors.New("invalid period")
	ErrBudgetExceeded = errors.New("budget exceeded")
)

// BudgetExceededError reports a transaction that does not fit into the
// budget of its category. errors.Is(err, ErrBudgetExceeded) matches it.
type BudgetExceededError struct {
	Category    string
	PeriodLabel string  // budget period, such as "2026-10" for a monthly budget; "" if the budget has no period
	Limit       float64 // budget limit of the category
	Spent       float64 // amount already spent in the category within the period
	Amount      float64 // amount of the rejected transaction
}

func (e *BudgetExceededError) Error() string {
	var period string
	if e.PeriodLabel != "" {
		period = " in " + e.PeriodLabel
	}
	return fmt.Sprintf("budget exceeded for %q%s: spent %.2f + new %.2f > limit %.2f",
		e.Category, period, e.Spent, e.Amount, e.Limit)
}

// Is makes errors.Is(err, ErrBudgetExceeded) report true.
func (e *BudgetExceededError) Is(target error) bool {
	return target == ErrBudgetExceeded
}

// budgets is the in-memory budget storage keyed by category. It starts empty
// and is not safe for concurrent use.
var budgets = map[string]Budget{}

// Validate reports whether b can be stored. The category must not be blank,
// the limit must be from 0.01 to 1e12, and the period must be "month",
// "year" or empty. Case and surrounding spaces of the category and the
// period do not matter.
func (b Budget) Validate() error {
	category := normalizeCategory(b.Category)
	if category == "" {
		return ErrEmptyCategory
	}
	if !inAmountRange(b.Limit) {
		return fmt.Errorf("%w %v for %q: must be from %g to %g",
			ErrInvalidLimit, b.Limit, category, minAmount, maxAmount)
	}
	if !normalizePeriod(b.Period).valid() {
		// Report the period as written, so it is easy to find in the input.
		return fmt.Errorf("%w %q for %q: must be %q, %q or empty",
			ErrInvalidPeriod, b.Period, category, PeriodMonth, PeriodYear)
	}
	return nil
}

// SetBudget checks b with Validate and adds a budget for b.Category or
// replaces the existing one as a whole, period included: a budget without
// a period replaces a monthly one with a limit for all time.
func SetBudget(b Budget) error {
	if err := b.Validate(); err != nil {
		return err
	}
	b = normalizeBudget(b)
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
// [{"category": "еда", "limit": 5000, "period": "month"}], checks each of
// them with Validate and sets them as SetBudget does. The period is optional.
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
		if err := b.Validate(); err != nil {
			return fmt.Errorf("load budgets: budget #%d: %w", i+1, err)
		}
		b = normalizeBudget(b)
		if seen[b.Category] {
			return fmt.Errorf("load budgets: budget #%d: duplicate category %q", i+1, b.Category)
		}
		seen[b.Category] = true
		list[i] = b
	}

	for _, b := range list {
		budgets[b.Category] = b
	}
	return nil
}

// normalizeBudget trims and lowercases the category and the period of
// a valid budget and rounds its limit to kopecks.
func normalizeBudget(b Budget) Budget {
	b.Category = normalizeCategory(b.Category)
	b.Period = normalizePeriod(b.Period)
	b.Limit = roundToKopecks(b.Limit)
	return b
}

// normalizePeriod trims and lowercases the period, so " Month" and "month"
// are the same period.
func normalizePeriod(p Period) Period {
	return Period(strings.ToLower(strings.TrimSpace(string(p))))
}

// checkBudget returns a *BudgetExceededError if tx does not fit into the
// budget of its category within the period that contains tx.Date.
// Categories without a budget are not limited.
func checkBudget(tx Transaction) error {
	b, ok := budgets[tx.Category]
	if !ok {
		return nil
	}
	spent := spentIn(tx.Category, b.Period, tx.Date)
	// Amounts and limits are whole kopecks, so compare kopecks: float sums
	// like 0.1+0.2 are not exactly 0.3. maxAmount keeps the product finite.
	if math.Round((spent+tx.Amount)*100) > math.Round(b.Limit*100) {
		return &BudgetExceededError{
			Category:    tx.Category,
			PeriodLabel: b.Period.label(tx.Date),
			Limit:       b.Limit,
			Spent:       spent,
			Amount:      tx.Amount,
		}
	}
	return nil
}

// spentIn returns the total amount of the stored transactions in category
// that fall into the same period of p as date.
func spentIn(category string, p Period, date time.Time) float64 {
	period := p.label(date)
	var total float64
	for _, tx := range transactions {
		if tx.Category == category && p.label(tx.Date) == period {
			total += tx.Amount
		}
	}
	return total
}
