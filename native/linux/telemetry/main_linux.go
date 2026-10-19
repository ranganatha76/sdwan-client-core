//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sdwan-client-core/internal/telemetry"
)

type collectorConfig struct {
	metricsURL         string
	deviceID           string
	vpnProvider        string
	vpnInterfacePrefix string
	probeHost          string
	dnsHost            string
	tlsHost            string
	interval           time.Duration
}

type probeResult struct {
	latencyMS float64
	jitterMS  float64
	lossPct   float64
}

var (
	packetLossPattern = regexp.MustCompile(`([0-9.]+)% packet loss`)
	pingTimePattern   = regexp.MustCompile(`time=([0-9.]+) ?ms`)
	wifiSignalPattern = regexp.MustCompile(`signal: (-?[0-9]+) dBm`)
)

// main runs one observe-only collection or repeats it at a configured interval.
// It requires an explicit device identifier and sends only aggregate host health metadata.
// Optional probe targets are operator-configured and are never included in the telemetry payload.
// The command does not invoke the enforcer, install routes, change QoS, or inspect user traffic.
func main() {
	config := parseConfig()
	if flag.NArg() != 1 || (flag.Arg(0) != "once" && flag.Arg(0) != "watch") {
		fatal(errors.New("usage: pulseroute-telemetry [flags] once|watch"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}
	if flag.Arg(0) == "once" {
		fatal(collectAndSend(ctx, client, config, ""))
		return
	}

	ticker := time.NewTicker(config.interval)
	defer ticker.Stop()
	previousInterface := ""
	for {
		if err := collectAndSend(ctx, client, config, previousInterface); err != nil {
			fmt.Fprintln(os.Stderr, "pulseroute-telemetry:", err)
		}
		previousInterface, _ = defaultInterface()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// parseConfig reads collector settings and rejects configurations that cannot identify the endpoint.
// It keeps every probe optional so deployment owners can select only approved reachability targets.
// The default metrics endpoint is loopback because the collector normally runs beside the local stack.
// Returning a value object makes collection and tests independent of process-global flag state.
func parseConfig() collectorConfig {
	config := collectorConfig{}
	flag.StringVar(&config.metricsURL, "metrics-url", "http://127.0.0.1:8083", "local metrics API URL")
	flag.StringVar(&config.deviceID, "device-id", os.Getenv("PULSEROUTE_DEVICE_ID"), "stable managed device identifier")
	flag.StringVar(&config.vpnProvider, "vpn-provider", "unknown", "configured VPN or SSE provider name")
	flag.StringVar(&config.vpnInterfacePrefix, "vpn-interface-prefix", "wg", "VPN interface name prefix")
	flag.StringVar(&config.probeHost, "probe-host", "", "approved ICMP probe target; omitted disables packet-loss probing")
	flag.StringVar(&config.dnsHost, "dns-host", "", "approved DNS probe name; omitted disables DNS reachability probing")
	flag.StringVar(&config.tlsHost, "tls-host", "", "approved TLS probe host on port 443; omitted disables TLS probing")
	flag.DurationVar(&config.interval, "interval", 30*time.Second, "watch collection interval")
	flag.Parse()
	if !safeIdentifier(config.deviceID) || !safeIdentifier(config.vpnProvider) || config.interval <= 0 {
		fatal(errors.New("device-id, vpn-provider, and positive interval are required"))
	}
	return config
}

// collectAndSend gathers one local snapshot and sends diagnostics before optional path quality data.
// It reports route availability even when no default interface exists, preserving failure visibility.
// Packet quality is posted only when an explicit approved probe host is configured.
// The returned error identifies local collection or metrics delivery failure without changing host state.
func collectAndSend(ctx context.Context, client *http.Client, config collectorConfig, previousInterface string) error {
	activeInterface, defaultRouteAvailable := defaultInterface()
	if activeInterface == "" {
		activeInterface = "none"
	}
	networkType := interfaceType(activeInterface)
	vpnConnected := vpnAvailable(config.vpnInterfacePrefix)
	cpuLoad1, memoryAvailableBytes := hostResources()
	diagnostics := telemetry.EndpointDiagnostics{
		DeviceID:              config.deviceID,
		NetworkType:           networkType,
		ActiveInterface:       activeInterface,
		VPNProvider:           config.vpnProvider,
		VPNConnected:          vpnConnected,
		InterfaceChanged:      previousInterface != "" && previousInterface != activeInterface,
		DefaultRouteAvailable: defaultRouteAvailable,
		CPULoad1:              cpuLoad1,
		MemoryAvailableBytes:  memoryAvailableBytes,
		WiFiSignalDBM:         wifiSignal(ctx, activeInterface),
	}
	if config.dnsHost != "" {
		reachable := dnsReachable(ctx, config.dnsHost)
		diagnostics.DNSReachable = &reachable
	}
	if config.tlsHost != "" {
		reachable := tlsReachable(ctx, config.tlsHost)
		diagnostics.TLSReachable = &reachable
	}
	if err := postJSON(ctx, client, config.metricsURL+"/v1/endpoint-diagnostics", diagnostics); err != nil {
		return fmt.Errorf("post endpoint diagnostics: %w", err)
	}
	if config.probeHost == "" {
		return nil
	}
	quality, err := probe(ctx, config.probeHost)
	if err != nil {
		return fmt.Errorf("probe approved target: %w", err)
	}
	receiveBytes, transmitBytes := interfaceCounters(activeInterface)
	endpointTelemetry := telemetry.EndpointTelemetry{
		DeviceID: config.deviceID, NetworkType: networkType, VPNProvider: config.vpnProvider, VPNConnected: vpnConnected,
		LatencyMS: quality.latencyMS, JitterMS: quality.jitterMS, LossPct: quality.lossPct, ReceiveBytes: receiveBytes, TransmitBytes: transmitBytes,
	}
	if err := postJSON(ctx, client, config.metricsURL+"/v1/endpoint-telemetry", endpointTelemetry); err != nil {
		return fmt.Errorf("post endpoint telemetry: %w", err)
	}
	return nil
}

// defaultInterface identifies the interface used by the Linux IPv4 default route.
// It reads the kernel route table instead of executing a shell or collecting full route metadata.
// A missing or unreadable default route returns an empty interface and false availability state.
// The caller reports only the interface name and availability, never the route gateway or prefix.
func defaultInterface() (string, bool) {
	routes, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", false
	}
	return parseDefaultInterface(string(routes))
}

// parseDefaultInterface extracts the active IPv4 default-route interface from procfs route contents.
// It ignores the header and requires a default destination with the route-up flag set.
// The pure parser allows route-table behavior to be tested without depending on the test host.
// It deliberately returns no gateway or destination data to preserve the collector's metadata boundary.
func parseDefaultInterface(routes string) (string, bool) {
	for _, line := range strings.Split(routes, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseInt(fields[3], 16, 64)
		if err == nil && flags&0x1 != 0 {
			return fields[0], true
		}
	}
	return "", false
}

// interfaceType classifies a local interface name into a coarse network transport category.
// It uses naming conventions only and does not inspect SSIDs, BSSIDs, carrier data, or locations.
// Unknown interfaces remain explicitly unknown rather than being misclassified as Wi-Fi or cellular.
// The category supports aggregate dashboards without exposing sensitive network identifiers.
func interfaceType(name string) string {
	switch {
	case strings.HasPrefix(name, "wl") || strings.HasPrefix(name, "wifi"):
		return "wifi"
	case strings.HasPrefix(name, "wwan") || strings.HasPrefix(name, "rmnet"):
		return "cellular"
	case strings.HasPrefix(name, "en") || strings.HasPrefix(name, "eth"):
		return "ethernet"
	default:
		return "unknown"
	}
}

// vpnAvailable reports whether any local interface has the configured VPN prefix.
// It checks interface names only and does not inspect tunnel configuration, keys, or peer addresses.
// Failure to enumerate interfaces returns false so diagnostics remain conservative under host errors.
// The configured provider label is supplied by the administrator rather than inferred from traffic.
func vpnAvailable(prefix string) bool {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, networkInterface := range interfaces {
		if strings.HasPrefix(networkInterface.Name, prefix) {
			return true
		}
	}
	return false
}

// hostResources reads aggregate CPU load and available memory from Linux procfs.
// It records resource pressure useful for diagnosing a poor endpoint experience without process data.
// Missing or malformed procfs values fall back to zero instead of interrupting telemetry collection.
// Values are local host aggregates and do not reveal application names, commands, or content.
func hostResources() (float64, int64) {
	loadAverage := 0.0
	if contents, err := os.ReadFile("/proc/loadavg"); err == nil {
		if fields := strings.Fields(string(contents)); len(fields) > 0 {
			loadAverage, _ = strconv.ParseFloat(fields[0], 64)
		}
	}
	memoryAvailableBytes := int64(0)
	if contents, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(contents), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "MemAvailable:" {
				kilobytes, _ := strconv.ParseInt(fields[1], 10, 64)
				memoryAvailableBytes = kilobytes * 1024
				break
			}
		}
	}
	return loadAverage, memoryAvailableBytes
}

// wifiSignal reads an optional dBm strength value from the Linux iw utility for a Wi-Fi interface.
// It does not read or transmit SSID, BSSID, channel, or any access-point identifier.
// Missing iw support, non-Wi-Fi interfaces, and command failures produce no signal field.
// The resulting pointer distinguishes unavailable radio data from a measured weak signal.
func wifiSignal(ctx context.Context, interfaceName string) *int {
	if interfaceType(interfaceName) != "wifi" {
		return nil
	}
	output, err := exec.CommandContext(ctx, "iw", "dev", interfaceName, "link").Output()
	if err != nil {
		return nil
	}
	matches := wifiSignalPattern.FindStringSubmatch(string(output))
	if len(matches) != 2 {
		return nil
	}
	signal, err := strconv.Atoi(matches[1])
	if err != nil || signal < -100 || signal > 0 {
		return nil
	}
	return &signal
}

// dnsReachable checks whether the configured approved probe name can currently resolve.
// It uses the operating system resolver and sends only the boolean outcome to the metrics service.
// The probe name itself remains local configuration and is not serialized as endpoint telemetry.
// A lookup error is reported as false so dashboards can correlate DNS availability with incidents.
func dnsReachable(ctx context.Context, host string) bool {
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	return err == nil && len(addresses) > 0
}

// tlsReachable attempts a TLS handshake with an approved host on the standard HTTPS port.
// It validates basic transport and TLS reachability but does not send HTTP requests or application data.
// The host stays local configuration and only the boolean handshake result is emitted.
// A short dialer timeout prevents a reachability check from delaying the collection loop indefinitely.
func tlsReachable(ctx context.Context, host string) bool {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return false
	}
	defer connection.Close()
	tlsConnection := tls.Client(connection, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host})
	defer tlsConnection.Close()
	return tlsConnection.HandshakeContext(ctx) == nil
}

// probe measures loss, latency, and jitter against an explicitly approved ICMP target.
// It uses a fixed three-packet Linux ping command and parses only aggregate timing output.
// The target is intentionally omitted from all metrics to avoid storing browsing or destination history.
// Command errors are returned so operators can distinguish an unavailable probe tool from packet loss.
func probe(ctx context.Context, host string) (probeResult, error) {
	output, err := exec.CommandContext(ctx, "ping", "-n", "-c", "3", "-W", "2", host).CombinedOutput()
	result, parsed := parsePingOutput(string(output))
	if !parsed {
		return probeResult{}, fmt.Errorf("parse ping output: %w", err)
	}
	return result, nil
}

// parsePingOutput extracts aggregate loss and individual timing samples from Linux ping output.
// It produces jitter as the population standard deviation across the successful ICMP responses.
// Full packet loss is valid telemetry and is represented with zero latency and jitter plus 100% loss.
// The parser is isolated so unit tests can verify behavior without invoking an external network command.
func parsePingOutput(output string) (probeResult, bool) {
	lossMatch := packetLossPattern.FindStringSubmatch(output)
	if len(lossMatch) != 2 {
		return probeResult{}, false
	}
	loss, err := strconv.ParseFloat(lossMatch[1], 64)
	if err != nil || loss < 0 || loss > 100 {
		return probeResult{}, false
	}
	times := pingTimePattern.FindAllStringSubmatch(output, -1)
	values := make([]float64, 0, len(times))
	for _, match := range times {
		value, err := strconv.ParseFloat(match[1], 64)
		if err == nil {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return probeResult{lossPct: loss}, loss == 100
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	average := total / float64(len(values))
	variance := 0.0
	for _, value := range values {
		variance += math.Pow(value-average, 2)
	}
	return probeResult{latencyMS: average, jitterMS: math.Sqrt(variance / float64(len(values))), lossPct: loss}, true
}

// interfaceCounters reads aggregate received and transmitted byte counters for one local interface.
// It reads the kernel-maintained sysfs counters rather than sampling packets or flow destinations.
// Missing counters return zero because diagnostics remain useful even when an interface disappears.
// The API stores these monotonically increasing counters for rate calculation in Grafana.
func interfaceCounters(interfaceName string) (int64, int64) {
	received := readCounter("/sys/class/net/" + interfaceName + "/statistics/rx_bytes")
	transmitted := readCounter("/sys/class/net/" + interfaceName + "/statistics/tx_bytes")
	return received, transmitted
}

// readCounter reads one non-negative integer kernel counter from an explicitly constructed sysfs path.
// It returns zero when the file is unavailable or malformed, avoiding collection failure on interface churn.
// The caller only passes a locally discovered interface name from procfs rather than remote input.
// This helper never reads packet data, process activity, or application traffic metadata.
func readCounter(path string) int64 {
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// postJSON sends one collector sample to the loopback or configured metrics ingestion API.
// It serializes only the supplied aggregate structure and sets an explicit JSON content type.
// Any non-202 response is returned as an error so watch mode can expose delivery failures.
// The request context bounds connection attempts and response processing to the caller lifecycle.
func postJSON(ctx context.Context, client *http.Client, endpoint string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("METRICS_AUTH_TOKEN"); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("metrics API returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	return nil
}

// safeIdentifier rejects empty identifiers and line-protocol separator characters in local configuration.
// It keeps device and VPN-provider values compatible with the metrics service tag validation rules.
// Configuration values never become shell fragments because all subprocess arguments are explicit.
// The check is intentionally narrow so managed deployments can use stable opaque endpoint identifiers.
func safeIdentifier(value string) bool {
	return value != "" && !strings.ContainsAny(value, ", =\n\r")
}

// fatal prints a collector startup or one-shot failure and exits with a non-zero status.
// Watch-mode sample failures are logged and retried, so they do not call this function.
// A nil error is ignored to keep command dispatch concise at successful return points.
// The stable prefix makes Linux service logs easy to filter alongside the enforcer output.
func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulseroute-telemetry:", err)
		os.Exit(1)
	}
}
