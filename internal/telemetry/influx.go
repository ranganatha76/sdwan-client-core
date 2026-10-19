package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// InfluxConfig supplies the connection settings needed to write PulseRoute telemetry.
// Values are supplied by the composition root so this package has no environment-variable dependency.
// Client may be nil, in which case NewInfluxWriter creates a bounded default HTTP client.
// The token is used only in outbound authorization headers and is never logged by this package.
type InfluxConfig struct {
	URL    string
	Token  string
	Org    string
	Bucket string
	Client *http.Client
}

// InfluxWriter persists validated PulseRoute telemetry using InfluxDB line protocol.
// It owns storage-specific serialization, escaping, authentication, and response validation.
// HTTP handlers and native collectors depend on typed methods rather than constructing line protocol.
// The writer observes reported data only and has no authority to alter endpoint networking.
type InfluxWriter struct {
	url    string
	token  string
	org    string
	bucket string
	client *http.Client
}

// NewInfluxWriter creates a storage writer from composition-root supplied settings.
// It accepts an injectable client so tests can use an in-memory server without global configuration.
// A nil client receives a short timeout appropriate for local metrics ingestion requests.
// Callers should validate required configuration before serving traffic through the returned writer.
func NewInfluxWriter(config InfluxConfig) *InfluxWriter {
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &InfluxWriter{url: config.URL, token: config.Token, org: config.Org, bucket: config.Bucket, client: client}
}

// Ready verifies that InfluxDB currently reports a healthy service state.
// It sends a context-bound health request and does not expose storage response details to callers.
// A nil result means the metrics service can accept ingestion with its configured backend reachable.
// Any transport or non-200 result is returned for the HTTP boundary to map to a retryable response.
func (w *InfluxWriter) Ready(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(w.url, "/")+"/health", nil)
	if err != nil {
		return err
	}
	response, err := w.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("influxdb health returned %s", response.Status)
	}
	return nil
}

// WriteApplicationMeasurement persists existing before-and-after application performance metrics.
// It validates the typed input before formatting a single measurement point for the configured bucket.
// The write timestamp reflects ingestion time because the legacy measurement contract has no event time.
// A validation failure is returned without creating an outbound storage request.
func (w *InfluxWriter) WriteApplicationMeasurement(ctx context.Context, measurement ApplicationMeasurement) error {
	if !measurement.Validate() {
		return fmt.Errorf("invalid application measurement")
	}
	line := fmt.Sprintf("application_performance,application=%s baseline_latency_ms=%s,optimized_latency_ms=%s,baseline_loss_pct=%s,optimized_loss_pct=%s,bandwidth_bytes=%di %d", measurement.Application, decimal(measurement.BaselineLatencyMS), decimal(measurement.OptimizedLatencyMS), decimal(measurement.BaselineLossPct), decimal(measurement.OptimizedLossPct), measurement.BandwidthBytes, time.Now().UnixNano())
	return w.writeLines(ctx, []string{line})
}

// WriteEndpointTelemetry persists aggregate endpoint VPN, quality, and interface-counter data.
// It validates the input before serializing tag-safe endpoint dimensions and numeric fields.
// The point deliberately excludes destinations, packet content, DNS names, and user activity.
// Grafana derives rates and connectivity trends from these time-series fields.
func (w *InfluxWriter) WriteEndpointTelemetry(ctx context.Context, telemetry EndpointTelemetry) error {
	if !telemetry.Validate() {
		return fmt.Errorf("invalid endpoint telemetry")
	}
	line := fmt.Sprintf("endpoint_telemetry,device_id=%s,network_type=%s,vpn_provider=%s vpn_connected=%t,latency_ms=%s,jitter_ms=%s,loss_pct=%s,receive_bytes=%di,transmit_bytes=%di %d", telemetry.DeviceID, telemetry.NetworkType, telemetry.VPNProvider, telemetry.VPNConnected, decimal(telemetry.LatencyMS), decimal(telemetry.JitterMS), decimal(telemetry.LossPct), telemetry.ReceiveBytes, telemetry.TransmitBytes, time.Now().UnixNano())
	return w.writeLines(ctx, []string{line})
}

// WriteEndpointDiagnostics persists the optional local health indicators supplied by native collectors.
// It omits DNS, TLS, and Wi-Fi signal fields when the corresponding probe or adapter is not configured.
// Tags retain coarse interface and VPN categories while mutable availability and resource indicators stay fields.
// This method never accepts raw route entries, probe targets, SSIDs, BSSIDs, or traffic payloads.
func (w *InfluxWriter) WriteEndpointDiagnostics(ctx context.Context, diagnostics EndpointDiagnostics) error {
	if !diagnostics.Validate() {
		return fmt.Errorf("invalid endpoint diagnostics")
	}
	fields := []string{fmt.Sprintf("vpn_connected=%t", diagnostics.VPNConnected), fmt.Sprintf("interface_changed=%t", diagnostics.InterfaceChanged), fmt.Sprintf("default_route_available=%t", diagnostics.DefaultRouteAvailable), "cpu_load_1=" + decimal(diagnostics.CPULoad1), fmt.Sprintf("memory_available_bytes=%di", diagnostics.MemoryAvailableBytes)}
	if diagnostics.DNSReachable != nil {
		fields = append(fields, fmt.Sprintf("dns_reachable=%t", *diagnostics.DNSReachable))
	}
	if diagnostics.TLSReachable != nil {
		fields = append(fields, fmt.Sprintf("tls_reachable=%t", *diagnostics.TLSReachable))
	}
	if diagnostics.WiFiSignalDBM != nil {
		fields = append(fields, fmt.Sprintf("wifi_signal_dbm=%di", *diagnostics.WiFiSignalDBM))
	}
	line := fmt.Sprintf("endpoint_diagnostics,device_id=%s,network_type=%s,active_interface=%s,vpn_provider=%s %s %d", diagnostics.DeviceID, diagnostics.NetworkType, diagnostics.ActiveInterface, diagnostics.VPNProvider, strings.Join(fields, ","), time.Now().UnixNano())
	return w.writeLines(ctx, []string{line})
}

// WriteApplicationExperience persists an SLO outcome before and after a routing decision.
// It uses fixed tag dimensions for the device, application, and supported SLO name.
// Decision IDs remain escaped fields so high-frequency decisions cannot create unbounded series.
// Avoided incidents become a queryable boolean for the product experience dashboard.
func (w *InfluxWriter) WriteApplicationExperience(ctx context.Context, experience ApplicationExperience) error {
	if !experience.Validate() {
		return fmt.Errorf("invalid application experience")
	}
	line := fmt.Sprintf("application_experience,device_id=%s,application=%s,slo=%s baseline_value=%s,optimized_value=%s,target_value=%s,avoided_incident=%t,decision_id=%s %d", experience.DeviceID, experience.Application, experience.SLO, decimal(experience.BaselineValue), decimal(experience.OptimizedValue), decimal(experience.TargetValue), experience.AvoidedIncident, lineString(experience.DecisionID), time.Now().UnixNano())
	return w.writeLines(ctx, []string{line})
}

// WriteRouteDecision persists one correlated explanation point for every candidate path considered.
// Shared decision fields make selected, rejected, and rollback outcomes reconstructable in Grafana.
// Candidate quality remains per-path so support teams can compare the selection with alternatives.
// The method records observability data only and does not control policy routing or rollback actions.
func (w *InfluxWriter) WriteRouteDecision(ctx context.Context, decision RouteDecision) error {
	if !decision.Validate() {
		return fmt.Errorf("invalid route decision")
	}
	timestamp := time.Now().UnixNano()
	lines := make([]string, 0, len(decision.CandidatePaths))
	for _, candidate := range decision.CandidatePaths {
		lines = append(lines, fmt.Sprintf("route_decision,device_id=%s,application=%s,destination_class=%s,policy_id=%s,candidate_path=%s latency_ms=%s,jitter_ms=%s,loss_pct=%s,score=%di,selected=%t,decision_id=%s,reason=%s,result=%s,rollback_event=%s %d", decision.DeviceID, decision.Application, decision.DestinationClass, decision.PolicyID, candidate.Name, decimal(candidate.LatencyMS), decimal(candidate.JitterMS), decimal(candidate.LossPct), candidate.Score, candidate.Selected, lineString(decision.DecisionID), lineString(decision.Reason), lineString(decision.Result), lineString(decision.RollbackEvent), timestamp))
	}
	return w.writeLines(ctx, lines)
}

// writeLines submits one or more prevalidated line-protocol records in a single InfluxDB request.
// It joins correlated candidate records with newlines so one decision has an atomic ingestion boundary.
// Authorization and response validation are centralized here for all telemetry write methods.
// InfluxDB must acknowledge the full batch with 204 or callers receive a storage error.
func (w *InfluxWriter) writeLines(ctx context.Context, lines []string) error {
	endpoint := strings.TrimRight(w.url, "/") + "/api/v2/write?" + url.Values{"org": {w.org}, "bucket": {w.bucket}, "precision": {"ns"}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(strings.Join(lines, "\n")))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Token "+w.token)
	request.Header.Set("Content-Type", "text/plain; charset=utf-8")
	response, err := w.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("influxdb returned %s", response.Status)
	}
	return nil
}

// lineString quotes and escapes one string field for InfluxDB line-protocol serialization.
// It applies backslash escaping before quote escaping to preserve literal input characters.
// Callers validate values with SafeField before reaching this helper.
// This keeps correlation and decision explanation values out of InfluxDB tag indexes.
func lineString(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

// decimal formats floating-point fields in a stable decimal form accepted by InfluxDB.
// It retains required precision without adding unnecessary trailing zeroes.
// Typed validation owns value bounds, leaving this helper responsible only for serialization.
// All latency, jitter, loss, SLO, and resource values use this one formatting rule.
func decimal(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
