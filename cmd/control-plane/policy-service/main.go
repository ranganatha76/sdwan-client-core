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

// main starts the authenticated policy distribution service.
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

// health reports that the policy service can accept requests.
func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// snapshot authenticates callers and returns the current versioned policy set.
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

// securityHeaders adds baseline response hardening before invoking a handler.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
