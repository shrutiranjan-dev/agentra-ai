package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/config"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/domain"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/events"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/lsp"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/ollama"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/store"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/tools"
)

var (
	ErrMongoUnavailable  = errors.New("mongo dependency unavailable")
	ErrNeo4jUnavailable  = errors.New("neo4j dependency unavailable")
	ErrPermissionDenied  = errors.New("permission denied")
	ErrMalformedToolCall = errors.New("malformed tool call")
	ErrToolLoopStalled   = errors.New("tool loop stalled after repeated unsuccessful attempts")
)

type Status struct {
	ServiceHealthy  bool     `json:"serviceHealthy"`
	MongoAvailable  bool     `json:"mongoAvailable"`
	Neo4jAvailable  bool     `json:"neo4jAvailable"`
	OllamaReachable bool     `json:"ollamaReachable"`
	Mode            string   `json:"mode"`
	Messages        []string `json:"messages"`
}

type App struct {
	Config          config.Config
	Mongo           *store.MongoStore
	Neo4j           *store.Neo4jStore
	Ollama          *ollama.Client
	Hub             *events.Hub
	pendingMu       sync.Mutex
	pendingReq      map[string]chan string
	allowMu         sync.RWMutex
	allowRules      map[string]struct{}
	startupMessages []string
	statusMu        sync.RWMutex
	memMu           sync.RWMutex
	memSessions     map[string]domain.Session
	memMessages     map[string][]domain.Message
	memPermissions  map[string]domain.PermissionRequest
}

type toolRunMeta struct {
	runID      string
	toolCallID string
	toolName   string
	sessionID  string
	input      string
	path       string
	summary    string
	startedAt  int64
}

type applyPatchResult struct {
	path      string
	mode      string
	content   string
	changes   int
	preview   string
	summary   string
	oldExists bool
}

type indexedSymbol struct {
	Name      string
	Kind      string
	Line      int
	Character int
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	app := &App{
		Config:         cfg,
		Ollama:         ollama.NewClient(cfg.OllamaBaseURL),
		Hub:            events.NewHub(),
		pendingReq:     make(map[string]chan string),
		allowRules:     make(map[string]struct{}),
		memSessions:    make(map[string]domain.Session),
		memMessages:    make(map[string][]domain.Message),
		memPermissions: make(map[string]domain.PermissionRequest),
	}

	mongoStore, err := store.NewMongoStore(ctx, cfg.MongoURI, cfg.MongoDatabase)
	if err != nil {
		app.recordStartupMessage(fmt.Sprintf("MongoDB unavailable: %v", err))
	} else {
		app.Mongo = mongoStore
	}
	neo4jStore, err := store.NewNeo4jStore(ctx, cfg.Neo4jURI, cfg.Neo4jUsername, cfg.Neo4jPassword)
	if err != nil {
		app.recordStartupMessage(fmt.Sprintf("Neo4j unavailable: %v", err))
	} else {
		app.Neo4j = neo4jStore
	}
	if app.Mongo == nil && app.Neo4j == nil {
		app.recordStartupMessage("Running in degraded mode without MongoDB and Neo4j")
	}
	return app, nil
}

func (a *App) Close(ctx context.Context) error {
	var errMongo error
	if a.Mongo != nil {
		errMongo = a.Mongo.Close(ctx)
	}
	var errNeo error
	if a.Neo4j != nil {
		errNeo = a.Neo4j.Close(ctx)
	}
	if errMongo != nil {
		return errMongo
	}
	return errNeo
}

func (a *App) CreateSession(ctx context.Context, title string) (domain.Session, error) {
	if strings.TrimSpace(title) == "" {
		title = "New Session"
	}
	now := time.Now().UTC()
	session := domain.Session{
		ID:        uuid.NewString(),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if a.Mongo != nil {
		if err := a.Mongo.SaveSession(ctx, session); err != nil {
			return session, err
		}
	} else {
		a.memMu.Lock()
		a.memSessions[session.ID] = session
		a.memMu.Unlock()
	}
	if a.Neo4j != nil {
		_ = a.Neo4j.UpsertSessionNode(ctx, session.ID, session.Title)
	}
	return session, nil
}

func (a *App) ListSessions(ctx context.Context) ([]domain.Session, error) {
	if a.Mongo != nil {
		items, err := a.Mongo.ListSessions(ctx)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []domain.Session{}
		}
		return items, nil
	}
	a.memMu.RLock()
	defer a.memMu.RUnlock()
	items := make([]domain.Session, 0, len(a.memSessions))
	for _, session := range a.memSessions {
		items = append(items, session)
	}
	return items, nil
}

func (a *App) getSession(ctx context.Context, sessionID string) (domain.Session, error) {
	if a.Mongo != nil {
		sessions, err := a.Mongo.ListSessions(ctx)
		if err != nil {
			return domain.Session{}, err
		}
		for _, session := range sessions {
			if session.ID == sessionID {
				return session, nil
			}
		}
		return domain.Session{}, fmt.Errorf("session not found: %s", sessionID)
	}
	a.memMu.RLock()
	defer a.memMu.RUnlock()
	session, ok := a.memSessions[sessionID]
	if !ok {
		return domain.Session{}, fmt.Errorf("session not found: %s", sessionID)
	}
	return session, nil
}

func (a *App) updateSession(ctx context.Context, sessionID string, mutate func(*domain.Session)) error {
	session, err := a.getSession(ctx, sessionID)
	if err != nil {
		return err
	}
	mutate(&session)
	session.UpdatedAt = time.Now().UTC()
	if a.Mongo != nil {
		return a.Mongo.SaveSession(ctx, session)
	}
	a.memMu.Lock()
	a.memSessions[sessionID] = session
	a.memMu.Unlock()
	return nil
}

func (a *App) ListMessages(ctx context.Context, sessionID string) ([]domain.Message, error) {
	if a.Mongo != nil {
		items, err := a.Mongo.ListMessages(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []domain.Message{}
		}
		return items, nil
	}
	a.memMu.RLock()
	defer a.memMu.RUnlock()
	items := a.memMessages[sessionID]
	out := make([]domain.Message, len(items))
	copy(out, items)
	return out, nil
}

func (a *App) SaveMessage(ctx context.Context, message domain.Message) error {
	if a.Mongo != nil {
		return a.Mongo.SaveMessage(ctx, message)
	}
	a.memMu.Lock()
	defer a.memMu.Unlock()
	a.memMessages[message.SessionID] = upsertMessage(a.memMessages[message.SessionID], message)
	return nil
}

func upsertMessage(items []domain.Message, message domain.Message) []domain.Message {
	for index, item := range items {
		if item.ID == message.ID {
			items[index] = message
			return items
		}
	}
	return append(items, message)
}

func (a *App) ListModels(ctx context.Context) ([]ollama.Model, error) {
	items, err := a.Ollama.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []ollama.Model{}
	}
	return items, nil
}

func (a *App) RunAgent(ctx context.Context, sessionID, model, prompt, repoPath string) (domain.Message, error) {
	if strings.TrimSpace(model) == "" {
		model = a.runtimeConfig(repoPath).DefaultModel
	}
	userMessage := domain.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      domain.RoleUser,
		Parts: []domain.ContentPart{
			{Type: "text", Text: prompt},
			{Type: "finish", Reason: "stop", Time: time.Now().Unix()},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := a.SaveMessage(ctx, userMessage); err != nil {
		return domain.Message{}, err
	}

	if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
		return a.executeCommand(ctx, sessionID, model, repoPath, prompt)
	}

	if pathPrompt := looksLikePathPrompt(prompt); pathPrompt != "" {
		return a.analyzePath(ctx, sessionID, model, pathPrompt)
	}

	if pathPrompt := analyzePromptForPath(prompt); pathPrompt != "" {
		return a.analyzePath(ctx, sessionID, model, pathPrompt)
	}

	return a.runLLMFlow(ctx, sessionID, model, repoPath, prompt, "")
}

func (a *App) RunSubtask(ctx context.Context, parentSessionID, parentRunID, model, title, prompt, repoPath string) (domain.SubtaskResult, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Subtask"
	}
	now := time.Now().UTC()
	session := domain.Session{
		ID:              uuid.NewString(),
		Title:           title,
		ParentSessionID: parentSessionID,
		ParentRunID:     parentRunID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if a.Mongo != nil {
		if err := a.Mongo.SaveSession(ctx, session); err != nil {
			return domain.SubtaskResult{}, err
		}
	} else {
		a.memMu.Lock()
		a.memSessions[session.ID] = session
		a.memMu.Unlock()
	}
	if a.Neo4j != nil {
		_ = a.Neo4j.UpsertSessionNode(ctx, parentSessionID, "Parent Session")
		_ = a.Neo4j.UpsertSessionNode(ctx, session.ID, session.Title)
		_ = a.Neo4j.LinkChildSession(ctx, parentSessionID, session.ID)
	}
	link := domain.SubtaskLink{
		ParentSessionID: parentSessionID,
		ParentRunID:     parentRunID,
		ChildSessionID:  session.ID,
		Title:           session.Title,
		Prompt:          prompt,
		Status:          "running",
	}
	message, err := a.RunAgent(ctx, session.ID, model, prompt, repoPath)
	if err != nil {
		link.Status = "failed"
		a.Hub.Broadcast(events.Event{
			Type: "run.status",
			Data: map[string]any{
				"sessionId": parentSessionID,
				"runId":     parentRunID,
				"status":    "tool_failed",
				"message":   fmt.Sprintf("subtask failed: %s", err.Error()),
				"subtask":   link,
				"time":      time.Now().UTC().Unix(),
			},
		})
		return domain.SubtaskResult{}, err
	}
	link.Status = "completed"
	link.Summary = truncateText(strings.TrimSpace(extractTextParts(message.Parts)), 1200)
	if strings.TrimSpace(parentSessionID) != "" {
		parentText := fmt.Sprintf("Subtask completed: %s\nChild session: %s", session.Title, session.ID)
		if strings.TrimSpace(link.Summary) != "" {
			parentText += "\nSummary:\n" + link.Summary
		}
		_, _ = a.saveAssistantTextMessage(ctx, parentSessionID, model, parentText, "stop")
	}
	if a.Neo4j != nil {
		subtaskMemoryID := uuid.NewString()
		label := fmt.Sprintf("%s: %s", session.Title, truncateText(strings.TrimSpace(extractTextParts(message.Parts)), 400))
		_ = a.Neo4j.UpsertMemoryNode(ctx, subtaskMemoryID, "subtask", label)
		_ = a.Neo4j.LinkMemoryToSession(ctx, subtaskMemoryID, session.ID)
		if strings.TrimSpace(parentSessionID) != "" {
			_ = a.Neo4j.LinkMemoryToSession(ctx, subtaskMemoryID, parentSessionID)
		}
	}
	a.Hub.Broadcast(events.Event{
		Type: "run.status",
		Data: map[string]any{
			"sessionId": parentSessionID,
			"runId":     parentRunID,
			"status":    "subtask_completed",
			"message":   fmt.Sprintf("subtask completed: %s (%s)", session.Title, session.ID),
			"subtask":   link,
			"time":      time.Now().UTC().Unix(),
		},
	})
	return domain.SubtaskResult{Session: session, Message: message, Link: link}, nil
}

func (a *App) runLLMFlow(ctx context.Context, sessionID, model, repoPath, prompt, commandLabel string) (domain.Message, error) {
	runID := uuid.NewString()
	_ = a.updateSession(ctx, sessionID, func(session *domain.Session) {
		session.LastRunID = runID
		session.LastRunStatus = "started"
	})
	a.emitRunStatus(sessionID, runID, "started", "agent run started", "", nil, nil)

	history, err := a.ListMessages(ctx, sessionID)
	if err != nil {
		a.emitRunStatus(sessionID, runID, "model_failed", err.Error(), "", nil, nil)
		return domain.Message{}, err
	}
	history, continuation, _ := a.maybeCompactHistory(ctx, sessionID, model, repoPath, history)
	if continuation != nil {
		a.emitRunStatus(sessionID, runID, "compacted_then_continued", "continuation summary refreshed", "", continuation, nil)
		_ = a.updateSession(ctx, sessionID, func(session *domain.Session) {
			session.LastRunStatus = "compacted_then_continued"
		})
	}

	if a.Neo4j != nil {
		memoryID := uuid.NewString()
		_ = a.Neo4j.UpsertSessionNode(ctx, sessionID, "")
		_ = a.Neo4j.UpsertMemoryNode(ctx, memoryID, "session", prompt)
		_ = a.Neo4j.LinkMemoryToSession(ctx, memoryID, sessionID)
	}

	ollamaMessages := make([]ollama.Message, 0, len(history)+2)
	if repoPath != "" {
		repoSummary, _ := a.RepoGraphSummary(ctx, repoPath, sessionID)
		relevantFiles, _ := a.RelevantRepoFiles(ctx, sessionID, repoPath, prompt)
		relevantSymbols, _ := a.RelevantRepoSymbols(ctx, sessionID, repoPath, prompt)
		relevantMemories, _ := a.RelevantSessionMemories(ctx, sessionID, prompt)
		if len(relevantMemories) > 0 {
			repoSummary.Memories = make([]store.MemorySummary, 0, len(relevantMemories))
			for _, item := range relevantMemories {
				repoSummary.Memories = append(repoSummary.Memories, store.MemorySummary{ID: item.ID, Kind: item.Kind, Label: item.Label})
			}
		}
		repoSummary.RelatedFileMatches = append([]store.FileMatch{}, relevantFiles...)
		repoSummary.RelatedFiles = fileMatchPaths(relevantFiles)
		repoSummary.RelatedSymbols = append([]store.SymbolMatch{}, relevantSymbols...)
		repoSummary.RelatedMemoryMatch = append([]store.MemoryMatch{}, relevantMemories...)
		repoContext := buildRepoContextPrompt(repoSummary, relevantFiles, relevantSymbols, relevantMemories)
		snippets := relevantSnippets(prompt, relevantSymbols, relevantFiles)
		ollamaMessages = append(ollamaMessages, ollama.Message{
			Role:    "system",
			Content: "Active workspace: " + repoPath + "\n" + repoContext + "\n" + buildMemoryContextPrompt(relevantMemories) + formatSnippetContext(snippets),
		})
	} else {
		relevantMemories, _ := a.RelevantSessionMemories(ctx, sessionID, prompt)
		if len(relevantMemories) > 0 {
			ollamaMessages = append(ollamaMessages, ollama.Message{
				Role:    "system",
				Content: buildMemoryContextPrompt(relevantMemories),
			})
		}
	}
	if commandLabel != "" {
		ollamaMessages = append(ollamaMessages, ollama.Message{
			Role:    "system",
			Content: "This response was triggered by " + commandLabel + ".",
		})
	}
	ollamaMessages = append(ollamaMessages, a.toOllamaMessages(history)...)

	unproductiveToolLoops := 0
	for toolLoop := 0; toolLoop < 6; toolLoop++ {
		result, err := a.Ollama.Chat(ctx, model, ollamaMessages, a.availableTools(repoPath))
		if err != nil {
			a.emitRunStatus(sessionID, runID, "model_failed", err.Error(), "", continuation, nil)
			_ = a.updateSession(ctx, sessionID, func(session *domain.Session) {
				session.LastRunStatus = "model_failed"
			})
			return domain.Message{}, err
		}

		if len(result.ToolCalls) == 0 {
			text := strings.TrimSpace(result.Content)
			if text == "" {
				text = "Done."
			}
			message, saveErr := a.saveAssistantTextMessage(ctx, sessionID, model, text, stopReason(result.Reason))
			if saveErr == nil {
				a.emitRunStatus(sessionID, runID, "completed", "assistant run completed", "", continuation, nil)
				_ = a.updateSession(ctx, sessionID, func(session *domain.Session) {
					session.LastRunStatus = "completed"
				})
			}
			return message, saveErr
		}
		a.emitRunStatus(sessionID, runID, "awaiting_tool", fmt.Sprintf("%d tool call(s) requested", len(result.ToolCalls)), "", continuation, nil)

		assistantToolMessage := domain.Message{
			ID:        uuid.NewString(),
			SessionID: sessionID,
			Role:      domain.RoleAssistant,
			Model:     model,
			Parts:     make([]domain.ContentPart, 0, len(result.ToolCalls)+1),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		nextMessages := []ollama.Message{{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		}}

		for _, call := range result.ToolCalls {
			callID := uuid.NewString()
			inputBytes, _ := json.Marshal(call.Function.Arguments)
			toolName := strings.TrimSpace(call.Function.Name)
			if toolName == "" {
				toolName = "unknown"
			}
			assistantToolMessage.Parts = append(assistantToolMessage.Parts, domain.ContentPart{
				Type:   "tool_call",
				ID:     callID,
				Name:   toolName,
				Input:  string(inputBytes),
				Status: "running",
			})
			a.emitToolLifecycle(toolRunMeta{
				runID:      runID,
				toolCallID: callID,
				toolName:   toolName,
				sessionID:  sessionID,
				input:      string(inputBytes),
				path:       stringArg(call.Function.Arguments, "path", ""),
				summary:    toolName,
			}, "requested", "", false)
		}
		assistantToolMessage.Parts = append(assistantToolMessage.Parts, domain.ContentPart{
			Type:   "finish",
			Reason: "tool_use",
			Time:   time.Now().Unix(),
		})
		if err := a.SaveMessage(ctx, assistantToolMessage); err != nil {
			return domain.Message{}, err
		}
		a.Hub.Broadcast(events.Event{Type: "message.completed", Data: assistantToolMessage})

		successfulToolCalls := 0
		for index, call := range result.ToolCalls {
			callMeta := toolRunMeta{
				runID:      runID,
				toolCallID: assistantToolMessage.Parts[index].ID,
				toolName:   assistantToolMessage.Parts[index].Name,
				sessionID:  sessionID,
				input:      assistantToolMessage.Parts[index].Input,
				path:       stringArg(call.Function.Arguments, "path", ""),
				summary:    assistantToolMessage.Parts[index].Name,
			}
			toolOutput, execErr := a.executeToolCall(ctx, sessionID, model, repoPath, call, callMeta)
			if index < len(assistantToolMessage.Parts) {
				assistantToolMessage.Parts[index].Status = toolExecutionStatus(execErr)
			}
			resultContent := formatToolResult(callMeta.toolName, toolOutput, execErr)
			if errors.Is(execErr, ErrPermissionDenied) {
				a.emitRunStatus(sessionID, runID, "tool_denied", fmt.Sprintf("tool denied: %s", callMeta.toolName), callMeta.toolName, continuation, nil)
			} else if errors.Is(execErr, ErrMalformedToolCall) {
				a.emitRunStatus(sessionID, runID, "tool_malformed", fmt.Sprintf("malformed tool call: %s", callMeta.toolName), callMeta.toolName, continuation, nil)
			} else if execErr != nil {
				a.emitRunStatus(sessionID, runID, "tool_failed", fmt.Sprintf("tool failed: %s", callMeta.toolName), callMeta.toolName, continuation, nil)
			} else {
				successfulToolCalls++
			}
			toolMessage := domain.Message{
				ID:        uuid.NewString(),
				SessionID: sessionID,
				Role:      domain.RoleTool,
				Parts: []domain.ContentPart{
					{
						Type:       "tool_result",
						ToolCallID: assistantToolMessage.Parts[index].ID,
						Name:       callMeta.toolName,
						Path:       stringArg(call.Function.Arguments, "path", ""),
						Content:    resultContent,
						IsError:    execErr != nil,
					},
					{Type: "finish", Reason: finishReasonForError(execErr), Time: time.Now().Unix()},
				},
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}
			if err := a.SaveMessage(ctx, toolMessage); err != nil {
				a.emitRunStatus(sessionID, runID, "tool_failed", err.Error(), callMeta.toolName, continuation, nil)
				return domain.Message{}, err
			}
			a.Hub.Broadcast(events.Event{Type: "message.completed", Data: toolMessage})
			nextMessages = append(nextMessages, ollama.Message{
				Role:     "tool",
				ToolName: callMeta.toolName,
				Content:  resultContent,
			})
		}

		if err := a.SaveMessage(ctx, assistantToolMessage); err != nil {
			a.emitRunStatus(sessionID, runID, "tool_failed", err.Error(), "", continuation, nil)
			return domain.Message{}, err
		}
		unproductiveToolLoops = nextUnproductiveToolLoops(unproductiveToolLoops, len(result.ToolCalls), successfulToolCalls)
		if shouldStopAfterToolLoop(unproductiveToolLoops) {
			message, stopErr := a.saveAssistantTextMessage(ctx, sessionID, model, "Stopped after repeated unsuccessful tool attempts. Please correct the tool arguments, choose a different action, or continue without the blocked tool.", "stop")
			if stopErr == nil {
				a.emitRunStatus(sessionID, runID, "stopped_after_tool_failures", ErrToolLoopStalled.Error(), "", continuation, nil)
				_ = a.updateSession(ctx, sessionID, func(session *domain.Session) {
					session.LastRunStatus = "stopped_after_tool_failures"
				})
			}
			return message, stopErr
		}
		a.emitRunStatus(sessionID, runID, "assistant_resumed", "assistant resumed after tool results", "", continuation, nil)
		ollamaMessages = append(ollamaMessages, nextMessages...)
	}

	message, err := a.saveAssistantTextMessage(ctx, sessionID, model, "Stopped after maximum tool iterations.", "length")
	if err == nil {
		a.emitRunStatus(sessionID, runID, "completed", "stopped after maximum tool iterations", "", continuation, nil)
		_ = a.updateSession(ctx, sessionID, func(session *domain.Session) {
			session.LastRunStatus = "completed"
		})
	}
	return message, err
}

func (a *App) analyzePath(ctx context.Context, sessionID, model, target string) (domain.Message, error) {
	info, err := os.Stat(target)
	if err != nil {
		return a.saveAssistantTextMessage(ctx, sessionID, model, "Path unavailable: "+err.Error(), "stop")
	}

	if info.IsDir() {
		files, err := a.ListFiles(ctx, target)
		if err != nil {
			return domain.Message{}, err
		}
		sample := strings.Join(files[:minInt(len(files), 50)], "\n")
		prompt := fmt.Sprintf("Analyze this repository path:\n%s\n\nSample files:\n%s", target, sample)
		return a.runLLMFlow(ctx, sessionID, model, target, prompt, "path analysis")
	}

	content, err := a.ReadFile(ctx, sessionID, target)
	if err != nil {
		return domain.Message{}, err
	}
	prompt := fmt.Sprintf("Analyze this file path:\n%s\n\nFile contents:\n%s", target, truncateText(content, 16000))
	return a.runLLMFlow(ctx, sessionID, model, filepath.Dir(target), prompt, "file analysis")
}

func toolSchema(name, description string, parameters map[string]any) ollama.Tool {
	return ollama.Tool{
		Type: "function",
		Function: ollama.ToolFunction{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	}
}

func (a *App) executeToolCall(ctx context.Context, sessionID, model, repoPath string, call ollama.ToolCall, meta toolRunMeta) (string, error) {
	toolName := strings.TrimSpace(call.Function.Name)
	args := call.Function.Arguments
	if toolName == "" {
		a.emitToolLifecycle(meta, "malformed", "missing tool name", true)
		return "", fmt.Errorf("%w: missing tool name", ErrMalformedToolCall)
	}
	if err := validateToolCall(toolName, args, repoPath); err != nil {
		a.emitToolLifecycle(meta, "malformed", err.Error(), true)
		return "", err
	}
	switch toolName {
	case "get_status":
		status := a.Status(ctx)
		bytes, _ := json.Marshal(status)
		return string(bytes), nil
	case "list_files":
		target := stringArg(args, "path", repoPath)
		files, err := a.ListFiles(ctx, target)
		if err != nil {
			return "", err
		}
		limit := minInt(len(files), 200)
		return strings.Join(files[:limit], "\n"), nil
	case "read_file":
		content, err := a.ReadFile(ctx, sessionID, stringArg(args, "path", ""), meta)
		if err != nil {
			return "", err
		}
		return truncateText(content, 20000), nil
	case "write_file":
		target := stringArg(args, "path", "")
		content := stringArg(args, "content", "")
		if err := a.WriteFile(ctx, sessionID, target, content, meta); err != nil {
			return "", err
		}
		return "wrote file: " + target, nil
	case "file_edit":
		target := stringArg(args, "path", "")
		oldText := stringArg(args, "old_text", "")
		newText := stringArg(args, "new_text", "")
		return a.EditFile(ctx, sessionID, target, oldText, newText, boolArg(args, "replace_all"), meta)
	case "apply_patch":
		target := stringArg(args, "path", "")
		patch := stringArg(args, "patch", "")
		return a.ApplyPatch(ctx, sessionID, target, patch, meta)
	case "shell_exec":
		cwd := stringArg(args, "cwd", repoPath)
		output, err := a.RunShell(ctx, sessionID, stringArg(args, "command", ""), cwd, meta)
		return truncateText(output, 12000), err
	case "index_repo":
		target := stringArg(args, "repo_path", repoPath)
		if err := a.IndexRepository(ctx, target); err != nil {
			return "", err
		}
		return "indexed repository: " + target, nil
	case "analyze_path":
		target := stringArg(args, "path", repoPath)
		info, err := os.Stat(target)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			files, err := a.ListFiles(ctx, target)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Directory: %s\nFiles:\n%s", target, strings.Join(files[:minInt(len(files), 50)], "\n")), nil
		}
		content, err := a.ReadFile(ctx, sessionID, target)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("File: %s\n%s", target, truncateText(content, 12000)), nil
	case "diagnostics":
		target := stringArg(args, "path", repoPath)
		items, err := a.diagnosticsForPath(ctx, target, repoPath)
		if err != nil {
			return "", err
		}
		return diagnosticsText(items), nil
	case "lsp_symbols":
		target := stringArg(args, "path", repoPath)
		items, err := a.DocumentSymbols(ctx, target, repoPath)
		if err != nil {
			return "", err
		}
		bytes, _ := json.Marshal(items)
		return string(bytes), nil
	case "lsp_definition":
		target := stringArg(args, "path", repoPath)
		items, err := a.Definitions(ctx, target, repoPath, intArg(args, "line", 1), intArg(args, "character", 1))
		if err != nil {
			return "", err
		}
		bytes, _ := json.Marshal(items)
		return string(bytes), nil
	case "lsp_references":
		target := stringArg(args, "path", repoPath)
		items, err := a.References(ctx, target, repoPath, intArg(args, "line", 1), intArg(args, "character", 1))
		if err != nil {
			return "", err
		}
		bytes, _ := json.Marshal(items)
		return string(bytes), nil
	case "lsp_workspace_symbols":
		items, err := a.WorkspaceSymbols(ctx, repoPath, stringArg(args, "query", ""))
		if err != nil {
			return "", err
		}
		bytes, _ := json.Marshal(items)
		return string(bytes), nil
	case "lsp_workspace_definitions":
		items, err := a.WorkspaceDefinitions(ctx, repoPath, stringArg(args, "query", ""))
		if err != nil {
			return "", err
		}
		bytes, _ := json.Marshal(items)
		return string(bytes), nil
	case "lsp_workspace_references":
		items, err := a.WorkspaceReferences(ctx, repoPath, stringArg(args, "query", ""))
		if err != nil {
			return "", err
		}
		bytes, _ := json.Marshal(items)
		return string(bytes), nil
	case "mcp_servers":
		items, err := a.MCPServerStatuses(ctx, repoPath)
		if err != nil {
			return "", err
		}
		bytes, _ := json.Marshal(items)
		return string(bytes), nil
	case "run_subtask":
		subtaskPrompt := stringArg(args, "prompt", "")
		title := stringArg(args, "title", "Subtask")
		result, err := a.RunSubtask(ctx, sessionID, meta.runID, model, title, subtaskPrompt, repoPath)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("subtask session: %s\nsubtask title: %s\n%s", result.Session.ID, result.Session.Title, extractTextParts(result.Message.Parts)), nil
	default:
		if strings.HasPrefix(toolName, "mcp_") {
			return a.executeMCPTool(ctx, repoPath, call)
		}
		a.emitToolLifecycle(meta, "malformed", "unsupported tool", true)
		return "", fmt.Errorf("%w: unsupported tool %s", ErrMalformedToolCall, toolName)
	}
}

func (a *App) executeMCPTool(ctx context.Context, repoPath string, call ollama.ToolCall) (string, error) {
	tools, err := a.listMCPTools(ctx, repoPath)
	if err != nil {
		return "", err
	}
	for _, item := range tools {
		candidate := "mcp_" + sanitizeToolName(item.Server) + "__" + sanitizeToolName(item.Name)
		if candidate == call.Function.Name {
			return a.callMCPTool(ctx, repoPath, item.Server, item.Name, call.Function.Arguments)
		}
	}
	return "", fmt.Errorf("invalid mcp tool name: %s", call.Function.Name)
}

func stringArg(args map[string]any, key, fallback string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	text, ok := value.(string)
	if !ok {
		return fallback
	}
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	return text
}

func intArg(args map[string]any, key string, fallback int) int {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}

func boolArg(args map[string]any, key string) bool {
	value, ok := args[key]
	if !ok || value == nil {
		return false
	}
	flag, ok := value.(bool)
	return ok && flag
}

func extractTextParts(parts []domain.ContentPart) string {
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text", "reasoning":
			if strings.TrimSpace(part.Text) != "" {
				lines = append(lines, part.Text)
			}
		case "tool_result":
			if strings.TrimSpace(part.Content) != "" {
				lines = append(lines, part.Content)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (a *App) toOllamaMessages(history []domain.Message) []ollama.Message {
	messages := make([]ollama.Message, 0, len(history))
	for _, item := range history {
		switch item.Role {
		case domain.RoleTool:
			content := extractToolContent(item.Parts)
			if strings.TrimSpace(content) == "" {
				continue
			}
			toolName := "tool"
			for _, part := range item.Parts {
				if part.Type == "tool_result" && part.Name != "" {
					toolName = part.Name
					break
				}
			}
			messages = append(messages, ollama.Message{
				Role:     "tool",
				ToolName: toolName,
				Content:  content,
			})
		default:
			text := extractText(item.Parts)
			if strings.TrimSpace(text) == "" && !hasToolCalls(item.Parts) {
				continue
			}
			message := ollama.Message{
				Role:    string(item.Role),
				Content: text,
			}
			if hasToolCalls(item.Parts) {
				message.ToolCalls = make([]ollama.ToolCall, 0)
				for _, part := range item.Parts {
					if part.Type != "tool_call" {
						continue
					}
					var arguments map[string]any
					_ = json.Unmarshal([]byte(part.Input), &arguments)
					message.ToolCalls = append(message.ToolCalls, ollama.ToolCall{
						Function: ollama.ToolFunction{
							Name:      part.Name,
							Arguments: arguments,
						},
					})
				}
			}
			messages = append(messages, message)
		}
	}
	return messages
}

func hasToolCalls(parts []domain.ContentPart) bool {
	for _, part := range parts {
		if part.Type == "tool_call" {
			return true
		}
	}
	return false
}

func extractText(parts []domain.ContentPart) string {
	var out strings.Builder
	for _, part := range parts {
		if part.Type == "text" || part.Type == "reasoning" {
			out.WriteString(part.Text)
		}
	}
	return out.String()
}

func extractToolContent(parts []domain.ContentPart) string {
	var out strings.Builder
	for _, part := range parts {
		if part.Type == "tool_result" {
			out.WriteString(part.Content)
		}
	}
	return out.String()
}

func truncateText(content string, max int) string {
	if len(content) <= max {
		return content
	}
	return content[:max] + "\n...[truncated]"
}

func stopReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "stop"
	}
	switch reason {
	case "stop", "tool_use", "permission_denied", "cancelled", "length":
		return reason
	default:
		return "stop"
	}
}

func finishReasonForError(err error) string {
	if err == nil {
		return "stop"
	}
	if errors.Is(err, ErrPermissionDenied) {
		return "permission_denied"
	}
	return "cancelled"
}

func (a *App) saveAssistantTextMessage(ctx context.Context, sessionID, model, text, reason string) (domain.Message, error) {
	if model == "" {
		model = "assistant"
	}
	message := domain.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      domain.RoleAssistant,
		Model:     model,
		Parts: []domain.ContentPart{
			{Type: "text", Text: text},
			{Type: "finish", Reason: reason, Time: time.Now().Unix()},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := a.SaveMessage(ctx, message); err != nil {
		return domain.Message{}, err
	}
	a.Hub.Broadcast(events.Event{Type: "message.completed", Data: message})
	return message, nil
}

func (a *App) RequestPermission(ctx context.Context, sessionID, toolName, action, path, description, params string) (string, error) {
	return a.requestPermission(ctx, sessionID, toolName, action, path, nil, description, params, "", toolRunMeta{})
}

func (a *App) RequestPermissionWithPreview(ctx context.Context, sessionID, toolName, action, path string, paths []string, description, params, preview string) (string, error) {
	return a.requestPermission(ctx, sessionID, toolName, action, path, paths, description, params, preview, toolRunMeta{})
}

func (a *App) requestPermission(ctx context.Context, sessionID, toolName, action, path string, paths []string, description, params, preview string, meta toolRunMeta) (string, error) {
	ruleKey := permissionRuleKey(sessionID, toolName, action, path)
	a.allowMu.RLock()
	_, allowed := a.allowRules[ruleKey]
	a.allowMu.RUnlock()
	if allowed {
		a.emitApprovalUpdate(meta, toolName, action, path, "allow_session", "")
		return "allow_session", nil
	}

	request := domain.PermissionRequest{
		ID:          uuid.NewString(),
		SessionID:   sessionID,
		RunID:       meta.runID,
		ToolCallID:  meta.toolCallID,
		ToolName:    toolName,
		Action:      action,
		Path:        path,
		Paths:       uniqueNonEmptyPaths(paths, path),
		Description: description,
		Params:      params,
		Preview:     preview,
		Status:      "pending",
		CreatedAt:   time.Now().UTC(),
	}
	if a.Mongo != nil {
		if err := a.Mongo.SavePermission(ctx, request); err != nil {
			return "", err
		}
	}
	a.memMu.Lock()
	a.memPermissions[request.ID] = request
	a.memMu.Unlock()

	decisionCh := make(chan string, 1)
	a.pendingMu.Lock()
	a.pendingReq[request.ID] = decisionCh
	a.pendingMu.Unlock()
	defer func() {
		a.pendingMu.Lock()
		delete(a.pendingReq, request.ID)
		a.pendingMu.Unlock()
	}()

	a.Hub.Broadcast(events.Event{
		Type: "permission.requested",
		Data: request,
	})
	a.emitApprovalUpdate(meta, toolName, action, path, "requested", request.ID)
	if meta.toolCallID != "" {
		a.emitToolLifecycle(meta, "awaiting_approval", "", false)
	}

	select {
	case decision := <-decisionCh:
		a.emitApprovalUpdate(meta, toolName, action, path, decision, request.ID)
		return decision, nil
	case <-time.After(time.Duration(a.Config.PermissionTTLMS) * time.Millisecond):
		_ = a.updatePermissionStatus(context.Background(), request.ID, "timeout")
		a.emitApprovalUpdate(meta, toolName, action, path, "timeout", request.ID)
		return "", errors.New("permission request timed out")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (a *App) ResolvePermission(ctx context.Context, requestID, decision string) error {
	a.pendingMu.Lock()
	decisionCh, ok := a.pendingReq[requestID]
	a.pendingMu.Unlock()

	requests, lookupErr := a.listPendingPermissions(ctx, requestID)
	if err := a.updatePermissionStatus(ctx, requestID, decision); err != nil {
		return err
	}
	if decision == "allow_session" {
		if lookupErr == nil && len(requests) > 0 {
			ruleKey := permissionRuleKey(requests[0].SessionID, requests[0].ToolName, requests[0].Action, requests[0].Path)
			a.allowMu.Lock()
			a.allowRules[ruleKey] = struct{}{}
			a.allowMu.Unlock()
		}
	}
	if lookupErr == nil && len(requests) > 0 {
		request := requests[0]
		a.emitApprovalUpdate(toolRunMeta{
			runID:      request.RunID,
			toolCallID: request.ToolCallID,
			sessionID:  request.SessionID,
		}, request.ToolName, request.Action, request.Path, decision, request.ID)
	}
	if ok {
		decisionCh <- decision
	}
	return nil
}

func (a *App) RunShell(ctx context.Context, sessionID, command, cwd string, meta ...toolRunMeta) (string, error) {
	runMeta := resolveToolMeta(sessionID, "shell_exec", command, cwd, command, meta...)
	decision, err := a.requestPermission(ctx, sessionID, "shell_exec", "execute", cwd, nil, "Execute shell command", command, "", runMeta)
	if err != nil {
		return "", err
	}
	if decision == "deny" {
		a.emitToolLifecycle(runMeta, "denied", "permission denied", true)
		return "", ErrPermissionDenied
	}

	a.emitToolLifecycle(runMeta, "approved", "", false)
	runID, runMeta := a.beginToolRun(runMeta)

	output, execErr := tools.RunShell(ctx, cwd, command)
	a.completeToolRun(runID, runMeta, output, execErr)

	return output, execErr
}

func (a *App) ReadFile(ctx context.Context, sessionID, path string, meta ...toolRunMeta) (string, error) {
	runMeta := resolveToolMeta(sessionID, "file_read", path, path, "read "+path, meta...)
	decision, err := a.requestPermission(ctx, sessionID, "file_read", "read", path, nil, "Read file", path, "", runMeta)
	if err != nil {
		return "", err
	}
	if decision == "deny" {
		a.emitToolLifecycle(runMeta, "denied", "permission denied", true)
		return "", ErrPermissionDenied
	}
	a.emitToolLifecycle(runMeta, "approved", "", false)
	runID, runMeta := a.beginToolRun(runMeta)
	bytes, err := os.ReadFile(path)
	if err != nil {
		a.completeToolRun(runID, runMeta, "", err)
		return "", err
	}
	if a.Neo4j != nil {
		_ = a.Neo4j.LinkSessionToFile(ctx, sessionID, path)
	}
	output := string(bytes)
	a.completeToolRun(runID, runMeta, output, nil)
	return output, nil
}

func (a *App) WriteFile(ctx context.Context, sessionID, path, content string, meta ...toolRunMeta) error {
	runMeta := resolveToolMeta(sessionID, "file_write", content, path, "write "+path, meta...)
	decision, err := a.requestPermission(ctx, sessionID, "file_write", "write", path, nil, "Write file", path, "", runMeta)
	if err != nil {
		return err
	}
	if decision == "deny" {
		a.emitToolLifecycle(runMeta, "denied", "permission denied", true)
		return ErrPermissionDenied
	}
	a.emitToolLifecycle(runMeta, "approved", "", false)
	runID, runMeta := a.beginToolRun(runMeta)
	err = a.writeFileRaw(ctx, sessionID, path, content)
	a.completeToolRun(runID, runMeta, "wrote file: "+path, err)
	return err
}

func (a *App) EditFile(ctx context.Context, sessionID, path, oldText, newText string, replaceAll bool, meta ...toolRunMeta) (string, error) {
	runMeta := resolveToolMeta(sessionID, "file_edit", oldText, path, fmt.Sprintf("edit %s", path), meta...)
	current, err := a.readFileRaw(path)
	if err != nil {
		a.emitToolLifecycle(runMeta, "failed", err.Error(), true)
		return "", err
	}
	result, err := tools.ReplaceExact(current, oldText, newText, replaceAll)
	if err != nil {
		a.emitToolLifecycle(runMeta, "failed", err.Error(), true)
		return "", err
	}
	decision, err := a.requestPermission(ctx, sessionID, "file_edit", "write", path, []string{path}, "Apply file edit", fmt.Sprintf("path: %s\nreplace_all: %t", path, replaceAll), withPreviewPath(path, result.Preview), runMeta)
	if err != nil {
		return "", err
	}
	if decision == "deny" {
		a.emitToolLifecycle(runMeta, "denied", "permission denied", true)
		return "", ErrPermissionDenied
	}
	a.emitToolLifecycle(runMeta, "approved", "", false)
	runID, runMeta := a.beginToolRun(runMeta)
	if err := a.writeFileRaw(ctx, sessionID, path, result.Content); err != nil {
		a.completeToolRun(runID, runMeta, "", err)
		return "", err
	}
	output := fmt.Sprintf("edited file: %s\nreplacements: %d\n\n%s", path, result.Replacements, withPreviewPath(path, result.Preview))
	a.completeToolRun(runID, runMeta, output, nil)
	return output, nil
}

func (a *App) ApplyPatch(ctx context.Context, sessionID, path, patch string, meta ...toolRunMeta) (string, error) {
	runMeta := resolveToolMeta(sessionID, "apply_patch", patch, path, "patch", meta...)
	targets, err := tools.ParsePatchTargets(path, patch)
	if err != nil {
		a.emitToolLifecycle(runMeta, "failed", err.Error(), true)
		return "", err
	}
	results := make([]applyPatchResult, 0, len(targets))
	previews := make([]string, 0, len(targets))
	paths := make([]string, 0, len(targets))
	totalChanges := 0
	for _, target := range targets {
		switch target.Mode {
		case "add":
			preview := withPreviewPath(target.Path, toolsPreviewForCreate(target.Path, target.NewFile))
			results = append(results, applyPatchResult{
				path:      target.Path,
				mode:      "add",
				content:   target.NewFile,
				changes:   1,
				preview:   preview,
				summary:   fmt.Sprintf("add %s", target.Path),
				oldExists: false,
			})
			previews = append(previews, preview)
			totalChanges++
		case "delete":
			current, readErr := a.readFileRaw(target.Path)
			if readErr != nil {
				a.emitToolLifecycle(runMeta, "failed", readErr.Error(), true)
				return "", readErr
			}
			preview := withPreviewPath(target.Path, toolsPreviewForDelete(target.Path, current))
			results = append(results, applyPatchResult{
				path:      target.Path,
				mode:      "delete",
				preview:   preview,
				changes:   1,
				summary:   fmt.Sprintf("delete %s", target.Path),
				oldExists: true,
			})
			previews = append(previews, preview)
			totalChanges++
		default:
			current, readErr := a.readFileRaw(target.Path)
			if readErr != nil {
				a.emitToolLifecycle(runMeta, "failed", readErr.Error(), true)
				return "", readErr
			}
			result, applyErr := tools.ApplyPatchBlocks(current, target.Blocks)
			if applyErr != nil {
				a.emitToolLifecycle(runMeta, "failed", applyErr.Error(), true)
				return "", applyErr
			}
			preview := withPreviewPath(target.Path, result.Preview)
			results = append(results, applyPatchResult{
				path:      target.Path,
				mode:      "update",
				content:   result.Content,
				changes:   result.Replacements,
				preview:   preview,
				summary:   fmt.Sprintf("update %s (%d block%s)", target.Path, result.Replacements, pluralSuffix(result.Replacements)),
				oldExists: true,
			})
			previews = append(previews, preview)
			totalChanges += result.Replacements
		}
		paths = append(paths, target.Path)
	}
	summary := formatPatchApprovalSummary(results, totalChanges)
	runMeta.path = firstOrEmpty(paths)
	runMeta.summary = summary
	decision, err := a.requestPermission(ctx, sessionID, "apply_patch", patchActionForResults(results), firstOrEmpty(paths), paths, fmt.Sprintf("Apply patch to %d file(s)", len(paths)), summary, strings.Join(previews, "\n\n"), runMeta)
	if err != nil {
		return "", err
	}
	if decision == "deny" {
		a.emitToolLifecycle(runMeta, "denied", "permission denied", true)
		return "", ErrPermissionDenied
	}
	a.emitToolLifecycle(runMeta, "approved", "", false)
	runID, runMeta := a.beginToolRun(runMeta)
	for _, result := range results {
		switch result.mode {
		case "delete":
			if err := os.Remove(result.path); err != nil {
				a.completeToolRun(runID, runMeta, "", err)
				return "", err
			}
		default:
			if err := a.writeFileRaw(ctx, sessionID, result.path, result.content); err != nil {
				a.completeToolRun(runID, runMeta, "", err)
				return "", err
			}
		}
	}
	output := fmt.Sprintf("%s\n\n%s", summary, strings.Join(previews, "\n\n"))
	a.completeToolRun(runID, runMeta, output, nil)
	return output, nil
}

func (a *App) readFileRaw(path string) (string, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func (a *App) writeFileRaw(ctx context.Context, sessionID, path, content string) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	if a.Neo4j != nil {
		return a.Neo4j.LinkSessionToFile(ctx, sessionID, path)
	}
	return nil
}

func resolveToolMeta(sessionID, toolName, input, path, summary string, meta ...toolRunMeta) toolRunMeta {
	if len(meta) > 0 {
		resolved := meta[0]
		if resolved.sessionID == "" {
			resolved.sessionID = sessionID
		}
		if resolved.toolName == "" {
			resolved.toolName = toolName
		}
		if resolved.input == "" {
			resolved.input = input
		}
		if resolved.path == "" {
			resolved.path = path
		}
		if resolved.summary == "" {
			resolved.summary = summary
		}
		if resolved.toolCallID == "" {
			resolved.toolCallID = uuid.NewString()
		}
		return resolved
	}
	return toolRunMeta{
		sessionID:  sessionID,
		toolName:   toolName,
		input:      input,
		path:       path,
		summary:    summary,
		toolCallID: uuid.NewString(),
	}
}

func (a *App) beginToolRun(meta toolRunMeta) (string, toolRunMeta) {
	runID := meta.toolCallID
	if runID == "" {
		runID = uuid.NewString()
		meta.toolCallID = runID
	}
	startedAt := meta.startedAt
	if startedAt == 0 {
		startedAt = time.Now().UTC().Unix()
		meta.startedAt = startedAt
	}
	a.emitToolLifecycle(meta, "running", "", false)
	a.Hub.Broadcast(events.Event{
		Type: "tool.started",
		Data: map[string]any{
			"sessionId":  meta.sessionID,
			"runId":      meta.runID,
			"toolCallId": runID,
			"toolName":   meta.toolName,
			"input":      meta.input,
			"path":       meta.path,
			"summary":    meta.summary,
			"startedAt":  startedAt,
		},
	})
	return runID, meta
}

func (a *App) completeToolRun(runID string, meta toolRunMeta, output string, execErr error) {
	completedAt := time.Now().UTC()
	status := "completed"
	if errors.Is(execErr, ErrPermissionDenied) {
		status = "denied"
	} else if execErr != nil {
		status = "failed"
	}
	run := domain.ToolExecution{
		ID:          runID,
		RunID:       meta.runID,
		SessionID:   meta.sessionID,
		ToolName:    meta.toolName,
		Path:        meta.path,
		Summary:     meta.summary,
		Status:      status,
		Input:       meta.input,
		Output:      output,
		IsError:     execErr != nil,
		StartedAt:   time.Unix(meta.startedAt, 0).UTC(),
		CompletedAt: completedAt,
		CreatedAt:   completedAt,
	}
	if a.Mongo != nil {
		_ = a.Mongo.SaveToolRun(context.Background(), run)
	}
	a.emitToolLifecycle(meta, status, outputOrError(output, execErr), execErr != nil)
	a.Hub.Broadcast(events.Event{
		Type: "tool.completed",
		Data: map[string]any{
			"sessionId":   meta.sessionID,
			"runId":       meta.runID,
			"toolCallId":  run.ID,
			"toolName":    meta.toolName,
			"path":        meta.path,
			"summary":     meta.summary,
			"output":      outputOrError(output, execErr),
			"isError":     execErr != nil,
			"completedAt": completedAt.Unix(),
		},
	})
}

func (a *App) emitToolLifecycle(meta toolRunMeta, status, output string, isError bool) {
	if meta.sessionID == "" || meta.toolName == "" {
		return
	}
	if meta.toolCallID == "" {
		meta.toolCallID = uuid.NewString()
	}
	a.Hub.Broadcast(events.Event{
		Type: "tool.lifecycle",
		Data: map[string]any{
			"sessionId":  meta.sessionID,
			"runId":      meta.runID,
			"toolCallId": meta.toolCallID,
			"toolName":   meta.toolName,
			"status":     status,
			"path":       meta.path,
			"input":      meta.input,
			"summary":    meta.summary,
			"output":     output,
			"isError":    isError,
			"time":       time.Now().UTC().Unix(),
		},
	})
}

func (a *App) emitApprovalUpdate(meta toolRunMeta, toolName, action, path, status, requestID string) {
	a.Hub.Broadcast(events.Event{
		Type: "approval.updated",
		Data: map[string]any{
			"requestId":  requestID,
			"sessionId":  meta.sessionID,
			"runId":      meta.runID,
			"toolCallId": meta.toolCallID,
			"toolName":   toolName,
			"action":     action,
			"status":     status,
			"path":       path,
			"time":       time.Now().UTC().Unix(),
		},
	})
}

func (a *App) emitRunStatus(sessionID, runID, status, message, toolName string, continuation *domain.ContinuationState, subtask *domain.SubtaskLink) {
	a.Hub.Broadcast(events.Event{
		Type: "run.status",
		Data: map[string]any{
			"sessionId":    sessionID,
			"runId":        runID,
			"status":       status,
			"message":      message,
			"toolName":     toolName,
			"continuation": continuation,
			"subtask":      subtask,
			"time":         time.Now().UTC().Unix(),
		},
	})
}

func outputOrError(output string, execErr error) string {
	if execErr != nil {
		if output != "" {
			return output + "\n" + execErr.Error()
		}
		return execErr.Error()
	}
	return output
}

func formatToolResult(toolName, output string, execErr error) string {
	status := toolExecutionStatus(execErr)
	var lines []string
	lines = append(lines, fmt.Sprintf("tool: %s", toolName))
	lines = append(lines, fmt.Sprintf("status: %s", status))
	if strings.TrimSpace(output) != "" {
		lines = append(lines, "output:")
		lines = append(lines, output)
	}
	if execErr != nil {
		lines = append(lines, "error:")
		lines = append(lines, execErr.Error())
		if recovery := toolRecoveryHint(execErr); recovery != "" {
			lines = append(lines, "recovery:")
			lines = append(lines, recovery)
		}
	}
	return strings.Join(lines, "\n")
}

func toolExecutionStatus(execErr error) string {
	if errors.Is(execErr, ErrPermissionDenied) {
		return "denied"
	}
	if errors.Is(execErr, ErrMalformedToolCall) {
		return "malformed"
	}
	if execErr != nil {
		return "failed"
	}
	return "completed"
}

func toolRecoveryHint(execErr error) string {
	switch {
	case errors.Is(execErr, ErrPermissionDenied):
		return "Continue without this action, ask for approval again, or choose a safer tool."
	case errors.Is(execErr, ErrMalformedToolCall):
		return "Retry with a supported tool name and all required arguments."
	case execErr != nil:
		return "Adjust the arguments or choose a different tool before retrying."
	default:
		return ""
	}
}

func nextUnproductiveToolLoops(current, totalCalls, successfulCalls int) int {
	if totalCalls == 0 || successfulCalls > 0 {
		return 0
	}
	return current + 1
}

func shouldStopAfterToolLoop(unproductiveLoops int) bool {
	return unproductiveLoops >= 2
}

func validateToolCall(toolName string, args map[string]any, repoPath string) error {
	if strings.TrimSpace(toolName) == "" {
		return fmt.Errorf("%w: missing tool name", ErrMalformedToolCall)
	}
	resolvedRepoPath := strings.TrimSpace(repoPath)
	requireString := func(key string) error {
		value := strings.TrimSpace(stringArg(args, key, ""))
		if value == "" {
			return fmt.Errorf("%w: %s requires %q", ErrMalformedToolCall, toolName, key)
		}
		return nil
	}
	requirePathOrRepo := func(key string) error {
		value := strings.TrimSpace(stringArg(args, key, resolvedRepoPath))
		if value == "" {
			return fmt.Errorf("%w: %s requires %q", ErrMalformedToolCall, toolName, key)
		}
		return nil
	}
	switch toolName {
	case "get_status", "mcp_servers":
		return nil
	case "list_files", "read_file", "analyze_path", "diagnostics", "lsp_symbols":
		return requirePathOrRepo("path")
	case "write_file":
		if err := requireString("path"); err != nil {
			return err
		}
		if _, ok := args["content"]; !ok {
			return fmt.Errorf("%w: %s requires %q", ErrMalformedToolCall, toolName, "content")
		}
		return nil
	case "file_edit":
		if err := requireString("path"); err != nil {
			return err
		}
		if err := requireString("old_text"); err != nil {
			return err
		}
		if _, ok := args["new_text"]; !ok {
			return fmt.Errorf("%w: %s requires %q", ErrMalformedToolCall, toolName, "new_text")
		}
		return nil
	case "apply_patch":
		return requireString("patch")
	case "shell_exec":
		return requireString("command")
	case "index_repo":
		value := strings.TrimSpace(stringArg(args, "repo_path", resolvedRepoPath))
		if value == "" {
			return fmt.Errorf("%w: %s requires %q", ErrMalformedToolCall, toolName, "repo_path")
		}
		return nil
	case "lsp_definition", "lsp_references":
		if err := requirePathOrRepo("path"); err != nil {
			return err
		}
		if intArg(args, "line", 0) < 1 {
			return fmt.Errorf("%w: %s requires line >= 1", ErrMalformedToolCall, toolName)
		}
		if intArg(args, "character", 0) < 1 {
			return fmt.Errorf("%w: %s requires character >= 1", ErrMalformedToolCall, toolName)
		}
		return nil
	case "lsp_workspace_symbols", "lsp_workspace_definitions", "lsp_workspace_references":
		if resolvedRepoPath == "" {
			return fmt.Errorf("%w: %s requires an active repository", ErrMalformedToolCall, toolName)
		}
		return nil
	case "run_subtask":
		return requireString("prompt")
	default:
		if strings.HasPrefix(toolName, "mcp_") {
			return nil
		}
		return fmt.Errorf("%w: unsupported tool %s", ErrMalformedToolCall, toolName)
	}
}

func uniqueNonEmptyPaths(paths []string, fallback string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(paths)+1)
	for _, item := range append(paths, fallback) {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func firstOrEmpty(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func withPreviewPath(path, preview string) string {
	if strings.TrimSpace(preview) == "" {
		return ""
	}
	if strings.Contains(preview, "--- ") || strings.Contains(preview, "+++ ") {
		return preview
	}
	return fmt.Sprintf("--- %s\n+++ %s\n%s", path, path, preview)
}

func formatPatchApprovalSummary(results []applyPatchResult, totalChanges int) string {
	lines := []string{
		fmt.Sprintf("files: %d", len(results)),
		fmt.Sprintf("operations: %d", totalChanges),
	}
	for _, result := range results {
		lines = append(lines, "- "+result.summary)
	}
	return strings.Join(lines, "\n")
}

func patchActionForResults(results []applyPatchResult) string {
	hasAddOrDelete := false
	for _, result := range results {
		if result.mode == "add" || result.mode == "delete" {
			hasAddOrDelete = true
			break
		}
	}
	if hasAddOrDelete {
		return "modify"
	}
	return "write"
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func toolsPreviewForCreate(path, content string) string {
	return fmt.Sprintf("--- %s\n+++ %s\n@@ create @@\n%s", path, path, prefixPreviewLines("+ ", content))
}

func toolsPreviewForDelete(path, content string) string {
	return fmt.Sprintf("--- %s\n+++ %s\n@@ delete @@\n%s", path, path, prefixPreviewLines("- ", content))
}

func prefixPreviewLines(prefix, content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) > 6 {
		lines = append(lines[:6], "...(truncated)")
	}
	for index := range lines {
		lines[index] = prefix + lines[index]
	}
	return strings.Join(lines, "\n")
}

func (a *App) ListFiles(_ context.Context, root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

func (a *App) IndexRepository(ctx context.Context, repoPath string) error {
	if a.Neo4j == nil {
		a.Hub.Broadcast(events.Event{
			Type: "repo.index.status",
			Data: domain.RepoIndexStatus{
				RepoPath: repoPath,
				State:    "failed",
				Message:  "Neo4j unavailable; repository graph indexing disabled in degraded mode",
			},
		})
		return ErrNeo4jUnavailable
	}

	a.Hub.Broadcast(events.Event{
		Type: "repo.index.status",
		Data: domain.RepoIndexStatus{
			RepoPath: repoPath,
			State:    "running",
		},
	})

	files, err := a.ListFiles(ctx, repoPath)
	if err != nil {
		a.Hub.Broadcast(events.Event{
			Type: "repo.index.status",
			Data: domain.RepoIndexStatus{
				RepoPath: repoPath,
				State:    "failed",
				Message:  err.Error(),
			},
		})
		return err
	}

	symbolIndex := make(map[string][]indexedSymbol, len(files))
	importIndex := make(map[string][]string, len(files))

	for _, file := range files {
		meta := fileMetadata(file)
		_ = a.Neo4j.UpsertRepoFile(ctx, repoPath, file, meta)
		content, readErr := os.ReadFile(file)
		if readErr == nil {
			text := string(content)
			imports := extractImports(file, text)
			importIndex[file] = imports
			symbols := a.loadIndexSymbols(ctx, file, repoPath, text)
			symbolIndex[file] = symbols
			_ = a.Neo4j.UpsertFileImports(ctx, file, imports)
			_ = a.Neo4j.UpsertFileSymbols(ctx, file, symbolMetadata(symbols))
		}
	}

	for _, file := range files {
		for _, refFile := range resolveImportReferences(repoPath, file, importIndex[file], files) {
			_ = a.Neo4j.LinkFileReference(ctx, file, refFile, "import")
		}
	}
	a.indexLSPReferenceEdges(ctx, repoPath, symbolIndex)

	a.Hub.Broadcast(events.Event{
		Type: "repo.index.status",
		Data: domain.RepoIndexStatus{
			RepoPath:     repoPath,
			State:        "complete",
			IndexedFiles: len(files),
		},
	})
	return nil
}

func permissionRuleKey(sessionID, toolName, action, path string) string {
	return sessionID + "::" + toolName + "::" + action + "::" + path
}

func resolveImportReferences(repoPath, sourceFile string, imports []string, files []string) []string {
	fileSet := make(map[string]struct{}, len(files))
	for _, item := range files {
		fileSet[filepath.Clean(item)] = struct{}{}
	}
	seen := make(map[string]struct{})
	refs := make([]string, 0)
	sourceDir := filepath.Dir(sourceFile)
	for _, item := range imports {
		candidates := importCandidates(repoPath, sourceDir, item)
		for _, candidate := range candidates {
			cleaned := filepath.Clean(candidate)
			if _, ok := fileSet[cleaned]; !ok {
				continue
			}
			if _, ok := seen[cleaned]; ok {
				continue
			}
			seen[cleaned] = struct{}{}
			refs = append(refs, cleaned)
		}
	}
	return refs
}

func importCandidates(repoPath, sourceDir, value string) []string {
	value = strings.TrimSpace(strings.Trim(value, `"'`))
	if value == "" {
		return nil
	}
	candidates := make([]string, 0, 8)
	add := func(path string) {
		if strings.TrimSpace(path) != "" {
			candidates = append(candidates, path)
		}
	}
	if strings.HasPrefix(value, ".") {
		base := filepath.Join(sourceDir, value)
		add(base)
		for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".go", ".py"} {
			add(base + ext)
		}
		for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
			add(filepath.Join(base, "index"+ext))
		}
		return candidates
	}
	if strings.HasPrefix(value, "/") {
		base := filepath.Join(repoPath, strings.TrimPrefix(value, "/"))
		add(base)
		for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".go", ".py"} {
			add(base + ext)
		}
	}
	return candidates
}

func (a *App) loadIndexSymbols(ctx context.Context, path string, repoPath string, content string) []indexedSymbol {
	lspSymbols, err := a.DocumentSymbols(ctx, path, repoPath)
	if err == nil && len(lspSymbols) > 0 {
		items := make([]indexedSymbol, 0, len(lspSymbols))
		for _, item := range lspSymbols {
			items = append(items, indexedSymbol{
				Name:      item.Name,
				Kind:      item.Kind,
				Line:      item.Line,
				Character: maxInt(item.Character, 1),
			})
		}
		return items
	}
	extracted := extractSymbols(path, content)
	items := make([]indexedSymbol, 0, len(extracted))
	for _, item := range extracted {
		items = append(items, indexedSymbol{
			Name:      item.Name,
			Kind:      item.Kind,
			Line:      item.Line,
			Character: 1,
		})
	}
	return items
}

func symbolMetadata(items []indexedSymbol) []store.SymbolMetadata {
	result := make([]store.SymbolMetadata, 0, len(items))
	for _, item := range items {
		result = append(result, store.SymbolMetadata{
			Name: item.Name,
			Kind: item.Kind,
			Line: item.Line,
		})
	}
	return result
}

func (a *App) indexLSPReferenceEdges(ctx context.Context, repoPath string, symbolIndex map[string][]indexedSymbol) {
	if a.Neo4j == nil {
		return
	}
	for file, symbols := range symbolIndex {
		limit := minInt(len(symbols), 12)
		for _, symbol := range symbols[:limit] {
			definitions, err := a.Definitions(ctx, file, repoPath, symbol.Line, symbol.Character)
			if err == nil {
				a.linkSymbolLocations(ctx, file, symbol, definitions, "definition", symbolIndex)
			}
			references, err := a.References(ctx, file, repoPath, symbol.Line, symbol.Character)
			if err == nil {
				a.linkSymbolLocations(ctx, file, symbol, references, "reference", symbolIndex)
			}
		}
	}
}

func (a *App) linkSymbolLocations(ctx context.Context, sourceFile string, source indexedSymbol, locations []lsp.Location, kind string, symbolIndex map[string][]indexedSymbol) {
	for _, location := range locations {
		targetPath := filepath.Clean(location.Path)
		if targetPath == "" {
			continue
		}
		if sourceFile == targetPath && absInt(source.Line-location.Line) <= 1 {
			continue
		}
		target := nearestIndexedSymbol(symbolIndex[targetPath], location.Line)
		targetName := ""
		targetKind := ""
		targetLine := location.Line
		if target.Line > 0 {
			targetName = target.Name
			targetKind = target.Kind
			targetLine = target.Line
		}
		_ = a.Neo4j.LinkFileReference(ctx, sourceFile, targetPath, "lsp_"+kind)
		_ = a.Neo4j.LinkSymbolReference(ctx, sourceFile, source.Name, source.Kind, source.Line, targetPath, targetName, targetKind, targetLine, "lsp_"+kind)
	}
}

func nearestIndexedSymbol(items []indexedSymbol, line int) indexedSymbol {
	best := indexedSymbol{}
	bestDistance := int(^uint(0) >> 1)
	for _, item := range items {
		distance := absInt(item.Line - line)
		if distance < bestDistance {
			best = item
			bestDistance = distance
		}
	}
	if bestDistance > 8 {
		return indexedSymbol{Line: line, Character: 1}
	}
	return best
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (a *App) Status(ctx context.Context) Status {
	messages := a.startupStatusMessages()
	ollamaReachable := true
	if _, err := a.Ollama.ListModels(ctx); err != nil {
		ollamaReachable = false
		messages = append(messages, fmt.Sprintf("Ollama unavailable: %v", err))
	}

	mode := "healthy"
	if a.Mongo == nil || a.Neo4j == nil || !ollamaReachable {
		mode = "degraded"
	}

	return Status{
		ServiceHealthy:  true,
		MongoAvailable:  a.Mongo != nil,
		Neo4jAvailable:  a.Neo4j != nil,
		OllamaReachable: ollamaReachable,
		Mode:            mode,
		Messages:        ensureStringSlice(messages),
	}
}

func (a *App) recordStartupMessage(message string) {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	a.startupMessages = append(a.startupMessages, message)
}

func (a *App) startupStatusMessages() []string {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	items := make([]string, len(a.startupMessages))
	copy(items, a.startupMessages)
	return ensureStringSlice(items)
}

func ensureStringSlice(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func (a *App) updatePermissionStatus(ctx context.Context, requestID, status string) error {
	if a.Mongo != nil {
		if err := a.Mongo.UpdatePermissionStatus(ctx, requestID, status); err != nil {
			return err
		}
	}
	a.memMu.Lock()
	defer a.memMu.Unlock()
	request, ok := a.memPermissions[requestID]
	if ok {
		request.Status = status
		a.memPermissions[requestID] = request
	}
	return nil
}

func (a *App) listPendingPermissions(ctx context.Context, requestID string) ([]domain.PermissionRequest, error) {
	if a.Mongo != nil {
		return a.Mongo.ListPendingPermissions(ctx, requestID)
	}
	a.memMu.RLock()
	defer a.memMu.RUnlock()
	request, ok := a.memPermissions[requestID]
	if !ok {
		return nil, nil
	}
	return []domain.PermissionRequest{request}, nil
}
