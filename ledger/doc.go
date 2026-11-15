// Package ledger is the business logic of the project: transactions,
// category budgets with periods and a report of spending by category. It
// checks the data with Validate and rejects transactions that do not fit
// into the budget of their category.
//
// After Connect the ledger keeps transactions and budgets in PostgreSQL and
// caches report summaries and the list of budgets in Redis. Until then it
// keeps the data in memory, which the tests and the -demo mode of
// cmd/ledger rely on.
//
// The gateway module exposes the package over a REST API, and the ledger
// command in cmd/ledger prints the stored data.
package ledger
