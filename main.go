package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) != 0 && (len(args) != 1 || args[0] != "healthz") {
		return fmt.Errorf("usage: everyoneagent [healthz]")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if len(args) == 1 {
		client := &http.Client{
			Timeout: 3 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		response, err := client.Get("http://127.0.0.1:" + port + "/healthz")
		if err != nil {
			return fmt.Errorf("healthz request failed: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("healthz returned HTTP %d", response.StatusCode)
		}
		return nil
	}
	addr := ":" + port
	log.Printf("EveryoneAgent listening on %s", addr)
	return http.ListenAndServe(addr, newServer())
}
