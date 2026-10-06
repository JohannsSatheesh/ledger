// Command ledgerd runs the ledger HTTP service.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/JohannsSatheesh/ledger/internal/api"
	"github.com/JohannsSatheesh/ledger/internal/ledger"
)

func main() {
	addr := ":8080"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewServer(ledger.New()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("ledgerd listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}
