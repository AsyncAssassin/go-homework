// Command gateway is the HTTP gateway of the project. It answers GET /ping
// and serves the REST API of the ledger under /api. The ledger keeps the
// data in PostgreSQL and caches reports in Redis; both must be running
// before the gateway starts.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AsyncAssassin/go-homework/gateway/internal/api"
	"github.com/AsyncAssassin/go-homework/ledger"
)

// shutdownTimeout limits the wait for the requests in progress on shutdown.
const shutdownTimeout = 10 * time.Second

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	if err := run(*addr); err != nil {
		log.Fatalf("Gateway service failed: %v", err)
	}
}

// run connects the ledger to PostgreSQL and Redis and serves HTTP on addr
// until SIGINT or SIGTERM. Then it waits for the requests in progress and
// closes the connections of the ledger. A second signal ends the process
// without waiting.
func run(addr string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := ledger.Connect(ctx); err != nil {
		return err
	}
	defer func() {
		if err := ledger.Close(); err != nil {
			log.Printf("close the ledger: %v", err)
		}
	}()

	// Listen before logging, so a busy port is reported as a startup error.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           api.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()
	log.Printf("Gateway service listening on %s", ln.Addr())

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	// Stop catching the signals, so a second Ctrl+C ends the process at once
	// instead of waiting for the requests in progress.
	stop()
	log.Print("Gateway service shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down: %w", err)
	}
	return nil
}
