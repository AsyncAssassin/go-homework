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

// checkResponse checks a recorded response like checkResult.
func checkResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantBody string) {
	t.Helper()
	checkResult(t, rec.Result(), wantStatus, wantBody)
}

// checkResult checks the status, the JSON Content-Type and, unless wantBody
// is empty, the exact body without the final newline.
func checkResult(t *testing.T, resp *http.Response, wantStatus int, wantBody string) {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the body: %v", err)
	}
	body := strings.TrimSuffix(string(b), "\n")
	if resp.StatusCode != wantStatus {
		t.Errorf("status = %d, want %d; body %s", resp.StatusCode, wantStatus, body)
	}
	if got := resp.Header.Get("Content-Type"); got != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeJSON)
	}
	if wantBody != "" && body != wantBody {
		t.Errorf("body = %s, want %s", body, wantBody)
	}
}

// step is one request of a flow and its expected response.
type step struct {
	name       string
	method     string
	path       string
	body       string // "" for none
	wantStatus int
	wantBody   string
}

// runFlow starts a test server with an empty ledger and sends the steps to
// it in order, each in its own subtest. The steps share the ledger, so they
// do not run in parallel: each one sees what the previous ones stored. The
// flow stops at the first failed step, because the next ones depend on it.
func runFlow(t *testing.T, steps []step) {
	t.Helper()
	ledger.Reset()
	srv := httptest.NewServer(NewRouter())
	t.Cleanup(func() {
		srv.Close()
		ledger.Reset()
	})

	for _, s := range steps {
		ok := t.Run(s.name, func(t *testing.T) {
			req, err := http.NewRequest(s.method, srv.URL+s.path, strings.NewReader(s.body))
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			if s.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", s.method, s.path, err)
			}
			defer resp.Body.Close()
			checkResult(t, resp, s.wantStatus, s.wantBody)
		})
		if !ok {
			return
		}
	}
}

func TestBudgetsFlow(t *testing.T) {
	const food = `[{"category":"еда","limit":5000}]`

	runFlow(t, []step{
		{
			name:   "create",
			method: http.MethodPost, path: "/api/budgets", body: `{"category":"еда","limit":5000}`,
			wantStatus: http.StatusCreated, wantBody: `{"category":"еда","limit":5000}`,
		},
		{
			name:   "list",
			method: http.MethodGet, path: "/api/budgets",
			wantStatus: http.StatusOK, wantBody: food,
		},
		{
			name:   "bad_json",
			method: http.MethodPost, path: "/api/budgets", body: `{"category":"еда",`,
			wantStatus: http.StatusBadRequest, wantBody: `{"error":"invalid JSON"}`,
		},
		{
			name:   "zero_limit",
			method: http.MethodPost, path: "/api/budgets", body: `{"category":"еда","limit":0}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid limit 0 for \"еда\": must be from 0.01 to 999999999999.99"}`,
		},
		{
			name:   "rejected_not_stored",
			method: http.MethodGet, path: "/api/budgets",
			wantStatus: http.StatusOK, wantBody: food,
		},
		{
			name:   "update",
			method: http.MethodPost, path: "/api/budgets", body: `{"category":"еда","limit":6000,"period":"month"}`,
			wantStatus: http.StatusCreated, wantBody: `{"category":"еда","limit":6000,"period":"month"}`,
		},
		{
			name:   "list_updated",
			method: http.MethodGet, path: "/api/budgets",
			wantStatus: http.StatusOK, wantBody: `[{"category":"еда","limit":6000,"period":"month"}]`,
		},
	})
}

func TestTransactionsFlow(t *testing.T) {
	const lunch = `{"id":1,"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}`

	runFlow(t, []step{
		{
			name:   "budget",
			method: http.MethodPost, path: "/api/budgets", body: `{"category":"еда","limit":5000}`,
			wantStatus: http.StatusCreated, wantBody: `{"category":"еда","limit":5000}`,
		},
		{
			name:   "ok",
			method: http.MethodPost, path: "/api/transactions",
			body:       `{"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}`,
			wantStatus: http.StatusCreated, wantBody: lunch,
		},
		{
			name:   "list",
			method: http.MethodGet, path: "/api/transactions",
			wantStatus: http.StatusOK, wantBody: `[` + lunch + `]`,
		},
		{
			// 450 + 4600 is over the limit of 5000.
			name:   "exceeded",
			method: http.MethodPost, path: "/api/transactions",
			body:       `{"amount":4600,"category":"еда","description":"ресторан","date":"2025-09-11"}`,
			wantStatus: http.StatusConflict, wantBody: `{"error":"budget exceeded"}`,
		},
		{
			name:   "bad_json",
			method: http.MethodPost, path: "/api/transactions", body: `{"amount":450,`,
			wantStatus: http.StatusBadRequest, wantBody: `{"error":"invalid JSON"}`,
		},
		{
			name:   "rejected_not_stored",
			method: http.MethodGet, path: "/api/transactions",
			wantStatus: http.StatusOK, wantBody: `[` + lunch + `]`,
		},
		{
			// The rejected transactions took neither the budget nor an ID.
			name:   "rest_of_the_limit",
			method: http.MethodPost, path: "/api/transactions", body: `{"amount":4550,"category":"еда","date":"2025-09-12"}`,
			wantStatus: http.StatusCreated,
			wantBody:   `{"id":2,"amount":4550,"category":"еда","description":"","date":"2025-09-12"}`,
		},
	})
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
			wantBody:   `{"error":"invalid amount 0: must be from 0.01 to 999999999999.99"}`,
		},
		{
			name:       "no category",
			body:       `{"amount":450}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"category must not be empty"}`,
		},
		{
			// PostgreSQL cannot store a NUL character in TEXT.
			name:       "NUL in the category",
			body:       `{"amount":450,"category":"е\u0000да"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid category: must be valid UTF-8 without NUL characters"}`,
		},
		{
			name:       "NUL in the description",
			body:       `{"amount":450,"category":"еда","description":"a\u0000b"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid description: must be valid UTF-8 without NUL characters"}`,
		},
		{
			name:       "category too long",
			body:       `{"amount":450,"category":"` + strings.Repeat("x", 3000) + `"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid category: must be at most 100 characters, got 3000"}`,
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
			// This date is the zero time, which ledger reads as "no date".
			name:       "first day of the year 1",
			body:       `{"amount":450,"category":"еда","date":"0001-01-01"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid date 0001-01-01: out of range"}`,
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
			name:       "invalid JSON after the object",
			body:       lunch + ` x`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid JSON"}`,
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
			if got := len(mustListTransactions(t)); got != wantStored {
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
			wantBody:   `{"error":"invalid limit 0 for \"еда\": must be from 0.01 to 999999999999.99"}`,
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
			name:       "NUL in the category",
			body:       `{"category":"е\u0000да","limit":5000}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid category: must be valid UTF-8 without NUL characters"}`,
			want:       []ledger.Budget{initial},
		},
		{
			name:       "category too long",
			body:       `{"category":"` + strings.Repeat("x", 3000) + `","limit":5000}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid category: must be at most 100 characters, got 3000"}`,
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
			if got := mustListBudgets(t); !slices.Equal(got, tt.want) {
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

func TestReportSummary(t *testing.T) {
	ledger.Reset()
	for _, body := range []string{
		`{"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}`,
		`{"amount":120,"category":"транспорт","date":"2025-09-11"}`,
		`{"amount":0.1,"category":"еда","date":"2025-09-30"}`,
		`{"amount":0.2,"category":"еда","date":"2025-09-30"}`,
		`{"amount":50,"category":"кафе","date":"2025-08-31"}`,  // the month before
		`{"amount":999,"category":"кафе","date":"2025-10-01"}`, // the month after
	} {
		checkResponse(t, serve(t, http.MethodPost, "/api/transactions", body), http.StatusCreated, "")
	}

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "month",
			query:      "?from=2025-09-01&to=2025-09-30",
			wantStatus: http.StatusOK,
			// 450 + 0.1 + 0.2 in kopecks, not 450.30000000000001.
			wantBody: `{"from":"2025-09-01","to":"2025-09-30","categories":[{"category":"еда","amount":450.3},{"category":"транспорт","amount":120}],"total":570.3}`,
		},
		{
			name:       "one day",
			query:      "?from=2025-09-30&to=2025-09-30",
			wantStatus: http.StatusOK,
			wantBody:   `{"from":"2025-09-30","to":"2025-09-30","categories":[{"category":"еда","amount":0.3}],"total":0.3}`,
		},
		{
			name:       "only from: to the end of its month",
			query:      "?from=2025-09-11",
			wantStatus: http.StatusOK,
			wantBody:   `{"from":"2025-09-11","to":"2025-09-30","categories":[{"category":"еда","amount":0.3},{"category":"транспорт","amount":120}],"total":120.3}`,
		},
		{
			name:       "no expenses",
			query:      "?from=2025-01-01&to=2025-01-31",
			wantStatus: http.StatusOK,
			wantBody:   `{"from":"2025-01-01","to":"2025-01-31","categories":[],"total":0}`,
		},
		{
			// The month of to is cut to the first day of reports.
			name:       "only to in the first month",
			query:      "?to=0001-01-15",
			wantStatus: http.StatusOK,
			wantBody:   `{"from":"0001-01-02","to":"0001-01-15","categories":[],"total":0}`,
		},
		{
			name:       "first days",
			query:      "?from=0001-01-02&to=0001-01-15",
			wantStatus: http.StatusOK,
			wantBody:   `{"from":"0001-01-02","to":"0001-01-15","categories":[],"total":0}`,
		},
		{
			// The month of from is cut to the last day of reports.
			name:       "only from in the last month",
			query:      "?from=9999-12-15",
			wantStatus: http.StatusOK,
			wantBody:   `{"from":"9999-12-15","to":"9999-12-30","categories":[],"total":0}`,
		},
		{
			// PostgreSQL has no year 0, so it must not reach the database.
			name:       "year 0",
			query:      "?from=0000-01-01&to=2025-09-30",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid date 0000-01-01: must be from 0001-01-02 to 9999-12-30"}`,
		},
		{
			name:       "after the last day",
			query:      "?from=9999-12-31",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid date 9999-12-31: must be from 0001-01-02 to 9999-12-30"}`,
		},
		{
			// The zero time, which ledger reads as "no date".
			name:       "first day of the year 1",
			query:      "?to=0001-01-01",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid date 0001-01-01: out of range"}`,
		},
		{
			name:       "invalid date",
			query:      "?from=2025-9-1&to=2025-09-30",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid date \"2025-9-1\": must be YYYY-MM-DD"}`,
		},
		{
			name:       "range ends before it starts",
			query:      "?from=2025-09-30&to=2025-09-01",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid report range: from 2025-09-30 is after to 2025-09-01"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, http.MethodGet, "/api/reports/summary"+tt.query, "")
			checkResponse(t, rec, tt.wantStatus, tt.wantBody)
		})
	}
}

func TestReportSummaryOfCurrentMonth(t *testing.T) {
	ledger.Reset()
	checkResponse(t, serve(t, http.MethodPost, "/api/transactions", `{"amount":100,"category":"еда"}`), http.StatusCreated, "")

	// Without parameters the report covers the current month, today included.
	rec := serve(t, http.MethodGet, "/api/reports/summary", "")
	checkResponse(t, rec, http.StatusOK, "")
	var got ReportSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	from, to, err := reportRange(nil, time.Now())
	if err != nil {
		t.Fatalf("reportRange() error = %v", err)
	}
	want := ReportSummaryResponse{
		From:       from.Format(dateLayout),
		To:         to.Format(dateLayout),
		Categories: []CategoryTotalResponse{{Category: "еда", Amount: 100}},
		Total:      100,
	}
	if got.From != want.From || got.To != want.To || !slices.Equal(got.Categories, want.Categories) || got.Total != want.Total {
		t.Errorf("report = %+v, want %+v", got, want)
	}
}

// TestLedgerError checks the answer to a failure of the ledger, such as a
// lost connection to PostgreSQL: 500 with a general message, while the error
// itself goes only to the log.
func TestLedgerError(t *testing.T) {
	ledger.Reset()
	t.Cleanup(ledger.Reset)
	if err := ledger.Close(); err != nil {
		t.Fatalf("ledger.Close() error = %v", err)
	}
	logs := captureLog(t)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create transaction", http.MethodPost, "/api/transactions", `{"amount":100,"category":"еда","date":"2025-09-10"}`},
		{"list transactions", http.MethodGet, "/api/transactions", ""},
		{"set budget", http.MethodPost, "/api/budgets", `{"category":"еда","limit":5000}`},
		{"list budgets", http.MethodGet, "/api/budgets", ""},
		{"report", http.MethodGet, "/api/reports/summary?from=2025-09-01&to=2025-09-30", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.Reset()
			rec := serve(t, tt.method, tt.path, tt.body)
			checkResponse(t, rec, http.StatusInternalServerError, `{"error":"internal server error"}`)
			if !strings.Contains(logs.String(), "internal error: ledger is closed") {
				t.Errorf("log = %q, want the error of the ledger", logs)
			}
		})
	}
}

// mustListTransactions returns ledger.ListTransactions() and stops the test
// if it fails.
func mustListTransactions(t *testing.T) []ledger.Transaction {
	t.Helper()
	list, err := ledger.ListTransactions()
	if err != nil {
		t.Fatalf("ledger.ListTransactions() error = %v", err)
	}
	return list
}

// mustListBudgets returns ledger.ListBudgets() and stops the test if it
// fails.
func mustListBudgets(t *testing.T) []ledger.Budget {
	t.Helper()
	list, err := ledger.ListBudgets()
	if err != nil {
		t.Fatalf("ledger.ListBudgets() error = %v", err)
	}
	return list
}
