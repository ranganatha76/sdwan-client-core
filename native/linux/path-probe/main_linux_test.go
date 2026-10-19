//go:build linux

package main

import "testing"

func TestPathListRejectsDuplicateNames(t *testing.T) {
	paths := pathList{}
	if err := paths.Set("Fiber primary,wlan0,192.0.2.1"); err != nil {
		t.Fatalf("first path rejected: %v", err)
	}
	if err := paths.Set("Fiber primary,wwan0,192.0.2.2"); err == nil {
		t.Fatal("duplicate path name was accepted")
	}
}

func TestParsePingOutputCalculatesPathQuality(t *testing.T) {
	output := "3 packets transmitted, 3 received, 0% packet loss, time 2002ms\n64 bytes from 192.0.2.1: icmp_seq=1 ttl=57 time=8.000 ms\n64 bytes from 192.0.2.1: icmp_seq=2 ttl=57 time=10.000 ms\n64 bytes from 192.0.2.1: icmp_seq=3 ttl=57 time=12.000 ms\n"
	result, ok := parsePingOutput(output)
	if !ok || result.lossPct != 0 || result.latencyMS != 10 || result.jitterMS <= 0 {
		t.Fatalf("unexpected result: %#v, parsed=%t", result, ok)
	}
}
