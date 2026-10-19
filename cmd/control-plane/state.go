package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// initialState defines centrally distributed policy without claiming measured performance.
func initialState() clientState {
	return clientState{
		Connected:       true,
		Location:        "Seattle, WA",
		PathsMeasuredAt: "Awaiting first probe",
		Apps: []application{
			pendingApplication("zoom", "Zoom", "Video collaboration"),
			pendingApplication("salesforce", "Salesforce", "Customer operations"),
			pendingApplication("teams", "Microsoft Teams", "Video collaboration"),
			pendingApplication("slack", "Slack", "Messaging"),
			pendingApplication("github", "GitHub", "Engineering"),
			pendingApplication("bitbucket", "Bitbucket", "Engineering"),
			pendingApplication("microsoft-365", "Microsoft 365", "Productivity"),
			pendingApplication("google-workspace", "Google Workspace", "Productivity"),
			pendingApplication("jira", "Jira", "Productivity"),
			pendingApplication("asana", "Asana", "Productivity"),
			pendingApplication("notion", "Notion", "Productivity"),
			pendingApplication("figma", "Figma", "Productivity"),
		},
		Paths: []path{
			{Name: "Fiber primary", Provider: "Home ISP", Loss: "--"},
			{Name: "LTE failover", Provider: "eSIM cellular", Loss: "--"},
			{Name: "Direct internet", Provider: "Home ISP", Loss: "--"},
		},
		Activity: []activity{
			{Time: "Just now", Message: "Awaiting a complete real path-probe cycle", Kind: "neutral"},
			{Time: "Just now", Message: "Work application policy loaded", Kind: "neutral"},
		},
	}
}

func pendingApplication(id, name, category string) application {
	return application{ID: id, Name: name, Category: category, Priority: "High", Route: "Awaiting probe", Health: "Pending measurement", Active: true}
}

// applyPathMeasurements validates and atomically applies one sample for every configured path.
func (s *server) applyPathMeasurements(measurements []pathMeasurement) error {
	if len(measurements) != len(s.state.Paths) {
		return fmt.Errorf("expected measurements for all %d configured paths", len(s.state.Paths))
	}
	pathsByName := make(map[string]*path, len(s.state.Paths))
	for index := range s.state.Paths {
		pathsByName[s.state.Paths[index].Name] = &s.state.Paths[index]
	}
	seen := make(map[string]bool, len(measurements))
	latestMeasurement := time.Time{}
	for _, measurement := range measurements {
		current, ok := pathsByName[measurement.Name]
		if !ok || seen[measurement.Name] {
			return fmt.Errorf("measurement name %q is not a unique configured path", measurement.Name)
		}
		if measurement.Latency < 0 || measurement.Jitter < 0 || measurement.LossPercent < 0 || measurement.LossPercent > 100 || measurement.MeasuredAt.IsZero() {
			return fmt.Errorf("measurement for %q has invalid quality values", measurement.Name)
		}
		seen[measurement.Name] = true
		current.Latency = measurement.Latency
		current.Jitter = measurement.Jitter
		current.Loss = fmt.Sprintf("%.2f%%", measurement.LossPercent)
		current.Score = max(0, min(100, 100-measurement.Latency/2-measurement.Jitter-int(measurement.LossPercent*20)))
		if measurement.MeasuredAt.After(latestMeasurement) {
			latestMeasurement = measurement.MeasuredAt
		}
	}
	s.state.PathsMeasuredAt = latestMeasurement.Local().Format(time.Kitchen)
	s.state.MeasurementsReady = true
	return nil
}

// selectBestPath assigns active applications to the highest-scoring reported path.
func (s *server) selectBestPath(recordActivity bool) {
	bestIndex := selectBestPath(s.state.Paths)
	for index := range s.state.Paths {
		s.state.Paths[index].Selected = index == bestIndex
	}
	for index := range s.state.Apps {
		if s.state.Apps[index].Active {
			s.state.Apps[index].Route = s.state.Paths[bestIndex].Name
		}
	}
	if recordActivity {
		s.addActivity("Route optimization completed: "+s.state.Paths[bestIndex].Name+" selected", "success")
	}
}

func (s *server) addActivity(message, kind string) {
	s.state.Activity = append([]activity{{Time: time.Now().Format("15:04"), Message: message, Kind: kind}}, s.state.Activity...)
	if len(s.state.Activity) > 4 {
		s.state.Activity = s.state.Activity[:4]
	}
}

func pathByName(paths []path, name string) path {
	for _, candidate := range paths {
		if candidate.Name == name {
			return candidate
		}
	}
	return selectedPath(paths)
}

func lossPercent(value string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
}

func selectBestPath(paths []path) int {
	bestIndex := 0
	for index := 1; index < len(paths); index++ {
		if paths[index].Score > paths[bestIndex].Score {
			bestIndex = index
		}
	}
	return bestIndex
}

func selectedPath(paths []path) path {
	for _, path := range paths {
		if path.Selected {
			return path
		}
	}
	return paths[0]
}
