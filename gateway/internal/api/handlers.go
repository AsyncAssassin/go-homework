package api

import (
	"errors"
	"net/http"

	"github.com/AsyncAssassin/go-homework/ledger"
)

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
func handleCreateTransaction(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[CreateTransactionRequest](w, r)
	if !ok {
		return
	}
	tx, err := toTransaction(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := tx.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	stored, err := ledger.AddTransaction(tx)
	switch {
	case errors.Is(err, ledger.ErrBudgetExceeded):
		writeError(w, http.StatusConflict, "budget exceeded")
	case err != nil:
		writeInternalError(w, err)
	default:
		writeJSON(w, http.StatusCreated, newTransactionResponse(stored))
	}
}

// handleListTransactions serves GET /api/transactions: 200 with all
// transactions, newest first.
func handleListTransactions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, newTransactionResponses(ledger.ListTransactions()))
}

// handleSetBudget serves POST /api/budgets: 201 with the stored budget,
// whether it is new or replaces the budget of the same category, and 400
// for an invalid one.
func handleSetBudget(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[CreateBudgetRequest](w, r)
	if !ok {
		return
	}
	b := toBudget(req)
	if err := b.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	stored, err := ledger.SetBudget(b)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newBudgetResponse(stored))
}

// handleListBudgets serves GET /api/budgets: 200 with all budgets sorted
// by category.
func handleListBudgets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, newBudgetResponses(ledger.ListBudgets()))
}
