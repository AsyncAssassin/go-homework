package api

import (
	"errors"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger"
)

func TestToTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     CreateTransactionRequest
		want    ledger.Transaction
		wantErr error
	}{
		{
			name: "with a date",
			req:  CreateTransactionRequest{Amount: 450, Category: "еда", Description: "ланч", Date: "2025-09-10"},
			want: ledger.Transaction{
				Amount: 450, Category: "еда", Description: "ланч",
				Date: time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local),
			},
		},
		{
			name: "without a date",
			req:  CreateTransactionRequest{Amount: 450, Category: "еда"},
			want: ledger.Transaction{Amount: 450, Category: "еда"},
		},
		{
			// Fields are passed as they are: ledger checks and normalizes them.
			name: "invalid fields",
			req:  CreateTransactionRequest{Amount: -1, Category: " "},
			want: ledger.Transaction{Amount: -1, Category: " "},
		},
		{name: "date with a time", req: CreateTransactionRequest{Date: "2025-09-10T12:00:00Z"}, wantErr: ledger.ErrInvalidDate},
		{name: "no such day", req: CreateTransactionRequest{Date: "2025-02-30"}, wantErr: ledger.ErrInvalidDate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := toTransaction(tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("toTransaction() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("toTransaction() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNewTransactionResponse(t *testing.T) {
	t.Parallel()

	// The first half hour of the day in the local zone, given in UTC-12: the
	// response shows the local day, the one the budget periods use.
	date := time.Date(2025, time.September, 10, 0, 30, 0, 0, time.Local)
	tx := ledger.Transaction{
		ID: 7, Amount: 450, Category: "еда", Description: "ланч",
		Date: date.In(time.FixedZone("UTC-12", -12*60*60)),
	}

	want := TransactionResponse{ID: 7, Amount: 450, Category: "еда", Description: "ланч", Date: "2025-09-10"}
	if got := newTransactionResponse(tx); got != want {
		t.Errorf("newTransactionResponse() = %+v, want %+v", got, want)
	}
}

func TestBudgetConversion(t *testing.T) {
	t.Parallel()

	req := CreateBudgetRequest{Category: "еда", Limit: 5000, Period: "month"}
	b := ledger.Budget{Category: "еда", Limit: 5000, Period: ledger.PeriodMonth}
	if got := toBudget(req); got != b {
		t.Errorf("toBudget() = %+v, want %+v", got, b)
	}
	want := BudgetResponse{Category: "еда", Limit: 5000, Period: "month"}
	if got := newBudgetResponse(b); got != want {
		t.Errorf("newBudgetResponse() = %+v, want %+v", got, want)
	}
}

func TestEmptyListsAreNotNil(t *testing.T) {
	t.Parallel()

	// A nil slice would be encoded as null instead of [].
	if got := newTransactionResponses(nil); got == nil {
		t.Error("newTransactionResponses(nil) = nil, want an empty slice")
	}
	if got := newBudgetResponses(nil); got == nil {
		t.Error("newBudgetResponses(nil) = nil, want an empty slice")
	}
}
