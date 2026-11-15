package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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

// bounds returns the start of the period of p that contains t and the
// start of the next period: the starts of their first days in the local
// time zone, see Day. The periods are the same as those of label. ok is
// false for PeriodNone, which has no bounds.
func (p Period) bounds(t time.Time) (start, end time.Time, ok bool) {
	y, m, _ := t.In(time.Local).Date()
	switch p {
	case PeriodMonth:
		return Day(y, m, 1), Day(y, m+1, 1), true
	case PeriodYear:
		return Day(y, time.January, 1), Day(y+1, time.January, 1), true
	default:
		return time.Time{}, time.Time{}, false
	}
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

// Validate reports whether b can be stored. The category must not be blank
// and must be at most 100 characters of valid UTF-8 without NUL characters,
// the limit must be from 0.01 to 999999999999.99, and the period must be
// "month", "year" or empty. Case and surrounding spaces of the category and
// the period do not matter.
func (b Budget) Validate() error {
	if err := checkCategory(b.Category); err != nil {
		return err
	}
	category := normalizeCategory(b.Category)
	if !inAmountRange(b.Limit) {
		return fmt.Errorf("%w %v for %q: must be from %.2f to %.2f",
			ErrInvalidLimit, b.Limit, category, minAmount, maxAmount)
	}
	if !normalizePeriod(b.Period).valid() {
		// Report the period as written, so it is easy to find in the input.
		return fmt.Errorf("%w %q for %q: must be %q, %q or empty",
			ErrInvalidPeriod, b.Period, category, PeriodMonth, PeriodYear)
	}
	return nil
}

// SetBudget checks b with Validate, adds a budget for b.Category or replaces
// the existing one as a whole, period included, and returns the stored
// budget. A budget without a period replaces a monthly one with a limit for
// all time. The category and the period are stored trimmed and lowercased,
// the limit is rounded to kopecks. It is safe for concurrent use.
func SetBudget(b Budget) (Budget, error) {
	if err := b.Validate(); err != nil {
		return Budget{}, err
	}
	b = normalizeBudget(b)

	ctx, cancel := queryContext()
	defer cancel()
	if err := store.setBudgets(ctx, b); err != nil {
		return Budget{}, err
	}
	return b, nil
}

// ListBudgets returns all budgets sorted by category. After Connect the
// list is cached in Redis for 30 seconds or until a budget changes.
func ListBudgets() ([]Budget, error) {
	ctx, cancel := queryContext()
	defer cancel()
	return store.listBudgets(ctx)
}

// BudgetUsage is a budget with the amount spent within one of its periods.
type BudgetUsage struct {
	Budget      Budget
	PeriodLabel string  // such as "2026-10" for a monthly budget; "" if the budget has no period
	Spent       float64 // amount spent in the category within the period
}

// ListBudgetUsage returns every budget, sorted by category, with the amount
// spent within its period that contains date.
func ListBudgetUsage(date time.Time) ([]BudgetUsage, error) {
	ctx, cancel := queryContext()
	defer cancel()
	return store.budgetUsage(ctx, date)
}

// LoadBudgets reads a JSON array of budgets from r, for example
// [{"category": "еда", "limit": 5000, "period": "month"}], checks each of
// them with Validate and sets them as SetBudget does, all at once. The period
// is optional. Nothing is changed unless the whole input is valid.
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

	ctx, cancel := queryContext()
	defer cancel()
	if err := store.setBudgets(ctx, list...); err != nil {
		return fmt.Errorf("load budgets: %w", err)
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

// checkBudget returns a *BudgetExceededError if tx does not fit into
// budget b of its category, given the amount already spent within the period
// of b that contains tx.Date.
func checkBudget(b Budget, spent float64, tx Transaction) error {
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

// spentIn returns the total amount of the transactions in txs that have
// the category of b and fall into the period of b that contains date.
func spentIn(txs []Transaction, b Budget, date time.Time) float64 {
	period := b.Period.label(date)
	var total float64
	for _, tx := range txs {
		if tx.Category == b.Category && b.Period.label(tx.Date) == period {
			total += tx.Amount
		}
	}
	// Float sums like 0.1+0.2 are not exactly 0.3; PostgreSQL sums the
	// amounts exactly.
	return roundToKopecks(total)
}
