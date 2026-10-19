package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"sdwan-client-core/internal/policy"
)

type flow struct {
	Hostname string `json:"hostname"`
}

type decision struct {
	Action        string `json:"action"`
	Priority      string `json:"priority"`
	MatchedRule   string `json:"matchedRule,omitempty"`
	PolicyVersion string `json:"policyVersion"`
	Mode          string `json:"mode"`
}

type policyClient struct {
	url        string
	token      string
	httpClient *http.Client
	mu         sync.Mutex
	snapshot   policy.Snapshot
	expiresAt  time.Time
}

// main starts the local flow-decision API consumed by platform-native adapters.
// It validates required policy credentials and restricts operation to observe or enforce mode.
// It configures the policy service location and routes health, readiness, and evaluation calls.
// The listener on port 8082 uses bounded request and connection timeouts.
func main() {
	token := os.Getenv("POLICY_SERVICE_TOKEN")
	if len(token) < 32 {
		log.Fatal("POLICY_SERVICE_TOKEN must contain at least 32 characters")
	}
	mode := os.Getenv("DATAPLANE_MODE")
	if mode == "" {
		mode = "observe"
	}
	if mode != "observe" && mode != "enforce" {
		log.Fatal("DATAPLANE_MODE must be observe or enforce")
	}
	serviceURL := os.Getenv("POLICY_SERVICE_URL")
	if serviceURL == "" {
		serviceURL = "http://policy-service:8081"
	}

	client := &policyClient{url: serviceURL, token: token, httpClient: &http.Client{Timeout: 3 * time.Second}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", client.ready)
	mux.HandleFunc("POST /v1/flows/evaluate", client.evaluate(mode))
	server := &http.Server{Addr: ":8082", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("data-plane agent listening on :8082 in %s mode", mode)
	log.Fatal(server.ListenAndServe())
}

// health reports that the data-plane agent process can accept HTTP requests.
// It deliberately avoids a policy fetch because liveness must not depend on downstream health.
// A 204 status gives local supervisors a low-cost process health signal.
// Callers that need usable routing decisions should use the readiness endpoint.
func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// ready confirms that the agent can obtain a usable policy snapshot now.
// It delegates to the cached snapshot loader, which refreshes expired policy when necessary.
// Policy retrieval failure produces a 503 so orchestrators do not route traffic prematurely.
// A 204 response means hostname evaluations can use a current or recently fetched snapshot.
func (c *policyClient) ready(w http.ResponseWriter, r *http.Request) {
	if _, err := c.getSnapshot(r.Context()); err != nil {
		http.Error(w, "policy unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// evaluate creates a flow-decision handler bound to the configured operating mode.
// It bounds and decodes the observed hostname before obtaining a cached policy snapshot.
// Matching rules return priority-overlay routing; unmatched names retain direct internet routing.
// The JSON response includes policy version and mode so adapters can audit each decision.
func (c *policyClient) evaluate(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var observed flow
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&observed); err != nil || observed.Hostname == "" {
			http.Error(w, "hostname is required", http.StatusBadRequest)
			return
		}
		snapshot, err := c.getSnapshot(r.Context())
		if err != nil {
			http.Error(w, "policy unavailable", http.StatusServiceUnavailable)
			return
		}
		result := decision{Action: "direct-internet", Priority: "standard", PolicyVersion: snapshot.Version, Mode: mode}
		if rule, matched := snapshot.Match(observed.Hostname); matched {
			result.Action = "priority-overlay"
			result.Priority = rule.Priority
			result.MatchedRule = rule.ID
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}
}

// getSnapshot returns a non-expired policy snapshot or retrieves one from the policy service.
// It serializes cache inspection and refresh so concurrent requests cannot stampede upstream.
// Fresh requests use bearer authentication and reject non-success or malformed responses.
// Successfully decoded snapshots are cached for thirty seconds before the next refresh.
func (c *policyClient) getSnapshot(ctx context.Context) (policy.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expiresAt) {
		return c.snapshot, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/v1/policy/snapshot", nil)
	if err != nil {
		return policy.Snapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.httpClient.Do(req)
	if err != nil {
		return policy.Snapshot{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return policy.Snapshot{}, errors.New("policy service returned non-success status")
	}
	var snapshot policy.Snapshot
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&snapshot); err != nil {
		return policy.Snapshot{}, err
	}
	c.snapshot, c.expiresAt = snapshot, time.Now().Add(30*time.Second)
	return snapshot, nil
}
