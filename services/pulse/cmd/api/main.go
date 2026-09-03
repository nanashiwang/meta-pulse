package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	addr := os.Getenv("PULSE_HTTP_ADDR")
	if addr == "" {
		addr = ":8088"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"meta-pulse-api"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"meta-pulse-api"}`))
	})

	log.Printf("meta-pulse-api bootstrap listening on %s", addr)
	log.Printf("NOTE: business HTTP routes will be implemented with Gin according to docs/ARCHITECTURE.md")
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
