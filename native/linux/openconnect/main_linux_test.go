//go:build linux

package main

import "testing"

func TestConfigValidationAllowsSupportedProtocols(t *testing.T) {
	for _, protocol := range []string{"anyconnect", "gp"} {
		config := Config{Protocol: protocol, Gateway: "https://vpn.example.test/connect", Interface: "pr-vpn0", PIDFile: "/run/pulseroute/openconnect.pid"}
		if err := config.Validate(); err != nil {
			t.Errorf("%s configuration was rejected: %v", protocol, err)
		}
	}
}

func TestConfigValidationRejectsUnsupportedProtocolAndPIDPath(t *testing.T) {
	config := Config{Protocol: "zscaler", Gateway: "https://vpn.example.test", Interface: "pr-vpn0", PIDFile: "/tmp/openconnect.pid"}
	if err := config.Validate(); err == nil {
		t.Fatal("unsupported protocol and pid path were accepted")
	}
}

func TestOpenConnectArgumentsExcludeCredentials(t *testing.T) {
	config := Config{Protocol: "anyconnect", Gateway: "https://vpn.example.test", Interface: "pr-vpn0", Username: "managed-user", AuthGroup: "engineering", PIDFile: "/run/pulseroute/openconnect.pid"}
	arguments := openConnectArguments(config)
	want := "--protocol=anyconnect --interface=pr-vpn0 --background --pid-file=/run/pulseroute/openconnect.pid --timestamp --reconnect-timeout=60 --authgroup=engineering --user=managed-user https://vpn.example.test"
	actual := ""
	for index, argument := range arguments {
		if index > 0 {
			actual += " "
		}
		actual += argument
	}
	if actual != want {
		t.Fatalf("arguments = %q, want %q", actual, want)
	}
}
