package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type measurement struct {
	Application        string  `json:"application"`
	BaselineLatencyMS  float64 `json:"baselineLatencyMs"`
	OptimizedLatencyMS float64 `json:"optimizedLatencyMs"`
	BaselineLossPct    float64 `json:"baselineLossPct"`
	OptimizedLossPct   float64 `json:"optimizedLossPct"`
	BandwidthBytes     int64   `json:"bandwidthBytes"`
}

// endpointTelemetry is an observe-only device and VPN health sample.
type endpointTelemetry struct {
	DeviceID      string  `json:"deviceId"`
	NetworkType   string  `json:"networkType"`
	VPNProvider   string  `json:"vpnProvider"`
	VPNConnected  bool    `json:"vpnConnected"`
	LatencyMS     float64 `json:"latencyMs"`
	JitterMS      float64 `json:"jitterMs"`
	LossPct       float64 `json:"lossPct"`
	ReceiveBytes  int64   `json:"receiveBytes"`
	TransmitBytes int64   `json:"transmitBytes"`
}

type influxWriter struct {
	url    string
	token  string
	org    string
	bucket string
	client *http.Client
}

// main configures the metrics ingestion service using required InfluxDB settings.
// It creates one timeout-bound client shared by readiness checks and write operations.
// It registers application and endpoint telemetry ingestion alongside health endpoints.
// The service listens on port 8083 after all mandatory storage configuration is present.
func main() {
	writer := &influxWriter{
		url:    requiredEnv("INFLUX_URL"),
		token:  requiredEnv("INFLUX_TOKEN"),
		org:    requiredEnv("INFLUX_ORG"),
		bucket: requiredEnv("INFLUX_BUCKET"),
		client: &http.Client{Timeout: 5 * time.Second},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", writer.ready)
	mux.HandleFunc("POST /v1/measurements", writer.writeMeasurement)
	mux.HandleFunc("POST /v1/endpoint-telemetry", writer.writeEndpointTelemetry)
	server := &http.Server{Addr: ":8083", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Println("metrics server listening on :8083")
	log.Fatal(server.ListenAndServe())
}

// requiredEnv obtains a mandatory configuration value from the process environment.
// It terminates startup immediately when the value is empty instead of running misconfigured.
// This prevents accepting telemetry that cannot be safely attributed to the intended store.
// A non-empty value is returned unchanged for the caller's connection configuration.
func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s must be set", name)
	}
	return value
}

// health reports that the metrics server process can receive HTTP requests.
// It is a liveness signal and therefore does not make a storage network call.
// The empty 204 response keeps frequent container probes inexpensive.
// Clients needing storage confirmation should use the readiness endpoint instead.
func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// ready verifies that the configured InfluxDB health endpoint is currently reachable.
// It propagates the caller context to bound the probe to the incoming request lifetime.
// Errors or non-200 storage responses return 503 without leaking storage implementation details.
// A 204 response confirms that subsequent telemetry writes have a reachable destination.
func (w *influxWriter) ready(response http.ResponseWriter, request *http.Request) {
	endpoint := strings.TrimRight(w.url, "/") + "/health"
	influxRequest, err := http.NewRequestWithContext(request.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		http.Error(response, "metrics storage unavailable", http.StatusServiceUnavailable)
		return
	}
	influxResponse, err := w.client.Do(influxRequest)
	if err != nil {
		http.Error(response, "metrics storage unavailable", http.StatusServiceUnavailable)
		return
	}
	defer influxResponse.Body.Close()
	if influxResponse.StatusCode != http.StatusOK {
		http.Error(response, "metrics storage unavailable", http.StatusServiceUnavailable)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

// writeMeasurement decodes and validates one application performance measurement request.
// It limits body size before rejecting missing application identifiers or negative measurements.
// A valid sample is written to InfluxDB, while storage failures become a retryable 503 response.
// Accepted writes return 202 because the API successfully handed data to persistent storage.
func (w *influxWriter) writeMeasurement(response http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var input measurement
	if err := json.NewDecoder(io.LimitReader(request.Body, 16<<10)).Decode(&input); err != nil || !input.valid() {
		http.Error(response, "valid application and non-negative measurements are required", http.StatusBadRequest)
		return
	}
	if err := w.write(request.Context(), input); err != nil {
		log.Printf("write measurement: %v", err)
		http.Error(response, "metrics storage unavailable", http.StatusServiceUnavailable)
		return
	}
	response.WriteHeader(http.StatusAccepted)
}

// writeEndpointTelemetry accepts an observe-only endpoint and VPN health sample.
// It bounds JSON input and rejects unsafe tags or invalid quality and counter values.
// Validated metadata is persisted without accepting payloads, destinations, or user activity.
// It returns 202 on storage acceptance or 503 when the metrics backend is unavailable.
func (w *influxWriter) writeEndpointTelemetry(response http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var input endpointTelemetry
	if err := json.NewDecoder(io.LimitReader(request.Body, 16<<10)).Decode(&input); err != nil || !input.valid() {
		http.Error(response, "valid device, network, VPN, and non-negative endpoint telemetry are required", http.StatusBadRequest)
		return
	}
	if err := w.persistEndpointTelemetry(request.Context(), input); err != nil {
		log.Printf("write endpoint telemetry: %v", err)
		http.Error(response, "metrics storage unavailable", http.StatusServiceUnavailable)
		return
	}
	response.WriteHeader(http.StatusAccepted)
}

// valid checks whether an application measurement can be represented safely in InfluxDB.
// It requires a non-empty application tag with no line-protocol separator characters.
// It rejects negative latency, loss, and bandwidth values before serialization.
// The handler uses this predicate to return a client error without contacting storage.
func (m measurement) valid() bool {
	return m.Application != "" && !strings.ContainsAny(m.Application, ", =") && m.BaselineLatencyMS >= 0 && m.OptimizedLatencyMS >= 0 && m.BaselineLossPct >= 0 && m.OptimizedLossPct >= 0 && m.BandwidthBytes >= 0
}

// valid verifies endpoint telemetry tags and measurements before line-protocol construction.
// It requires non-empty, separator-free device, network, and VPN-provider identifiers.
// It bounds loss to a valid percentage and rejects negative quality values or byte counters.
// VPN connection state is boolean and therefore needs no additional range validation.
func (m endpointTelemetry) valid() bool {
	return safeTag(m.DeviceID) && safeTag(m.NetworkType) && safeTag(m.VPNProvider) && m.LatencyMS >= 0 && m.JitterMS >= 0 && m.LossPct >= 0 && m.LossPct <= 100 && m.ReceiveBytes >= 0 && m.TransmitBytes >= 0
}

// write formats one application measurement as InfluxDB line protocol and submits it.
// It includes a nanosecond timestamp so time-series ordering reflects write-time observation.
// The request carries the configured organization, bucket, token, and explicit text content type.
// Non-204 storage responses become errors so the HTTP handler can report temporary failure.
func (w *influxWriter) write(ctx context.Context, m measurement) error {
	line := fmt.Sprintf("application_performance,application=%s baseline_latency_ms=%s,optimized_latency_ms=%s,baseline_loss_pct=%s,optimized_loss_pct=%s,bandwidth_bytes=%di %d", m.Application, decimal(m.BaselineLatencyMS), decimal(m.OptimizedLatencyMS), decimal(m.BaselineLossPct), decimal(m.OptimizedLossPct), m.BandwidthBytes, time.Now().UnixNano())
	endpoint := w.url + "/api/v2/write?" + url.Values{"org": {w.org}, "bucket": {w.bucket}, "precision": {"ns"}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(line))
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

// persistEndpointTelemetry serializes endpoint health metadata as one InfluxDB time-series point.
// It stores device, network, and VPN attributes as tags for dashboard filtering and grouping.
// Connection state, quality values, and byte counters are stored as queryable fields.
// The function deliberately excludes packet payloads, destinations, DNS queries, and app content.
func (w *influxWriter) persistEndpointTelemetry(ctx context.Context, m endpointTelemetry) error {
	line := fmt.Sprintf("endpoint_telemetry,device_id=%s,network_type=%s,vpn_provider=%s vpn_connected=%t,latency_ms=%s,jitter_ms=%s,loss_pct=%s,receive_bytes=%di,transmit_bytes=%di %d", m.DeviceID, m.NetworkType, m.VPNProvider, m.VPNConnected, decimal(m.LatencyMS), decimal(m.JitterMS), decimal(m.LossPct), m.ReceiveBytes, m.TransmitBytes, time.Now().UnixNano())
	return w.writeLine(ctx, line)
}

// writeLine sends one prevalidated Influx line-protocol record to the configured write API.
// It builds authenticated bucket and organization query parameters for the target database.
// The caller owns validation, allowing this helper to be shared by multiple metric types.
// InfluxDB must return 204 or the resulting error is propagated to the HTTP boundary.
func (w *influxWriter) writeLine(ctx context.Context, line string) error {
	endpoint := w.url + "/api/v2/write?" + url.Values{"org": {w.org}, "bucket": {w.bucket}, "precision": {"ns"}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(line))
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

// safeTag reports whether a tag has content and cannot break Influx line-protocol syntax.
// It rejects separators that would inject tag, field, or measurement boundaries into a point.
// Newline rejection prevents one user-supplied value from creating multiple records.
// Callers use the predicate before formatting endpoint telemetry tags into a write request.
func safeTag(value string) bool {
	return value != "" && !strings.ContainsAny(value, ", =\n\r")
}

// decimal formats a floating-point field in the non-exponent form accepted by InfluxDB.
// It preserves necessary precision without adding insignificant trailing zeroes.
// The format is shared by latency, jitter, and loss fields across metric records.
// Values are validated for acceptable ranges by their callers before this formatting step.
func decimal(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
