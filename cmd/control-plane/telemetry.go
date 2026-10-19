package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// reportPathMetrics publishes probe-derived application and decision telemetry without affecting routing.
func (s *server) reportPathMetrics(state clientState) {
	if s.metricsURL == "" || s.metricsClient == nil {
		return
	}
	baseline := pathByName(state.Paths, "Direct internet")
	optimized := selectedPath(state.Paths)
	baselineLoss, err := lossPercent(baseline.Loss)
	if err != nil {
		logTelemetryFailure("parse baseline loss", "", err)
		return
	}
	optimizedLoss, err := lossPercent(optimized.Loss)
	if err != nil {
		logTelemetryFailure("parse optimized loss", "", err)
		return
	}

	for _, app := range state.Apps {
		if !app.Active {
			continue
		}
		input := performanceMeasurement{
			Application:        app.ID,
			BaselineLatencyMS:  float64(baseline.Latency),
			OptimizedLatencyMS: float64(optimized.Latency),
			BaselineLossPct:    baselineLoss,
			OptimizedLossPct:   optimizedLoss,
		}
		body, err := json.Marshal(input)
		if err != nil {
			logTelemetryFailure("marshal metrics", app.ID, err)
			continue
		}
		request, err := http.NewRequest(http.MethodPost, strings.TrimRight(s.metricsURL, "/")+"/v1/measurements", bytes.NewReader(body))
		if err != nil {
			logTelemetryFailure("create metrics request", app.ID, err)
			continue
		}
		request.Header.Set("Content-Type", "application/json")
		if s.metricsAuthToken != "" {
			request.Header.Set("Authorization", "Bearer "+s.metricsAuthToken)
		}
		response, err := s.metricsClient.Do(request)
		if err != nil {
			logTelemetryFailure("report metrics", app.ID, err)
			continue
		}
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			logTelemetryFailure("report metrics", app.ID, fmt.Errorf("metrics server returned %s", response.Status))
		}
		s.reportRouteDecision(app, state.Paths)
	}
}

func (s *server) reportRouteDecision(app application, paths []path) {
	candidates := make([]candidatePath, 0, len(paths))
	for _, current := range paths {
		loss, err := lossPercent(current.Loss)
		if err != nil {
			logTelemetryFailure("parse route-decision loss", app.ID, err)
			return
		}
		candidates = append(candidates, candidatePath{
			Name:      strings.ReplaceAll(strings.ToLower(current.Name), " ", "-"),
			LatencyMS: float64(current.Latency),
			JitterMS:  float64(current.Jitter),
			LossPct:   loss,
			Score:     current.Score,
			Selected:  current.Selected,
		})
	}
	input := routeDecisionMeasurement{
		DecisionID:       fmt.Sprintf("route-%d-%s", time.Now().UnixNano(), app.ID),
		DeviceID:         "local-dashboard",
		Application:      app.ID,
		DestinationClass: strings.ReplaceAll(strings.ToLower(app.Category), " ", "-"),
		PolicyID:         app.ID,
		Reason:           "highest-quality-score",
		Result:           "selected",
		RollbackEvent:    "none",
		CandidatePaths:   candidates,
	}
	body, err := json.Marshal(input)
	if err != nil {
		logTelemetryFailure("marshal route decision", app.ID, err)
		return
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(s.metricsURL, "/")+"/v1/route-decisions", bytes.NewReader(body))
	if err != nil {
		logTelemetryFailure("create route-decision request", app.ID, err)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	if s.metricsAuthToken != "" {
		request.Header.Set("Authorization", "Bearer "+s.metricsAuthToken)
	}
	response, err := s.metricsClient.Do(request)
	if err != nil {
		logTelemetryFailure("report route decision", app.ID, err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		logTelemetryFailure("report route decision", app.ID, fmt.Errorf("metrics server returned %s", response.Status))
	}
}
