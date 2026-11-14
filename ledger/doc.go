// Package ledger is the business logic of the project. It keeps transactions
// and category budgets in memory, checks them with Validate and rejects
// transactions that do not fit into the budget of their category.
//
// The gateway module exposes the package over a REST API, and the ledger
// command in cmd/ledger shows it in a console demo.
package ledger
