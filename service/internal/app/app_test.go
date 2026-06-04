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
	if denied == "" || !containsAll(denied, []string{"tool: read_file", "status: denied", "permission denied", "Continue without this action"}) {
		t.Fatalf("unexpected denied result: %s", denied)
	}

	failed := formatToolResult("shell_exec", "partial", errors.New("boom"))
	if !containsAll(failed, []string{"tool: shell_exec", "status: failed", "partial", "boom", "Adjust the arguments"}) {
		t.Fatalf("unexpected failed result: %s", failed)
	}
}

func TestValidateToolCall(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     map[string]any
		repoPath string
		wantErr  bool
	}{
		{name: "read file ok", toolName: "read_file", args: map[string]any{"path": "src/app.ts"}, wantErr: false},
		{name: "read file missing path", toolName: "read_file", args: map[string]any{}, wantErr: true},
		{name: "write file allows empty content when provided", toolName: "write_file", args: map[string]any{"path": "a.txt", "content": ""}, wantErr: false},
		{name: "write file missing content key", toolName: "write_file", args: map[string]any{"path": "a.txt"}, wantErr: true},
		{name: "workspace symbols needs repo", toolName: "lsp_workspace_symbols", args: map[string]any{}, wantErr: true},
		{name: "workspace symbols with repo", toolName: "lsp_workspace_symbols", args: map[string]any{}, repoPath: "C:/repo", wantErr: false},
		{name: "definition requires line", toolName: "lsp_definition", args: map[string]any{"path": "a.go", "line": 0, "character": 2}, wantErr: true},
		{name: "subtask requires prompt", toolName: "run_subtask", args: map[string]any{"title": "x"}, wantErr: true},
		{name: "unknown tool malformed", toolName: "wat", args: map[string]any{}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateToolCall(test.toolName, test.args, test.repoPath)
			if test.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestToolLoopUnproductiveThreshold(t *testing.T) {
	loops := 0
	loops = nextUnproductiveToolLoops(loops, 1, 0)
	if loops != 1 {
		t.Fatalf("expected first unproductive loop, got %d", loops)
	}
	if shouldStopAfterToolLoop(loops) {
		t.Fatalf("should not stop after one unproductive loop")
	}
	loops = nextUnproductiveToolLoops(loops, 2, 0)
	if loops != 2 {
		t.Fatalf("expected second unproductive loop, got %d", loops)
	}
	if !shouldStopAfterToolLoop(loops) {
		t.Fatalf("expected stop after repeated unproductive loops")
	}
	loops = nextUnproductiveToolLoops(loops, 1, 1)
	if loops != 0 {
		t.Fatalf("expected productive loop to reset counter, got %d", loops)
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
