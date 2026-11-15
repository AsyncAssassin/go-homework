package api

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger"
	"github.com/AsyncAssassin/go-homework/ledger/domain"
	"github.com/AsyncAssassin/go-homework/ledger/service"
)

type canceledService struct {
	service.Service
	wait func(context.Context) error
}

func (s canceledService) AddTransaction(ctx context.Context, _ domain.Transaction) (domain.Transaction, error) {
	return domain.Transaction{}, s.wait(ctx)
}
func (s canceledService) ListTransactions(ctx context.Context) ([]domain.Transaction, error) {
	return nil, s.wait(ctx)
}
func (s canceledService) SetBudget(ctx context.Context, _ domain.Budget) (domain.Budget, error) {
	return domain.Budget{}, s.wait(ctx)
}
func (s canceledService) ListBudgets(ctx context.Context) ([]domain.Budget, error) {
	return nil, s.wait(ctx)
}
func (s canceledService) GetReportSummary(ctx context.Context, _, _ time.Time) (domain.ReportSummary, error) {
	return domain.ReportSummary{}, s.wait(ctx)
}

func TestAPITimeout(t *testing.T) {
	routes := []struct{ method, path, body string }{
		{"GET", "/api/transactions", ""},
		{"POST", "/api/transactions", `{"amount":10,"category":"food"}`},
		{"GET", "/api/budgets", ""},
		{"POST", "/api/budgets", `{"limit":100,"category":"food"}`},
		{"GET", "/api/reports/summary?from=2026-11-01&to=2026-11-30", ""},
	}
	for _, route := range routes {
		for _, mode := range []string{"deadline", "parent cancellation", "service cancellation", "late success"} {
			t.Run(route.method+route.path+"/"+mode, func(t *testing.T) {
				parent, cancel := context.WithCancel(context.Background())
				defer cancel()
				calls := 0
				svc := canceledService{wait: func(ctx context.Context) error {
					calls++
					if _, ok := ctx.Deadline(); !ok {
						t.Error("service received no deadline")
					}
					if mode == "parent cancellation" || mode == "late success" {
						cancel()
					}
					if mode == "service cancellation" {
						return fmt.Errorf("query: %w", context.Canceled)
					}
					<-ctx.Done()
					if mode == "late success" {
						return nil
					}
					return fmt.Errorf("query: %w", ctx.Err())
				}}
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body)).WithContext(parent)
				NewRouter(svc, 10*time.Millisecond).ServeHTTP(rec, req)
				checkResponse(t, rec, http.StatusGatewayTimeout, `{"error":"request timed out"}`)
				if calls != 1 {
					t.Errorf("service calls = %d", calls)
				}
			})
		}
	}
}

func TestTimeoutPreservesEarlierDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	parentDeadline, _ := parent.Deadline()
	called := false
	handler := Timeout(DefaultRequestTimeout, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		got, _ := r.Context().Deadline()
		if got != parentDeadline {
			t.Errorf("deadline = %v, want %v", got, parentDeadline)
		}
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil).WithContext(parent))
	if !called {
		t.Fatal("handler was not called")
	}
	cancel()
	rec := httptest.NewRecorder()
	called = false
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil).WithContext(parent))
	checkResponse(t, rec, http.StatusGatewayTimeout, `{"error":"request timed out"}`)
	if called {
		t.Fatal("handler called for an already canceled request")
	}
}

func TestSlowRequestBodyTimeout(t *testing.T) {
	srv := httptest.NewServer(NewRouter(ledger.NewMemory(), 40*time.Millisecond))
	defer srv.Close()
	conn, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// Send valid headers but leave the declared body incomplete. With only
	// WithTimeout, Decode would still be blocked in Read after the deadline.
	if _, err := fmt.Fprint(conn, "POST /api/transactions HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\nContent-Type: application/json\r\n\r\n{"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	checkResult(t, response, http.StatusGatewayTimeout, `{"error":"request timed out"}`)
}
