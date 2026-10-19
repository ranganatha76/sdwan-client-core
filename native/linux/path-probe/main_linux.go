//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type configuredPath struct {
	name          string
	interfaceName string
	target        string
}

type pathList []configuredPath

type pathMeasurement struct {
	Name        string    `json:"name"`
	Latency     int       `json:"latency"`
	LossPercent float64   `json:"lossPercent"`
	Jitter      int       `json:"jitter"`
	MeasuredAt  time.Time `json:"measuredAt"`
}

type probeResult struct {
	latencyMS float64
	jitterMS  float64
	lossPct   float64
}

var (
	interfaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)
	targetPattern        = regexp.MustCompile(`^[a-zA-Z0-9:.-]{1,253}$`)
	packetLossPattern    = regexp.MustCompile(`([0-9.]+)% packet loss`)
	pingTimePattern      = regexp.MustCompile(`time=([0-9.]+) ?ms`)
)

// Set parses one administrator-approved path in name,interface,target form.
func (paths *pathList) Set(value string) error {
	parts := strings.SplitN(value, ",", 3)
	if len(parts) != 3 {
		return errors.New("path must be name,interface,target")
	}
	path := configuredPath{name: strings.TrimSpace(parts[0]), interfaceName: strings.TrimSpace(parts[1]), target: strings.TrimSpace(parts[2])}
	if path.name == "" || !interfaceNamePattern.MatchString(path.interfaceName) || !targetPattern.MatchString(path.target) {
		return errors.New("path requires a name, safe interface, and safe target")
	}
	for _, existing := range *paths {
		if existing.name == path.name {
			return fmt.Errorf("duplicate path name %q", path.name)
		}
	}
	*paths = append(*paths, path)
	return nil
}

func (paths *pathList) String() string {
	values := make([]string, 0, len(*paths))
	for _, path := range *paths {
		values = append(values, strings.Join([]string{path.name, path.interfaceName, path.target}, ","))
	}
	return strings.Join(values, ";")
}

// main posts one complete path cycle or continuously posts cycles to the local control plane.
func main() {
	controlPlaneURL := flag.String("control-plane-url", "http://127.0.0.1:8080", "local control-plane API URL")
	interval := flag.Duration("interval", 30*time.Second, "watch interval")
	var paths pathList
	flag.Var(&paths, "path", "approved path as name,interface,target; repeat for every configured control-plane path")
	flag.Parse()
	if flag.NArg() != 1 || (flag.Arg(0) != "once" && flag.Arg(0) != "watch") || len(paths) == 0 || *interval < 10*time.Second {
		fatal(errors.New("usage: pulseroute-path-probe --path name,interface,target [--path ...] [--control-plane-url URL] [--interval 30s] once|watch"))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}
	if flag.Arg(0) == "once" {
		fatal(probeAndPost(ctx, client, *controlPlaneURL, paths))
		return
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		if err := probeAndPost(ctx, client, *controlPlaneURL, paths); err != nil {
			fmt.Fprintln(os.Stderr, "pulseroute-path-probe:", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// probeAndPost keeps one timestamp across the entire cycle so the control plane receives an atomic comparison.
func probeAndPost(ctx context.Context, client *http.Client, controlPlaneURL string, paths []configuredPath) error {
	measurements := make([]pathMeasurement, 0, len(paths))
	measuredAt := time.Now().UTC()
	for _, path := range paths {
		result, err := probePath(ctx, path)
		if err != nil {
			return fmt.Errorf("probe %q: %w", path.name, err)
		}
		measurements = append(measurements, pathMeasurement{
			Name: path.name, Latency: int(math.Round(result.latencyMS)), Jitter: int(math.Round(result.jitterMS)), LossPercent: result.lossPct, MeasuredAt: measuredAt,
		})
	}
	body, err := json.Marshal(measurements)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(controlPlaneURL, "/")+"/api/paths/measurements", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("control plane returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	return nil
}

// probePath binds the Linux ICMP probe to one configured underlay interface without invoking a shell.
func probePath(ctx context.Context, path configuredPath) (probeResult, error) {
	output, err := exec.CommandContext(ctx, "ping", "-n", "-I", path.interfaceName, "-c", "3", "-W", "2", path.target).CombinedOutput()
	result, parsed := parsePingOutput(string(output))
	if !parsed {
		return probeResult{}, fmt.Errorf("parse ping output: %w", err)
	}
	return result, nil
}

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
		value, parseErr := strconv.ParseFloat(match[1], 64)
		if parseErr == nil {
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

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulseroute-path-probe:", err)
		os.Exit(1)
	}
}
