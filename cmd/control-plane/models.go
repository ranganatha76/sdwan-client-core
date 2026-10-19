package main

import "time"

type application struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Priority string `json:"priority"`
	Route    string `json:"route"`
	Health   string `json:"health"`
	Active   bool   `json:"active"`
}

type path struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Latency  int    `json:"latency"`
	Loss     string `json:"loss"`
	Jitter   int    `json:"jitter"`
	Score    int    `json:"score"`
	Selected bool   `json:"selected"`
}

// pathMeasurement is a real-time quality sample reported by a native probe or gateway.
type pathMeasurement struct {
	Name        string    `json:"name"`
	Latency     int       `json:"latency"`
	LossPercent float64   `json:"lossPercent"`
	Jitter      int       `json:"jitter"`
	MeasuredAt  time.Time `json:"measuredAt"`
}

type activity struct {
	Time    string `json:"time"`
	Message string `json:"message"`
	Kind    string `json:"kind"`
}

type clientState struct {
	Connected         bool          `json:"connected"`
	Location          string        `json:"location"`
	PathsMeasuredAt   string        `json:"pathsMeasuredAt"`
	MeasurementsReady bool          `json:"measurementsReady"`
	Apps              []application `json:"apps"`
	Paths             []path        `json:"paths"`
	Activity          []activity    `json:"activity"`
}

type performanceMeasurement struct {
	Application        string  `json:"application"`
	BaselineLatencyMS  float64 `json:"baselineLatencyMs"`
	OptimizedLatencyMS float64 `json:"optimizedLatencyMs"`
	BaselineLossPct    float64 `json:"baselineLossPct"`
	OptimizedLossPct   float64 `json:"optimizedLossPct"`
	BandwidthBytes     int64   `json:"bandwidthBytes"`
}

type routeDecisionMeasurement struct {
	DecisionID       string          `json:"decisionId"`
	DeviceID         string          `json:"deviceId"`
	Application      string          `json:"application"`
	DestinationClass string          `json:"destinationClass"`
	PolicyID         string          `json:"policyId"`
	Reason           string          `json:"reason"`
	Result           string          `json:"result"`
	RollbackEvent    string          `json:"rollbackEvent"`
	CandidatePaths   []candidatePath `json:"candidatePaths"`
}

type candidatePath struct {
	Name      string  `json:"name"`
	LatencyMS float64 `json:"latencyMs"`
	JitterMS  float64 `json:"jitterMs"`
	LossPct   float64 `json:"lossPct"`
	Score     int     `json:"score"`
	Selected  bool    `json:"selected"`
}
