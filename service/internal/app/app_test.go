package app

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveToolMetaAssignsDefaults(t *testing.T) {
	meta := resolveToolMeta("session-1", "shell_exec", "pwd", "C:/repo", "run pwd")

	if meta.sessionID != "session-1" {
		t.Fatalf("expected session id to be set")
	}
	if meta.toolName != "shell_exec" {
		t.Fatalf("expected tool name to be set")
	}
	if meta.toolCallID == "" {
		t.Fatalf("expected tool call id to be generated")
	}
}

func TestFormatToolResultStatuses(t *testing.T) {
	denied := formatToolResult("read_file", "", ErrPermissionDenied)
	if denied == "" || !containsAll(denied, []string{"tool: read_file", "status: denied", "permission denied"}) {
		t.Fatalf("unexpected denied result: %s", denied)
	}

	failed := formatToolResult("shell_exec", "partial", errors.New("boom"))
	if !containsAll(failed, []string{"tool: shell_exec", "status: failed", "partial", "boom"}) {
		t.Fatalf("unexpected failed result: %s", failed)
	}
}

func containsAll(value string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
