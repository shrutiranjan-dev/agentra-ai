package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/config"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/domain"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/lsp"
)

func (a *App) ListCommands(_ context.Context, repoPath string) ([]domain.CommandDefinition, error) {
	commands := []domain.CommandDefinition{
		{ID: "help", Title: "Help", Description: "List available commands", Kind: "builtin", Usage: "/help"},
		{ID: "status", Title: "Status", Description: "Show backend dependency status", Kind: "builtin", Usage: "/status"},
		{ID: "models", Title: "Models", Description: "List installed Ollama models", Kind: "builtin", Usage: "/models"},
		{ID: "new", Title: "New Session", Description: "Create a new session note in the current conversation", Kind: "builtin", Usage: "/new [title]", Arguments: []domain.CommandArgumentDefinition{{Name: "title", Description: "Optional session title", Required: false}}},
		{ID: "repo", Title: "Repo", Description: "Show or set the active repository path", Kind: "builtin", Usage: "/repo [path]", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "Optional repo path", Required: false}}},
		{ID: "index", Title: "Index Repo", Description: "Index a repository into Neo4j", Kind: "builtin", Usage: "/index [path]", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "Repo path to index", Required: false}}},
		{ID: "read", Title: "Read File", Description: "Read a local file", Kind: "builtin", Usage: "/read <path>", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "File path to read", Required: true}}},
		{ID: "edit", Title: "Edit File", Description: "Apply a single search/replace edit", Kind: "builtin", Usage: "/edit <path> ::: <search> ::: <replace>", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "File path to edit", Required: true}, {Name: "search", Description: "Exact text to replace", Required: true}, {Name: "replace", Description: "Replacement text", Required: true}}},
		{ID: "patch", Title: "Patch File", Description: "Apply SEARCH/REPLACE patch blocks to one or more files", Kind: "builtin", Usage: "/patch <path> then paste SEARCH/REPLACE blocks", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "Optional path when patch body omits file markers", Required: false}}},
		{ID: "ls", Title: "List Files", Description: "List files under a directory", Kind: "builtin", Usage: "/ls [path]", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "Directory path to list", Required: false}}},
		{ID: "shell", Title: "Shell", Description: "Run a shell command", Kind: "builtin", Usage: "/shell <command>", Arguments: []domain.CommandArgumentDefinition{{Name: "command", Description: "Shell command to run", Required: true}}},
		{ID: "analyze", Title: "Analyze Path", Description: "Analyze a file or repository path", Kind: "builtin", Usage: "/analyze <path>", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "Path to inspect", Required: true}}},
		{ID: "diagnostics", Title: "Diagnostics", Description: "Run LSP diagnostics for a file", Kind: "builtin", Usage: "/diagnostics <path>", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "File path for diagnostics", Required: true}}},
		{ID: "symbols", Title: "Document Symbols", Description: "List LSP document symbols for a file", Kind: "builtin", Usage: "/symbols <path>", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "File path for symbol lookup", Required: true}}},
		{ID: "workspace-symbols", Title: "Workspace Symbols", Description: "Search symbols across the active repository", Kind: "builtin", Usage: "/workspace-symbols [query]", Arguments: []domain.CommandArgumentDefinition{{Name: "query", Description: "Optional symbol query", Required: false}}},
		{ID: "definition", Title: "Definition", Description: "Find LSP definition at a file position", Kind: "builtin", Usage: "/definition <path> <line> <character>", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "File path", Required: true}, {Name: "line", Description: "1-based line", Required: true}, {Name: "character", Description: "1-based character", Required: true}}},
		{ID: "references", Title: "References", Description: "Find LSP references at a file position", Kind: "builtin", Usage: "/references <path> <line> <character>", Arguments: []domain.CommandArgumentDefinition{{Name: "path", Description: "File path", Required: true}, {Name: "line", Description: "1-based line", Required: true}, {Name: "character", Description: "1-based character", Required: true}}},
		{ID: "mcp", Title: "MCP Tools", Description: "List configured MCP tools", Kind: "builtin", Usage: "/mcp"},
		{ID: "mcp-status", Title: "MCP Status", Description: "Show configured MCP servers and reachability", Kind: "builtin", Usage: "/mcp-status"},
		{ID: "compact", Title: "Compact Session", Description: "Summarize older conversation state", Kind: "builtin", Usage: "/compact"},
		{ID: "task", Title: "Subtask", Description: "Run a focused subtask in a child session", Kind: "builtin", Usage: "/task [title] ::: <prompt>", Arguments: []domain.CommandArgumentDefinition{{Name: "title", Description: "Optional subtask title", Required: false}, {Name: "prompt", Description: "Subtask prompt", Required: true}}},
	}

	custom, err := loadCustomCommands(repoPath, a.runtimeConfig(repoPath))
	if err != nil {
		return nil, err
	}
	commands = append(commands, custom...)
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].ID < commands[j].ID
	})
	return commands, nil
}

func (a *App) executeCommand(ctx context.Context, sessionID, model, repoPath, input string) (domain.Message, error) {
	commandName, args := parseCommandInput(input)
	if commandName == "" {
		return domain.Message{}, fmt.Errorf("invalid command input")
	}

	customCommands, err := loadCustomCommands(repoPath, a.runtimeConfig(repoPath))
	if err != nil {
		return domain.Message{}, err
	}
	parsedArgs := parseCommandArguments(args)
	for _, command := range customCommands {
		if command.ID == commandName {
			rendered, missing := renderCommandTemplate(command.Template, command.Arguments, args)
			if len(missing) > 0 {
				return a.saveAssistantTextMessage(ctx, sessionID, model, "Missing required arguments: "+strings.Join(missing, ", ")+"\nUsage: "+commandUsage(command), "stop")
			}
			if strings.TrimSpace(rendered) == "" {
				rendered = strings.Join(parsedArgs.Positional, " ")
			}
			return a.runLLMFlow(ctx, sessionID, model, repoPath, rendered, fmt.Sprintf("custom command %s", command.ID))
		}
	}

	switch commandName {
	case "help":
		commands, err := a.ListCommands(ctx, repoPath)
		if err != nil {
			return domain.Message{}, err
		}
		lines := make([]string, 0, len(commands)+1)
		lines = append(lines, "Available commands:")
		for _, command := range commands {
			usage := command.Usage
			if usage == "" {
				usage = "/" + command.ID
			}
			lines = append(lines, fmt.Sprintf("- %s — %s", usage, command.Description))
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, strings.Join(lines, "\n"), "stop")
	case "status":
		status := a.Status(ctx)
		text := fmt.Sprintf(
			"Service: %s\nMongoDB: %t\nNeo4j: %t\nOllama: %t\nMessages: %s",
			status.Mode,
			status.MongoAvailable,
			status.Neo4jAvailable,
			status.OllamaReachable,
			strings.Join(status.Messages, " | "),
		)
		return a.saveAssistantTextMessage(ctx, sessionID, model, text, "stop")
	case "models":
		models, err := a.ListModels(ctx)
		if err != nil {
			return domain.Message{}, err
		}
		if len(models) == 0 {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "No Ollama models found.", "stop")
		}
		lines := []string{"Installed Ollama models:"}
		for _, item := range models {
			lines = append(lines, "- "+item.Name)
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, strings.Join(lines, "\n"), "stop")
	case "new":
		title := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["title"], strings.Join(parsedArgs.Positional, " ")))
		if title == "" {
			title = "New Session"
		}
		session, err := a.CreateSession(ctx, title)
		if err != nil {
			return domain.Message{}, err
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, "Created session: "+session.Title, "stop")
	case "repo":
		target := repoPath
		if candidate := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["path"], strings.Join(parsedArgs.Positional, " "))); candidate != "" {
			target = candidate
		}
		if target == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "No repository path selected.", "stop")
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, "Active repository: "+target, "stop")
	case "index":
		target := repoPath
		if candidate := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["path"], strings.Join(parsedArgs.Positional, " "))); candidate != "" {
			target = candidate
		}
		if target == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /index <repo-path>", "stop")
		}
		if err := a.IndexRepository(ctx, target); err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Indexing failed: "+err.Error(), "stop")
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, "Indexed repository: "+target, "stop")
	case "read":
		target := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["path"], strings.Join(parsedArgs.Positional, " ")))
		if target == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /read <path>", "stop")
		}
		content, err := a.ReadFile(ctx, sessionID, target)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Read failed: "+err.Error(), "stop")
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, content, "stop")
	case "edit":
		if strings.TrimSpace(parsedArgs.Named["path"]) != "" && strings.TrimSpace(parsedArgs.Named["search"]) != "" {
			path := strings.TrimSpace(parsedArgs.Named["path"])
			search := parsedArgs.Named["search"]
			replace := parsedArgs.Named["replace"]
			output, err := a.EditFile(ctx, sessionID, path, search, replace, false)
			if err != nil {
				return a.saveAssistantTextMessage(ctx, sessionID, model, "Edit failed: "+err.Error(), "stop")
			}
			return a.saveAssistantTextMessage(ctx, sessionID, model, output, "stop")
		}
		payload := strings.TrimSpace(strings.TrimPrefix(input, "/edit"))
		segments := strings.Split(payload, ":::")
		if len(segments) != 3 {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /edit <path> ::: <search> ::: <replace>", "stop")
		}
		path := strings.TrimSpace(segments[0])
		search := strings.TrimSpace(segments[1])
		replace := strings.TrimSpace(segments[2])
		output, err := a.EditFile(ctx, sessionID, path, search, replace, false)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Edit failed: "+err.Error(), "stop")
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, output, "stop")
	case "patch":
		if strings.TrimSpace(parsedArgs.Named["patch"]) != "" {
			output, err := a.ApplyPatch(ctx, sessionID, strings.TrimSpace(parsedArgs.Named["path"]), parsedArgs.Named["patch"])
			if err != nil {
				return a.saveAssistantTextMessage(ctx, sessionID, model, "Patch failed: "+err.Error(), "stop")
			}
			return a.saveAssistantTextMessage(ctx, sessionID, model, output, "stop")
		}
		payload := strings.TrimSpace(strings.TrimPrefix(input, "/patch"))
		if payload == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /patch <path> then paste SEARCH/REPLACE blocks on following lines.", "stop")
		}
		firstLineBreak := strings.Index(payload, "\n")
		if firstLineBreak == -1 {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /patch <path> then paste SEARCH/REPLACE blocks on following lines.", "stop")
		}
		path := strings.TrimSpace(payload[:firstLineBreak])
		patchBody := strings.TrimSpace(payload[firstLineBreak+1:])
		output, err := a.ApplyPatch(ctx, sessionID, path, patchBody)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Patch failed: "+err.Error(), "stop")
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, output, "stop")
	case "ls":
		target := repoPath
		if candidate := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["path"], strings.Join(parsedArgs.Positional, " "))); candidate != "" {
			target = candidate
		}
		if target == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /ls <path>", "stop")
		}
		files, err := a.ListFiles(ctx, target)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "List failed: "+err.Error(), "stop")
		}
		if len(files) == 0 {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "No files found.", "stop")
		}
		limit := minInt(len(files), 200)
		return a.saveAssistantTextMessage(ctx, sessionID, model, strings.Join(files[:limit], "\n"), "stop")
	case "shell":
		command := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["command"], strings.Join(parsedArgs.Positional, " ")))
		if command == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /shell <command>", "stop")
		}
		output, err := a.RunShell(ctx, sessionID, command, repoPath)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Shell failed: "+err.Error(), "stop")
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, output, "stop")
	case "analyze":
		target := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["path"], strings.Join(parsedArgs.Positional, " ")))
		if target == "" {
			target = repoPath
		}
		if target == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /analyze <path>", "stop")
		}
		return a.analyzePath(ctx, sessionID, model, target)
	case "diagnostics":
		target := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["path"], strings.Join(parsedArgs.Positional, " ")))
		if target == "" {
			target = repoPath
		}
		if target == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /diagnostics <file-path>", "stop")
		}
		items, err := a.Diagnostics(ctx, target, repoPath)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Diagnostics failed: "+err.Error(), "stop")
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, diagnosticsText(items), "stop")
	case "symbols":
		target := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["path"], strings.Join(parsedArgs.Positional, " ")))
		if target == "" {
			target = repoPath
		}
		if target == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /symbols <file-path>", "stop")
		}
		items, err := a.DocumentSymbols(ctx, target, repoPath)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Symbols failed: "+err.Error(), "stop")
		}
		lines := []string{"Document symbols:"}
		for _, item := range items {
			lines = append(lines, fmt.Sprintf("- %s (%s) at %s:%d:%d", item.Name, item.Kind, item.Path, item.Line, item.Character))
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, strings.Join(lines, "\n"), "stop")
	case "workspace-symbols":
		if strings.TrimSpace(repoPath) == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /workspace-symbols [query] with an active repository selected.", "stop")
		}
		query := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["query"], strings.Join(parsedArgs.Positional, " ")))
		items, err := a.WorkspaceSymbols(ctx, repoPath, query)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Workspace symbols failed: "+err.Error(), "stop")
		}
		if len(items) == 0 {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Workspace symbols: no results", "stop")
		}
		lines := []string{"Workspace symbols:"}
		for _, item := range items[:minInt(len(items), 60)] {
			lines = append(lines, fmt.Sprintf("- %s (%s) at %s:%d:%d", item.Name, item.Kind, item.Path, item.Line, item.Character))
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, strings.Join(lines, "\n"), "stop")
	case "definition":
		target := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["path"], firstPositional(parsedArgs, 0)))
		lineArg := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["line"], firstPositional(parsedArgs, 1)))
		charArg := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["character"], firstPositional(parsedArgs, 2)))
		if target == "" || lineArg == "" || charArg == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /definition <path> <line> <character>", "stop")
		}
		line, character := parsePositionArgs(lineArg, charArg)
		items, err := a.Definitions(ctx, target, repoPath, line, character)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Definition failed: "+err.Error(), "stop")
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, locationsText("Definitions", items), "stop")
	case "references":
		target := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["path"], firstPositional(parsedArgs, 0)))
		lineArg := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["line"], firstPositional(parsedArgs, 1)))
		charArg := strings.TrimSpace(firstNonEmpty(parsedArgs.Named["character"], firstPositional(parsedArgs, 2)))
		if target == "" || lineArg == "" || charArg == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /references <path> <line> <character>", "stop")
		}
		line, character := parsePositionArgs(lineArg, charArg)
		items, err := a.References(ctx, target, repoPath, line, character)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "References failed: "+err.Error(), "stop")
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, locationsText("References", items), "stop")
	case "mcp":
		items, err := a.listMCPTools(ctx, repoPath)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "MCP discovery failed: "+err.Error(), "stop")
		}
		if len(items) == 0 {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "No MCP tools configured.", "stop")
		}
		lines := []string{"Configured MCP tools:"}
		for _, item := range items {
			lines = append(lines, fmt.Sprintf("- %s/%s — %s", item.Server, item.Name, item.Description))
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, strings.Join(lines, "\n"), "stop")
	case "mcp-status":
		items, err := a.MCPServerStatuses(ctx, repoPath)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "MCP server status failed: "+err.Error(), "stop")
		}
		if len(items) == 0 {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "No MCP servers configured.", "stop")
		}
		lines := []string{"MCP servers:"}
		for _, item := range items {
			line := fmt.Sprintf("- %s [%s] reachable=%t tools=%d", item.Server, item.Transport, item.Reachable, item.ToolCount)
			if strings.TrimSpace(item.Error) != "" {
				line += " error=" + item.Error
			}
			lines = append(lines, line)
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, strings.Join(lines, "\n"), "stop")
	case "compact":
		history, err := a.ListMessages(ctx, sessionID)
		if err != nil {
			return domain.Message{}, err
		}
		compacted, _, _ := a.maybeCompactHistory(ctx, sessionID, model, repoPath, history)
		if latestSummaryIndex(compacted) == -1 {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Nothing to compact yet.", "stop")
		}
		return a.saveAssistantTextMessage(ctx, sessionID, model, "Conversation compacted for continuation.", "stop")
	case "task":
		payload := strings.TrimSpace(strings.TrimPrefix(input, "/task"))
		title := "Subtask"
		subtaskPrompt := payload
		if namedPrompt := strings.TrimSpace(parsedArgs.Named["prompt"]); namedPrompt != "" {
			subtaskPrompt = namedPrompt
			if namedTitle := strings.TrimSpace(parsedArgs.Named["title"]); namedTitle != "" {
				title = namedTitle
			}
		}
		if strings.Contains(payload, ":::") {
			segments := strings.SplitN(payload, ":::", 2)
			title = strings.TrimSpace(segments[0])
			subtaskPrompt = strings.TrimSpace(segments[1])
		}
		if strings.TrimSpace(subtaskPrompt) == "" {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Usage: /task [title] ::: <prompt>", "stop")
		}
		result, err := a.RunSubtask(ctx, sessionID, "", model, title, subtaskPrompt, repoPath)
		if err != nil {
			return a.saveAssistantTextMessage(ctx, sessionID, model, "Subtask failed: "+err.Error(), "stop")
		}
		text := fmt.Sprintf("Created subtask session %s (%s)\n%s", result.Session.Title, result.Session.ID, extractTextParts(result.Message.Parts))
		return a.saveAssistantTextMessage(ctx, sessionID, model, text, "stop")
	default:
		return a.saveAssistantTextMessage(ctx, sessionID, model, "Unknown command: /"+commandName, "stop")
	}
}

func parsePositionArgs(lineArg, charArg string) (int, int) {
	line := 1
	character := 1
	_, _ = fmt.Sscanf(strings.TrimSpace(lineArg), "%d", &line)
	_, _ = fmt.Sscanf(strings.TrimSpace(charArg), "%d", &character)
	if line < 1 {
		line = 1
	}
	if character < 1 {
		character = 1
	}
	return line, character
}

func locationsText(label string, items []lsp.Location) string {
	if len(items) == 0 {
		return label + ": no results"
	}
	lines := []string{label + ":"}
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("- %s:%d:%d", item.Path, item.Line, item.Character))
		if strings.TrimSpace(item.Preview) != "" {
			lines = append(lines, item.Preview)
		}
	}
	return strings.Join(lines, "\n")
}

func parseCommandInput(input string) (string, []string) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), "/"))
	if trimmed == "" {
		return "", nil
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

func looksLikePathPrompt(input string) string {
	trimmed := strings.TrimSpace(strings.Trim(input, `"'`))
	if trimmed == "" {
		return ""
	}
	if _, err := os.Stat(trimmed); err == nil {
		return trimmed
	}
	return ""
}

func analyzePromptForPath(input string) string {
	markers := []string{"analyze ", "inspect ", "open "}
	lower := strings.ToLower(input)
	for _, marker := range markers {
		index := strings.Index(lower, marker)
		if index == -1 {
			continue
		}
		candidate := strings.TrimSpace(strings.Trim(input[index+len(marker):], `"'`))
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func loadCustomCommands(repoPath string, runtimeConfig config.RuntimeConfig) ([]domain.CommandDefinition, error) {
	dirs := []struct {
		root string
		kind string
	}{
		{root: filepath.Join(os.Getenv("USERPROFILE"), ".opencode", "commands"), kind: "user"},
		{root: filepath.Join(os.Getenv("HOME"), ".opencode", "commands"), kind: "user"},
	}
	if repoPath != "" {
		dirs = append(dirs, struct {
			root string
			kind string
		}{root: filepath.Join(repoPath, ".opencode", "commands"), kind: "project"})
	}
	for _, dir := range runtimeConfig.CommandDirs {
		dirs = append(dirs, struct {
			root string
			kind string
		}{
			root: dir,
			kind: customCommandKind(repoPath, dir),
		})
	}

	commands := make([]domain.CommandDefinition, 0)
	seen := map[string]struct{}{}
	for _, dir := range dirs {
		info, err := os.Stat(dir.root)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(dir.root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".md" {
				return nil
			}
			relative, err := filepath.Rel(dir.root, path)
			if err != nil {
				return err
			}
			id := strings.TrimSuffix(filepath.ToSlash(relative), ".md")
			id = dir.kind + ":" + strings.ReplaceAll(id, "/", ":")
			if _, ok := seen[id]; ok {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			template, metadata := parseCommandTemplate(string(content))
			title := metadata.Title
			if strings.TrimSpace(title) == "" {
				title = strings.TrimSpace(strings.SplitN(template, "\n", 2)[0])
			}
			if title == "" {
				title = id
			}
			description := metadata.Description
			if strings.TrimSpace(description) == "" {
				description = title
			}
			arguments := metadata.Arguments
			if len(arguments) == 0 {
				arguments = inferTemplateArguments(template)
			}
			commands = append(commands, domain.CommandDefinition{
				ID:          id,
				Title:       title,
				Description: description,
				Kind:        dir.kind,
				Usage:       firstNonEmpty(metadata.Usage, commandUsage(domain.CommandDefinition{ID: id, Arguments: arguments})),
				Template:    template,
				SourcePath:  path,
				Arguments:   arguments,
			})
			seen[id] = struct{}{}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return commands, nil
}

type parsedCommandArguments struct {
	Positional []string
	Named      map[string]string
}

type commandTemplateMetadata struct {
	Title       string
	Description string
	Usage       string
	Arguments   []domain.CommandArgumentDefinition
}

func customCommandKind(repoPath, commandDir string) string {
	if repoPath != "" {
		repoCommands := filepath.Clean(filepath.Join(repoPath, ".opencode", "commands"))
		if filepath.Clean(commandDir) == repoCommands || strings.HasPrefix(filepath.Clean(commandDir), repoCommands+string(os.PathSeparator)) {
			return "project"
		}
	}
	return "user"
}

func parseCommandTemplate(content string) (string, commandTemplateMetadata) {
	trimmed := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(trimmed, "---\n") {
		return strings.TrimSpace(trimmed), commandTemplateMetadata{}
	}
	endIndex := strings.Index(trimmed[4:], "\n---\n")
	if endIndex == -1 {
		return strings.TrimSpace(trimmed), commandTemplateMetadata{}
	}
	endIndex += 4
	header := trimmed[4:endIndex]
	body := strings.TrimSpace(trimmed[endIndex+5:])
	metadata := commandTemplateMetadata{Arguments: []domain.CommandArgumentDefinition{}}
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		value = strings.TrimSpace(value)
		switch key {
		case "title":
			metadata.Title = value
		case "description":
			metadata.Description = value
		case "usage":
			metadata.Usage = value
		case "arg":
			parts := strings.SplitN(value, "|", 4)
			if len(parts) == 0 {
				continue
			}
			arg := domain.CommandArgumentDefinition{Name: strings.TrimSpace(parts[0])}
			if len(parts) > 1 {
				arg.Required = strings.EqualFold(strings.TrimSpace(parts[1]), "required")
			}
			if len(parts) > 2 {
				arg.Description = strings.TrimSpace(parts[2])
			}
			if len(parts) > 3 {
				arg.DefaultValue = strings.TrimSpace(parts[3])
			}
			if arg.Name != "" {
				metadata.Arguments = append(metadata.Arguments, arg)
			}
		}
	}
	return body, metadata
}

func inferTemplateArguments(template string) []domain.CommandArgumentDefinition {
	re := regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_-]*)(?:\|([^}]+))?\s*\}\}|\$\{([A-Za-z_][A-Za-z0-9_-]*)\}|\$([A-Za-z_][A-Za-z0-9_-]*)`)
	matches := re.FindAllStringSubmatch(template, -1)
	seen := map[string]struct{}{}
	args := make([]domain.CommandArgumentDefinition, 0)
	for _, match := range matches {
		name := firstNonEmpty(match[1], match[3], match[4])
		if name == "" || name == "ARGUMENTS" || regexp.MustCompile(`^\d+$`).MatchString(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		arg := domain.CommandArgumentDefinition{
			Name:         name,
			Required:     strings.TrimSpace(match[2]) == "",
			DefaultValue: strings.TrimSpace(match[2]),
		}
		args = append(args, arg)
	}
	return args
}

func parseCommandArguments(args []string) parsedCommandArguments {
	result := parsedCommandArguments{
		Positional: make([]string, 0, len(args)),
		Named:      map[string]string{},
	}
	for index := 0; index < len(args); index++ {
		current := strings.TrimSpace(args[index])
		if current == "" {
			continue
		}
		if strings.HasPrefix(current, "--") {
			keyValue := strings.TrimPrefix(current, "--")
			if key, value, ok := strings.Cut(keyValue, "="); ok {
				result.Named[key] = value
				continue
			}
			if index+1 < len(args) && !strings.HasPrefix(strings.TrimSpace(args[index+1]), "--") {
				result.Named[keyValue] = strings.TrimSpace(args[index+1])
				index++
				continue
			}
			result.Named[keyValue] = "true"
			continue
		}
		if key, value, ok := strings.Cut(current, "="); ok && !strings.Contains(key, " ") {
			result.Named[key] = value
			continue
		}
		result.Positional = append(result.Positional, current)
	}
	return result
}

func renderCommandTemplate(template string, definitions []domain.CommandArgumentDefinition, args []string) (string, []string) {
	parsed := parseCommandArguments(args)
	rendered := strings.ReplaceAll(template, "$ARGUMENTS", strings.Join(parsed.Positional, " "))
	for index, arg := range parsed.Positional {
		rendered = strings.ReplaceAll(rendered, fmt.Sprintf("$%d", index+1), arg)
	}

	order := definitions
	if len(order) == 0 {
		order = inferTemplateArguments(template)
	}
	values := map[string]string{}
	for index, definition := range order {
		value := strings.TrimSpace(parsed.Named[definition.Name])
		if value == "" && index < len(parsed.Positional) {
			value = parsed.Positional[index]
		}
		if value == "" {
			value = definition.DefaultValue
		}
		values[definition.Name] = value
	}

	missing := make([]string, 0)
	for _, definition := range order {
		if definition.Required && strings.TrimSpace(values[definition.Name]) == "" {
			missing = append(missing, definition.Name)
		}
	}
	for name, value := range values {
		rendered = strings.ReplaceAll(rendered, "${"+name+"}", value)
		rendered = strings.ReplaceAll(rendered, "$"+name, value)
	}

	placeholderRe := regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_-]*)(?:\|([^}]+))?\s*\}\}`)
	rendered = placeholderRe.ReplaceAllStringFunc(rendered, func(match string) string {
		parts := placeholderRe.FindStringSubmatch(match)
		name := strings.TrimSpace(parts[1])
		fallback := strings.TrimSpace(parts[2])
		if value := strings.TrimSpace(values[name]); value != "" {
			return value
		}
		return fallback
	})
	return strings.TrimSpace(rendered), uniqueStrings(missing)
}

func commandUsage(command domain.CommandDefinition) string {
	usage := "/" + command.ID
	if len(command.Arguments) == 0 {
		return usage
	}
	parts := make([]string, 0, len(command.Arguments)+1)
	parts = append(parts, usage)
	for _, arg := range command.Arguments {
		token := fmt.Sprintf("--%s <%s>", arg.Name, arg.Name)
		if !arg.Required || strings.TrimSpace(arg.DefaultValue) != "" {
			token = "[" + token + "]"
		}
		parts = append(parts, token)
	}
	return strings.Join(parts, " ")
}

func firstPositional(parsed parsedCommandArguments, index int) string {
	if index < 0 || index >= len(parsed.Positional) {
		return ""
	}
	return parsed.Positional[index]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
