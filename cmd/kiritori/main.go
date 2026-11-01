package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"kiritori/internal/server"
	"kiritori/internal/store"
	"kiritori/internal/youtube"
)

func main() {
	ctx := context.Background()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	addr := os.Getenv("KIRITORI_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	if err := store.Migrate(ctx, dbURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	h, err := server.New(server.Deps{Store: st, Resolver: youtube.NewClient()})
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}
