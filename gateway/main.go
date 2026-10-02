// Command gateway is the HTTP gateway of the project. For now it only
// exposes a health check: GET /ping answers "pong".
package main

import (
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	// Listen before logging, so a busy port is reported as a startup error.
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Gateway service failed to start: %v", err)
	}

	srv := &http.Server{
		Handler:           newRouter(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Gateway service listening on %s", *addr)
	if err := srv.Serve(ln); err != nil {
		log.Fatalf("Gateway service stopped: %v", err)
	}
}

// newRouter registers the gateway's HTTP handlers.
func newRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", handlePing)
	return mux
}

// handlePing answers "pong" so clients can check that the gateway is up.
func handlePing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "pong")
}
