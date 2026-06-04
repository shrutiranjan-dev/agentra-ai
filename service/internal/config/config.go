package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Host            string
	Port            string
	AuthToken       string
	OllamaBaseURL   string
	MongoURI        string
	MongoDatabase   string
	Neo4jURI        string
	Neo4jUsername   string
	Neo4jPassword   string
	DefaultModel    string
	PermissionTTLMS int
	LSPConfigPath   string
	MCPConfigPath   string
	AutoCompact     bool
	CompactLimit    int
	CompactKeep     int
}

type RuntimeConfig struct {
	DefaultModel  string
	LSPConfigPath string
	MCPConfigPath string
	AutoCompact   bool
	CompactLimit  int
	CompactKeep   int
	CommandDirs   []string
}

type fileConfig struct {
	Defaults struct {
		Model string `json:"model"`
	} `json:"defaults"`
	Integrations struct {
		LSPConfigPath string `json:"lspConfigPath"`
		MCPConfigPath string `json:"mcpConfigPath"`
	} `json:"integrations"`
	Compact struct {
		Enabled        *bool `json:"enabled"`
		MessageLimit   int   `json:"messageLimit"`
		PreserveRecent int   `json:"preserveRecent"`
	} `json:"compact"`
	Commands struct {
		Directories []string `json:"directories"`
	} `json:"commands"`
}

func Load() Config {
	token := os.Getenv("APP_AUTH_TOKEN")
	if token == "" {
		token = "local-dev-token"
	}
	return Config{
		Host:            getenv("APP_HOST", "127.0.0.1"),
		Port:            getenv("APP_PORT", "8088"),
		AuthToken:       token,
		OllamaBaseURL:   getenv("OLLAMA_BASE_URL", "http://127.0.0.1:11434"),
		MongoURI:        getenv("MONGODB_URI", "mongodb://localhost:27017"),
		MongoDatabase:   getenv("MONGODB_DATABASE", "assistant"),
		Neo4jURI:        getenv("NEO4J_URI", "neo4j://localhost:7687"),
		Neo4jUsername:   getenv("NEO4J_USERNAME", "neo4j"),
		Neo4jPassword:   getenv("NEO4J_PASSWORD", "password12345"),
		DefaultModel:    getenv("OLLAMA_DEFAULT_MODEL", "llama3.1"),
		PermissionTTLMS: 60000,
		LSPConfigPath:   getenv("LSP_CONFIG_PATH", ".opencode/lsp.json"),
		MCPConfigPath:   getenv("MCP_CONFIG_PATH", ".opencode/mcp.json"),
		AutoCompact:     getenvBool("AUTO_COMPACT", true),
		CompactLimit:    getenvInt("AUTO_COMPACT_MESSAGE_LIMIT", 24),
		CompactKeep:     getenvInt("AUTO_COMPACT_PRESERVE_RECENT", 8),
	}
}

func (c Config) Address() string {
	return c.Host + ":" + c.Port
}

func (c Config) Runtime(repoPath string) RuntimeConfig {
	runtime := RuntimeConfig{
		DefaultModel:  c.DefaultModel,
		LSPConfigPath: c.LSPConfigPath,
		MCPConfigPath: c.MCPConfigPath,
		AutoCompact:   c.AutoCompact,
		CompactLimit:  c.CompactLimit,
		CompactKeep:   c.CompactKeep,
		CommandDirs:   []string{},
	}

	for _, candidate := range configCandidates(repoPath) {
		overlay, err := loadFileConfig(candidate)
		if err != nil {
			continue
		}
		applyOverlay(&runtime, overlay, filepath.Dir(candidate))
	}

	runtime.CommandDirs = uniqueStrings(runtime.CommandDirs)
	return runtime
}

func configCandidates(repoPath string) []string {
	candidates := []string{}
	if userRoot := userOpenCodeRoot(); userRoot != "" {
		candidates = append(candidates, filepath.Join(userRoot, "config.json"))
	}
	if strings.TrimSpace(repoPath) != "" {
		candidates = append(candidates, filepath.Join(repoPath, ".opencode", "config.json"))
	}
	return candidates
}

func userOpenCodeRoot() string {
	for _, root := range []string{os.Getenv("USERPROFILE"), os.Getenv("HOME")} {
		if strings.TrimSpace(root) != "" {
			return filepath.Join(root, ".opencode")
		}
	}
	return ""
}

func loadFileConfig(path string) (fileConfig, error) {
	var cfg fileConfig
	bytes, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(bytes, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyOverlay(runtime *RuntimeConfig, overlay fileConfig, baseDir string) {
	if value := strings.TrimSpace(overlay.Defaults.Model); value != "" {
		runtime.DefaultModel = value
	}
	if value := strings.TrimSpace(overlay.Integrations.LSPConfigPath); value != "" {
		runtime.LSPConfigPath = resolveConfigPath(baseDir, value)
	}
	if value := strings.TrimSpace(overlay.Integrations.MCPConfigPath); value != "" {
		runtime.MCPConfigPath = resolveConfigPath(baseDir, value)
	}
	if overlay.Compact.Enabled != nil {
		runtime.AutoCompact = *overlay.Compact.Enabled
	}
	if overlay.Compact.MessageLimit > 0 {
		runtime.CompactLimit = overlay.Compact.MessageLimit
	}
	if overlay.Compact.PreserveRecent > 0 {
		runtime.CompactKeep = overlay.Compact.PreserveRecent
	}
	for _, dir := range overlay.Commands.Directories {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		runtime.CommandDirs = append(runtime.CommandDirs, resolveConfigPath(baseDir, dir))
	}
}

func resolveConfigPath(baseDir, value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return fallback
	}
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	var result int
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return fallback
		}
		result = result*10 + int(ch-'0')
	}
	if result <= 0 {
		return fallback
	}
	return result
}
