package mcp

import "testing"

func TestTransportNameDefaultsToStdio(t *testing.T) {
	if got := transportName(ServerConfig{}); got != "stdio" {
		t.Fatalf("expected stdio default, got %s", got)
	}
	if got := transportName(ServerConfig{Transport: "HTTP"}); got != "http" {
		t.Fatalf("expected lowercased transport, got %s", got)
	}
}
