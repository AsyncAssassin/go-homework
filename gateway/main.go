// Command gateway is the HTTP gateway of the project. It answers GET /ping
// and serves the REST API of the ledger under /api. Transactions and
// budgets are kept in memory and are lost when the gateway stops.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/AsyncAssassin/go-homework/gateway/internal/api"
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
		Handler:           api.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Gateway service listening on %s", ln.Addr())
	if err := srv.Serve(ln); err != nil {
		log.Fatalf("Gateway service stopped: %v", err)
	}
}
