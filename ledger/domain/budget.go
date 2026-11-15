package domain

import (
	"errors"
	"fmt"
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

// Label returns the name of the period of p that contains t, such as
// "2026-10" for PeriodMonth, "2026" for PeriodYear and "" for PeriodNone.
// Transactions with equal labels share one limit. Months and years are
// counted in the local time zone, so a moment falls into the same period
// whatever the location of t.
func (p Period) Label(t time.Time) string {
	return t.In(time.Local).Format(periodLayouts[p])
}

// Bounds returns the start of the period of p that contains t and the
// start of the next period: the starts of their first days in the local
// time zone, see Day. The periods are the same as those of Label. ok is
// false for PeriodNone, which has no bounds.
func (p Period) Bounds(t time.Time) (start, end time.Time, ok bool) {
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
	category := strings.ToLower(strings.TrimSpace(b.Category))
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

// BudgetUsage is a budget with the amount spent within one of its periods.
type BudgetUsage struct {
	Budget      Budget
	PeriodLabel string  // such as "2026-10" for a monthly budget; "" if the budget has no period
	Spent       float64 // amount spent in the category within the period
}

// normalizePeriod trims and lowercases the period, so " Month" and "month"
// are the same period.
func normalizePeriod(p Period) Period {
	return Period(strings.ToLower(strings.TrimSpace(string(p))))
}
