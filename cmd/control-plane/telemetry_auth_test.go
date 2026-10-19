package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestMetricsPublishersAuthenticate(t *testing.T) {
	for _, token := range []string{"", "test-ingestion-credential"} {
		t.Run(token, func(t *testing.T) {
			t.Setenv("METRICS_AUTH_TOKEN", token)
			var mu sync.Mutex
			seen := make(map[string]int)
			sink := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				expected := ""
				if token != "" {
					expected = "Bearer " + token
				}
				if actual := request.Header.Get("Authorization"); actual != expected {
					t.Errorf("authorization = %q, want %q", actual, expected)
				}
				mu.Lock()
				seen[request.URL.Path]++
				mu.Unlock()
				response.WriteHeader(http.StatusAccepted)
			}))
			defer sink.Close()
			publisher := newServer()
			publisher.metricsURL = sink.URL
			publisher.metricsClient = sink.Client()
			state := initialState()
			for index := range state.Paths {
				state.Paths[index].Loss = "0%"
			}
			publisher.reportPathMetrics(state)
			mu.Lock()
			defer mu.Unlock()
			for _, path := range []string{"/v1/measurements", "/v1/route-decisions"} {
				if seen[path] == 0 {
					t.Errorf("publisher did not send %s", path)
				}
			}
		})
	}
}
