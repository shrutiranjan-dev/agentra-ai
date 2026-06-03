package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg := Load()
	if cfg.Host == "" || cfg.Port == "" || cfg.OllamaBaseURL == "" {
		t.Fatal("expected default config values")
	}
	if cfg.AuthToken == "" {
		t.Fatal("expected auth token to be generated")
	}
}
