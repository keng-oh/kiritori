package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"kiritori/internal/auth"
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

	deps := server.Deps{Store: st, Resolver: youtube.NewClient()}
	if clientID := os.Getenv("GOOGLE_CLIENT_ID"); clientID != "" {
		baseURL := strings.TrimRight(os.Getenv("KIRITORI_BASE_URL"), "/")
		deps.Auth = auth.NewGoogle(ctx, clientID, os.Getenv("GOOGLE_CLIENT_SECRET"), baseURL+"/auth/callback")
		deps.Sessions = st
	} else {
		log.Print("GOOGLE_CLIENT_ID is not set: login is disabled")
	}
	h, err := server.New(deps)
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
