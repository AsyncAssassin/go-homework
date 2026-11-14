// Package api is the HTTP layer of the gateway. It converts JSON requests
// to ledger models, calls the ledger package and converts the results back
// to JSON responses.
package api

import "net/http"

// NewRouter returns the HTTP handler of the gateway: GET /ping and the REST
// API of the ledger under /api. Every response has the JSON Content-Type,
// every error is {"error": "..."}, and every request is logged.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", handlePing)
	mux.HandleFunc("/ping", methodNotAllowed("GET, HEAD"))
	mux.Handle("/api/", http.StripPrefix("/api", newAPIRouter()))
	// Without this route ServeMux would redirect /api to /api/.
	mux.HandleFunc("/api", handleNotFound)
	mux.HandleFunc("/", handleNotFound)
	return Logging(Recover(JSONContentType(mux)))
}

// newAPIRouter returns the routes of the REST API. Their paths are relative
// to /api. A route without a method takes the methods the other routes of
// its path do not; without it ServeMux would answer them with a plain-text
// 405.
func newAPIRouter() http.Handler {
	const allowed = "GET, HEAD, POST"

	mux := http.NewServeMux()
	mux.HandleFunc("GET /transactions", handleListTransactions)
	mux.HandleFunc("POST /transactions", handleCreateTransaction)
	mux.HandleFunc("/transactions", methodNotAllowed(allowed))
	mux.HandleFunc("GET /budgets", handleListBudgets)
	mux.HandleFunc("POST /budgets", handleSetBudget)
	mux.HandleFunc("/budgets", methodNotAllowed(allowed))
	mux.HandleFunc("/", handleNotFound)
	return mux
}

// handleNotFound answers 404 for a path without a route.
func handleNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

// methodNotAllowed returns a handler that answers 405 and lists the
// allowed methods in the Allow header.
func methodNotAllowed(allowed string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", allowed)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
