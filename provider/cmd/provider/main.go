package main

import (
	"log"
	"net/http"
	"time"

	"github.com/kentaro-kiuchi/oidc-provider/provider/internal/config"
	"github.com/kentaro-kiuchi/oidc-provider/provider/internal/httpapi"
)

func main() {
	cfg := config.Load()
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.New(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("OIDC provider listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
