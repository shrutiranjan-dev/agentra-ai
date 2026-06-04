package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/config"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/domain"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/events"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/lsp"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/mcp"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/ollama"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/store"
)

const autoCompactPrefix = "[auto-compact summary]"

type RepoRetrievalPreview struct {
	RepoPath      string              `json:"repoPath"`
	SessionID     string              `json:"sessionId,omitempty"`
	Prompt        string              `json:"prompt"`
	FileMatches   []store.FileMatch   `json:"fileMatches"`
	MemoryMatches []store.MemoryMatch `json:"memoryMatches"`
	SymbolMatches []store.SymbolMatch `json:"symbolMatches"`
	Snippets      []string            `json:"snippets"`
}

func (a *App) availableTools(repoPath string) []ollama.Tool {
	tools := []ollama.Tool{
		toolSchema("get_status", "Get backend dependency status", map[string]any{"type": "object", "properties": map[string]any{}}),
		toolSchema("list_files", "List files under a directory", map[string]any{
			"type":       "object",
			"required":   []string{"path"},
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
		}),
		toolSchema("read_file", "Read a file from disk", map[string]any{
			"type":       "object",
			"required":   []string{"path"},
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
		}),
		toolSchema("write_file", "Write content to a file", map[string]any{
			"type":     "object",
			"required": []string{"path", "content"},
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
		}),
		toolSchema("file_edit", "Apply a search and replace edit to a file", map[string]any{
			"type":     "object",
			"required": []string{"path", "old_text", "new_text"},
			"properties": map[string]any{
				"path":        map[string]any{"type": "string"},
				"old_text":    map[string]any{"type": "string"},
				"new_text":    map[string]any{"type": "string"},
				"replace_all": map[string]any{"type": "boolean"},
			},
		}),
		toolSchema("apply_patch", "Apply SEARCH/REPLACE patch blocks to one or more files", map[string]any{
			"type":     "object",
			"required": []string{"patch"},
			"properties": map[string]any{
				"path":  map[string]any{"type": "string"},
				"patch": map[string]any{"type": "string"},
			},
		}),
		toolSchema("shell_exec", "Execute a shell command in the workspace", map[string]any{
			"type":     "object",
			"required": []string{"command"},
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
				"cwd":     map[string]any{"type": "string"},
			},
		}),
		toolSchema("index_repo", "Index a repository into the graph store", map[string]any{
			"type":       "object",
			"required":   []string{"repo_path"},
			"properties": map[string]any{"repo_path": map[string]any{"type": "string"}},
		}),
		toolSchema("analyze_path", "Analyze a repository or file path", map[string]any{
			"type":       "object",
			"required":   []string{"path"},
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
		}),
		toolSchema("diagnostics", "Collect LSP diagnostics for a file path", map[string]any{
			"type":       "object",
			"required":   []string{"path"},
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
		}),
		toolSchema("lsp_symbols", "List document symbols for a file path", map[string]any{
			"type":       "object",
			"required":   []string{"path"},
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
		}),
		toolSchema("lsp_definition", "Find LSP definitions near a position in a file", map[string]any{
			"type":     "object",
			"required": []string{"path", "line", "character"},
			"properties": map[string]any{
				"path":      map[string]any{"type": "string"},
				"line":      map[string]any{"type": "integer"},
				"character": map[string]any{"type": "integer"},
			},
		}),
		toolSchema("lsp_references", "Find LSP references near a position in a file", map[string]any{
			"type":     "object",
			"required": []string{"path", "line", "character"},
			"properties": map[string]any{
				"path":      map[string]any{"type": "string"},
				"line":      map[string]any{"type": "integer"},
				"character": map[string]any{"type": "integer"},
			},
		}),
		toolSchema("lsp_workspace_symbols", "Search workspace symbols for the active repository", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		}),
		toolSchema("mcp_servers", "List configured MCP servers and their status", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}),
		toolSchema("run_subtask", "Create a child session and run a focused subtask", map[string]any{
			"type":     "object",
			"required": []string{"prompt"},
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string"},
				"title":  map[string]any{"type": "string"},
			},
		}),
	}

	mcpTools, err := a.listMCPTools(context.Background(), repoPath)
	if err == nil {
		for _, item := range mcpTools {
			toolName := "mcp_" + sanitizeToolName(item.Server) + "__" + sanitizeToolName(item.Name)
			description := strings.TrimSpace(strings.TrimSpace(item.Description))
			if description == "" {
				description = fmt.Sprintf("MCP tool %s from server %s", item.Name, item.Server)
			}
			parameters := item.InputSchema
			if len(parameters) == 0 {
				parameters = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, toolSchema(toolName, description, parameters))
		}
	}
	return tools
}

func (a *App) runtimeConfig(repoPath string) config.RuntimeConfig {
	return a.Config.Runtime(repoPath)
}

func sanitizeToolName(input string) string {
	replacer := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "-", "_", ".", "_", " ", "_")
	return replacer.Replace(strings.ToLower(input))
}

func (a *App) diagnosticsForPath(ctx context.Context, path string, repoPath string) ([]lsp.Diagnostic, error) {
	runtime := a.runtimeConfig(repoPath)
	configs, err := lsp.LoadConfig(runtime.LSPConfigPath, repoPath)
	if err != nil {
		return nil, err
	}
	language := lsp.DetectLanguage(path)
	if language == "" {
		return nil, fmt.Errorf("no diagnostics language mapping for %s", filepath.Ext(path))
	}
	serverConfig, ok := configs[language]
	if !ok {
		return nil, fmt.Errorf("no lsp server configured for %s", language)
	}
	client := &lsp.Client{}
	return client.Diagnostics(ctx, path, serverConfig)
}

func (a *App) Diagnostics(ctx context.Context, path string, repoPath string) ([]lsp.Diagnostic, error) {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return a.workspaceDiagnostics(ctx, path, repoPath)
	}
	return a.diagnosticsForPath(ctx, path, repoPath)
}

func (a *App) workspaceDiagnostics(ctx context.Context, root string, repoPath string) ([]lsp.Diagnostic, error) {
	files, err := a.ListFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	items := make([]lsp.Diagnostic, 0)
	for _, file := range files {
		if lsp.DetectLanguage(file) == "" {
			continue
		}
		diagnostics, diagErr := a.diagnosticsForPath(ctx, file, repoPath)
		if diagErr != nil {
			continue
		}
		items = append(items, diagnostics...)
		if len(items) >= 200 {
			break
		}
	}
	return items, nil
}

func (a *App) DocumentSymbols(ctx context.Context, path string, repoPath string) ([]lsp.DocumentSymbol, error) {
	runtime := a.runtimeConfig(repoPath)
	configs, err := lsp.LoadConfig(runtime.LSPConfigPath, repoPath)
	if err != nil {
		return nil, err
	}
	language := lsp.DetectLanguage(path)
	if language == "" {
		return nil, fmt.Errorf("no symbol language mapping for %s", filepath.Ext(path))
	}
	serverConfig, ok := configs[language]
	if !ok {
		return nil, fmt.Errorf("no lsp server configured for %s", language)
	}
	client := &lsp.Client{}
	return client.DocumentSymbols(ctx, path, serverConfig)
}

func (a *App) Definitions(ctx context.Context, path string, repoPath string, line int, character int) ([]lsp.Location, error) {
	runtime := a.runtimeConfig(repoPath)
	configs, err := lsp.LoadConfig(runtime.LSPConfigPath, repoPath)
	if err != nil {
		return nil, err
	}
	language := lsp.DetectLanguage(path)
	if language == "" {
		return nil, fmt.Errorf("no definition language mapping for %s", filepath.Ext(path))
	}
	serverConfig, ok := configs[language]
	if !ok {
		return nil, fmt.Errorf("no lsp server configured for %s", language)
	}
	client := &lsp.Client{}
	return client.Definition(ctx, path, serverConfig, line, character)
}

func (a *App) References(ctx context.Context, path string, repoPath string, line int, character int) ([]lsp.Location, error) {
	runtime := a.runtimeConfig(repoPath)
	configs, err := lsp.LoadConfig(runtime.LSPConfigPath, repoPath)
	if err != nil {
		return nil, err
	}
	language := lsp.DetectLanguage(path)
	if language == "" {
		return nil, fmt.Errorf("no references language mapping for %s", filepath.Ext(path))
	}
	serverConfig, ok := configs[language]
	if !ok {
		return nil, fmt.Errorf("no lsp server configured for %s", language)
	}
	client := &lsp.Client{}
	return client.References(ctx, path, serverConfig, line, character)
}

func (a *App) WorkspaceSymbols(ctx context.Context, repoPath string, query string) ([]lsp.WorkspaceSymbol, error) {
	if strings.TrimSpace(repoPath) == "" {
		return []lsp.WorkspaceSymbol{}, nil
	}
	files, err := a.ListFiles(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	loweredQuery := strings.ToLower(strings.TrimSpace(query))
	items := make([]lsp.WorkspaceSymbol, 0)
	for _, file := range files {
		if lsp.DetectLanguage(file) == "" {
			continue
		}
		symbols, symbolErr := a.DocumentSymbols(ctx, file, repoPath)
		if symbolErr != nil {
			continue
		}
		for _, symbol := range symbols {
			if loweredQuery != "" && !strings.Contains(strings.ToLower(symbol.Name), loweredQuery) && !strings.Contains(strings.ToLower(symbol.Kind), loweredQuery) {
				continue
			}
			items = append(items, lsp.WorkspaceSymbol{
				Name:      symbol.Name,
				Kind:      symbol.Kind,
				Path:      symbol.Path,
				Line:      symbol.Line,
				Character: symbol.Character,
			})
			if len(items) >= 120 {
				return items, nil
			}
		}
	}
	return items, nil
}

func (a *App) listMCPTools(ctx context.Context, repoPath string) ([]mcp.ToolInfo, error) {
	runtime := a.runtimeConfig(repoPath)
	configs, err := mcp.LoadConfig(runtime.MCPConfigPath, repoPath)
	if err != nil {
		return nil, err
	}
	client := &mcp.Client{}
	allTools := make([]mcp.ToolInfo, 0)
	for serverName, cfg := range configs {
		items, err := client.ListTools(ctx, serverName, cfg)
		if err != nil {
			continue
		}
		allTools = append(allTools, items...)
	}
	return allTools, nil
}

func (a *App) MCPTools(ctx context.Context, repoPath string) ([]mcp.ToolInfo, error) {
	return a.listMCPTools(ctx, repoPath)
}

func (a *App) MCPServerStatuses(ctx context.Context, repoPath string) ([]mcp.ServerStatus, error) {
	runtime := a.runtimeConfig(repoPath)
	configs, err := mcp.LoadConfig(runtime.MCPConfigPath, repoPath)
	if err != nil {
		return nil, err
	}
	client := &mcp.Client{}
	statuses := make([]mcp.ServerStatus, 0, len(configs))
	for serverName, cfg := range configs {
		statuses = append(statuses, client.Probe(ctx, serverName, cfg))
	}
	return statuses, nil
}

func (a *App) RepoGraphSummary(ctx context.Context, repoPath string, sessionID string) (store.RepoGraphSummary, error) {
	if a.Neo4j == nil {
		return store.RepoGraphSummary{
			RepoPath:           repoPath,
			IndexedFiles:       0,
			IndexedDirectories: 0,
			ImportEdges:        0,
			ReferenceEdges:     0,
			SymbolCount:        0,
			TouchedFiles:       []string{},
			RelatedFiles:       []string{},
			RelatedFileMatches: []store.FileMatch{},
			RelatedMemoryMatch: []store.MemoryMatch{},
			RelatedSymbols:     []store.SymbolMatch{},
			ChildSessions:      []string{},
			LineageSessions:    []string{},
			Memories:           []store.MemorySummary{},
		}, ErrNeo4jUnavailable
	}
	return a.Neo4j.RepoGraphSummary(ctx, repoPath, sessionID)
}

func (a *App) RelevantRepoFiles(ctx context.Context, sessionID string, repoPath string, prompt string) ([]store.FileMatch, error) {
	if a.Neo4j == nil || strings.TrimSpace(repoPath) == "" || strings.TrimSpace(prompt) == "" {
		return []store.FileMatch{}, nil
	}
	tokens := promptTokens(prompt)
	graphFiles, err := a.Neo4j.RelevantFiles(ctx, repoPath, tokens, 8)
	if err != nil {
		return nil, err
	}
	lineageFiles, err := a.Neo4j.RelevantTouchedFiles(ctx, sessionID, repoPath, tokens, 6)
	if err != nil {
		return graphFiles, nil
	}
	return mergeRankedFileMatches(lineageFiles, graphFiles, 10), nil
}

func (a *App) RelevantRepoSymbols(ctx context.Context, repoPath string, prompt string) ([]store.SymbolMatch, error) {
	if a.Neo4j == nil || strings.TrimSpace(repoPath) == "" || strings.TrimSpace(prompt) == "" {
		return []store.SymbolMatch{}, nil
	}
	return a.Neo4j.RelevantSymbols(ctx, repoPath, promptTokens(prompt), 10)
}

func (a *App) RelevantSessionMemories(ctx context.Context, sessionID string, prompt string) ([]store.MemoryMatch, error) {
	if a.Neo4j == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(prompt) == "" {
		return []store.MemoryMatch{}, nil
	}
	return a.Neo4j.RelevantMemorySummaries(ctx, sessionID, promptTokens(prompt), 8)
}

func (a *App) RepoRetrievalPreview(ctx context.Context, sessionID string, repoPath string, prompt string) (store.RepoGraphSummary, []string, error) {
	summary, err := a.RepoGraphSummary(ctx, repoPath, sessionID)
	if err != nil {
		return summary, nil, err
	}
	fileMatches, _ := a.RelevantRepoFiles(ctx, sessionID, repoPath, prompt)
	symbolMatches, _ := a.RelevantRepoSymbols(ctx, repoPath, prompt)
	memoryMatches, _ := a.RelevantSessionMemories(ctx, sessionID, prompt)
	summary.RelatedFileMatches = append([]store.FileMatch{}, fileMatches...)
	summary.RelatedMemoryMatch = append([]store.MemoryMatch{}, memoryMatches...)
	summary.RelatedSymbols = append([]store.SymbolMatch{}, symbolMatches...)
	summary.RelatedFiles = fileMatchPaths(fileMatches)
	if len(memoryMatches) > 0 {
		summary.Memories = make([]store.MemorySummary, 0, len(memoryMatches))
		for _, item := range memoryMatches {
			summary.Memories = append(summary.Memories, store.MemorySummary{ID: item.ID, Kind: item.Kind, Label: item.Label})
		}
	}
	snippets := relevantSnippets(prompt, symbolMatches, fileMatches)
	return summary, snippets, nil
}

func (a *App) RetrievalPreview(ctx context.Context, sessionID string, repoPath string, prompt string) (RepoRetrievalPreview, error) {
	summary, snippets, err := a.RepoRetrievalPreview(ctx, sessionID, repoPath, prompt)
	if err != nil {
		return RepoRetrievalPreview{
			RepoPath:      repoPath,
			SessionID:     sessionID,
			Prompt:        prompt,
			FileMatches:   []store.FileMatch{},
			MemoryMatches: []store.MemoryMatch{},
			SymbolMatches: []store.SymbolMatch{},
			Snippets:      []string{},
		}, err
	}
	return RepoRetrievalPreview{
		RepoPath:      repoPath,
		SessionID:     sessionID,
		Prompt:        prompt,
		FileMatches:   summary.RelatedFileMatches,
		MemoryMatches: summary.RelatedMemoryMatch,
		SymbolMatches: summary.RelatedSymbols,
		Snippets:      snippets,
	}, nil
}

func (a *App) callMCPTool(ctx context.Context, repoPath string, serverName string, toolName string, arguments map[string]any) (string, error) {
	runtime := a.runtimeConfig(repoPath)
	configs, err := mcp.LoadConfig(runtime.MCPConfigPath, repoPath)
	if err != nil {
		return "", err
	}
	cfg, ok := configs[serverName]
	if !ok {
		return "", fmt.Errorf("unknown mcp server: %s", serverName)
	}
	client := &mcp.Client{}
	return client.CallTool(ctx, cfg, toolName, arguments)
}

func (a *App) maybeCompactHistory(ctx context.Context, sessionID, model, repoPath string, history []domain.Message) ([]domain.Message, *domain.ContinuationState, error) {
	runtime := a.runtimeConfig(repoPath)
	if !runtime.AutoCompact {
		return history, nil, nil
	}
	limit := runtime.CompactLimit
	keep := runtime.CompactKeep
	if limit <= 0 || keep <= 0 || len(history) <= limit {
		return history, nil, nil
	}

	summaryIndex := latestSummaryIndex(history)
	working := history
	if summaryIndex >= 0 && summaryIndex+1 < len(history) {
		working = history[summaryIndex+1:]
	}
	if len(working) <= limit {
		if summaryIndex >= 0 {
			return append([]domain.Message{history[summaryIndex]}, working...), nil, nil
		}
		return history, nil, nil
	}
	if len(working) <= keep {
		return history, nil, nil
	}

	older := working[:len(working)-keep]
	recent := working[len(working)-keep:]
	baseSummary := ""
	if summaryIndex >= 0 {
		baseSummary = strings.TrimSpace(strings.TrimPrefix(extractText(history[summaryIndex].Parts), autoCompactPrefix))
	}

	var content strings.Builder
	content.WriteString("Summarize this coding conversation for continuation. Preserve decisions, file paths, repo context, tool results, and pending tasks.\n")
	if baseSummary != "" {
		content.WriteString("\nExisting summary:\n")
		content.WriteString(baseSummary)
		content.WriteString("\n")
	}
	content.WriteString("\nConversation to summarize:\n")
	for _, message := range older {
		text := extractText(message.Parts)
		if text == "" {
			text = extractToolContent(message.Parts)
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		content.WriteString(string(message.Role))
		content.WriteString(": ")
		content.WriteString(text)
		content.WriteString("\n")
	}

	result, err := a.Ollama.Chat(ctx, model, []ollama.Message{{
		Role:    "system",
		Content: "You are generating compact continuation summaries for an AI coding assistant.",
	}, {
		Role:    "user",
		Content: content.String(),
	}}, nil)
	if err != nil {
		return history, nil, nil
	}

	summaryText := strings.TrimSpace(result.Content)
	if summaryText == "" {
		return history, nil, nil
	}
	parentSummaryID := ""
	if summaryIndex >= 0 {
		parentSummaryID = history[summaryIndex].ID
	}
	fromMessageID := ""
	toMessageID := ""
	if len(older) > 0 {
		fromMessageID = older[0].ID
		toMessageID = older[len(older)-1].ID
	}
	summaryMessage := domain.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      domain.RoleAssistant,
		Model:     model,
		Parts: []domain.ContentPart{
			{Type: "text", Text: autoCompactPrefix + "\n" + summaryText},
			{Type: "finish", Reason: "stop", Time: time.Now().Unix()},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := a.SaveMessage(ctx, summaryMessage); err != nil {
		return history, nil, nil
	}
	_ = a.updateSession(ctx, sessionID, func(session *domain.Session) {
		session.SummaryMessageID = summaryMessage.ID
		session.SummaryParentMessageID = parentSummaryID
		session.SummaryFromMessageID = fromMessageID
		session.SummaryToMessageID = toMessageID
		session.LastCompactedAt = summaryMessage.CreatedAt
	})
	continuation := &domain.ContinuationState{
		SessionID:              sessionID,
		SummaryMessageID:       summaryMessage.ID,
		SummaryParentMessageID: parentSummaryID,
		SummaryFromMessageID:   fromMessageID,
		SummaryToMessageID:     toMessageID,
		CompactedMessages:      len(older),
		RecentMessages:         len(recent),
		CreatedAt:              summaryMessage.CreatedAt,
	}
	a.Hub.Broadcast(events.Event{
		Type: "log",
		Data: map[string]string{
			"level":   "info",
			"message": "auto-compact completed for session " + sessionID,
		},
	})
	return append([]domain.Message{summaryMessage}, recent...), continuation, nil
}

func latestSummaryIndex(history []domain.Message) int {
	for index := len(history) - 1; index >= 0; index-- {
		text := extractText(history[index].Parts)
		if strings.HasPrefix(strings.TrimSpace(text), autoCompactPrefix) {
			return index
		}
	}
	return -1
}

func diagnosticsText(items []lsp.Diagnostic) string {
	if len(items) == 0 {
		return "No diagnostics."
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("%s:%d:%d [%s] %s", item.Path, item.Line, item.Character, item.Severity, item.Message))
	}
	return strings.Join(lines, "\n")
}

func marshalIndented(value any) string {
	bytes, _ := json.MarshalIndent(value, "", "  ")
	return string(bytes)
}

func fileMetadata(path string) store.FileMetadata {
	info, err := os.Stat(path)
	sizeBytes := int64(0)
	if err == nil {
		sizeBytes = info.Size()
	}
	return store.FileMetadata{
		Name:      filepath.Base(path),
		Ext:       strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		Directory: filepath.Dir(path),
		SizeBytes: sizeBytes,
	}
}

func extractImports(path string, content string) []string {
	ext := strings.ToLower(filepath.Ext(path))
	lines := strings.Split(content, "\n")
	imports := make([]string, 0)
	seen := map[string]struct{}{}

	add := func(value string) {
		value = strings.TrimSpace(strings.Trim(value, `"'`))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		imports = append(imports, value)
	}

	switch ext {
	case ".go":
		inBlock := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "import (") {
				inBlock = true
				continue
			}
			if inBlock {
				if trimmed == ")" {
					inBlock = false
					continue
				}
				fields := strings.Fields(trimmed)
				if len(fields) > 0 {
					add(fields[len(fields)-1])
				}
				continue
			}
			if strings.HasPrefix(trimmed, "import ") {
				fields := strings.Fields(trimmed)
				if len(fields) > 1 {
					add(fields[len(fields)-1])
				}
			}
		}
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		re := regexp.MustCompile(`(?m)(?:import\s+.*?\s+from\s+|require\()\s*["']([^"']+)["']`)
		for _, match := range re.FindAllStringSubmatch(content, -1) {
			if len(match) > 1 {
				add(match[1])
			}
		}
	case ".py":
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "import ") {
				rest := strings.TrimPrefix(trimmed, "import ")
				for _, part := range strings.Split(rest, ",") {
					add(strings.Fields(strings.TrimSpace(part))[0])
				}
				continue
			}
			if strings.HasPrefix(trimmed, "from ") {
				fields := strings.Fields(trimmed)
				if len(fields) >= 2 {
					add(fields[1])
				}
			}
		}
	}

	return imports
}

func promptTokens(prompt string) []string {
	re := regexp.MustCompile(`[A-Za-z0-9._/-]+`)
	matches := re.FindAllString(strings.ToLower(prompt), -1)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, item := range matches {
		if len(item) < 2 {
			continue
		}
		item = strings.TrimPrefix(item, ".")
		item = strings.TrimPrefix(item, "/")
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

func buildRepoContextPrompt(summary store.RepoGraphSummary, relevantFiles []store.FileMatch, relevantSymbols []store.SymbolMatch, relevantMemories []store.MemoryMatch) string {
	if summary.RepoPath == "" {
		return ""
	}
	var out strings.Builder
	out.WriteString("Repository graph summary:\n")
	out.WriteString(fmt.Sprintf("- indexed files: %d\n", summary.IndexedFiles))
	out.WriteString(fmt.Sprintf("- indexed directories: %d\n", summary.IndexedDirectories))
	out.WriteString(fmt.Sprintf("- import edges: %d\n", summary.ImportEdges))
	out.WriteString(fmt.Sprintf("- reference edges: %d\n", summary.ReferenceEdges))
	out.WriteString(fmt.Sprintf("- symbols: %d\n", summary.SymbolCount))
	if len(summary.TouchedFiles) > 0 {
		out.WriteString("- recently tracked files:\n")
		for _, file := range summary.TouchedFiles[:minInt(len(summary.TouchedFiles), 6)] {
			out.WriteString("  - ")
			out.WriteString(file)
			out.WriteString("\n")
		}
	}
	if len(relevantFiles) > 0 {
		out.WriteString("- prompt-relevant files:\n")
		for _, file := range relevantFiles[:minInt(len(relevantFiles), 8)] {
			out.WriteString("  - ")
			out.WriteString(file.Path)
			out.WriteString(fmt.Sprintf(" (score %d)", file.Score))
			if len(file.Reasons) > 0 {
				out.WriteString(" — ")
				out.WriteString(strings.Join(file.Reasons, ", "))
			}
			out.WriteString("\n")
		}
	}
	if len(relevantSymbols) > 0 {
		out.WriteString("- prompt-relevant symbols:\n")
		for _, item := range relevantSymbols[:minInt(len(relevantSymbols), 6)] {
			out.WriteString("  - ")
			out.WriteString(fmt.Sprintf("%s (%s) in %s:%d", item.Name, item.Kind, item.FilePath, item.Line))
			if item.Score > 0 {
				out.WriteString(fmt.Sprintf(" [score %d]", item.Score))
			}
			out.WriteString("\n")
		}
	}
	if len(relevantMemories) > 0 {
		out.WriteString("- ranked memories:\n")
		for _, item := range relevantMemories[:minInt(len(relevantMemories), 5)] {
			out.WriteString("  - ")
			out.WriteString(fmt.Sprintf("%s: %s (score %d)", item.Kind, item.Label, item.Score))
			if len(item.Reasons) > 0 {
				out.WriteString(" — ")
				out.WriteString(strings.Join(item.Reasons, ", "))
			}
			out.WriteString("\n")
		}
	} else if len(summary.Memories) > 0 {
		out.WriteString("- memory nodes:\n")
		for _, item := range summary.Memories[:minInt(len(summary.Memories), 5)] {
			out.WriteString("  - ")
			out.WriteString(item.Kind)
			out.WriteString(": ")
			out.WriteString(item.Label)
			out.WriteString("\n")
		}
	}
	if len(summary.ChildSessions) > 0 {
		out.WriteString("- child sessions:\n")
		for _, item := range summary.ChildSessions[:minInt(len(summary.ChildSessions), 5)] {
			out.WriteString("  - ")
			out.WriteString(item)
			out.WriteString("\n")
		}
	}
	if len(summary.LineageSessions) > 0 {
		out.WriteString("- lineage sessions:\n")
		for _, item := range summary.LineageSessions[:minInt(len(summary.LineageSessions), 6)] {
			out.WriteString("  - ")
			out.WriteString(item)
			out.WriteString("\n")
		}
	}
	return strings.TrimSpace(out.String())
}

func buildMemoryContextPrompt(memories []store.MemoryMatch) string {
	if len(memories) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\nRelevant session memories:\n")
	for _, item := range memories[:minInt(len(memories), 6)] {
		out.WriteString("- ")
		out.WriteString(item.Kind)
		out.WriteString(": ")
		out.WriteString(item.Label)
		if item.Score > 0 {
			out.WriteString(fmt.Sprintf(" (score %d)", item.Score))
		}
		if len(item.Reasons) > 0 {
			out.WriteString(" — ")
			out.WriteString(strings.Join(item.Reasons, ", "))
		}
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String())
}

func mergeUniqueStrings(groups ...any) []string {
	limit := 0
	if len(groups) > 0 {
		if typed, ok := groups[len(groups)-1].(int); ok {
			limit = typed
			groups = groups[:len(groups)-1]
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, group := range groups {
		items, ok := group.([]string)
		if !ok {
			continue
		}
		for _, item := range items {
			if strings.TrimSpace(item) == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return out[:limit]
			}
		}
	}
	return out
}

func mergeRankedFileMatches(groups ...any) []store.FileMatch {
	limit := 0
	if len(groups) > 0 {
		if typed, ok := groups[len(groups)-1].(int); ok {
			limit = typed
			groups = groups[:len(groups)-1]
		}
	}
	merged := make(map[string]store.FileMatch)
	for _, group := range groups {
		items, ok := group.([]store.FileMatch)
		if !ok {
			continue
		}
		for _, item := range items {
			if strings.TrimSpace(item.Path) == "" {
				continue
			}
			existing, ok := merged[item.Path]
			if !ok {
				merged[item.Path] = item
				continue
			}
			existing.Score += item.Score
			existing.Reasons = mergeUniqueStrings(existing.Reasons, item.Reasons)
			if existing.Source == "" {
				existing.Source = item.Source
			} else if item.Source != "" && !strings.Contains(existing.Source, item.Source) {
				existing.Source += "+" + item.Source
			}
			merged[item.Path] = existing
		}
	}
	result := make([]store.FileMatch, 0, len(merged))
	for _, item := range merged {
		item.Reasons = item.Reasons[:minInt(len(item.Reasons), 4)]
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].Path < result[j].Path
		}
		return result[i].Score > result[j].Score
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

func fileMatchPaths(items []store.FileMatch) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Path) == "" {
			continue
		}
		paths = append(paths, item.Path)
	}
	return paths
}

func formatSnippetContext(snippets []string) string {
	if len(snippets) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\nRelevant code snippets:\n")
	for _, snippet := range snippets[:minInt(len(snippets), 6)] {
		out.WriteString("---\n")
		out.WriteString(snippet)
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String())
}

func extractSymbols(path string, content string) []store.SymbolMetadata {
	ext := strings.ToLower(filepath.Ext(path))
	lines := strings.Split(content, "\n")
	symbols := make([]store.SymbolMetadata, 0)
	seen := map[string]struct{}{}

	add := func(name, kind string, line int) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := fmt.Sprintf("%s:%s:%d", kind, name, line)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		symbols = append(symbols, store.SymbolMetadata{Name: name, Kind: kind, Line: line})
	}

	switch ext {
	case ".go":
		reFunc := regexp.MustCompile(`^\s*func\s+(?:\([^)]+\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
		reType := regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s+`)
		reVar := regexp.MustCompile(`^\s*(?:var|const)\s+([A-Za-z_][A-Za-z0-9_]*)`)
		for index, line := range lines {
			if match := reFunc.FindStringSubmatch(line); len(match) > 1 {
				add(match[1], "function", index+1)
			}
			if match := reType.FindStringSubmatch(line); len(match) > 1 {
				add(match[1], "type", index+1)
			}
			if match := reVar.FindStringSubmatch(line); len(match) > 1 {
				add(match[1], "variable", index+1)
			}
		}
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		reFunc := regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
		reClass := regexp.MustCompile(`^\s*(?:export\s+)?class\s+([A-Za-z_][A-Za-z0-9_]*)`)
		reConst := regexp.MustCompile(`^\s*(?:export\s+)?const\s+([A-Za-z_][A-Za-z0-9_]*)\s*=`)
		for index, line := range lines {
			if match := reFunc.FindStringSubmatch(line); len(match) > 1 {
				add(match[1], "function", index+1)
			}
			if match := reClass.FindStringSubmatch(line); len(match) > 1 {
				add(match[1], "class", index+1)
			}
			if match := reConst.FindStringSubmatch(line); len(match) > 1 {
				add(match[1], "constant", index+1)
			}
		}
	case ".py":
		reFunc := regexp.MustCompile(`^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
		reClass := regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)`)
		for index, line := range lines {
			if match := reFunc.FindStringSubmatch(line); len(match) > 1 {
				add(match[1], "function", index+1)
			}
			if match := reClass.FindStringSubmatch(line); len(match) > 1 {
				add(match[1], "class", index+1)
			}
		}
	}

	return symbols
}

func relevantSnippets(prompt string, symbols []store.SymbolMatch, files []store.FileMatch) []string {
	tokens := promptTokens(prompt)
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, symbol := range symbols {
		snippet, err := fileSnippetAround(symbol.FilePath, symbol.Line, tokens, symbol.Name)
		if err != nil || strings.TrimSpace(snippet) == "" {
			continue
		}
		key := symbol.FilePath + ":" + symbol.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, fmt.Sprintf("Symbol %s (%s) in %s:\n%s", symbol.Name, symbol.Kind, symbol.FilePath, snippet))
		if len(out) >= 6 {
			return out
		}
	}
	for _, file := range files {
		if _, ok := seen[file.Path]; ok {
			continue
		}
		snippet, err := fileSnippetAround(file.Path, 0, tokens, "")
		if err != nil || strings.TrimSpace(snippet) == "" {
			continue
		}
		seen[file.Path] = struct{}{}
		out = append(out, fmt.Sprintf("Relevant file %s (score %d):\n%s", file.Path, file.Score, snippet))
		if len(out) >= 8 {
			return out
		}
	}
	return out
}

func fileSnippetAround(path string, preferredLine int, tokens []string, symbolName string) (string, error) {
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(contentBytes), "\n")
	if len(lines) == 0 {
		return "", nil
	}
	targetLine := preferredLine
	if targetLine <= 0 {
		targetLine = bestMatchingLine(lines, tokens, symbolName)
	}
	if targetLine <= 0 {
		targetLine = 1
	}
	start := maxInt(targetLine-4, 1)
	end := minInt(targetLine+6, len(lines))
	snippetLines := make([]string, 0, end-start+1)
	for index := start; index <= end; index++ {
		snippetLines = append(snippetLines, fmt.Sprintf("%d: %s", index, lines[index-1]))
	}
	return strings.Join(snippetLines, "\n"), nil
}

func bestMatchingLine(lines []string, tokens []string, symbolName string) int {
	bestLine := 1
	bestScore := -1
	loweredSymbol := strings.ToLower(symbolName)
	for index, line := range lines {
		lowered := strings.ToLower(line)
		score := 0
		if loweredSymbol != "" && strings.Contains(lowered, loweredSymbol) {
			score += 8
		}
		for _, token := range tokens {
			if strings.Contains(lowered, token) {
				score += 2
			}
		}
		if score > bestScore {
			bestScore = score
			bestLine = index + 1
		}
	}
	return bestLine
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
