package ledger

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

// memoryStore keeps transactions and budgets in memory. It is safe for
// concurrent use: every method holds mu for the whole operation, so the
// budget check and the insert of a transaction cannot interleave with
// another change, and two transactions cannot both use the same remaining
// limit. Its methods never fail.
type memoryStore struct {
	mu           sync.Mutex
	transactions []Transaction     // in the order they were added: ID i is at index i-1
	budgets      map[string]Budget // keyed by category
}

func newMemoryStore() *memoryStore {
	return &memoryStore{budgets: map[string]Budget{}}
}

func (s *memoryStore) addTransaction(_ context.Context, tx Transaction) (Transaction, error) {
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

func (s *memoryStore) listTransactions(context.Context) ([]Transaction, error) {
	s.mu.Lock()
	list := slices.Clone(s.transactions)
	s.mu.Unlock()

	slices.SortFunc(list, func(a, b Transaction) int {
		if c := b.Date.Compare(a.Date); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return list, nil
}

func (s *memoryStore) setBudgets(_ context.Context, list ...Budget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range list {
		s.budgets[b.Category] = b
	}
	return nil
}

func (s *memoryStore) listBudgets(context.Context) ([]Budget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sortedBudgets(), nil
}

func (s *memoryStore) budgetUsage(_ context.Context, date time.Time) ([]BudgetUsage, error) {
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
	return usage, nil
}

func (s *memoryStore) reportSummary(_ context.Context, from, to time.Time) ([]CategoryTotal, error) {
	s.mu.Lock()
	sums := map[string]float64{}
	for _, tx := range s.transactions {
		if !tx.Date.Before(from) && !tx.Date.After(to) {
			sums[tx.Category] += tx.Amount
		}
	}
	s.mu.Unlock()

	totals := make([]CategoryTotal, 0, len(sums))
	for category, amount := range sums {
		totals = append(totals, CategoryTotal{Category: category, Amount: roundToKopecks(amount)})
	}
	slices.SortFunc(totals, func(a, b CategoryTotal) int {
		return strings.Compare(a.Category, b.Category)
	})
	return totals, nil
}

func (s *memoryStore) close() error {
	return nil
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
