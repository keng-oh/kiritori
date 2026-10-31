package main

import (
	"log"
	"net/http"
	"os"

	"kiritori/internal/server"
)

func main() {
	addr := os.Getenv("KIRITORI_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, server.New()); err != nil {
		log.Fatal(err)
	}
}
