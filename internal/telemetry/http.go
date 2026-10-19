package telemetry

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// NewHandler constructs the metrics ingestion HTTP API around one configured InfluxDB writer.
// It exposes liveness, readiness, legacy performance, endpoint, SLO, and decision-explanation routes.
// Request decoding and response mapping remain here so storage and domain code have no HTTP dependency.
// The returned handler accepts telemetry only and never issues routing, policy, or endpoint control commands.
func NewHandler(writer *InfluxWriter) http.Handler {
	server := &httpServer{writer: writer}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", server.ready)
	mux.HandleFunc("POST /v1/measurements", server.writeApplicationMeasurement)
	mux.HandleFunc("POST /v1/endpoint-telemetry", server.writeEndpointTelemetry)
	mux.HandleFunc("POST /v1/endpoint-diagnostics", server.writeEndpointDiagnostics)
	mux.HandleFunc("POST /v1/application-experience", server.writeApplicationExperience)
	mux.HandleFunc("POST /v1/route-decisions", server.writeRouteDecision)
	return mux
}

// httpServer owns HTTP-specific dependencies for the telemetry service endpoints.
// It delegates validation to typed telemetry models and persistence to the InfluxDB writer.
// Keeping this wrapper small makes the API transport independently replaceable or testable.
// The writer is created once by the application composition root and shared by every request.
type httpServer struct {
	writer *InfluxWriter
}

// health reports that the metrics HTTP process can accept requests.
// It intentionally avoids a storage call because it is a liveness rather than readiness probe.
// The empty 204 response keeps high-frequency container checks inexpensive.
// Callers that require backend reachability should use the separate readiness endpoint.
func health(response http.ResponseWriter, request *http.Request) {
	response.WriteHeader(http.StatusNoContent)
}

// ready verifies that the configured telemetry storage backend is available now.
// It uses the incoming request context to bound the InfluxDB health request lifetime.
// Storage failures map to a generic 503 without leaking backend credentials or details.
// A 204 response signals that typed telemetry writes can be accepted by this service.
func (s *httpServer) ready(response http.ResponseWriter, request *http.Request) {
	if err := s.writer.Ready(request.Context()); err != nil {
		http.Error(response, "metrics storage unavailable", http.StatusServiceUnavailable)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

// writeApplicationMeasurement accepts the legacy aggregate performance metric used by existing panels.
// It bounds the request body, validates the typed payload, and persists it through the shared writer.
// Invalid callers receive a 400 without causing an outbound storage request.
// Storage failures remain retryable 503 responses, while persisted records receive 202.
func (s *httpServer) writeApplicationMeasurement(response http.ResponseWriter, request *http.Request) {
	input, ok := decodeJSON(response, request, 16<<10, func(value ApplicationMeasurement) bool { return value.Validate() }, "valid application and non-negative measurements are required")
	if !ok {
		return
	}
	if err := s.writer.WriteApplicationMeasurement(request.Context(), input); err != nil {
		writeStorageError(response, "write application measurement", err)
		return
	}
	response.WriteHeader(http.StatusAccepted)
}

// writeEndpointTelemetry accepts aggregate endpoint VPN, quality, and byte-counter metadata.
// It bounds JSON input and relies on the typed model to reject unsafe tags or invalid quality values.
// The endpoint persists only metadata permitted by the documented privacy boundary.
// Accepted samples return 202, while temporary metrics storage failure returns 503.
func (s *httpServer) writeEndpointTelemetry(response http.ResponseWriter, request *http.Request) {
	input, ok := decodeJSON(response, request, 16<<10, func(value EndpointTelemetry) bool { return value.Validate() }, "valid device, network, VPN, and non-negative endpoint telemetry are required")
	if !ok {
		return
	}
	if err := s.writer.WriteEndpointTelemetry(request.Context(), input); err != nil {
		writeStorageError(response, "write endpoint telemetry", err)
		return
	}
	response.WriteHeader(http.StatusAccepted)
}

// writeEndpointDiagnostics accepts optional local interface, reachability, Wi-Fi, and resource metadata.
// It preserves the distinction between unconfigured probes and failed configured probes through pointer fields.
// Raw routes, probe targets, packet content, and Wi-Fi identity data are not part of this contract.
// Valid diagnostic snapshots are acknowledged with 202 after InfluxDB confirms persistence.
func (s *httpServer) writeEndpointDiagnostics(response http.ResponseWriter, request *http.Request) {
	input, ok := decodeJSON(response, request, 16<<10, func(value EndpointDiagnostics) bool { return value.Validate() }, "valid endpoint diagnostics are required")
	if !ok {
		return
	}
	if err := s.writer.WriteEndpointDiagnostics(request.Context(), input); err != nil {
		writeStorageError(response, "write endpoint diagnostics", err)
		return
	}
	response.WriteHeader(http.StatusAccepted)
}

// writeApplicationExperience accepts an application SLO outcome before and after one decision.
// It accepts only allowlisted SLO names so Grafana dimensions and alerts remain stable.
// Decision correlation is persisted as a field and not a time-series tag to bound cardinality.
// The API is observe-only and does not instruct an endpoint to select or change a path.
func (s *httpServer) writeApplicationExperience(response http.ResponseWriter, request *http.Request) {
	input, ok := decodeJSON(response, request, 16<<10, func(value ApplicationExperience) bool { return value.Validate() }, "valid application experience telemetry is required")
	if !ok {
		return
	}
	if err := s.writer.WriteApplicationExperience(request.Context(), input); err != nil {
		writeStorageError(response, "write application experience", err)
		return
	}
	response.WriteHeader(http.StatusAccepted)
}

// writeRouteDecision accepts an explanation with one selected path and its alternative candidates.
// It uses a larger bounded request size because each decision can carry up to eight candidate summaries.
// The typed model guarantees exactly one selected candidate before persistence reaches InfluxDB.
// The stored decision explains control-plane behavior but cannot itself alter routes or enforce policy.
func (s *httpServer) writeRouteDecision(response http.ResponseWriter, request *http.Request) {
	input, ok := decodeJSON(response, request, 64<<10, func(value RouteDecision) bool { return value.Validate() }, "valid route decision telemetry is required")
	if !ok {
		return
	}
	if err := s.writer.WriteRouteDecision(request.Context(), input); err != nil {
		writeStorageError(response, "write route decision", err)
		return
	}
	response.WriteHeader(http.StatusAccepted)
}

// decodeJSON reads one bounded JSON body and validates the resulting typed telemetry value.
// It closes the request body on return, avoiding connection leakage across all telemetry endpoints.
// The supplied validation function keeps parsing mechanics independent of individual domain contracts.
// Invalid JSON or values receive the caller-provided 400 message and a false success indicator.
func decodeJSON[T any](response http.ResponseWriter, request *http.Request, limit int64, validate func(T) bool, message string) (T, bool) {
	defer request.Body.Close()
	var input T
	if err := json.NewDecoder(io.LimitReader(request.Body, limit)).Decode(&input); err != nil || !validate(input) {
		http.Error(response, message, http.StatusBadRequest)
		return input, false
	}
	return input, true
}

// writeStorageError logs internal persistence detail while returning a stable client-safe error response.
// It keeps InfluxDB connection details, response bodies, and credentials out of caller-visible messages.
// Every endpoint maps storage availability consistently so collectors can implement a predictable retry policy.
// The helper does not retry because retry ownership belongs to the native collector or calling service.
func writeStorageError(response http.ResponseWriter, operation string, err error) {
	log.Printf("%s: %v", operation, err)
	http.Error(response, "metrics storage unavailable", http.StatusServiceUnavailable)
}
