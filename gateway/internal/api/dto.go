package api

import (
	"fmt"
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

// toTransaction converts a request to a ledger transaction. A date is a day
// in the local time zone, the one ledger counts budget periods in. Without
// a date the transaction gets a zero Date, which ledger replaces with the
// current time.
func toTransaction(req CreateTransactionRequest) (ledger.Transaction, error) {
	tx := ledger.Transaction{
		Amount:      req.Amount,
		Category:    req.Category,
		Description: req.Description,
	}
	if req.Date == "" {
		return tx, nil
	}
	date, err := time.ParseInLocation(dateLayout, req.Date, time.Local)
	if err != nil {
		return ledger.Transaction{}, fmt.Errorf("%w %q: must be YYYY-MM-DD", ledger.ErrInvalidDate, req.Date)
	}
	// For ledger the zero time means "no date", so in UTC the date 0001-01-01
	// would silently become today.
	if date.IsZero() {
		return ledger.Transaction{}, fmt.Errorf("%w %s: out of range", ledger.ErrInvalidDate, req.Date)
	}
	tx.Date = date
	return tx, nil
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
