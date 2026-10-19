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
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Config contains the root-owned routing parameters for the Linux adapter.
type Config struct {
	AgentURL               string `json:"agentURL"`
	WireGuardDevice        string `json:"wireGuardDevice"`
	RouteTable             int    `json:"routeTable"`
	FirewallMark           int    `json:"firewallMark"`
	LinkRateMbit           int    `json:"linkRateMbit"`
	HighPriorityRateMbit   int    `json:"highPriorityRateMbit"`
	HighPriorityDSCP       string `json:"highPriorityDSCP"`
	HealthCheckIntervalSec int    `json:"healthCheckIntervalSec"`
}

type flowRequest struct {
	Hostname      string `json:"hostname"`
	DestinationIP string `json:"destinationIP"`
}

type decision struct {
	Action   string `json:"action"`
	Priority string `json:"priority"`
	Mode     string `json:"mode"`
}

var interfaceName = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)

// main installs routing primitives or evaluates one classified destination.
func main() {
	configPath := flag.String("config", "/etc/pulseroute/enforcer.json", "root-owned enforcer configuration")
	hostname := flag.String("hostname", "", "hostname associated with the destination")
	destinationIP := flag.String("destination-ip", "", "destination IP address")
	flag.Parse()
	if flag.NArg() != 1 || (flag.Arg(0) != "install" && flag.Arg(0) != "uninstall" && flag.Arg(0) != "watch" && flag.Arg(0) != "evaluate") {
		fmt.Fprintln(os.Stderr, "usage: pulseroute-enforcer [--config PATH] [--hostname HOST --destination-ip IP] install|uninstall|watch|evaluate")
		os.Exit(2)
	}
	config, err := loadConfig(*configPath)
	if err != nil {
		fatal(err)
	}
	if flag.Arg(0) == "install" {
		if os.Geteuid() != 0 {
			fatal(errors.New("install requires root"))
		}
		fatal(install(context.Background(), config))
		return
	}
	if flag.Arg(0) == "uninstall" {
		if os.Geteuid() != 0 {
			fatal(errors.New("uninstall requires root"))
		}
		fatal(uninstall(context.Background(), config))
		return
	}
	if flag.Arg(0) == "watch" {
		if os.Geteuid() != 0 {
			fatal(errors.New("watch requires root"))
		}
		fatal(watch(context.Background(), config))
		return
	}

	if *hostname == "" || *destinationIP == "" {
		fatal(errors.New("evaluate requires --hostname and --destination-ip"))
	}
	if err := validateDestinationIPv4(*destinationIP); err != nil {
		fatal(err)
	}
	fatal(evaluate(context.Background(), config, *hostname, *destinationIP))
}

// loadConfig reads and validates a root-owned configuration file without secrets.
func loadConfig(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return Config{}, err
	}
	if !config.valid() {
		return Config{}, errors.New("config requires agentURL, safe wireGuardDevice, positive routeTable and firewallMark, valid link rates, supported highPriorityDSCP, and positive healthCheckIntervalSec")
	}
	return config, nil
}

// valid checks configuration values before they are passed to operating-system commands.
func (c Config) valid() bool {
	return c.AgentURL != "" && interfaceName.MatchString(c.WireGuardDevice) && c.RouteTable > 0 && c.FirewallMark > 0 && c.LinkRateMbit > 0 && c.HighPriorityRateMbit > 0 && c.HighPriorityRateMbit <= c.LinkRateMbit && validDSCP(c.HighPriorityDSCP) && c.HealthCheckIntervalSec > 0
}

// install creates a dedicated mark, nftables destination set, and WireGuard route table.
func install(ctx context.Context, config Config) error {
	if err := ensureNftObject(ctx, []string{"table", "inet", "pulseroute"}, []string{"add", "table", "inet", "pulseroute"}); err != nil {
		return err
	}
	if err := ensureNftObject(ctx, []string{"set", "inet", "pulseroute", "priority_destinations"}, []string{"add", "set", "inet", "pulseroute", "priority_destinations", "{", "type", "ipv4_addr", ";", "flags", "timeout", ";", "}"}); err != nil {
		return err
	}
	if err := ensureNftObject(ctx, []string{"set", "inet", "pulseroute", "high_priority_destinations"}, []string{"add", "set", "inet", "pulseroute", "high_priority_destinations", "{", "type", "ipv4_addr", ";", "flags", "timeout", ";", "}"}); err != nil {
		return err
	}
	if err := ensureNftObject(ctx, []string{"chain", "inet", "pulseroute", "output"}, []string{"add", "chain", "inet", "pulseroute", "output", "{", "type", "route", "hook", "output", "priority", "mangle", ";", "policy", "accept", ";", "ip", "daddr", "@priority_destinations", "meta", "mark", "set", fmt.Sprint(config.FirewallMark), ";", "}"}); err != nil {
		return err
	}
	if err := ensureNftObject(ctx, []string{"chain", "inet", "pulseroute", "qos_output"}, []string{"add", "chain", "inet", "pulseroute", "qos_output", "{", "type", "route", "hook", "output", "priority", "mangle", ";", "policy", "accept", ";", "ip", "daddr", "@high_priority_destinations", "ip", "dscp", "set", config.HighPriorityDSCP, "meta", "priority", "set", "1:10", ";", "}"}); err != nil {
		return err
	}
	commands := [][]string{
		{"ip", "route", "replace", "default", "dev", config.WireGuardDevice, "table", fmt.Sprint(config.RouteTable)},
		{"ip", "rule", "replace", "fwmark", fmt.Sprint(config.FirewallMark), "lookup", fmt.Sprint(config.RouteTable)},
		{"tc", "qdisc", "replace", "dev", config.WireGuardDevice, "root", "handle", "1:", "htb", "default", "30"},
		{"tc", "class", "replace", "dev", config.WireGuardDevice, "parent", "1:", "classid", "1:1", "htb", "rate", rate(config.LinkRateMbit), "ceil", rate(config.LinkRateMbit)},
		{"tc", "class", "replace", "dev", config.WireGuardDevice, "parent", "1:1", "classid", "1:10", "htb", "rate", rate(config.HighPriorityRateMbit), "ceil", rate(config.LinkRateMbit), "prio", "0"},
		{"tc", "class", "replace", "dev", config.WireGuardDevice, "parent", "1:1", "classid", "1:30", "htb", "rate", "1mbit", "ceil", rate(config.LinkRateMbit), "prio", "3"},
	}
	for _, command := range commands {
		if err := run(ctx, command[0], command[1:]...); err != nil {
			return fmt.Errorf("install %s: %w", strings.Join(command, " "), err)
		}
	}
	return nil
}

// uninstall removes only the route and firewall objects owned by this adapter.
func uninstall(ctx context.Context, config Config) error {
	if runQuiet(ctx, "nft", "list", "table", "inet", "pulseroute") == nil {
		if err := run(ctx, "nft", "delete", "table", "inet", "pulseroute"); err != nil {
			return fmt.Errorf("uninstall nft table: %w", err)
		}
	}
	// These resources no longer affect routing after the nftables table is absent. Their
	// removal is best effort so a first-start health failure remains fail-open.
	_ = runQuiet(ctx, "tc", "qdisc", "delete", "dev", config.WireGuardDevice, "root")
	_ = runQuiet(ctx, "ip", "rule", "delete", "fwmark", fmt.Sprint(config.FirewallMark), "lookup", fmt.Sprint(config.RouteTable))
	if err := run(ctx, "ip", "route", "flush", "table", fmt.Sprint(config.RouteTable)); err != nil {
		return fmt.Errorf("uninstall route table: %w", err)
	}
	return nil
}

// watch maintains enforcement only while the decision agent and WireGuard interface are healthy.
func watch(ctx context.Context, config Config) error {
	interval := time.Duration(config.HealthCheckIntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := reconcile(ctx, config); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// reconcile installs enforcement only for a healthy agent and WireGuard device, otherwise fails open.
func reconcile(ctx context.Context, config Config) error {
	if agentReady(ctx, config.AgentURL) && wireGuardReady(ctx, config.WireGuardDevice) {
		return install(ctx, config)
	}
	if err := uninstall(ctx, config); err != nil {
		return fmt.Errorf("fail-open rollback: %w", err)
	}
	return nil
}

// agentReady checks the local agent's dependency-aware readiness endpoint.
func agentReady(ctx context.Context, agentURL string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(agentURL, "/")+"/readyz", nil)
	if err != nil {
		return false
	}
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusNoContent
}

// wireGuardReady checks that the configured WireGuard device exists and is up.
func wireGuardReady(ctx context.Context, device string) bool {
	return runQuiet(ctx, "ip", "link", "show", "dev", device) == nil
}

// evaluate asks the decision agent whether a destination should receive the priority mark.
func evaluate(ctx context.Context, config Config, hostname, destinationIP string) error {
	body, err := json.Marshal(flowRequest{Hostname: hostname, DestinationIP: destinationIP})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(config.AgentURL, "/")+"/v1/flows/evaluate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("decision agent returned %s", response.Status)
	}
	var result decision
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result); err != nil {
		return err
	}
	if !shouldRoute(result) {
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("priority decision received, but adding an nftables destination requires root")
	}
	if err := run(ctx, "nft", "add", "element", "inet", "pulseroute", "priority_destinations", "{", destinationIP, "timeout", "10m", "}"); err != nil {
		return err
	}
	if shouldPrioritize(result) {
		return run(ctx, "nft", "add", "element", "inet", "pulseroute", "high_priority_destinations", "{", destinationIP, "timeout", "10m", "}")
	}
	return nil
}

// shouldRoute permits operating-system route changes only for an enforce-mode overlay decision.
func shouldRoute(result decision) bool {
	return result.Mode == "enforce" && result.Action == "priority-overlay"
}

// shouldPrioritize limits DSCP and traffic-control priority to high-priority overlay traffic.
func shouldPrioritize(result decision) bool {
	return shouldRoute(result) && result.Priority == "high"
}

// validDSCP restricts nftables input to the supported, safe DSCP class names.
func validDSCP(value string) bool {
	return value == "cs5" || value == "ef" || value == "af41"
}

// rate formats a positive configured link rate for tc.
func rate(megabits int) string {
	return fmt.Sprintf("%dmbit", megabits)
}

// validateDestinationIPv4 rejects malformed and IPv6 input because the nftables set is IPv4-only.
func validateDestinationIPv4(destinationIP string) error {
	address, err := netip.ParseAddr(destinationIP)
	if err != nil || !address.Is4() {
		return errors.New("destination-ip must be a valid IPv4 address")
	}
	return nil
}

// ensureNftObject creates an nftables object only if it is not already present.
func ensureNftObject(ctx context.Context, lookup, create []string) error {
	if err := run(ctx, "nft", append([]string{"list"}, lookup...)...); err == nil {
		return nil
	}
	if err := run(ctx, "nft", create...); err != nil {
		return fmt.Errorf("create nft %s: %w", strings.Join(lookup, " "), err)
	}
	return nil
}

// run invokes one fixed system command without a shell.
func run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// runQuiet invokes a fixed health-check command without emitting normal failure output.
func runQuiet(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

// fatal exits only when a command reports an error.
func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulseroute-enforcer:", err)
		os.Exit(1)
	}
}