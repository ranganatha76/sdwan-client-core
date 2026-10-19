package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *server) getState(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, s.state)
}

func (s *server) togglePolicy(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/policies/"), "/toggle")
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.state.Apps {
		app := &s.state.Apps[index]
		if app.ID != id {
			continue
		}
		app.Active = !app.Active
		if app.Active {
			app.Priority = "High"
			if s.state.MeasurementsReady {
				app.Health, app.Route = "Protected", selectedPath(s.state.Paths).Name
			} else {
				app.Health, app.Route = "Pending measurement", "Awaiting probe"
			}
		} else {
			app.Priority = "Standard"
			app.Health, app.Route = "Standard", "Direct internet"
		}
		s.addActivity("Policy updated for "+app.Name, "success")
		writeJSON(w, app)
		return
	}
	http.NotFound(w, r)
}

func (s *server) enableAllPolicies(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.state.Apps {
		app := &s.state.Apps[index]
		app.Active = true
		app.Priority = "High"
		if s.state.MeasurementsReady {
			app.Health, app.Route = "Protected", selectedPath(s.state.Paths).Name
		} else {
			app.Health, app.Route = "Pending measurement", "Awaiting probe"
		}
	}
	s.addActivity("All application policies enabled", "success")
	writeJSON(w, s.state)
}

func (s *server) optimize(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.MeasurementsReady {
		http.Error(w, "a complete path measurement cycle is required before optimization", http.StatusConflict)
		return
	}
	s.selectBestPath(true)
	writeJSON(w, s.state)
}

func (s *server) recordPathMeasurements(w http.ResponseWriter, r *http.Request) {
	var measurements []pathMeasurement
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	if err := decoder.Decode(&measurements); err != nil {
		http.Error(w, "invalid measurement payload", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if err := s.applyPathMeasurements(measurements); err != nil {
		s.mu.Unlock()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.selectBestPath(true)
	state := s.state
	s.mu.Unlock()

	go s.reportPathMetrics(state)
	writeJSON(w, state)
}

func (s *server) toggleConnection(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Connected = !s.state.Connected
	if s.state.Connected {
		s.addActivity("SD-WAN protection enabled", "success")
	} else {
		s.addActivity("SD-WAN protection paused", "warning")
	}
	writeJSON(w, s.state)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
