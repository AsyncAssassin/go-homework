package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger"
)

// The handlers share the ledger storage, so these tests reset it and do not
// run in parallel.

// serve sends a request with the given body ("" for none) through the
// router and returns the recorded response.
func serve(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, httptest.NewRequest(method, path, r))
	return rec
}

// checkResponse checks the status, the JSON Content-Type and, unless
// wantBody is empty, the exact body without the final newline.
func checkResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantBody string) {
	t.Helper()
	body := strings.TrimSuffix(rec.Body.String(), "\n")
	if rec.Code != wantStatus {
		t.Errorf("status = %d, want %d; body %s", rec.Code, wantStatus, body)
	}
	if got := rec.Header().Get("Content-Type"); got != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeJSON)
	}
	if wantBody != "" && body != wantBody {
		t.Errorf("body = %s, want %s", body, wantBody)
	}
}

func TestCreateTransaction(t *testing.T) {
	const lunch = `{"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}`

	tests := []struct {
		name       string
		budget     *ledger.Budget // set before the request
		body       string
		wantStatus int
		wantBody   string // "" to check only the status
	}{
		{
			name:       "created",
			body:       lunch,
			wantStatus: http.StatusCreated,
			wantBody:   `{"id":1,"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}`,
		},
		{
			name:       "fields are normalized",
			body:       `{"amount":99.999,"category":" Еда ","description":" ланч ","date":"2025-09-10"}`,
			wantStatus: http.StatusCreated,
			wantBody:   `{"id":1,"amount":100,"category":"еда","description":"ланч","date":"2025-09-10"}`,
		},
		{
			name:       "exactly up to the budget",
			budget:     &ledger.Budget{Category: "еда", Limit: 450, Period: ledger.PeriodMonth},
			body:       lunch,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "budget exceeded",
			budget:     &ledger.Budget{Category: "еда", Limit: 449.99},
			body:       lunch,
			wantStatus: http.StatusConflict,
			wantBody:   `{"error":"budget exceeded"}`,
		},
		{
			name:       "zero amount",
			body:       `{"amount":0,"category":"еда"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid amount 0: must be from 0.01 to 1e+12"}`,
		},
		{
			name:       "no category",
			body:       `{"amount":450}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"category must not be empty"}`,
		},
		{
			name:       "date in another format",
			body:       `{"amount":450,"category":"еда","date":"10.09.2025"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid date \"10.09.2025\": must be YYYY-MM-DD"}`,
		},
		{
			name:       "date out of range",
			body:       `{"amount":450,"category":"еда","date":"0000-03-01"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid date 0000-03-01: must be from 0001-01-02 to 9999-12-30 UTC"}`,
		},
		{
			// In UTC this date is the zero time, which ledger reads as "no date".
			name:       "first day of the year 1",
			body:       `{"amount":450,"category":"еда","date":"0001-01-01"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid JSON",
			body:       `{"amount":`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid JSON"}`,
		},
		{
			name:       "empty body",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"request body is empty"}`,
		},
		{
			name:       "amount as a string",
			body:       `{"amount":"450","category":"еда"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"field \"amount\" must be a number"}`,
		},
		{
			name:       "category as a number",
			body:       `{"amount":450,"category":1}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"field \"category\" must be a string"}`,
		},
		{
			name:       "unknown field",
			body:       `{"amount":450,"category":"еда","note":"x"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"unknown field \"note\""}`,
		},
		{
			name:       "amount too large for a number",
			body:       `{"amount":1e400,"category":"еда"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"field \"amount\" is out of range"}`,
		},
		{
			name:       "null instead of an object",
			body:       `null`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"request body must be a JSON object"}`,
		},
		{
			name:       "array instead of an object",
			body:       `[]`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"request body must be a JSON object"}`,
		},
		{
			name:       "two objects",
			body:       lunch + ` {}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"request body must contain a single JSON object"}`,
		},
		{
			name:       "body too large",
			body:       `{"description":"` + strings.Repeat("x", maxBodyBytes) + `"}`,
			wantStatus: http.StatusRequestEntityTooLarge,
			wantBody:   `{"error":"request body is too large: the limit is 1048576 bytes"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledger.Reset()
			if tt.budget != nil {
				if _, err := ledger.SetBudget(*tt.budget); err != nil {
					t.Fatalf("SetBudget() error = %v", err)
				}
			}

			rec := serve(t, http.MethodPost, "/api/transactions", tt.body)
			checkResponse(t, rec, tt.wantStatus, tt.wantBody)

			// Only a created transaction is stored.
			wantStored := 0
			if tt.wantStatus == http.StatusCreated {
				wantStored = 1
			}
			if got := len(ledger.ListTransactions()); got != wantStored {
				t.Errorf("stored %d transactions, want %d", got, wantStored)
			}
		})
	}
}

func TestCreateTransactionWithoutDate(t *testing.T) {
	ledger.Reset()
	before := time.Now().Format(dateLayout)
	rec := serve(t, http.MethodPost, "/api/transactions", `{"amount":450,"category":"еда"}`)
	after := time.Now().Format(dateLayout)
	checkResponse(t, rec, http.StatusCreated, "")

	var got TransactionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Date != before && got.Date != after {
		t.Errorf("date = %q, want today: %q", got.Date, before)
	}
}

func TestListTransactions(t *testing.T) {
	ledger.Reset()
	checkResponse(t, serve(t, http.MethodGet, "/api/transactions", ""), http.StatusOK, `[]`)

	for _, body := range []string{
		`{"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}`,
		`{"amount":120,"category":"транспорт","date":"2025-09-11"}`,
	} {
		checkResponse(t, serve(t, http.MethodPost, "/api/transactions", body), http.StatusCreated, "")
	}

	// Newest first.
	checkResponse(t, serve(t, http.MethodGet, "/api/transactions", ""), http.StatusOK,
		`[{"id":2,"amount":120,"category":"транспорт","description":"","date":"2025-09-11"},`+
			`{"id":1,"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}]`)
}

func TestSetBudget(t *testing.T) {
	// Every case starts with this budget.
	initial := ledger.Budget{Category: "транспорт", Limit: 2000}

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantBody   string
		want       []ledger.Budget // budgets after the request
	}{
		{
			name:       "created",
			body:       `{"category":"еда","limit":5000}`,
			wantStatus: http.StatusCreated,
			wantBody:   `{"category":"еда","limit":5000}`,
			want:       []ledger.Budget{{Category: "еда", Limit: 5000}, initial},
		},
		{
			name:       "created with a period",
			body:       `{"category":" Отпуск ","limit":90000,"period":" Year "}`,
			wantStatus: http.StatusCreated,
			wantBody:   `{"category":"отпуск","limit":90000,"period":"year"}`,
			want:       []ledger.Budget{{Category: "отпуск", Limit: 90000, Period: ledger.PeriodYear}, initial},
		},
		{
			name:       "updated",
			body:       `{"category":"транспорт","limit":2500,"period":"month"}`,
			wantStatus: http.StatusCreated,
			wantBody:   `{"category":"транспорт","limit":2500,"period":"month"}`,
			want:       []ledger.Budget{{Category: "транспорт", Limit: 2500, Period: ledger.PeriodMonth}},
		},
		{
			name:       "zero limit",
			body:       `{"category":"еда","limit":0}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid limit 0 for \"еда\": must be from 0.01 to 1e+12"}`,
			want:       []ledger.Budget{initial},
		},
		{
			name:       "no category",
			body:       `{"limit":5000}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"category must not be empty"}`,
			want:       []ledger.Budget{initial},
		},
		{
			name:       "unknown period",
			body:       `{"category":"еда","limit":5000,"period":"week"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid period \"week\" for \"еда\": must be \"month\", \"year\" or empty"}`,
			want:       []ledger.Budget{initial},
		},
		{
			name:       "limit as a string",
			body:       `{"category":"еда","limit":"5000"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"field \"limit\" must be a number"}`,
			want:       []ledger.Budget{initial},
		},
		{
			name:       "invalid JSON",
			body:       `{"category":"еда",`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid JSON"}`,
			want:       []ledger.Budget{initial},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledger.Reset()
			if _, err := ledger.SetBudget(initial); err != nil {
				t.Fatalf("SetBudget() error = %v", err)
			}

			checkResponse(t, serve(t, http.MethodPost, "/api/budgets", tt.body), tt.wantStatus, tt.wantBody)
			if got := ledger.ListBudgets(); !slices.Equal(got, tt.want) {
				t.Errorf("budgets = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestListBudgets(t *testing.T) {
	ledger.Reset()
	checkResponse(t, serve(t, http.MethodGet, "/api/budgets", ""), http.StatusOK, `[]`)

	for _, b := range []ledger.Budget{
		{Category: "ремонт", Limit: 100000},
		{Category: "еда", Limit: 5000, Period: ledger.PeriodMonth},
	} {
		if _, err := ledger.SetBudget(b); err != nil {
			t.Fatalf("SetBudget() error = %v", err)
		}
	}

	// Sorted by category; a budget without a period has no "period" field.
	checkResponse(t, serve(t, http.MethodGet, "/api/budgets", ""), http.StatusOK,
		`[{"category":"еда","limit":5000,"period":"month"},{"category":"ремонт","limit":100000}]`)
}
