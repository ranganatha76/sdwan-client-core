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
	mux.HandleFunc("POST /v1/measurements", writer.writeMeasurement)
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

// valid rejects empty application names and negative or unsafe metric values.
func (m measurement) valid() bool {
	return m.Application != "" && !strings.ContainsAny(m.Application, ", =") && m.BaselineLatencyMS >= 0 && m.OptimizedLatencyMS >= 0 && m.BaselineLossPct >= 0 && m.OptimizedLossPct >= 0 && m.BandwidthBytes >= 0
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

// decimal formats floating-point fields for Influx line protocol.
func decimal(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
