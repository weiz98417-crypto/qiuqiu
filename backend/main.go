package main

import (
	"log"
	"net/http"

	"qiuqiu/internal/config"
	"qiuqiu/internal/ws"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env for local development (ignore errors in production)
	_ = godotenv.Load()

	cfg := config.Load()
	hub := ws.NewHub(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hub.HandleHealth)
	mux.HandleFunc("/ws/match/", hub.HandleWS)

	addr := ":" + cfg.Port
	log.Printf("qiuqiu backend starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
