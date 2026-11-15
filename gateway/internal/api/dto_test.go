package api

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
)

func TestToTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     CreateTransactionRequest
		want    domain.Transaction
		wantErr error
	}{
		{
			name: "with a date",
			req:  CreateTransactionRequest{Amount: 450, Category: "еда", Description: "ланч", Date: "2025-09-10"},
			want: domain.Transaction{
				Amount: 450, Category: "еда", Description: "ланч",
				Date: time.Date(2025, time.September, 10, 0, 0, 0, 0, time.Local),
			},
		},
		{
			// In America/Santiago the clocks skip midnight of this day.
			name: "day without midnight",
			req:  CreateTransactionRequest{Amount: 450, Category: "еда", Date: "2026-09-06"},
			want: domain.Transaction{Amount: 450, Category: "еда", Date: domain.Day(2026, time.September, 6)},
		},
		{
			name: "without a date",
			req:  CreateTransactionRequest{Amount: 450, Category: "еда"},
			want: domain.Transaction{Amount: 450, Category: "еда"},
		},
		{
			// Fields are passed as they are: ledger checks and normalizes them.
			name: "invalid fields",
			req:  CreateTransactionRequest{Amount: -1, Category: " "},
			want: domain.Transaction{Amount: -1, Category: " "},
		},
		{name: "date with a time", req: CreateTransactionRequest{Date: "2025-09-10T12:00:00Z"}, wantErr: domain.ErrInvalidDate},
		{name: "no such day", req: CreateTransactionRequest{Date: "2025-02-30"}, wantErr: domain.ErrInvalidDate},
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
			// The date stays the same day in the local time zone.
			if day := got.Date.In(time.Local).Format(dateLayout); tt.wantErr == nil && tt.req.Date != "" && day != tt.req.Date {
				t.Errorf("toTransaction() date = %s, want %s", day, tt.req.Date)
			}
		})
	}
}

func TestNewTransactionResponse(t *testing.T) {
	t.Parallel()

	// The first half hour of the day in the local zone, given in UTC-12: the
	// response shows the local day, the one the budget periods use.
	date := time.Date(2025, time.September, 10, 0, 30, 0, 0, time.Local)
	tx := domain.Transaction{
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
	b := domain.Budget{Category: "еда", Limit: 5000, Period: domain.PeriodMonth}
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

func TestReportRange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 14, 18, 0, 0, 0, time.Local)
	day := domain.Day
	tests := []struct {
		name     string
		query    url.Values
		wantFrom time.Time
		wantTo   time.Time
		wantErr  error
	}{
		{name: "current month by default", wantFrom: day(2026, time.February, 1), wantTo: day(2026, time.February, 28)},
		{
			name:     "both days",
			query:    url.Values{"from": {"2025-09-01"}, "to": {"2025-09-30"}},
			wantFrom: day(2025, time.September, 1),
			wantTo:   day(2025, time.September, 30),
		},
		{
			name:     "only the first day: to the end of its month",
			query:    url.Values{"from": {"2026-01-15"}},
			wantFrom: day(2026, time.January, 15),
			wantTo:   day(2026, time.January, 31),
		},
		{
			name:     "only the first day of a later month",
			query:    url.Values{"from": {"2026-12-01"}},
			wantFrom: day(2026, time.December, 1),
			wantTo:   day(2026, time.December, 31),
		},
		{
			name:     "only the last day: from the start of its month",
			query:    url.Values{"to": {"2025-09-10"}},
			wantFrom: day(2025, time.September, 1),
			wantTo:   day(2025, time.September, 10),
		},
		{
			// In America/Santiago the clocks skip midnight of this day.
			name:     "day without midnight",
			query:    url.Values{"from": {"2026-09-06"}, "to": {"2026-09-06"}},
			wantFrom: day(2026, time.September, 6),
			wantTo:   day(2026, time.September, 6),
		},
		{
			// Reports start from 0001-01-02.
			name:     "only the last day in the first month",
			query:    url.Values{"to": {"0001-01-15"}},
			wantFrom: day(1, time.January, 2),
			wantTo:   day(1, time.January, 15),
		},
		{
			// Reports end on 9999-12-30.
			name:     "only the first day in the last month",
			query:    url.Values{"from": {"9999-12-15"}},
			wantFrom: day(9999, time.December, 15),
			wantTo:   day(9999, time.December, 30),
		},
		{name: "invalid first day", query: url.Values{"from": {"01.02.2026"}}, wantErr: domain.ErrInvalidDate},
		{name: "invalid last day", query: url.Values{"to": {"2026-02-30"}}, wantErr: domain.ErrInvalidDate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			from, to, err := reportRange(tt.query, now)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("reportRange() error = %v, want %v", err, tt.wantErr)
			}
			if !from.Equal(tt.wantFrom) || !to.Equal(tt.wantTo) {
				t.Errorf("reportRange() = %v, %v; want %v, %v", from, to, tt.wantFrom, tt.wantTo)
			}
			// The days given stay the same days in the local time zone.
			for param, got := range map[string]time.Time{"from": from, "to": to} {
				day := got.In(time.Local).Format(dateLayout)
				if want := tt.query.Get(param); tt.wantErr == nil && want != "" && day != want {
					t.Errorf("reportRange() %s = %s, want %s", param, day, want)
				}
			}
		})
	}
}
