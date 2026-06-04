package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

type Diagnostic struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	Source    string `json:"source,omitempty"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

type Location struct {
	Path         string `json:"path"`
	Line         int    `json:"line"`
	Character    int    `json:"character"`
	EndLine      int    `json:"endLine,omitempty"`
	EndCharacter int    `json:"endCharacter,omitempty"`
	Preview      string `json:"preview,omitempty"`
}

type DocumentSymbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Client struct{}

type WorkspaceSymbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

type session struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	reader   *bufio.Reader
	stderr   *bytes.Buffer
	path     string
	language string
	fileURI  string
}

func LoadConfig(configPath string, repoPath string) (map[string]ServerConfig, error) {
	resolved := resolveConfigPath(configPath, repoPath)
	payload, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, err
	}

	var direct map[string]ServerConfig
	if err := json.Unmarshal(payload, &direct); err == nil {
		return mergeDefaults(direct), nil
	}

	var wrapped struct {
		LSP map[string]ServerConfig `json:"lsp"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return nil, err
	}
	return mergeDefaults(wrapped.LSP), nil
}

func defaultConfig() map[string]ServerConfig {
	return map[string]ServerConfig{
		"go":         {Command: "gopls"},
		"typescript": {Command: "typescript-language-server", Args: []string{"--stdio"}},
		"javascript": {Command: "typescript-language-server", Args: []string{"--stdio"}},
		"python":     {Command: "pylsp"},
	}
}

func mergeDefaults(items map[string]ServerConfig) map[string]ServerConfig {
	defaults := defaultConfig()
	for key, value := range items {
		defaults[key] = value
	}
	return defaults
}

func resolveConfigPath(configPath string, repoPath string) string {
	if filepath.IsAbs(configPath) {
		return configPath
	}
	if repoPath != "" {
		return filepath.Join(repoPath, configPath)
	}
	return configPath
}

func DetectLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".py":
		return "python"
	default:
		return ""
	}
}

func (c *Client) Diagnostics(ctx context.Context, path string, cfg ServerConfig) ([]Diagnostic, error) {
	sess, err := startSession(ctx, path, cfg)
	if err != nil {
		return nil, err
	}
	defer sess.close()

	diagnosticsCh := make(chan []Diagnostic, 1)
	errCh := make(chan error, 1)
	var once sync.Once
	go func() {
		for {
			msg, readErr := readMessage(sess.reader)
			if readErr != nil {
				if readErr == io.EOF {
					return
				}
				once.Do(func() { errCh <- wrapStderr(readErr, sess.stderr) })
				return
			}

			var envelope rpcEnvelope
			if err := json.Unmarshal(msg, &envelope); err != nil {
				once.Do(func() { errCh <- err })
				return
			}
			if envelope.Method != "textDocument/publishDiagnostics" {
				continue
			}
			var params struct {
				URI         string `json:"uri"`
				Diagnostics []struct {
					Source   string `json:"source"`
					Severity int    `json:"severity"`
					Message  string `json:"message"`
					Range    struct {
						Start struct {
							Line      int `json:"line"`
							Character int `json:"character"`
						} `json:"start"`
					} `json:"range"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal(envelope.Params, &params); err != nil {
				once.Do(func() { errCh <- err })
				return
			}
			items := make([]Diagnostic, 0, len(params.Diagnostics))
			for _, item := range params.Diagnostics {
				items = append(items, Diagnostic{
					Path:      path,
					Language:  sess.language,
					Source:    item.Source,
					Severity:  severityName(item.Severity),
					Message:   item.Message,
					Line:      item.Range.Start.Line + 1,
					Character: item.Range.Start.Character + 1,
				})
			}
			once.Do(func() { diagnosticsCh <- items })
			return
		}
	}()
	select {
	case diagnostics := <-diagnosticsCh:
		_ = sess.shutdown()
		return diagnostics, nil
	case readErr := <-errCh:
		return nil, readErr
	case <-time.After(2 * time.Second):
		_ = sess.shutdown()
		return []Diagnostic{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Client) DocumentSymbols(ctx context.Context, path string, cfg ServerConfig) ([]DocumentSymbol, error) {
	sess, err := startSession(ctx, path, cfg)
	if err != nil {
		return nil, err
	}
	defer sess.close()

	result, err := sess.request(ctx, 2, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": sess.fileURI},
	})
	language := DetectLanguage(path)
	_ = language
	if err != nil {
		return nil, err
	}
	var payload []struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location struct {
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
		} `json:"location"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, err
	}
	items := make([]DocumentSymbol, 0, len(payload))
	for _, item := range payload {
		line := item.Range.Start.Line + 1
		character := item.Range.Start.Character + 1
		if line == 0 && item.Location.Range.Start.Line >= 0 {
			line = item.Location.Range.Start.Line + 1
			character = item.Location.Range.Start.Character + 1
		}
		items = append(items, DocumentSymbol{
			Name:      item.Name,
			Kind:      symbolKindName(item.Kind),
			Path:      path,
			Line:      line,
			Character: character,
		})
	}
	return items, nil
}

func (c *Client) Definition(ctx context.Context, path string, cfg ServerConfig, line int, character int) ([]Location, error) {
	return c.locationsRequest(ctx, path, cfg, line, character, "textDocument/definition")
}

func (c *Client) References(ctx context.Context, path string, cfg ServerConfig, line int, character int) ([]Location, error) {
	return c.locationsRequest(ctx, path, cfg, line, character, "textDocument/references")
}

func (c *Client) locationsRequest(ctx context.Context, path string, cfg ServerConfig, line int, character int, method string) ([]Location, error) {
	sess, err := startSession(ctx, path, cfg)
	if err != nil {
		return nil, err
	}
	defer sess.close()
	params := map[string]any{
		"textDocument": map[string]any{"uri": sess.fileURI},
		"position": map[string]any{
			"line":      maxInt(line-1, 0),
			"character": maxInt(character-1, 0),
		},
	}
	if method == "textDocument/references" {
		params["context"] = map[string]any{"includeDeclaration": true}
	}
	result, err := sess.request(ctx, 2, method, params)
	if err != nil {
		return nil, err
	}
	type locationPayload struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
	}
	var payload []locationPayload
	if len(result) > 0 && result[0] == '{' {
		var single locationPayload
		if err := json.Unmarshal(result, &single); err != nil {
			return nil, err
		}
		payload = append(payload, single)
	} else if err := json.Unmarshal(result, &payload); err != nil {
		return nil, err
	}
	locations := make([]Location, 0, len(payload))
	for _, item := range payload {
		resolvedPath := fromFileURI(item.URI)
		locations = append(locations, Location{
			Path:         resolvedPath,
			Line:         item.Range.Start.Line + 1,
			Character:    item.Range.Start.Character + 1,
			EndLine:      item.Range.End.Line + 1,
			EndCharacter: item.Range.End.Character + 1,
			Preview:      previewAroundLine(resolvedPath, item.Range.Start.Line+1),
		})
	}
	return locations, nil
}

func startSession(ctx context.Context, path string, cfg ServerConfig) (*session, error) {
	language := DetectLanguage(path)
	if language == "" {
		return nil, fmt.Errorf("no LSP language mapping for %s", path)
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("no LSP command configured for %s", language)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = filepath.Dir(path)
	cmd.Env = os.Environ()
	for key, value := range cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stderrBytes := &bytes.Buffer{}
	go func() {
		_, _ = io.Copy(stderrBytes, stderr)
	}()
	sess := &session{
		cmd:      cmd,
		stdin:    stdin,
		reader:   bufio.NewReader(stdout),
		stderr:   stderrBytes,
		path:     path,
		language: language,
		fileURI:  toFileURI(path),
	}
	rootURI := toFileURI(filepath.Dir(path))
	if err := writeRequest(stdin, 1, "initialize", map[string]any{
		"processId": nil,
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{},
			},
		},
		"clientInfo": map[string]any{
			"name":    "personal-assistant",
			"version": "0.1.0",
		},
	}); err != nil {
		sess.close()
		return nil, err
	}
	if _, err := readMessage(sess.reader); err != nil {
		sess.close()
		return nil, wrapStderr(err, sess.stderr)
	}
	if err := writeNotification(stdin, "initialized", map[string]any{}); err != nil {
		sess.close()
		return nil, err
	}
	if err := writeNotification(stdin, "textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        sess.fileURI,
			"languageId": language,
			"version":    1,
			"text":       string(content),
		},
	}); err != nil {
		sess.close()
		return nil, err
	}
	return sess, nil
}

func (s *session) request(ctx context.Context, id int64, method string, params map[string]any) (json.RawMessage, error) {
	if err := writeRequest(s.stdin, id, method, params); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		msg, err := readMessage(s.reader)
		if err != nil {
			return nil, wrapStderr(err, s.stderr)
		}
		var envelope rpcEnvelope
		if err := json.Unmarshal(msg, &envelope); err != nil {
			return nil, err
		}
		if envelope.ID != id {
			continue
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, envelope.Error.Message)
		}
		return envelope.Result, nil
	}
}

func (s *session) shutdown() error {
	_ = writeRequest(s.stdin, 999, "shutdown", map[string]any{})
	_ = writeNotification(s.stdin, "exit", map[string]any{})
	return nil
}

func (s *session) close() {
	_ = s.shutdown()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	if s.cmd != nil {
		_ = s.cmd.Wait()
	}
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			fmt.Sscanf(line, "Content-Length: %d", &contentLength)
		}
	}
	if contentLength <= 0 {
		return nil, io.EOF
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeRequest(writer io.Writer, id int64, method string, params map[string]any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(payload), payload)
	return err
}

func writeNotification(writer io.Writer, method string, params map[string]any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(payload), payload)
	return err
}

func toFileURI(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return "file://" + slashed
}

func fromFileURI(uri string) string {
	value := strings.TrimPrefix(uri, "file://")
	if len(value) >= 3 && value[0] == '/' && value[2] == ':' {
		value = value[1:]
	}
	return filepath.FromSlash(value)
}

func severityName(value int) string {
	switch value {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "information"
	default:
		return "hint"
	}
}

func symbolKindName(value int) string {
	switch value {
	case 5:
		return "class"
	case 6:
		return "method"
	case 12:
		return "function"
	case 13:
		return "variable"
	case 23:
		return "struct"
	default:
		return fmt.Sprintf("kind_%d", value)
	}
}

func wrapStderr(err error, stderr *bytes.Buffer) error {
	if stderr != nil && stderr.Len() > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return err
}

func previewAroundLine(path string, line int) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	start := maxInt(line-2, 1)
	end := line + 2
	if end > len(lines) {
		end = len(lines)
	}
	selected := make([]string, 0, end-start+1)
	for idx := start; idx <= end; idx++ {
		selected = append(selected, fmt.Sprintf("%d: %s", idx, lines[idx-1]))
	}
	return strings.Join(selected, "\n")
}

func maxInt(value int, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
