package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultWorkApplicationsAreHighPriority(t *testing.T) {
	state := initialState()
	wanted := map[string]bool{
		"zoom": false, "slack": false, "teams": false, "github": false, "bitbucket": false,
		"microsoft-365": false, "google-workspace": false, "jira": false, "asana": false, "notion": false, "figma": false,
	}

	for _, app := range state.Apps {
		if _, ok := wanted[app.ID]; ok {
			wanted[app.ID] = app.Active && app.Priority == "High"
		}
	}

	for appID, prioritized := range wanted {
		if !prioritized {
			t.Errorf("%s should be enabled with high priority", appID)
		}
	}
}

func TestEnableAllPolicies(t *testing.T) {
	server := &server{state: initialState()}
	server.state.Apps[0].Active = false
	server.state.Apps[0].Priority = "Standard"

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/policies/enable-all", nil)
	server.enableAllPolicies(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d", recorder.Code)
	}
	for _, app := range server.state.Apps {
		if !app.Active || app.Priority != "High" {
			t.Errorf("%s was not enabled with high priority", app.ID)
		}
	}
}

func TestSelectBestPathUsesCurrentQualityScore(t *testing.T) {
	paths := []path{
		{Name: "Fiber primary", Score: 82, Selected: true},
		{Name: "LTE failover", Score: 96},
		{Name: "Direct internet", Score: 89},
	}

	if bestIndex := selectBestPath(paths); bestIndex != 1 {
		t.Fatalf("expected LTE failover to be selected from current quality, got index %d", bestIndex)
	}
}

func TestRecordPathMeasurementsSelectsBestReportedPath(t *testing.T) {
	server := &server{state: initialState()}
	measuredAt := time.Date(2026, time.October, 19, 10, 30, 0, 0, time.UTC)
	payload, err := json.Marshal([]pathMeasurement{
		{Name: "Fiber primary", Latency: 48, LossPercent: 0.20, Jitter: 9, MeasuredAt: measuredAt},
		{Name: "LTE failover", Latency: 19, LossPercent: 0.02, Jitter: 3, MeasuredAt: measuredAt},
		{Name: "Direct internet", Latency: 27, LossPercent: 0.10, Jitter: 5, MeasuredAt: measuredAt},
	})
	if err != nil {
		t.Fatalf("marshal path measurements: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/paths/measurements", bytes.NewReader(payload))
	server.recordPathMeasurements(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if selected := selectedPath(server.state.Paths); selected.Name != "LTE failover" {
		t.Fatalf("expected best reported path to be selected, got %q", selected.Name)
	}
	for _, app := range server.state.Apps {
		if app.Active && app.Route != "LTE failover" {
			t.Errorf("active app %q uses %q, want LTE failover", app.Name, app.Route)
		}
	}
}
