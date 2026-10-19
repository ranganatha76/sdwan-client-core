package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"sdwan-client-core/internal/telemetry"
)

// main composes the metrics HTTP service from environment configuration and internal modules.
// It keeps process startup separate from telemetry models, InfluxDB serialization, and HTTP endpoint logic.
// Required storage settings are read once before the listener can accept collector or control-plane traffic.
// The server then exposes the reusable internal telemetry handler on port 8083 with bounded timeouts.
func main() {
	writer := telemetry.NewInfluxWriter(telemetry.InfluxConfig{
		URL:    requiredEnv("INFLUX_URL"),
		Token:  requiredEnv("INFLUX_TOKEN"),
		Org:    requiredEnv("INFLUX_ORG"),
		Bucket: requiredEnv("INFLUX_BUCKET"),
	})
	server := &http.Server{
		Addr:              ":8083",
		Handler:           telemetry.NewHandler(writer),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Println("metrics server listening on :8083")
	log.Fatal(server.ListenAndServe())
}

// requiredEnv obtains one mandatory service configuration value from the process environment.
// It terminates startup before binding the listener when a required storage setting is missing.
// Failing fast prevents the API from accepting telemetry it cannot attribute to a configured backend.
// A non-empty value is returned unchanged so secret handling remains at the deployment boundary.
func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s must be set", name)
	}
	return value
}
