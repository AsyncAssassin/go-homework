package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
	"github.com/AsyncAssassin/go-homework/ledger/service"
)

type failingService struct{ service.Service }

var errStorage = errors.New("storage unavailable")

func (failingService) AddTransaction(context.Context, domain.Transaction) (domain.Transaction, error) {
	return domain.Transaction{}, errStorage
}
func (failingService) ListTransactions(context.Context) ([]domain.Transaction, error) {
	return nil, errStorage
}
func (failingService) SetBudget(context.Context, domain.Budget) (domain.Budget, error) {
	return domain.Budget{}, errStorage
}
func (failingService) ListBudgets(context.Context) ([]domain.Budget, error) {
	return nil, errStorage
}
func (failingService) GetReportSummary(context.Context, time.Time, time.Time) (domain.ReportSummary, error) {
	return domain.ReportSummary{}, errStorage
}

type transactionService struct {
	service.Service
	add func(context.Context, domain.Transaction) (domain.Transaction, error)
}

func (s transactionService) AddTransaction(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	return s.add(ctx, tx)
}

func TestHandlerUsesInjectedService(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request")
	called := false
	svc := transactionService{add: func(got context.Context, tx domain.Transaction) (domain.Transaction, error) {
		called = true
		if got.Value(contextKey{}) != "request" {
			t.Error("request context value was lost")
		}
		if _, ok := got.Deadline(); !ok {
			t.Error("request deadline is missing")
		}
		if tx.Amount != -1 || tx.Category != " Еда " {
			t.Errorf("handler changed the domain input: %+v", tx)
		}
		return domain.Transaction{}, fmt.Errorf("validation: %w", domain.ErrInvalidAmount)
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/transactions", strings.NewReader(`{"amount":-1,"category":" Еда "}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	NewRouter(svc, DefaultRequestTimeout).ServeHTTP(rec, req)
	if !called {
		t.Fatal("service was not called")
	}
	checkResponse(t, rec, http.StatusBadRequest, `{"error":"validation: invalid amount"}`)
}
