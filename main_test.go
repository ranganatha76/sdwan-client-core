package main

import "testing"

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
