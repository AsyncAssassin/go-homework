package ledger

// store holds the transactions and budgets behind the package functions.
var store = newMemoryStore()

// Reset removes all transactions and budgets, so tests and demos can start
// from an empty ledger.
func Reset() {
	store.reset()
}
