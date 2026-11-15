package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// postgresStore keeps transactions and budgets in the PostgreSQL tables
// expenses and budgets, created by the migrations in the migrations
// directory. Dates are stored as calendar days in the local time zone.
type postgresStore struct {
	db *sql.DB
}

// addTransaction checks the budget and inserts tx in one database
// transaction. The budget row stays locked until the end of it, so
// transactions of one category are checked one after another and cannot
// exceed the limit together.
func (s *postgresStore) addTransaction(ctx context.Context, tx Transaction) (Transaction, error) {
	err := s.inTx(ctx, func(dbTx *sql.Tx) error {
		b, found, err := lockBudget(ctx, dbTx, tx.Category)
		if err != nil {
			return err
		}
		if found {
			spent, err := spentInPeriod(ctx, dbTx, b, tx.Date)
			if err != nil {
				return err
			}
			if err := checkBudget(b, spent, tx); err != nil {
				return err
			}
		}
		err = dbTx.QueryRowContext(ctx,
			`INSERT INTO expenses (amount, category, description, date) VALUES ($1, $2, $3, $4) RETURNING id`,
			tx.Amount, tx.Category, tx.Description, formatDate(tx.Date),
		).Scan(&tx.ID)
		if err != nil {
			return fmt.Errorf("insert the transaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return Transaction{}, err
	}
	return tx, nil
}

func (s *postgresStore) listTransactions(ctx context.Context) ([]Transaction, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, amount, category, COALESCE(description, ''), date FROM expenses ORDER BY date DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	defer rows.Close()

	var list []Transaction
	for rows.Next() {
		var tx Transaction
		var date time.Time
		if err := rows.Scan(&tx.ID, &tx.Amount, &tx.Category, &tx.Description, &date); err != nil {
			return nil, fmt.Errorf("list transactions: %w", err)
		}
		tx.Date = localDay(date)
		list = append(list, tx)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	return list, nil
}

// setBudgets inserts or updates the budgets in one database transaction.
// It writes them sorted by category, so concurrent calls lock the rows in
// the same order and cannot deadlock.
func (s *postgresStore) setBudgets(ctx context.Context, list ...Budget) error {
	list = slices.Clone(list)
	slices.SortFunc(list, func(a, b Budget) int {
		return strings.Compare(a.Category, b.Category)
	})
	return s.inTx(ctx, func(dbTx *sql.Tx) error {
		for _, b := range list {
			_, err := dbTx.ExecContext(ctx,
				`INSERT INTO budgets (category, limit_amount, period) VALUES ($1, $2, $3)
				ON CONFLICT (category) DO UPDATE SET limit_amount = EXCLUDED.limit_amount, period = EXCLUDED.period`,
				b.Category, b.Limit, string(b.Period))
			if err != nil {
				return fmt.Errorf("save the budget of %q: %w", b.Category, err)
			}
		}
		return nil
	})
}

// listBudgets sorts the budgets by the bytes of their categories, as
// memoryStore does: COLLATE "C" ignores the rules of the database locale.
func (s *postgresStore) listBudgets(ctx context.Context) ([]Budget, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, limit_amount, period FROM budgets ORDER BY category COLLATE "C"`)
	if err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	defer rows.Close()

	// Not nil, as in memory: no budgets are cached as [], not null.
	list := []Budget{}
	for rows.Next() {
		var b Budget
		if err := rows.Scan(&b.Category, &b.Limit, &b.Period); err != nil {
			return nil, fmt.Errorf("list budgets: %w", err)
		}
		list = append(list, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	return list, nil
}

func (s *postgresStore) budgetUsage(ctx context.Context, date time.Time) ([]BudgetUsage, error) {
	budgets, err := s.listBudgets(ctx)
	if err != nil {
		return nil, err
	}
	usage := make([]BudgetUsage, len(budgets))
	for i, b := range budgets {
		spent, err := spentInPeriod(ctx, s.db, b, date)
		if err != nil {
			return nil, err
		}
		usage[i] = BudgetUsage{Budget: b, PeriodLabel: b.Period.label(date), Spent: spent}
	}
	return usage, nil
}

func (s *postgresStore) reportSummary(ctx context.Context, from, to time.Time) ([]CategoryTotal, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, SUM(amount) FROM expenses WHERE date BETWEEN $1 AND $2
		GROUP BY category ORDER BY category COLLATE "C"`,
		formatDate(from), formatDate(to))
	if err != nil {
		return nil, fmt.Errorf("sum the expenses: %w", err)
	}
	defer rows.Close()

	// Not nil, as in memory: a report without expenses is cached as [], not
	// null.
	totals := []CategoryTotal{}
	for rows.Next() {
		var t CategoryTotal
		if err := rows.Scan(&t.Category, &t.Amount); err != nil {
			return nil, fmt.Errorf("sum the expenses: %w", err)
		}
		totals = append(totals, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sum the expenses: %w", err)
	}
	return totals, nil
}

func (s *postgresStore) close() error {
	return s.db.Close()
}

// checkTables returns an error if the database has no table of the ledger,
// for example because the migrations were not applied. target names the
// database in the error.
func checkTables(ctx context.Context, pool *sql.DB, target string) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	for _, table := range []string{"budgets", "expenses"} {
		var exists bool
		if err := pool.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			return fmt.Errorf("check the tables of PostgreSQL at %s: %w", target, err)
		}
		if !exists {
			return fmt.Errorf("PostgreSQL at %s has no table %s: apply the migrations with goose up, see ledger/README.md", target, table)
		}
	}
	return nil
}

// inTx runs f in a database transaction and commits it if f succeeds.
func (s *postgresStore) inTx(ctx context.Context, f func(*sql.Tx) error) error {
	dbTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin a database transaction: %w", err)
	}
	if err := f(dbTx); err != nil {
		// The error of f explains the failure; a failed rollback adds nothing.
		_ = dbTx.Rollback()
		return err
	}
	if err := dbTx.Commit(); err != nil {
		return fmt.Errorf("commit a database transaction: %w", err)
	}
	return nil
}

// lockBudget reads the budget of category and locks its row until the end
// of dbTx. found is false if the category has no budget.
func lockBudget(ctx context.Context, dbTx *sql.Tx, category string) (b Budget, found bool, err error) {
	err = dbTx.QueryRowContext(ctx,
		`SELECT category, limit_amount, period FROM budgets WHERE category = $1 FOR UPDATE`,
		category,
	).Scan(&b.Category, &b.Limit, &b.Period)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Budget{}, false, nil
	case err != nil:
		return Budget{}, false, fmt.Errorf("read the budget of %q: %w", category, err)
	}
	return b, true, nil
}

// queryRower runs a query that returns one row: *sql.DB and *sql.Tx.
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// spentInPeriod returns the amount spent in the category of b within the
// period of b that contains date.
func spentInPeriod(ctx context.Context, q queryRower, b Budget, date time.Time) (float64, error) {
	query := `SELECT COALESCE(SUM(amount), 0) FROM expenses WHERE category = $1`
	args := []any{b.Category}
	if start, end, ok := b.Period.bounds(date); ok {
		query += ` AND date >= $2 AND date < $3`
		args = append(args, formatDate(start), formatDate(end))
	}
	var spent float64
	if err := q.QueryRowContext(ctx, query, args...).Scan(&spent); err != nil {
		return 0, fmt.Errorf("sum the expenses of %q: %w", b.Category, err)
	}
	return spent, nil
}
