package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	url := os.Getenv("HEALTHCHECK_URL")
	if url == "" {
		url = "http://127.0.0.1:8080/readyz"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		os.Exit(1)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
