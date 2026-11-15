package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
	"github.com/AsyncAssassin/go-homework/ledger/service"
)

type handlers struct{ service service.Service }

// pingResponse is the body of the answer to GET /ping.
type pingResponse struct {
	Message string `json:"message"`
}

// handlePing answers {"message": "pong"} so clients can check that the
// gateway is up.
func handlePing(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, pingResponse{Message: "pong"})
}

// handleCreateTransaction serves POST /api/transactions: 201 with the
// stored transaction, 400 for an invalid one, 409 if it does not fit into
// the budget of its category.
func (h *handlers) handleCreateTransaction(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[CreateTransactionRequest](w, r)
	if !ok {
		return
	}
	tx, err := toTransaction(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	stored, err := h.service.AddTransaction(r.Context(), tx)
	if r.Context().Err() != nil {
		err = r.Context().Err()
	}
	switch {
	case isValidationError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrBudgetExceeded):
		writeError(w, http.StatusConflict, "budget exceeded")
	case err != nil:
		writeServiceError(w, r, err)
	default:
		writeJSON(w, http.StatusCreated, newTransactionResponse(stored))
	}
}

// handleListTransactions serves GET /api/transactions: 200 with all
// transactions, newest first.
func (h *handlers) handleListTransactions(w http.ResponseWriter, r *http.Request) {
	txs, err := h.service.ListTransactions(r.Context())
	if r.Context().Err() != nil {
		err = r.Context().Err()
	}
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTransactionResponses(txs))
}

// handleSetBudget serves POST /api/budgets: 201 with the stored budget,
// whether it is new or replaces the budget of the same category, and 400
// for an invalid one.
func (h *handlers) handleSetBudget(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[CreateBudgetRequest](w, r)
	if !ok {
		return
	}
	b := toBudget(req)

	stored, err := h.service.SetBudget(r.Context(), b)
	if r.Context().Err() != nil {
		err = r.Context().Err()
	}
	if isValidationError(err) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newBudgetResponse(stored))
}

// handleListBudgets serves GET /api/budgets: 200 with all budgets sorted
// by category.
func (h *handlers) handleListBudgets(w http.ResponseWriter, r *http.Request) {
	budgets, err := h.service.ListBudgets(r.Context())
	if r.Context().Err() != nil {
		err = r.Context().Err()
	}
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newBudgetResponses(budgets))
}

// handleReportSummary serves GET /api/reports/summary?from=YYYY-MM-DD&to=YYYY-MM-DD:
// 200 with the amount spent in every category from the day from to the day
// to, both included, and 400 for an invalid date or a range that ends
// before it starts. The default range is described at reportRange. The
// ledger caches the report in Redis for 30 seconds.
func (h *handlers) handleReportSummary(w http.ResponseWriter, r *http.Request) {
	from, to, err := reportRange(r.URL.Query(), time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	report, err := h.service.GetReportSummary(r.Context(), from, to)
	if r.Context().Err() != nil {
		err = r.Context().Err()
	}
	switch {
	case errors.Is(err, domain.ErrInvalidRange), errors.Is(err, domain.ErrInvalidDate):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeServiceError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, newReportSummaryResponse(report))
	}
}

// isValidationError maps service validation failures to the existing HTTP 400.
func isValidationError(err error) bool {
	return errors.Is(err, domain.ErrInvalidAmount) || errors.Is(err, domain.ErrEmptyCategory) ||
		errors.Is(err, domain.ErrInvalidCategory) || errors.Is(err, domain.ErrInvalidDescription) ||
		errors.Is(err, domain.ErrInvalidDate) || errors.Is(err, domain.ErrInvalidLimit) || errors.Is(err, domain.ErrInvalidPeriod)
}
