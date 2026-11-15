package api

import (
	"bytes"
	"errors"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger"
)

// The tests below change the standard logger, so they do not run in
// parallel.

// captureLog sends the standard log to a buffer until the test ends.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	})
	return &buf
}

func TestLogging(t *testing.T) {
	logs := captureLog(t)
	svc := ledger.NewMemory()

	serve(t, svc, http.MethodPost, "/api/budgets", `{"category":"еда","limit":5000}`)
	serve(t, svc, http.MethodGet, "/api/transactions?limit=1", "") // the query is not logged
	serve(t, svc, http.MethodDelete, "/api/budgets", "")
	serve(t, svc, http.MethodGet, "/api/%0Afake", "") // an escaped line break stays escaped

	// Method, path, status and duration, one line per request.
	want := []string{
		`POST /api/budgets 201 `,
		`GET /api/transactions 200 `,
		`DELETE /api/budgets 405 `,
		`GET /api/%0Afake 404 `,
	}
	duration := regexp.MustCompile(`^[0-9.]+(ns|µs|ms|s)$`)
	lines := strings.Split(strings.TrimSuffix(logs.String(), "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("logged %d lines, want %d:\n%s", len(lines), len(want), logs)
	}
	for i, prefix := range want {
		rest, ok := strings.CutPrefix(lines[i], prefix)
		if !ok || !duration.MatchString(rest) {
			t.Errorf("line %d = %q, want %q and a duration", i+1, lines[i], prefix)
		}
	}
}

func TestLoggingImplicitStatus(t *testing.T) {
	logs := captureLog(t)

	// A handler that writes a body without WriteHeader answers 200.
	h := Logging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
		w.WriteHeader(http.StatusTeapot) // too late, ignored by net/http
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if got := logs.String(); !strings.HasPrefix(got, "GET /x 200 ") {
		t.Errorf("log = %q, want status 200", got)
	}
}

func TestRecover(t *testing.T) {
	logs := captureLog(t)

	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	checkResponse(t, rec, http.StatusInternalServerError, `{"error":"internal server error"}`)
	if got := logs.String(); !strings.Contains(got, "panic serving GET /x: boom") {
		t.Errorf("log = %q, want the panic", got)
	}
}

func TestRecoverAfterPartialWrite(t *testing.T) {
	logs := captureLog(t)

	h := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"partial":`))
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	defer func() {
		// The started response is aborted, not completed with an error.
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler", v)
		}
		if got := rec.Body.String(); got != `{"partial":` {
			t.Errorf("body = %q, want only the part written before the panic", got)
		}
		if !strings.Contains(logs.String(), "panic serving GET /x: boom") {
			t.Errorf("log = %q, want the panic", logs)
		}
	}()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	t.Error("ServeHTTP returned, want it to abort the response")
}

func TestLoggingAbortedRequest(t *testing.T) {
	logs := captureLog(t)

	h := Logging(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler", v)
		}
		if got := logs.String(); !strings.HasPrefix(got, "GET /x aborted ") {
			t.Errorf("log = %q, want the request logged as aborted", got)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	t.Error("ServeHTTP returned, want it to panic again with http.ErrAbortHandler")
}

func TestRecoverAbortHandler(t *testing.T) {
	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	t.Error("ServeHTTP returned, want it to panic again with http.ErrAbortHandler")
}

func TestRecoverAfterFlush(t *testing.T) {
	logs := captureLog(t)

	// A flush sends the status and the headers, so the response has started
	// even without a body.
	h := Logging(Recover(JSONContentType(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush() error = %v", err)
		}
		panic("boom")
	}))))
	rec := httptest.NewRecorder()
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler", v)
		}
		if !rec.Flushed || rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Errorf("flushed %t, status %d, body %q; want a flushed 200 without a body", rec.Flushed, rec.Code, rec.Body)
		}
		if !strings.Contains(logs.String(), "GET /x aborted ") {
			t.Errorf("log = %q, want the request logged as aborted", logs)
		}
	}()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	t.Error("ServeHTTP returned, want it to abort the response")
}

func TestRecoverAfterFailedFlush(t *testing.T) {
	// The embedding hides the Flush method of the recorder, so the flush
	// fails and the response has not started yet.
	rec := httptest.NewRecorder()
	w := struct{ http.ResponseWriter }{rec}
	h := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); !errors.Is(err, http.ErrNotSupported) {
			t.Errorf("Flush() error = %v, want %v", err, http.ErrNotSupported)
		}
		panic("boom")
	}))
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	checkResponse(t, rec, http.StatusInternalServerError, `{"error":"internal server error"}`)
}

func TestMiddlewareUnwrap(t *testing.T) {
	// http.ResponseController reaches the methods of the original
	// ResponseWriter, such as SetWriteDeadline, through the wrappers.
	srv := httptest.NewServer(Logging(Recover(JSONContentType(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
	})))))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	checkResult(t, resp, http.StatusOK, "")
}

func TestJSONContentType(t *testing.T) {
	// Even a handler that sets nothing answers with the JSON Content-Type.
	h := JSONContentType(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	checkResponse(t, rec, http.StatusNoContent, "")
}

func TestWriteJSONEncodeError(t *testing.T) {
	logs := captureLog(t)

	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, math.NaN()) // JSON has no NaN

	checkResponse(t, rec, http.StatusInternalServerError, `{"error":"internal server error"}`)
	if !strings.Contains(logs.String(), "encode response") {
		t.Errorf("log = %q, want the encoding error", logs)
	}
}

func TestWriteInternalError(t *testing.T) {
	logs := captureLog(t)

	rec := httptest.NewRecorder()
	writeInternalError(rec, errors.New("disk is full"))

	// The details go to the log, not to the client.
	checkResponse(t, rec, http.StatusInternalServerError, `{"error":"internal server error"}`)
	if !strings.Contains(logs.String(), "disk is full") {
		t.Errorf("log = %q, want the error", logs)
	}
}
