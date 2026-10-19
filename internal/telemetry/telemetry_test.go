package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestApplicationExperienceValidation accepts supported application SLO outcome telemetry.
// It uses a Zoom jitter outcome with a stable device and decision correlation identifier.
// The valid payload demonstrates the before, after, target, and avoided-incident contract.
// Unsupported SLO values are rejected to keep dashboard measurement dimensions bounded.
func TestApplicationExperienceValidation(t *testing.T) {
	experience := ApplicationExperience{DeviceID: "laptop-42", Application: "zoom", SLO: "zoom_jitter_ms", BaselineValue: 25, OptimizedValue: 4, TargetValue: 10, AvoidedIncident: true, DecisionID: "decision-42"}
	if !experience.Validate() {
		t.Fatal("valid application experience telemetry was rejected")
	}
	experience.SLO = "custom_metric"
	if experience.Validate() {
		t.Fatal("unsupported application SLO was accepted")
	}
}

// TestRouteDecisionValidationRequiresOneSelectedCandidate verifies explainability selection semantics.
// It starts with two candidate paths and exactly one selected path, which is accepted.
// The test then marks both candidates as selected and expects the decision to be rejected.
// This keeps before-and-after dashboards from representing ambiguous routing outcomes.
func TestRouteDecisionValidationRequiresOneSelectedCandidate(t *testing.T) {
	decision := testRouteDecision()
	if !decision.Validate() {
		t.Fatal("valid route decision was rejected")
	}
	decision.CandidatePaths[1].Selected = true
	if decision.Validate() {
		t.Fatal("route decision with two selected paths was accepted")
	}
}

// TestWriteRouteDecisionWritesEveryCandidate verifies candidate comparisons remain correlated in storage.
// It uses an in-memory InfluxDB-compatible endpoint to capture the emitted line-protocol batch.
// Both candidate path names and the decision identifier must appear in the same write request.
// This ensures Grafana can reconstruct selected and rejected paths for one routing explanation.
func TestWriteRouteDecisionWritesEveryCandidate(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v2/write" {
			t.Errorf("unexpected path %q", request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		received = string(body)
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	writer := NewInfluxWriter(InfluxConfig{URL: server.URL, Token: "test", Org: "pulseroute", Bucket: "performance", Client: server.Client()})
	if err := writer.WriteRouteDecision(context.Background(), testRouteDecision()); err != nil {
		t.Fatalf("write route decision: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(received), "\n"); len(lines) != 2 {
		t.Fatalf("expected two candidate points, got %d: %q", len(lines), received)
	}
	if !strings.Contains(received, "candidate_path=fiber") || !strings.Contains(received, "candidate_path=lte") || !strings.Contains(received, `decision_id="decision-42"`) {
		t.Fatalf("missing expected route decision fields: %q", received)
	}
}

// TestEndpointDiagnosticsValidationAllowsOptionalProbes verifies absence differs from failed probe data.
// It supplies no DNS, TLS, or Wi-Fi signal values while retaining required endpoint metadata.
// The snapshot must be accepted because those probes are configured independently by operators.
// An implausible positive Wi-Fi dBm reading is then rejected before InfluxDB serialization.
func TestEndpointDiagnosticsValidationAllowsOptionalProbes(t *testing.T) {
	diagnostics := EndpointDiagnostics{DeviceID: "laptop-42", NetworkType: "wifi", ActiveInterface: "wlan0", VPNProvider: "cisco", CPULoad1: 0.7, MemoryAvailableBytes: 1024}
	if !diagnostics.Validate() {
		t.Fatal("valid diagnostics with optional probes absent was rejected")
	}
	signal := 5
	diagnostics.WiFiSignalDBM = &signal
	if diagnostics.Validate() {
		t.Fatal("invalid Wi-Fi signal was accepted")
	}
}

// testRouteDecision returns one representative selected and rejected path comparison for tests.
// It centralizes the valid fixture so validation and persistence tests share identical decision semantics.
// The values mimic a collaboration application preferring lower-jitter fiber over LTE failover.
// Each caller may mutate its returned value safely because the fixture creates fresh slice storage.
func testRouteDecision() RouteDecision {
	return RouteDecision{
		DecisionID: "decision-42", DeviceID: "laptop-42", Application: "teams", DestinationClass: "collaboration", PolicyID: "teams", Reason: "lower-jitter", Result: "selected", RollbackEvent: "none",
		CandidatePaths: []CandidatePath{{Name: "fiber", LatencyMS: 18, JitterMS: 3, LossPct: 0.02, Score: 98, Selected: true}, {Name: "lte", LatencyMS: 42, JitterMS: 8, LossPct: 0.11, Score: 84}},
	}
}
