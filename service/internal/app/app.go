package app

import (
	"context"
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
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/ollama"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/store"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/tools"
)

var (
	ErrMongoUnavailable = errors.New("mongo dependency unavailable")
	ErrNeo4jUnavailable = errors.New("neo4j dependency unavailable")
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
		return session, a.Mongo.SaveSession(ctx, session)
	}
	a.memMu.Lock()
	a.memSessions[session.ID] = session
	a.memMu.Unlock()
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
	a.memMessages[message.SessionID] = append(a.memMessages[message.SessionID], message)
	return nil
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

func (a *App) RunAgent(ctx context.Context, sessionID, model, prompt string) (domain.Message, error) {
	now := time.Now().UTC()
	userMessage := domain.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      domain.RoleUser,
		Parts: []domain.ContentPart{
			{Type: "text", Text: prompt},
			{Type: "finish", Reason: "stop", Time: time.Now().Unix()},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.SaveMessage(ctx, userMessage); err != nil {
		return domain.Message{}, err
	}

	history, err := a.ListMessages(ctx, sessionID)
	if err != nil {
		return domain.Message{}, err
	}

	assistantMessage := domain.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      domain.RoleAssistant,
		Model:     model,
		Parts:     []domain.ContentPart{{Type: "text", Text: ""}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.SaveMessage(ctx, assistantMessage); err != nil {
		return domain.Message{}, err
	}

	if a.Neo4j != nil {
		_ = a.Neo4j.UpsertMemoryNode(ctx, sessionID, "session", prompt)
	}

	ollamaMessages := make([]ollama.Message, 0, len(history))
	for _, item := range history {
		text := extractText(item.Parts)
		if strings.TrimSpace(text) == "" {
			continue
		}
		ollamaMessages = append(ollamaMessages, ollama.Message{
			Role:    string(item.Role),
			Content: text,
		})
	}

	var fullText strings.Builder
	err = a.Ollama.StreamChat(ctx, model, ollamaMessages, func(delta string) error {
		fullText.WriteString(delta)
		a.Hub.Broadcast(events.Event{
			Type: "message.delta",
			Data: map[string]string{
				"sessionId": sessionID,
				"messageId": assistantMessage.ID,
				"delta":     delta,
			},
		})
		return nil
	})
	if err != nil {
		assistantMessage.Parts = []domain.ContentPart{
			{Type: "text", Text: fullText.String()},
			{Type: "finish", Reason: "cancelled", Time: time.Now().Unix()},
		}
		_ = a.SaveMessage(context.Background(), assistantMessage)
		return assistantMessage, err
	}

	assistantMessage.Parts = []domain.ContentPart{
		{Type: "text", Text: fullText.String()},
		{Type: "finish", Reason: "stop", Time: time.Now().Unix()},
	}
	if err := a.SaveMessage(ctx, assistantMessage); err != nil {
		return domain.Message{}, err
	}

	a.Hub.Broadcast(events.Event{
		Type: "message.completed",
		Data: assistantMessage,
	})

	return assistantMessage, nil
}

func (a *App) RequestPermission(ctx context.Context, sessionID, toolName, action, path, description, params string) (string, error) {
	ruleKey := permissionRuleKey(sessionID, toolName, action, path)
	a.allowMu.RLock()
	_, allowed := a.allowRules[ruleKey]
	a.allowMu.RUnlock()
	if allowed {
		return "allow_session", nil
	}

	request := domain.PermissionRequest{
		ID:          uuid.NewString(),
		SessionID:   sessionID,
		ToolName:    toolName,
		Action:      action,
		Path:        path,
		Description: description,
		Params:      params,
		Status:      "pending",
		CreatedAt:   time.Now().UTC(),
	}
	if err := a.Mongo.SavePermission(ctx, request); err != nil {
		if a.Mongo != nil {
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

	select {
	case decision := <-decisionCh:
		return decision, nil
	case <-time.After(time.Duration(a.Config.PermissionTTLMS) * time.Millisecond):
		_ = a.updatePermissionStatus(context.Background(), request.ID, "timeout")
		return "", errors.New("permission request timed out")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (a *App) ResolvePermission(ctx context.Context, requestID, decision string) error {
	a.pendingMu.Lock()
	decisionCh, ok := a.pendingReq[requestID]
	a.pendingMu.Unlock()

	if err := a.updatePermissionStatus(ctx, requestID, decision); err != nil {
		return err
	}
	if decision == "allow_session" {
		requests, err := a.listPendingPermissions(ctx, requestID)
		if err == nil && len(requests) > 0 {
			ruleKey := permissionRuleKey(requests[0].SessionID, requests[0].ToolName, requests[0].Action, requests[0].Path)
			a.allowMu.Lock()
			a.allowRules[ruleKey] = struct{}{}
			a.allowMu.Unlock()
		}
	}
	if ok {
		decisionCh <- decision
	}
	return nil
}

func (a *App) RunShell(ctx context.Context, sessionID, command, cwd string) (string, error) {
	decision, err := a.RequestPermission(ctx, sessionID, "shell_exec", "execute", cwd, "Execute shell command", command)
	if err != nil {
		return "", err
	}
	if decision == "deny" {
		return "", errors.New("permission denied")
	}

	a.Hub.Broadcast(events.Event{
		Type: "tool.started",
		Data: map[string]string{
			"sessionId":  sessionID,
			"toolCallId": uuid.NewString(),
			"toolName":   "shell_exec",
		},
	})

	output, execErr := tools.RunShell(ctx, cwd, command)
	run := domain.ToolExecution{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		ToolName:  "shell_exec",
		Input:     command,
		Output:    output,
		IsError:   execErr != nil,
		CreatedAt: time.Now().UTC(),
	}
	if a.Mongo != nil {
		_ = a.Mongo.SaveToolRun(context.Background(), run)
	}
	a.Hub.Broadcast(events.Event{
		Type: "tool.completed",
		Data: map[string]any{
			"sessionId":  sessionID,
			"toolCallId": run.ID,
			"output":     output,
			"isError":    execErr != nil,
		},
	})

	return output, execErr
}

func (a *App) ReadFile(_ context.Context, path string) (string, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func (a *App) WriteFile(ctx context.Context, sessionID, path, content string) error {
	decision, err := a.RequestPermission(ctx, sessionID, "file_write", "write", path, "Write file", path)
	if err != nil {
		return err
	}
	if decision == "deny" {
		return errors.New("permission denied")
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	if a.Neo4j != nil {
		return a.Neo4j.LinkSessionToFile(ctx, sessionID, path)
	}
	return nil
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

	for _, file := range files {
		_ = a.Neo4j.UpsertRepoFile(ctx, repoPath, file)
	}

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

func extractText(parts []domain.ContentPart) string {
	var out strings.Builder
	for _, part := range parts {
		if part.Type == "text" || part.Type == "reasoning" {
			out.WriteString(part.Text)
		}
	}
	return out.String()
}

func permissionRuleKey(sessionID, toolName, action, path string) string {
	return sessionID + "::" + toolName + "::" + action + "::" + path
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
