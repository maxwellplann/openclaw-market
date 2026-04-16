package main

import (
	"log"
	"net/http"
	"os"

	"openclaw-market/internal/market"
)

func main() {
	addr := os.Getenv("OPENCLAW_MARKET_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	server, err := market.NewServer("data/store.json")
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	log.Printf("openclaw-market listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
