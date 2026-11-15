package ledger

import (
	"context"
	"errors"
	"testing"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
	"github.com/AsyncAssassin/go-homework/ledger/internal/repository/memory"
	"github.com/AsyncAssassin/go-homework/ledger/internal/repository/pg"
)

func TestRepositoryTransactionRollback(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) (domain.TransactionRunner, domain.ExpenseRepository)
	}{
		{"memory", func(*testing.T) (domain.TransactionRunner, domain.ExpenseRepository) { r := memory.New(); return r, r }},
		{"postgres", func(t *testing.T) (domain.TransactionRunner, domain.ExpenseRepository) {
			s := connectTest(t)
			r := pg.New(s.db)
			return r, r
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, repo := tt.open(t)
			failure := errors.New("callback failed")
			for _, mode := range []string{"error", "panic", "cancel"} {
				t.Run(mode, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					var err error
					func() {
						defer func() {
							got := recover()
							if mode == "panic" && got != failure {
								t.Errorf("panic = %v", got)
							}
							if mode != "panic" && got != nil {
								panic(got)
							}
						}()
						err = runner.WithinTransaction(ctx, func(_ domain.BudgetRepository, expenses domain.ExpenseRepository) error {
							_, err := expenses.AddTransaction(ctx, domain.Transaction{Amount: 1, Category: "еда", Date: domain.Day(2026, 11, 15)})
							if err != nil {
								return err
							}
							switch mode {
							case "panic":
								panic(failure)
							case "cancel":
								cancel()
								return nil
							default:
								return failure
							}
						})
					}()
					if mode == "error" && !errors.Is(err, failure) {
						t.Errorf("error = %v", err)
					}
					if mode == "cancel" && err == nil {
						t.Error("canceled transaction committed")
					}
					list, err := repo.ListTransactions(context.Background())
					if err != nil || len(list) != 0 {
						t.Fatalf("rollback left data or resources locked: %v, %v", list, err)
					}
				})
			}
		})
	}
}
