package api

import (
	"log"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"
)

// Logging logs the method, the path, the status and the duration of every
// request, for example "POST /api/transactions 201 512.25µs". A response
// aborted by a panic is logged with "aborted" instead of the status. The
// path is escaped, so a request cannot put a line break into the log.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			status := strconv.Itoa(rec.status)
			v := recover()
			if v != nil {
				status = "aborted"
			}
			log.Printf("%s %s %s %s", r.Method, r.URL.EscapedPath(), status, time.Since(start))
			if v != nil {
				panic(v) // net/http aborts the response
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

// statusRecorder remembers the status of a response and whether it has
// started. A handler that writes a body without calling WriteHeader
// answers 200.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the original ResponseWriter.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Recover answers 500 with a JSON error if a handler panics and logs the
// panic with the stack, so a bug in one handler does not drop the
// connection without an answer. If the handler has already started the
// response, Recover aborts it instead: the client must not take a part of
// a response for the whole one.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v) // the handler aborts the response on purpose
			}
			log.Printf("panic serving %s %s: %v\n%s", r.Method, r.URL.EscapedPath(), v, debug.Stack())
			if rec.wroteHeader {
				panic(http.ErrAbortHandler)
			}
			writeError(w, http.StatusInternalServerError, "internal server error")
		}()
		next.ServeHTTP(rec, r)
	})
}

// JSONContentType sets Content-Type: application/json; charset=utf-8 on
// every response, including those net/http writes itself, such as the
// redirect from a path like /api//budgets to /api/budgets. Such a redirect
// has an empty body: http.Redirect writes its HTML body only when no
// Content-Type is set.
func JSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentTypeJSON)
		next.ServeHTTP(w, r)
	})
}
