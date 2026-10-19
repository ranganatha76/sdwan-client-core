//go:build linux

package main

import "testing"

// TestShouldMarkRequiresEnforceMode ensures observe-mode decisions never change packet routing.
func TestShouldMarkRequiresEnforceMode(t *testing.T) {
	tests := []struct {
		name   string
		result decision
		want   bool
	}{
		{name: "enforce priority", result: decision{Mode: "enforce", Action: "priority-overlay"}, want: true},
		{name: "observe priority", result: decision{Mode: "observe", Action: "priority-overlay"}, want: false},
		{name: "enforce direct", result: decision{Mode: "enforce", Action: "direct-internet"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldMark(test.result); got != test.want {
				t.Fatalf("shouldMark(%+v) = %t, want %t", test.result, got, test.want)
			}
		})
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