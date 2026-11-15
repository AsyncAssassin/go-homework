package domain

import (
	"errors"
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
