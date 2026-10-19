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
	Action       string `json:"action"`
	Priority     string `json:"priority"`
	MatchedRule  string `json:"matchedRule,omitempty"`
	PolicyVersion string `json:"policyVersion"`
	Mode         string `json:"mode"`
}

type policyClient struct {
	url        string
	token      string
	httpClient *http.Client
	mu         sync.Mutex
	snapshot   policy.Snapshot
	expiresAt  time.Time
}

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
	mux.HandleFunc("POST /v1/flows/evaluate", client.evaluate(mode))
	server := &http.Server{Addr: ":8082", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("data-plane agent listening on :8082 in %s mode", mode)
	log.Fatal(server.ListenAndServe())
}

func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

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