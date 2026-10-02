package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouter(t *testing.T) {
	const textPlain = "text/plain; charset=utf-8"

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantType   string
		wantBody   string
	}{
		{name: "GET /ping", method: http.MethodGet, path: "/ping", wantStatus: http.StatusOK, wantType: textPlain, wantBody: "pong"},
		{name: "HEAD /ping", method: http.MethodHead, path: "/ping", wantStatus: http.StatusOK, wantType: textPlain},
		{name: "POST /ping", method: http.MethodPost, path: "/ping", wantStatus: http.StatusMethodNotAllowed},
		{name: "unknown path", method: http.MethodGet, path: "/unknown", wantStatus: http.StatusNotFound},
	}

	router := newRouter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantType != "" {
				if got := rec.Header().Get("Content-Type"); got != tt.wantType {
					t.Errorf("Content-Type = %q, want %q", got, tt.wantType)
				}
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}
