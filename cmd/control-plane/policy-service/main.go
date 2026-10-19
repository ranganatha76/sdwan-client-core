package main

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"sdwan-client-core/internal/policy"
)

// main initializes the authenticated policy distribution service.
// It requires a sufficiently long shared bearer token before opening a listener.
// It registers health and immutable policy snapshot endpoints behind security headers.
// The server then listens on port 8081 with bounded HTTP timeouts.
func main() {
	token := os.Getenv("POLICY_SERVICE_TOKEN")
	if len(token) < 32 {
		log.Fatal("POLICY_SERVICE_TOKEN must contain at least 32 characters")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /v1/policy/snapshot", snapshot(token))

	server := &http.Server{
		Addr:              ":8081",
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Println("policy service listening on :8081")
	log.Fatal(server.ListenAndServe())
}

// health reports that the policy service process is able to accept HTTP requests.
// It does not authenticate or fetch policy because it is a liveness-only endpoint.
// The empty 204 response minimizes work for container health probes.
// Readiness of downstream agents is checked through the snapshot endpoint instead.
func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// snapshot creates the handler that serves the current versioned policy snapshot.
// It extracts the bearer token and compares it in constant time to avoid timing leaks.
// Authorized callers receive private, short-lived JSON suitable for agent caching.
// Unauthorized callers receive no policy content and a standard 401 response.
func snapshot(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private, max-age=30")
		_ = json.NewEncoder(w).Encode(policy.DefaultSnapshot())
	}
}

// securityHeaders wraps handlers with baseline HTTP response hardening.
// It prevents content sniffing so clients honor the media type emitted by each handler.
// The wrapper does not change routing, authentication, or handler error behavior.
// Every request flows to the wrapped handler after the response header is established.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
