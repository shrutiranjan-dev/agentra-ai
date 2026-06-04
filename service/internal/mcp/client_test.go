package mcp

import (
	"bufio"
	"context"
	"strings"
	"testing"
)

func TestTransportNameDefaultsToStdio(t *testing.T) {
	if got := transportName(ServerConfig{}); got != "stdio" {
		t.Fatalf("expected stdio default, got %s", got)
	}
	if got := transportName(ServerConfig{Transport: "HTTP"}); got != "http" {
		t.Fatalf("expected lowercased transport, got %s", got)
	}
}

func TestResolveSSEURL(t *testing.T) {
	got := resolveSSEURL("http://127.0.0.1:8080/sse", "/messages")
	if got != "http://127.0.0.1:8080/messages" {
		t.Fatalf("expected resolved absolute url, got %s", got)
	}
}

func TestDiscoverSSEMessageURLUsesOverride(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(""))
	got, err := discoverSSEMessageURL(context.Background(), ServerConfig{
		URL:        "http://127.0.0.1:8080/sse",
		MessageURL: "/messages",
	}, reader)
	if err != nil {
		t.Fatalf("discoverSSEMessageURL returned error: %v", err)
	}
	if got != "http://127.0.0.1:8080/messages" {
		t.Fatalf("expected override message url, got %s", got)
	}
}

func TestReadSSEEvent(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("event: endpoint\ndata: /messages\n\n"))
	event, err := readSSEEvent(context.Background(), reader)
	if err != nil {
		t.Fatalf("readSSEEvent returned error: %v", err)
	}
	if event.Name != "endpoint" {
		t.Fatalf("expected endpoint event, got %s", event.Name)
	}
	if event.Data != "/messages" {
		t.Fatalf("expected /messages data, got %s", event.Data)
	}
}

func TestStatusDetailIncludesTimeout(t *testing.T) {
	detail := statusDetail(ServerConfig{Transport: "http", TimeoutMs: 2500})
	if !strings.Contains(detail, "2500ms") {
		t.Fatalf("expected timeout detail, got %s", detail)
	}
}
