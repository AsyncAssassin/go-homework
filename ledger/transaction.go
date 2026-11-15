package ledger

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// Transaction is a single expense recorded in the ledger. AddTransaction
// keeps only the day of Date: its start in the local time zone, see Day.
type Transaction struct {
	ID          int
	Amount      float64
	Category    string
	Description string
	Date        time.Time
}

// Accepted range for transaction amounts and budget limits, in rubles. The
// upper bound is the largest value of the NUMERIC(14,2) columns in
// PostgreSQL.
const (
	minAmount = 0.01 // one kopeck
	maxAmount = 999999999999.99
)

// Transaction dates must be at or after minDate and before maxDate. The day
// of margin on each side keeps a valid date within the years 1-9999 in every
// time zone, so it can always be written as YYYY-MM-DD, and makes the check
// depend only on the moment, not on the time zone it is given in. Reports
// take the calendar days of this range, see checkDay.
var (
	minDate = time.Date(1, time.January, 2, 0, 0, 0, 0, time.UTC)
	maxDate = time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)
)

// maxCategoryLength is the length limit of a category in characters, after
// normalizeCategory. PostgreSQL indexes the category, and an index entry
// must fit into 2704 bytes.
const maxCategoryLength = 100

// Errors returned by Transaction.Validate and Budget.Validate.
var (
	ErrInvalidAmount      = errors.New("invalid amount")
	ErrEmptyCategory      = errors.New("category must not be empty")
	ErrInvalidCategory    = errors.New("invalid category")
	ErrInvalidDescription = errors.New("invalid description")
	ErrInvalidDate        = errors.New("invalid date")
)

// Validate reports whether tx can be stored. The amount must be from 0.01
// to 999999999999.99. The category must not be blank and must be at most
// 100 characters long. The category and the description must be valid
// UTF-8 without NUL characters. A set date must be from 0001-01-02 to
// 9999-12-30 UTC. A zero date means "no date" and is valid: AddTransaction
// replaces it with today.
func (tx Transaction) Validate() error {
	if !inAmountRange(tx.Amount) {
		return fmt.Errorf("%w %v: must be from %.2f to %.2f", ErrInvalidAmount, tx.Amount, minAmount, maxAmount)
	}
	if err := checkCategory(tx.Category); err != nil {
		return err
	}
	if !validText(tx.Description) {
		return fmt.Errorf("%w: %s", ErrInvalidDescription, textRule)
	}
	if !tx.Date.IsZero() {
		return checkDate(tx.Date)
	}
	return nil
}

// checkCategory returns ErrEmptyCategory for a blank category and an error
// wrapping ErrInvalidCategory for a category that PostgreSQL cannot store or
// index. The error does not quote the category, which may be long.
func checkCategory(category string) error {
	normalized := normalizeCategory(category)
	if normalized == "" {
		return ErrEmptyCategory
	}
	// normalizeCategory replaces invalid UTF-8, so check the category as given.
	if !validText(category) {
		return fmt.Errorf("%w: %s", ErrInvalidCategory, textRule)
	}
	if n := utf8.RuneCountInString(normalized); n > maxCategoryLength {
		return fmt.Errorf("%w: must be at most %d characters, got %d", ErrInvalidCategory, maxCategoryLength, n)
	}
	return nil
}

// textRule describes the text that validText accepts.
const textRule = "must be valid UTF-8 without NUL characters"

// validText reports whether PostgreSQL can store s in a TEXT column: s must
// be valid UTF-8 without NUL characters.
func validText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// checkDate returns an error wrapping ErrInvalidDate if t is not from
// 0001-01-02 to 9999-12-30 UTC.
func checkDate(t time.Time) error {
	if t.Before(minDate) || !t.Before(maxDate) {
		return fmt.Errorf("%w %s: must be from %s to %s UTC", ErrInvalidDate,
			t.Format(time.DateOnly), minDate.Format(time.DateOnly),
			maxDate.Add(-time.Nanosecond).Format(time.DateOnly))
	}
	return nil
}

// checkDay returns an error wrapping ErrInvalidDate if the day of t in the
// local time zone is not from 0001-01-02 to 9999-12-30. Unlike checkDate it
// checks the calendar day, not the moment, so each of these days is valid
// in every time zone.
func checkDay(t time.Time) error {
	y, m, d := t.In(time.Local).Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if day.Before(minDate) || !day.Before(maxDate) {
		return fmt.Errorf("%w %s: must be from %s to %s", ErrInvalidDate,
			day.Format(time.DateOnly), minDate.Format(time.DateOnly),
			maxDate.Add(-time.Nanosecond).Format(time.DateOnly))
	}
	return nil
}

// AddTransaction checks tx with Validate, assigns it the next ID, stores it
// and returns the stored transaction. The amount is rounded to kopecks, the
// category is normalized with normalizeCategory, the description is trimmed,
// and the date is reduced to its day, today for a zero date. If the category
// has a budget and tx does not fit into it within the budget period of
// tx.Date, AddTransaction returns a *BudgetExceededError and stores nothing.
// It is safe for concurrent use.
func AddTransaction(tx Transaction) (Transaction, error) {
	if err := tx.Validate(); err != nil {
		return Transaction{}, err
	}
	tx.Amount = roundToKopecks(tx.Amount)
	tx.Category = normalizeCategory(tx.Category)
	tx.Description = strings.TrimSpace(tx.Description)
	// The budget period depends on the date, so set it before the check.
	if tx.Date.IsZero() {
		tx.Date = time.Now()
	}
	tx.Date = startOfDay(tx.Date)

	ctx, cancel := queryContext()
	defer cancel()
	return store.addTransaction(ctx, tx)
}

// ListTransactions returns all stored transactions, newest first: by date,
// and by ID for equal dates. Callers cannot change the storage through the
// returned slice.
func ListTransactions() ([]Transaction, error) {
	ctx, cancel := queryContext()
	defer cancel()
	return store.listTransactions(ctx)
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

// Day returns the start of a calendar day in the local time zone: midnight,
// or the first moment of the day if the clocks skip midnight, as in
// America/Santiago on 2026-09-06. The date is normalized as in time.Date,
// so Day(2026, time.October, 0) is September 30.
func Day(year int, month time.Month, day int) time.Time {
	return dayStart(year, month, day, time.Local)
}

// dayStart returns the start of a calendar day in loc, see Day.
func dayStart(year int, month time.Month, day int, loc *time.Location) time.Time {
	year, month, day = time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Date()
	t := time.Date(year, month, day, 0, 0, 0, 0, loc)
	if t.Day() != day {
		// The clocks skip midnight, and time.Date has gone back to the
		// previous day. Its zone ends when the clocks change: at the start
		// of the day, or of the next one if the whole day is skipped.
		_, t = t.ZoneBounds()
	}
	return t
}

// startOfDay returns the start of the day of t in the local time zone, see
// Day.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.In(time.Local).Date()
	return Day(y, m, d)
}

// localDay returns the start in the local time zone of the calendar day of
// t in its own location. PostgreSQL returns DATE values at midnight UTC.
func localDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return Day(y, m, d)
}

// formatDate returns the day of t in the local time zone as YYYY-MM-DD.
func formatDate(t time.Time) string {
	return t.In(time.Local).Format(time.DateOnly)
}

// normalizeCategory trims spaces and lowercases the category, so "Еда " and
// "еда" are the same category.
func normalizeCategory(category string) string {
	return strings.ToLower(strings.TrimSpace(category))
}
