package api

import (
	"io"
	"log"
	"net/http"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Keep the request log of the router out of the test output.
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

func TestRouter(t *testing.T) {
	const (
		notFound   = `{"error":"not found"}`
		notAllowed = `{"error":"method not allowed"}`
	)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantAllow  string
	}{
		{name: "ping", method: http.MethodGet, path: "/ping", wantStatus: http.StatusOK, wantBody: `{"message":"pong"}`},
		{name: "HEAD ping", method: http.MethodHead, path: "/ping", wantStatus: http.StatusOK},
		{name: "POST ping", method: http.MethodPost, path: "/ping", wantStatus: http.StatusMethodNotAllowed, wantBody: notAllowed, wantAllow: "GET, HEAD"},
		{name: "HEAD transactions", method: http.MethodHead, path: "/api/transactions", wantStatus: http.StatusOK},
		{name: "PUT transactions", method: http.MethodPut, path: "/api/transactions", wantStatus: http.StatusMethodNotAllowed, wantBody: notAllowed, wantAllow: "GET, HEAD, POST"},
		{name: "DELETE budgets", method: http.MethodDelete, path: "/api/budgets", wantStatus: http.StatusMethodNotAllowed, wantBody: notAllowed, wantAllow: "GET, HEAD, POST"},
		{name: "unknown path", method: http.MethodGet, path: "/unknown", wantStatus: http.StatusNotFound, wantBody: notFound},
		{name: "api without a slash", method: http.MethodGet, path: "/api", wantStatus: http.StatusNotFound, wantBody: notFound},
		{name: "api root", method: http.MethodGet, path: "/api/", wantStatus: http.StatusNotFound, wantBody: notFound},
		{name: "unknown api path", method: http.MethodPost, path: "/api/unknown", wantStatus: http.StatusNotFound, wantBody: notFound},
		{name: "trailing slash", method: http.MethodGet, path: "/api/transactions/", wantStatus: http.StatusNotFound, wantBody: notFound},
		{name: "route without the prefix", method: http.MethodGet, path: "/transactions", wantStatus: http.StatusNotFound, wantBody: notFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, tt.method, tt.path, "")
			checkResponse(t, rec, tt.wantStatus, tt.wantBody)
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
		})
	}
}

func TestRouterRedirect(t *testing.T) {
	// ServeMux redirects to the clean path, with 301 in Go 1.22 and 307 in
	// later versions. The response still has the JSON Content-Type.
	rec := serve(t, http.MethodGet, "/api//budgets", "")
	if rec.Code < 300 || rec.Code > 399 {
		t.Errorf("status = %d, want a redirect", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/api/budgets" {
		t.Errorf("Location = %q, want %q", got, "/api/budgets")
	}
	if got := rec.Header().Get("Content-Type"); got != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeJSON)
	}
}
