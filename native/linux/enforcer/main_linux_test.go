//go:build linux

package main

import "testing"

// TestShouldRouteRequiresEnforceMode ensures observe-mode decisions never change packet routing.
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

// TestShouldPrioritizeRequiresHighPriority ensures only high-priority overlay traffic receives QoS treatment.
func TestShouldPrioritizeRequiresHighPriority(t *testing.T) {
	if !shouldPrioritize(decision{Mode: "enforce", Action: "priority-overlay", Priority: "high"}) {
		t.Fatal("high-priority overlay decision was not prioritized")
	}
	if shouldPrioritize(decision{Mode: "enforce", Action: "priority-overlay", Priority: "standard"}) {
		t.Fatal("standard overlay decision was prioritized")
	}
}

// TestValidDSCPAcceptsOnlySupportedValues ensures nftables DSCP input cannot be injected through configuration.
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

// TestRateFormatsTrafficControlRates ensures tc receives the configured rate unit.
func TestRateFormatsTrafficControlRates(t *testing.T) {
	if got := rate(100); got != "100mbit" {
		t.Fatalf("rate(100) = %q, want 100mbit", got)
	}
}

// TestConfigValidationRequiresWatchInterval ensures the fail-open watchdog cannot use a busy loop.
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

// TestValidateDestinationIPv4 rejects values that cannot safely enter the IPv4 nftables set.
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