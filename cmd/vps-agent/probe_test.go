package main

import (
	"net"
	"testing"
)

func TestProbeIPFiltering(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.1.1", "100.64.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "::1", "fc00::1", "fe80::1", "2001:db8::1"} {
		if isPublicProbeIP(net.ParseIP(value)) {
			t.Fatalf("reserved address accepted: %s", value)
		}
	}
	for _, value := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !isPublicProbeIP(net.ParseIP(value)) {
			t.Fatalf("public address rejected: %s", value)
		}
	}
}
