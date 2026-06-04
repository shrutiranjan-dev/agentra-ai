package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg := Load()
	if cfg.Host == "" || cfg.Port == "" || cfg.OllamaBaseURL == "" {
		t.Fatal("expected default config values")
	}
	if cfg.AuthToken == "" {
		t.Fatal("expected auth token to be generated")
	}
}

func TestRuntimeAppliesUserThenProjectOverlay(t *testing.T) {
	tempDir := t.TempDir()
	userDir := filepath.Join(tempDir, "user-home")
	repoDir := filepath.Join(tempDir, "repo")
	if err := os.MkdirAll(filepath.Join(userDir, ".opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, ".opencode"), 0o755); err != nil {
		t.Fatal(err)
	}

	userConfig := `{
		"defaults": {"model": "user-model"},
		"integrations": {"lspConfigPath": "./lsp-user.json"},
		"compact": {"enabled": false, "messageLimit": 10, "preserveRecent": 3},
		"commands": {"directories": ["./commands-user"]}
	}`
	projectConfig := `{
		"defaults": {"model": "project-model"},
		"integrations": {"mcpConfigPath": "./mcp-project.json"},
		"compact": {"enabled": true, "preserveRecent": 5},
		"commands": {"directories": ["./commands-project"]}
	}`

	if err := os.WriteFile(filepath.Join(userDir, ".opencode", "config.json"), []byte(userConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".opencode", "config.json"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	originalUser := os.Getenv("USERPROFILE")
	originalHome := os.Getenv("HOME")
	defer func() {
		_ = os.Setenv("USERPROFILE", originalUser)
		_ = os.Setenv("HOME", originalHome)
	}()
	_ = os.Setenv("USERPROFILE", userDir)
	_ = os.Setenv("HOME", userDir)

	cfg := Load()
	runtime := cfg.Runtime(repoDir)

	if runtime.DefaultModel != "project-model" {
		t.Fatalf("expected project model to win, got %s", runtime.DefaultModel)
	}
	if runtime.LSPConfigPath != filepath.Join(userDir, ".opencode", "lsp-user.json") {
		t.Fatalf("unexpected lsp path: %s", runtime.LSPConfigPath)
	}
	if runtime.MCPConfigPath != filepath.Join(repoDir, ".opencode", "mcp-project.json") {
		t.Fatalf("unexpected mcp path: %s", runtime.MCPConfigPath)
	}
	if !runtime.AutoCompact || runtime.CompactLimit != 10 || runtime.CompactKeep != 5 {
		t.Fatalf("unexpected compact settings: %+v", runtime)
	}
	if len(runtime.CommandDirs) != 2 {
		t.Fatalf("expected merged command dirs, got %v", runtime.CommandDirs)
	}
}
