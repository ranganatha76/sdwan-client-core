package main

import (
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// server owns the in-memory routing state and its external dependencies.
type server struct {
	mu            sync.RWMutex
	state         clientState
	metricsURL    string
	metricsClient *http.Client
}

// newServer creates the control-plane with an unmeasured initial state.
func newServer() *server {
	return &server{
		state:         initialState(),
		metricsURL:    os.Getenv("METRICS_URL"),
		metricsClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// listenAndServe registers the public control-plane HTTP contract.
func (s *server) listenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /api/state", s.getState)
	mux.HandleFunc("POST /api/paths/measurements", s.recordPathMeasurements)
	mux.HandleFunc("POST /api/policies/enable-all", s.enableAllPolicies)
	mux.HandleFunc("POST /api/policies/", s.togglePolicy)
	mux.HandleFunc("POST /api/optimize", s.optimize)
	mux.HandleFunc("POST /api/connection", s.toggleConnection)
	mux.HandleFunc("GET /", s.dashboard)
	return http.ListenAndServe(":8080", mux)
}

// health reports that the control-plane process can accept requests.
func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func logTelemetryFailure(operation, application string, err error) {
	if application == "" {
		log.Printf("%s: %v", operation, err)
		return
	}
	log.Printf("%s for %s: %v", operation, application, err)
}
