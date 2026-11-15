package api

import (
	"fmt"
	"net/url"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger"
)

// dateLayout is the format of dates in requests and responses.
const dateLayout = time.DateOnly

// CreateTransactionRequest is the body of POST /api/transactions.
type CreateTransactionRequest struct {
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Date        string  `json:"date"` // YYYY-MM-DD; empty means today
}

// TransactionResponse is a stored transaction as the API returns it.
type TransactionResponse struct {
	ID          int     `json:"id"`
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Date        string  `json:"date"` // YYYY-MM-DD
}

// CreateBudgetRequest is the body of POST /api/budgets. Period is optional:
// "month" and "year" give a separate limit for every calendar month or
// year, and an empty period gives one limit for all time.
type CreateBudgetRequest struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
	Period   string  `json:"period"`
}

// BudgetResponse is a stored budget as the API returns it. A budget without
// a period has no "period" field.
type BudgetResponse struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
	Period   string  `json:"period,omitempty"`
}

// ReportSummaryResponse is the answer to GET /api/reports/summary.
type ReportSummaryResponse struct {
	From       string                  `json:"from"` // YYYY-MM-DD
	To         string                  `json:"to"`   // YYYY-MM-DD, included
	Categories []CategoryTotalResponse `json:"categories"`
	Total      float64                 `json:"total"`
}

// CategoryTotalResponse is the amount spent in one category of a report.
type CategoryTotalResponse struct {
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
}

// toTransaction converts a request to a ledger transaction. Without a date
// the transaction gets a zero Date, which ledger replaces with today.
func toTransaction(req CreateTransactionRequest) (ledger.Transaction, error) {
	tx := ledger.Transaction{
		Amount:      req.Amount,
		Category:    req.Category,
		Description: req.Description,
	}
	if req.Date == "" {
		return tx, nil
	}
	date, err := parseDate(req.Date)
	if err != nil {
		return ledger.Transaction{}, err
	}
	tx.Date = date
	return tx, nil
}

// parseDate parses a YYYY-MM-DD date as the start of that day in the local
// time zone, the one ledger counts budget periods in.
func parseDate(s string) (time.Time, error) {
	date, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w %q: must be YYYY-MM-DD", ledger.ErrInvalidDate, s)
	}
	// 0001-01-01 is the zero time in UTC, and for ledger the zero time means
	// "no date", so it would silently become today. ledger rejects this day
	// in every time zone anyway.
	if date.IsZero() {
		return time.Time{}, fmt.Errorf("%w %s: out of range", ledger.ErrInvalidDate, s)
	}
	// time.ParseInLocation would give 23:00 of the previous day on a day
	// without midnight, such as 2026-09-06 in America/Santiago.
	y, m, d := date.Date()
	return ledger.Day(y, m, d), nil
}

// reportRange returns the first and the last day of a report from the query
// parameters from and to, YYYY-MM-DD. Without both the report covers the
// month of now. With only one of them it covers the rest of that day's
// month: from=2025-09-10 means up to 2025-09-30, to=2025-09-10 means from
// 2025-09-01. The added day stays within the range of ledger, see monthOf.
func reportRange(q url.Values, now time.Time) (from, to time.Time, err error) {
	fromParam, toParam := q.Get("from"), q.Get("to")
	if fromParam != "" {
		if from, err = parseDate(fromParam); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if toParam != "" {
		if to, err = parseDate(toParam); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	switch {
	case fromParam == "" && toParam == "":
		from, to = monthOf(now)
	case fromParam == "":
		from, _ = monthOf(to)
	case toParam == "":
		_, to = monthOf(from)
	}
	return from, to, nil
}

// monthOf returns the first and the last day of the month of t in the local
// time zone. Reports of ledger cover days from 0001-01-02 to 9999-12-30, so
// the months at the ends of this range are cut to it.
func monthOf(t time.Time) (first, last time.Time) {
	y, m, _ := t.In(time.Local).Date()
	first, last = ledger.Day(y, m, 1), ledger.Day(y, m+1, 0)
	if y == 1 && m == time.January {
		first = ledger.Day(1, time.January, 2)
	}
	if y == 9999 && m == time.December {
		last = ledger.Day(9999, time.December, 30)
	}
	return first, last
}

// newTransactionResponse converts a stored transaction to a response.
func newTransactionResponse(tx ledger.Transaction) TransactionResponse {
	return TransactionResponse{
		ID:          tx.ID,
		Amount:      tx.Amount,
		Category:    tx.Category,
		Description: tx.Description,
		Date:        tx.Date.In(time.Local).Format(dateLayout),
	}
}

// newTransactionResponses converts stored transactions to responses. The
// result is never nil, so no transactions give [] in JSON, not null.
func newTransactionResponses(txs []ledger.Transaction) []TransactionResponse {
	list := make([]TransactionResponse, len(txs))
	for i, tx := range txs {
		list[i] = newTransactionResponse(tx)
	}
	return list
}

// toBudget converts a request to a ledger budget.
func toBudget(req CreateBudgetRequest) ledger.Budget {
	return ledger.Budget{
		Category: req.Category,
		Limit:    req.Limit,
		Period:   ledger.Period(req.Period),
	}
}

// newBudgetResponse converts a stored budget to a response.
func newBudgetResponse(b ledger.Budget) BudgetResponse {
	return BudgetResponse{
		Category: b.Category,
		Limit:    b.Limit,
		Period:   string(b.Period),
	}
}

// newBudgetResponses converts stored budgets to responses. The result is
// never nil, so no budgets give [] in JSON, not null.
func newBudgetResponses(budgets []ledger.Budget) []BudgetResponse {
	list := make([]BudgetResponse, len(budgets))
	for i, b := range budgets {
		list[i] = newBudgetResponse(b)
	}
	return list
}

// newReportSummaryResponse converts a report to a response. The list of
// categories is never nil, so a report without expenses gives [] in JSON.
func newReportSummaryResponse(r ledger.ReportSummary) ReportSummaryResponse {
	categories := make([]CategoryTotalResponse, len(r.Categories))
	for i, c := range r.Categories {
		categories[i] = CategoryTotalResponse{Category: c.Category, Amount: c.Amount}
	}
	return ReportSummaryResponse{
		From:       r.From.Format(dateLayout),
		To:         r.To.Format(dateLayout),
		Categories: categories,
		Total:      r.Total,
	}
}
