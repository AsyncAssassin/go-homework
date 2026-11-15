package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidRange is returned by GetReportSummary if the range ends before
// it starts.
var ErrInvalidRange = errors.New("invalid report range")

// CategoryTotal is the amount spent in one category.
type CategoryTotal struct {
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
}

// ReportSummary is the amount spent in every category within a range of
// days.
type ReportSummary struct {
	From       time.Time       // first day of the range, at its start in the local time zone, see Day
	To         time.Time       // last day of the range, included
	Categories []CategoryTotal // categories with expenses in the range, sorted by category
	Total      float64         // amount spent in all categories
}

// GetReportSummary returns the amount spent in every category from the day
// of from to the day of to, both included, in the local time zone. Both
// days must be from 0001-01-02 to 9999-12-30, otherwise the error wraps
// ErrInvalidDate. After Connect the report is cached in Redis for 30
// seconds: a repeated request for the same days is answered from the cache,
// and the log shows "report cache miss" or "report cache hit".
func GetReportSummary(ctx context.Context, from, to time.Time) (ReportSummary, error) {
	from, to = startOfDay(from), startOfDay(to)
	// PostgreSQL has no year 0, so such a day must not reach the query.
	if err := checkDay(from); err != nil {
		return ReportSummary{}, err
	}
	if err := checkDay(to); err != nil {
		return ReportSummary{}, err
	}
	if to.Before(from) {
		return ReportSummary{}, fmt.Errorf("%w: from %s is after to %s", ErrInvalidRange, formatDate(from), formatDate(to))
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	totals, err := store.reportSummary(ctx, from, to)
	if err != nil {
		return ReportSummary{}, err
	}

	report := ReportSummary{From: from, To: to, Categories: totals}
	for _, t := range totals {
		report.Total += t.Amount
	}
	// Float sums like 0.1+0.2 are not exactly 0.3.
	report.Total = roundToKopecks(report.Total)
	return report, nil
}
