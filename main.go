package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("EveryoneAgent listening on %s", addr)
	if err := http.ListenAndServe(addr, newServer()); err != nil {
		log.Fatal(err)
	}
}
