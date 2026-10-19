package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
