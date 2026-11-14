package ledger

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"time"
)

// memoryStore keeps transactions and budgets in memory. It is safe for
// concurrent use: every method holds mu for the whole operation, so the
// budget check and the insert of a transaction cannot interleave with
// another change, and two transactions cannot both use the same remaining
// limit.
type memoryStore struct {
	mu           sync.Mutex
	transactions []Transaction     // in the order they were added: ID i is at index i-1
	budgets      map[string]Budget // keyed by category
}

func newMemoryStore() *memoryStore {
	return &memoryStore{budgets: map[string]Budget{}}
}

// reset removes all transactions and budgets.
func (s *memoryStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transactions = nil
	s.budgets = map[string]Budget{}
}

// addTransaction stores tx with the next ID if it fits into the budget of
// its category and returns the stored transaction.
func (s *memoryStore) addTransaction(tx Transaction) (Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.budgets[tx.Category]; ok {
		if err := checkBudget(b, spentIn(s.transactions, b, tx.Date), tx); err != nil {
			return Transaction{}, err
		}
	}
	tx.ID = len(s.transactions) + 1
	s.transactions = append(s.transactions, tx)
	return tx, nil
}

// listTransactions returns a copy of the transactions, newest first: by
// date, and by ID for equal dates.
func (s *memoryStore) listTransactions() []Transaction {
	s.mu.Lock()
	list := slices.Clone(s.transactions)
	s.mu.Unlock()

	slices.SortFunc(list, func(a, b Transaction) int {
		if c := b.Date.Compare(a.Date); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return list
}

// setBudgets stores the budgets at once, each replacing the budget of its
// category.
func (s *memoryStore) setBudgets(list ...Budget) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range list {
		s.budgets[b.Category] = b
	}
}

// listBudgets returns all budgets sorted by category.
func (s *memoryStore) listBudgets() []Budget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sortedBudgets()
}

// budgetUsage returns every budget, sorted by category, with the amount
// spent within its period that contains date.
func (s *memoryStore) budgetUsage(date time.Time) []BudgetUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.sortedBudgets()
	usage := make([]BudgetUsage, len(list))
	for i, b := range list {
		usage[i] = BudgetUsage{
			Budget:      b,
			PeriodLabel: b.Period.label(date),
			Spent:       spentIn(s.transactions, b, date),
		}
	}
	return usage
}

// sortedBudgets returns the budgets sorted by category. The caller must
// hold s.mu.
func (s *memoryStore) sortedBudgets() []Budget {
	list := make([]Budget, 0, len(s.budgets))
	for _, b := range s.budgets {
		list = append(list, b)
	}
	slices.SortFunc(list, func(a, b Budget) int {
		return strings.Compare(a.Category, b.Category)
	})
	return list
}
