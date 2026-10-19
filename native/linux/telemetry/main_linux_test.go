//go:build linux

package main

import "testing"

// TestParseDefaultInterfaceFindsActiveDefaultRoute verifies procfs route parsing without host dependencies.
// It supplies a route table containing a loopback route and an active wireless default route.
// The parser must select only the default destination whose route-up flag is present.
// This protects collector interface-change telemetry from unrelated route-table rows.
func TestParseDefaultInterfaceFindsActiveDefaultRoute(t *testing.T) {
	routes := "Iface\tDestination\tGateway\tFlags\nlo\t0000007F\t00000000\t0001\nwlan0\t00000000\t0101A8C0\t0003\n"
	interfaceName, available := parseDefaultInterface(routes)
	if !available || interfaceName != "wlan0" {
		t.Fatalf("parseDefaultInterface() = %q, %t; want wlan0, true", interfaceName, available)
	}
}

// TestInterfaceTypeUsesCoarseTransportClasses verifies the privacy-preserving interface classifier.
// It checks representative wireless, cellular, Ethernet, and unknown interface naming patterns.
// The classification intentionally exposes no SSID, carrier, gateway, or location metadata.
// This keeps endpoint dashboards useful while retaining a narrow collection boundary.
func TestInterfaceTypeUsesCoarseTransportClasses(t *testing.T) {
	for name, expected := range map[string]string{"wlan0": "wifi", "wwan0": "cellular", "eth0": "ethernet", "tun0": "unknown"} {
		if actual := interfaceType(name); actual != expected {
			t.Errorf("interfaceType(%q) = %q, want %q", name, actual, expected)
		}
	}
}

// TestParsePingOutputCalculatesQuality verifies packet-loss, latency, and jitter extraction.
// It uses deterministic Linux ping output with three successful samples around ten milliseconds.
// The expected average and positive jitter demonstrate that timing variation is retained.
// The parser is tested without opening a network connection or invoking the ping executable.
func TestParsePingOutputCalculatesQuality(t *testing.T) {
	output := "3 packets transmitted, 3 received, 0% packet loss, time 2002ms\nrtt min/avg/max/mdev = 8.000/10.000/12.000/1.633 ms\n64 bytes from 192.0.2.1: icmp_seq=1 ttl=57 time=8.000 ms\n64 bytes from 192.0.2.1: icmp_seq=2 ttl=57 time=10.000 ms\n64 bytes from 192.0.2.1: icmp_seq=3 ttl=57 time=12.000 ms\n"
	result, ok := parsePingOutput(output)
	if !ok || result.lossPct != 0 || result.latencyMS != 10 || result.jitterMS <= 0 {
		t.Fatalf("unexpected ping result: %#v, parsed=%t", result, ok)
	}
}

// TestParsePingOutputAcceptsCompleteLoss verifies outages remain reportable endpoint telemetry.
// It provides common Linux ping output where all packets are lost and no timing samples exist.
// The parser must retain the 100 percent loss outcome instead of treating it as malformed output.
// This allows the metrics service to correlate a path outage with route-decision rollback events.
func TestParsePingOutputAcceptsCompleteLoss(t *testing.T) {
	result, ok := parsePingOutput("3 packets transmitted, 0 received, 100% packet loss, time 2002ms\n")
	if !ok || result.lossPct != 100 || result.latencyMS != 0 || result.jitterMS != 0 {
		t.Fatalf("unexpected full-loss result: %#v, parsed=%t", result, ok)
	}
}
