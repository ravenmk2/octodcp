// Program entry point: assembly and startup only, no business logic.
package main

import (
	"log"
	"net/http"

	"github.com/ravenmk2/octodcp/internal/server"
)

var version = "dev" // default when not injected; release builds inject via -ldflags "-X main.version="

func main() {
	handler := server.New()
	log.Printf("listening on :8080 (version %s)", version)
	log.Fatal(http.ListenAndServe(":8080", handler))
}
