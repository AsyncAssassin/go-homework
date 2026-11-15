package ledger

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/internal/cache"
	"github.com/AsyncAssassin/go-homework/ledger/internal/db"
)

// storage keeps the transactions and budgets behind the package functions.
// memoryStore keeps them in memory, postgresStore in PostgreSQL, and
// cachedStore adds a Redis cache to another storage. The package functions
// check and normalize the values before they reach the storage.
type storage interface {
	// addTransaction stores tx if it fits into the budget of its category
	// and returns it with its new ID.
	addTransaction(ctx context.Context, tx Transaction) (Transaction, error)
	// listTransactions returns all transactions, newest first: by date,
	// and by ID for equal dates.
	listTransactions(ctx context.Context) ([]Transaction, error)
	// setBudgets stores the budgets at once, each replacing the budget of
	// its category.
	setBudgets(ctx context.Context, list ...Budget) error
	// listBudgets returns all budgets sorted by category.
	listBudgets(ctx context.Context) ([]Budget, error)
	// budgetUsage returns every budget, sorted by category, with the amount
	// spent within its period that contains date.
	budgetUsage(ctx context.Context, date time.Time) ([]BudgetUsage, error)
	// reportSummary returns the amount spent in every category from the
	// day from to the day to, both included, sorted by category.
	reportSummary(ctx context.Context, from, to time.Time) ([]CategoryTotal, error)
	// close releases the connections of the storage.
	close() error
}

// store is the storage of the package. It keeps the data in memory until
// Connect switches it to PostgreSQL. Connect, Close and Reset change it, so
// they must not run at the same time as the other functions of the package:
// call Connect at startup and Close at shutdown.
var store storage = newMemoryStore()

// queryTimeout limits every operation on the storage.
const queryTimeout = 5 * time.Second

// queryContext returns the context for one operation on the storage.
func queryContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), queryTimeout)
}

// Connect connects the ledger to PostgreSQL and Redis and keeps all data
// there from then on: transactions and budgets in PostgreSQL, report
// summaries and the list of budgets cached in Redis. The settings come from
// the environment: DATABASE_URL or DB_HOST, DB_PORT, DB_USER, DB_PASS and
// DB_NAME for PostgreSQL, REDIS_ADDR, REDIS_DB and REDIS_PASSWORD for Redis.
// Both connections are logged. If either of them fails, or the database has
// no tables of the ledger because the migrations were not applied, Connect
// returns an error and changes nothing.
func Connect(ctx context.Context) error {
	dsn := db.DSN()
	target, err := db.Target(dsn)
	if err != nil {
		return err
	}
	pg, err := db.Open(ctx, dsn)
	if err != nil {
		return err
	}
	if err := checkTables(ctx, pg, target); err != nil {
		pg.Close()
		return err
	}
	log.Printf("connected to PostgreSQL at %s", target)

	cfg, err := cache.ConfigFromEnv()
	if err != nil {
		pg.Close()
		return err
	}
	c, err := cache.Open(ctx, cfg)
	if err != nil {
		pg.Close()
		return err
	}
	log.Printf("connected to Redis at %s", cfg)

	old := store
	store = &cachedStore{storage: &postgresStore{db: pg}, cache: c}
	// The new connections work, so a failure to close the old ones only
	// goes to the log.
	if err := old.close(); err != nil {
		log.Printf("close the previous connections: %v", err)
	}
	return nil
}

// Close closes the connections opened by Connect. After it the package
// functions return errClosed until Connect or Reset.
func Close() error {
	err := store.close()
	store = closedStore{}
	return err
}

// Reset closes the connections opened by Connect, if any, and starts over
// with an empty ledger in memory, so tests and demos can start from scratch.
// It deletes nothing from PostgreSQL.
func Reset() {
	// The old storage is not used any more, so an error closing it does not
	// matter.
	_ = store.close()
	store = newMemoryStore()
}

// errClosed is returned by the package functions after Close.
var errClosed = errors.New("ledger is closed: call Connect first")

// closedStore is the storage after Close: every operation fails.
type closedStore struct{}

func (closedStore) addTransaction(context.Context, Transaction) (Transaction, error) {
	return Transaction{}, errClosed
}

func (closedStore) listTransactions(context.Context) ([]Transaction, error) {
	return nil, errClosed
}

func (closedStore) setBudgets(context.Context, ...Budget) error {
	return errClosed
}

func (closedStore) listBudgets(context.Context) ([]Budget, error) {
	return nil, errClosed
}

func (closedStore) budgetUsage(context.Context, time.Time) ([]BudgetUsage, error) {
	return nil, errClosed
}

func (closedStore) reportSummary(context.Context, time.Time, time.Time) ([]CategoryTotal, error) {
	return nil, errClosed
}

func (closedStore) close() error {
	return nil
}
