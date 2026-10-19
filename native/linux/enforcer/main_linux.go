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

// main parses the Linux enforcer command line and dispatches the requested root operation.
// It loads validated root-owned configuration before install, uninstall, watch, or evaluation.
// Mutating operations require root, while a single evaluation also validates the IPv4 input.
// Any returned error is reported through fatal so automation receives a non-zero exit code.
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

// loadConfig reads the root-owned JSON configuration used by the enforcement adapter.
// It unmarshals the document into Config and rejects missing or unsafe operational settings.
// The validation happens before any OS command can consume configuration-derived arguments.
// Successful calls return a complete configuration value without loading credentials or secrets.
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

// valid verifies every Config value that can influence routing, nftables, or tc commands.
// It limits interface names, marks, table IDs, rates, DSCP classes, and watch intervals.
// The rate relationship prevents a priority class from exceeding its physical link class.
// This predicate is the first defense against unsafe command arguments from configuration.
func (c Config) valid() bool {
	return c.AgentURL != "" && interfaceName.MatchString(c.WireGuardDevice) && c.RouteTable > 0 && c.FirewallMark > 0 && c.LinkRateMbit > 0 && c.HighPriorityRateMbit > 0 && c.HighPriorityRateMbit <= c.LinkRateMbit && validDSCP(c.HighPriorityDSCP) && c.HealthCheckIntervalSec > 0
}

// install creates the nftables, policy-route, and traffic-control objects owned by PulseRoute.
// It makes nftables setup idempotent before replacing deterministic route and QoS definitions.
// Matching destinations receive a firewall mark that selects the dedicated WireGuard route table.
// Any failed command stops installation and returns context needed for operational diagnosis.
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

// uninstall removes the nftables, QoS, policy-rule, and route-table state owned by this adapter.
// It never deletes the WireGuard interface or modifies resources outside the PulseRoute namespace.
// Best-effort cleanup supports fail-open behavior when an object is already absent or degraded.
// Route-table flushing is required so marked traffic cannot continue using an old overlay route.
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

// watch continuously reconciles enforcement against agent and WireGuard health.
// It performs an immediate reconciliation before waiting for the configured interval.
// Context cancellation stops the ticker and returns to the caller for service shutdown.
// Any reconciliation error is returned so the supervisor can surface an actionable failure.
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

// reconcile decides whether the Linux adapter should enforce or remove its routing configuration.
// It installs only when both the local decision agent and target WireGuard device are ready.
// If either dependency is unhealthy, it removes PulseRoute-owned state to fail open.
// Cleanup errors are wrapped so operators can distinguish rollback failure from health loss.
func reconcile(ctx context.Context, config Config) error {
	if agentReady(ctx, config.AgentURL) && wireGuardReady(ctx, config.WireGuardDevice) {
		return install(ctx, config)
	}
	if err := uninstall(ctx, config); err != nil {
		return fmt.Errorf("fail-open rollback: %w", err)
	}
	return nil
}

// agentReady probes the data-plane agent readiness endpoint with a short request timeout.
// It trims a trailing URL slash so configured endpoints produce one stable readiness path.
// Request construction, transport, and status failures all evaluate to not ready.
// Returning a boolean lets reconcile choose fail-open cleanup without exposing probe details.
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

// wireGuardReady verifies that the configured network interface exists and can be inspected.
// It executes a fixed ip-link command without a shell or configuration-derived command syntax.
// A non-zero command result is treated as unavailable for conservative fail-open operation.
// The interface is not created here because tunnel lifecycle remains externally managed.
func wireGuardReady(ctx context.Context, device string) bool {
	return runQuiet(ctx, "ip", "link", "show", "dev", device) == nil
}

// evaluate asks the local policy agent whether one hostname and IPv4 destination needs overlay routing.
// It posts a bounded JSON request, rejects failed agent responses, and decodes the decision.
// Only enforce-mode overlay decisions add destinations to nftables sets, and root is required.
// High-priority decisions additionally enter the DSCP and HTB QoS destination set.
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

// shouldRoute gates operating-system route changes on the agent's explicit enforcement decision.
// Observe mode always returns false even when the policy would otherwise select the overlay.
// Direct-internet decisions also return false so they never enter the nftables destination set.
// This small predicate is the central defense against accidental routing changes in observe mode.
func shouldRoute(result decision) bool {
	return result.Mode == "enforce" && result.Action == "priority-overlay"
}

// shouldPrioritize narrows QoS treatment to high-priority traffic that is already overlay eligible.
// It delegates overlay and enforce-mode verification to shouldRoute before inspecting priority.
// Standard-priority overlay traffic can route through the tunnel without receiving preferential QoS.
// The result controls only DSCP and tc classification, not whether a policy route is installed.
func shouldPrioritize(result decision) bool {
	return shouldRoute(result) && result.Priority == "high"
}

// validDSCP accepts only the small allowlist of DSCP classes supported by this adapter.
// It prevents configuration text from becoming arbitrary nftables tokens or command fragments.
// The accepted classes represent expedited, class-selector, and assured-forwarding treatment.
// Config validation calls this predicate before installation constructs the nftables command.
func validDSCP(value string) bool {
	return value == "cs5" || value == "ef" || value == "af41"
}

// rate formats an already-validated integer Mbps rate for the tc command-line interface.
// It appends the unit expected by HTB rate and ceiling arguments.
// Numeric validation is owned by Config.valid rather than repeated in this formatting helper.
// Keeping formatting centralized makes generated traffic-control commands consistent.
func rate(megabits int) string {
	return fmt.Sprintf("%dmbit", megabits)
}

// validateDestinationIPv4 accepts only syntactically valid IPv4 destination addresses.
// It rejects malformed input and all IPv6 addresses because the managed nftables sets are IPv4-only.
// Validation occurs before the address is included in an nftables element command.
// Future IPv6 support requires separate set types and matching routing configuration.
func validateDestinationIPv4(destinationIP string) error {
	address, err := netip.ParseAddr(destinationIP)
	if err != nil || !address.Is4() {
		return errors.New("destination-ip must be a valid IPv4 address")
	}
	return nil
}

// ensureNftObject makes an nftables table, set, or chain available without duplicating it.
// It first executes the fixed lookup command and returns early when the object already exists.
// Missing objects are created with the supplied fixed argument sequence and contextual errors.
// This idempotence allows recurring health reconciliation to reuse the same installation path.
func ensureNftObject(ctx context.Context, lookup, create []string) error {
	if err := run(ctx, "nft", append([]string{"list"}, lookup...)...); err == nil {
		return nil
	}
	if err := run(ctx, "nft", create...); err != nil {
		return fmt.Errorf("create nft %s: %w", strings.Join(lookup, " "), err)
	}
	return nil
}

// run executes one command with explicit arguments while inheriting standard output and error.
// It avoids shell interpretation, which prevents configuration values from becoming shell syntax.
// The supplied context cancels the subprocess when the calling operation is stopped or timed out.
// Callers wrap its error with command-specific context when an operation cannot continue.
func run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// runQuiet executes one fixed probe or cleanup command without forwarding normal output streams.
// It uses the caller's context and avoids a shell for the same command-injection boundary as run.
// Non-zero status is returned to the caller but does not pollute normal watchdog logs.
// Health checks and best-effort cleanup use this quieter execution path intentionally.
func runQuiet(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

// fatal reports a non-nil command error with the enforcer prefix and exits unsuccessfully.
// A nil error returns immediately so main can use it uniformly after each command dispatch.
// Reporting to standard error keeps stdout available for future machine-readable commands.
// The non-zero exit code lets systemd and shell automation detect failed enforcement operations.
func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulseroute-enforcer:", err)
		os.Exit(1)
	}
}
