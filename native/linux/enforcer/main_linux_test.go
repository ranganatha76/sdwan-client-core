//go:build linux

package main

import "testing"

// TestShouldRouteRequiresEnforceMode verifies the primary observe-only safety boundary.
// It covers an enforce overlay decision and two decisions that must not mutate packet routing.
// Table-driven cases make the required action and mode combinations explicit.
// A failure would indicate that observe mode could incorrectly install an overlay route.
func TestShouldRouteRequiresEnforceMode(t *testing.T) {
	tests := []struct {
		name   string
		result decision
		want   bool
	}{
		{name: "enforce overlay", result: decision{Mode: "enforce", Action: "priority-overlay"}, want: true},
		{name: "observe overlay", result: decision{Mode: "observe", Action: "priority-overlay"}, want: false},
		{name: "enforce direct", result: decision{Mode: "enforce", Action: "direct-internet"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldRoute(test.result); got != test.want {
				t.Fatalf("shouldRoute(%+v) = %t, want %t", test.result, got, test.want)
			}
		})
	}
}

// TestShouldPrioritizeRequiresHighPriority verifies that QoS is narrower than overlay routing.
// It accepts an enforce-mode high-priority decision and rejects a standard-priority equivalent.
// The assertions protect DSCP and HTB treatment from being applied to ordinary overlay traffic.
// Routing itself is tested separately because priority classification is an additional gate.
func TestShouldPrioritizeRequiresHighPriority(t *testing.T) {
	if !shouldPrioritize(decision{Mode: "enforce", Action: "priority-overlay", Priority: "high"}) {
		t.Fatal("high-priority overlay decision was not prioritized")
	}
	if shouldPrioritize(decision{Mode: "enforce", Action: "priority-overlay", Priority: "standard"}) {
		t.Fatal("standard overlay decision was prioritized")
	}
}

// TestValidDSCPAcceptsOnlySupportedValues verifies the DSCP configuration allowlist.
// It accepts every supported class and rejects empty, differently cased, and injected values.
// The malicious-looking input demonstrates that arbitrary nftables text cannot pass validation.
// This test protects the command-construction boundary in the root-owned enforcer.
func TestValidDSCPAcceptsOnlySupportedValues(t *testing.T) {
	for _, value := range []string{"cs5", "ef", "af41"} {
		if !validDSCP(value) {
			t.Fatalf("supported DSCP %q rejected", value)
		}
	}
	for _, value := range []string{"", "CS5", "cs5; flush ruleset"} {
		if validDSCP(value) {
			t.Fatalf("unsupported DSCP %q accepted", value)
		}
	}
}

// TestRateFormatsTrafficControlRates verifies the unit syntax passed to the tc utility.
// It uses a representative 100 Mbps input rather than relying on command execution.
// The assertion preserves the expected lowercase mbit suffix required by HTB configuration.
// This small unit test keeps traffic-control formatting deterministic and reviewable.
func TestRateFormatsTrafficControlRates(t *testing.T) {
	if got := rate(100); got != "100mbit" {
		t.Fatalf("rate(100) = %q, want 100mbit", got)
	}
}

// TestConfigValidationRequiresWatchInterval verifies a complete valid watchdog configuration.
// It then removes only the health interval and expects the same configuration to be rejected.
// The check prevents the continuous reconcile loop from becoming an unbounded busy loop.
// Remaining fields also exercise the normal positive configuration path for Config.valid.
func TestConfigValidationRequiresWatchInterval(t *testing.T) {
	config := Config{
		AgentURL:               "http://127.0.0.1:8082",
		WireGuardDevice:        "wg-pulseroute",
		RouteTable:             51820,
		FirewallMark:           51820,
		LinkRateMbit:           100,
		HighPriorityRateMbit:   40,
		HighPriorityDSCP:       "cs5",
		HealthCheckIntervalSec: 10,
	}
	if !config.valid() {
		t.Fatal("valid configuration was rejected")
	}
	config.HealthCheckIntervalSec = 0
	if config.valid() {
		t.Fatal("zero health check interval was accepted")
	}
}

// TestValidateDestinationIPv4 verifies the address boundary for the IPv4 nftables set.
// It accepts one valid IPv4 address and rejects empty, malformed, IPv6, and invalid-octet values.
// The cases ensure unsafe destination strings never reach nftables element construction.
// IPv6 rejection is intentional until the adapter owns separate IPv6 route and set handling.
func TestValidateDestinationIPv4(t *testing.T) {
	if err := validateDestinationIPv4("140.82.112.6"); err != nil {
		t.Fatalf("valid IPv4 rejected: %v", err)
	}
	for _, destination := range []string{"", "not-an-ip", "2001:db8::1", "1.2.3.999"} {
		if err := validateDestinationIPv4(destination); err == nil {
			t.Fatalf("invalid destination %q was accepted", destination)
		}
	}
}
