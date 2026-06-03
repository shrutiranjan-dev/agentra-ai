package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/app"
)

type Server struct {
	app      *app.App
	upgrader websocket.Upgrader
}

func New(app *app.App) *Server {
	return &Server{
		app: app,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/ws", s.withAuth(s.handleWS))
	mux.HandleFunc("/api/models", s.withAuth(s.handleModels))
	mux.HandleFunc("/api/sessions", s.withAuth(s.handleSessions))
	mux.HandleFunc("/api/messages", s.withAuth(s.handleMessages))
	mux.HandleFunc("/api/agent/run", s.withAuth(s.handleRunAgent))
	mux.HandleFunc("/api/permissions/decide", s.withAuth(s.handlePermissionDecision))
	mux.HandleFunc("/api/tools/shell", s.withAuth(s.handleShell))
	mux.HandleFunc("/api/files/read", s.withAuth(s.handleReadFile))
	mux.HandleFunc("/api/files/write", s.withAuth(s.handleWriteFile))
	mux.HandleFunc("/api/files/list", s.withAuth(s.handleListFiles))
	mux.HandleFunc("/api/repo/index", s.withAuth(s.handleRepoIndex))
	return s.withCORS(mux)
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next(w, r)
			return
		}
		token := r.Header.Get("Authorization")
		token = strings.TrimPrefix(token, "Bearer ")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != s.app.Config.AuthToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := s.app.Status(r.Context())
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.app.Hub.Add(conn)
	defer func() {
		s.app.Hub.Remove(conn)
		_ = conn.Close()
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.app.ListModels(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.app.ListSessions(r.Context())
		if err != nil {
			writeAPIError(w, http.StatusServiceUnavailable, "sessions_unavailable", err.Error(), "mongo")
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var payload struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		session, err := s.app.CreateSession(r.Context(), payload.Title)
		if err != nil {
			writeAPIError(w, http.StatusServiceUnavailable, "sessions_unavailable", err.Error(), "mongo")
			return
		}
		writeJSON(w, http.StatusCreated, session)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := r.URL.Query().Get("sessionId")
	items, err := s.app.ListMessages(r.Context(), sessionID)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "messages_unavailable", err.Error(), "mongo")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleRunAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		SessionID string `json:"sessionId"`
		Prompt    string `json:"prompt"`
		Model     string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	message, err := s.app.RunAgent(r.Context(), payload.SessionID, payload.Model, payload.Prompt)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "agent_run_failed", err.Error(), "ollama")
		return
	}
	writeJSON(w, http.StatusOK, message)
}

func (s *Server) handlePermissionDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		RequestID string `json:"requestId"`
		Decision  string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.app.ResolvePermission(r.Context(), payload.RequestID, payload.Decision); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "permission_update_failed", err.Error(), "mongo")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		SessionID string `json:"sessionId"`
		Command   string `json:"command"`
		CWD       string `json:"cwd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	output, err := s.app.RunShell(r.Context(), payload.SessionID, payload.Command, payload.CWD)
	resp := map[string]any{"output": output}
	if err != nil {
		dependency := ""
		if err.Error() == "permission denied" {
			dependency = "service"
		}
		writeAPIError(w, http.StatusBadRequest, "shell_run_failed", err.Error(), dependency)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Query().Get("path")
	content, err := s.app.ReadFile(r.Context(), path)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "file_read_failed", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
		Content   string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.app.WriteFile(r.Context(), payload.SessionID, payload.Path, payload.Content); err != nil {
		dependency := ""
		if err == app.ErrNeo4jUnavailable {
			dependency = "neo4j"
		}
		writeAPIError(w, http.StatusBadRequest, "file_write_failed", err.Error(), dependency)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root := r.URL.Query().Get("path")
	items, err := s.app.ListFiles(r.Context(), root)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "file_list_failed", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleRepoIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		RepoPath string `json:"repoPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	go func(repoPath string) {
		_ = s.app.IndexRepository(context.Background(), repoPath)
	}(payload.RepoPath)

	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

func writeAPIError(w http.ResponseWriter, status int, code, message, dependency string) {
	payload := map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	if dependency != "" {
		payload["error"].(map[string]any)["dependency"] = dependency
	}
	writeJSON(w, status, payload)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
