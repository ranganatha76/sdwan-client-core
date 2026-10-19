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

// main starts the metrics ingestion API that persists application measurements.
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

// requiredEnv returns a required setting and stops startup when it is absent.
func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s must be set", name)
	}
	return value
}

// health reports that the metrics service is ready to receive measurements.
func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// ready verifies that InfluxDB is reachable before the service advertises ingestion readiness.
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

// writeMeasurement validates a measurement request and persists it to InfluxDB.
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

// writeEndpointTelemetry persists an observe-only device and VPN health sample.
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

// valid rejects empty application names and negative or unsafe metric values.
func (m measurement) valid() bool {
	return m.Application != "" && !strings.ContainsAny(m.Application, ", =") && m.BaselineLatencyMS >= 0 && m.OptimizedLatencyMS >= 0 && m.BaselineLossPct >= 0 && m.OptimizedLossPct >= 0 && m.BandwidthBytes >= 0
}

// valid limits tags to safe identifiers and counters to non-negative values.
func (m endpointTelemetry) valid() bool {
	return safeTag(m.DeviceID) && safeTag(m.NetworkType) && safeTag(m.VPNProvider) && m.LatencyMS >= 0 && m.JitterMS >= 0 && m.LossPct >= 0 && m.LossPct <= 100 && m.ReceiveBytes >= 0 && m.TransmitBytes >= 0
}

// write converts a measurement into Influx line protocol and submits it.
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

// persistEndpointTelemetry stores health metadata only; it does not collect payloads or destinations.
func (w *influxWriter) persistEndpointTelemetry(ctx context.Context, m endpointTelemetry) error {
	line := fmt.Sprintf("endpoint_telemetry,device_id=%s,network_type=%s,vpn_provider=%s vpn_connected=%t,latency_ms=%s,jitter_ms=%s,loss_pct=%s,receive_bytes=%di,transmit_bytes=%di %d", m.DeviceID, m.NetworkType, m.VPNProvider, m.VPNConnected, decimal(m.LatencyMS), decimal(m.JitterMS), decimal(m.LossPct), m.ReceiveBytes, m.TransmitBytes, time.Now().UnixNano())
	return w.writeLine(ctx, line)
}

// writeLine submits a single already-validated Influx line-protocol point.
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

func safeTag(value string) bool {
	return value != "" && !strings.ContainsAny(value, ", =\n\r")
}

// decimal formats floating-point fields for Influx line protocol.
func decimal(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
