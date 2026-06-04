package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ServerConfig struct {
	Transport  string            `json:"transport"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
	URL        string            `json:"url"`
	MessageURL string            `json:"messageUrl"`
	TimeoutMs  int               `json:"timeoutMs"`
	Headers    map[string]string `json:"headers"`
}

type ToolInfo struct {
	Server      string         `json:"server"`
	Transport   string         `json:"transport,omitempty"`
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type ServerStatus struct {
	Server    string   `json:"server"`
	Transport string   `json:"transport"`
	Reachable bool     `json:"reachable"`
	ToolCount int      `json:"toolCount"`
	ToolNames []string `json:"toolNames,omitempty"`
	LatencyMs int64    `json:"latencyMs,omitempty"`
	Endpoint  string   `json:"endpoint,omitempty"`
	Detail    string   `json:"detail,omitempty"`
	Error     string   `json:"error,omitempty"`
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

func LoadConfig(configPath string, repoPath string) (map[string]ServerConfig, error) {
	resolved := resolveConfigPath(configPath, repoPath)
	payload, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]ServerConfig{}, nil
		}
		return nil, err
	}

	var direct map[string]ServerConfig
	if err := json.Unmarshal(payload, &direct); err == nil {
		return direct, nil
	}

	var wrapped struct {
		Servers map[string]ServerConfig `json:"servers"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return nil, err
	}
	if wrapped.Servers == nil {
		return map[string]ServerConfig{}, nil
	}
	return wrapped.Servers, nil
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

func (c *Client) ListTools(ctx context.Context, serverName string, cfg ServerConfig) ([]ToolInfo, error) {
	result, err := c.call(ctx, cfg, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tools []struct {
			Name        string         `json:"name"`
			Title       string         `json:"title"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, err
	}
	items := make([]ToolInfo, 0, len(payload.Tools))
	for _, item := range payload.Tools {
		items = append(items, ToolInfo{
			Server:      serverName,
			Transport:   transportName(cfg),
			Name:        item.Name,
			Title:       item.Title,
			Description: item.Description,
			InputSchema: item.InputSchema,
		})
	}
	return items, nil
}

func (c *Client) CallTool(ctx context.Context, cfg ServerConfig, toolName string, arguments map[string]any) (string, error) {
	result, err := c.call(ctx, cfg, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": arguments,
	})
	if err != nil {
		return "", err
	}
	var payload struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent map[string]any `json:"structuredContent"`
		IsError           bool           `json:"isError"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", err
	}
	parts := make([]string, 0, len(payload.Content)+1)
	for _, item := range payload.Content {
		if item.Type == "text" {
			parts = append(parts, item.Text)
		}
	}
	if len(parts) == 0 && len(payload.StructuredContent) > 0 {
		bytes, _ := json.MarshalIndent(payload.StructuredContent, "", "  ")
		parts = append(parts, string(bytes))
	}
	if payload.IsError {
		return strings.Join(parts, "\n"), fmt.Errorf(strings.Join(parts, "\n"))
	}
	return strings.Join(parts, "\n"), nil
}

func (c *Client) Probe(ctx context.Context, serverName string, cfg ServerConfig) ServerStatus {
	started := time.Now()
	items, err := c.ListTools(ctx, serverName, cfg)
	if err != nil {
		return ServerStatus{
			Server:    serverName,
			Transport: transportName(cfg),
			Reachable: false,
			LatencyMs: time.Since(started).Milliseconds(),
			Endpoint:  statusEndpoint(cfg),
			Error:     err.Error(),
		}
	}
	toolNames := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		toolNames = append(toolNames, item.Name)
		if len(toolNames) >= 8 {
			break
		}
	}
	return ServerStatus{
		Server:    serverName,
		Transport: transportName(cfg),
		Reachable: true,
		ToolCount: len(items),
		ToolNames: toolNames,
		LatencyMs: time.Since(started).Milliseconds(),
		Endpoint:  statusEndpoint(cfg),
		Detail:    statusDetail(cfg),
	}
}

func (c *Client) call(ctx context.Context, cfg ServerConfig, method string, params map[string]any) (json.RawMessage, error) {
	switch transportName(cfg) {
	case "http":
		return c.callHTTP(ctx, cfg, method, params)
	case "sse":
		return c.callSSE(ctx, cfg, method, params)
	default:
		return c.callStdio(ctx, cfg, method, params)
	}
}

func (c *Client) callStdio(ctx context.Context, cfg ServerConfig, method string, params map[string]any) (json.RawMessage, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("mcp server command is empty")
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
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
	if _, err := cmd.StderrPipe(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	reader := bufio.NewReader(stdout)
	if err := writeMessage(stdin, 1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "personal-assistant",
			"version": "0.1.0",
		},
	}); err != nil {
		return nil, err
	}
	if _, err := readEnvelope(reader); err != nil {
		return nil, err
	}
	if err := writeNotification(stdin, "notifications/initialized", map[string]any{}); err != nil {
		return nil, err
	}
	if err := writeMessage(stdin, 2, method, params); err != nil {
		return nil, err
	}
	response, err := readEnvelope(reader)
	if err != nil {
		return nil, err
	}
	return response.Result, nil
}

func (c *Client) callHTTP(ctx context.Context, cfg ServerConfig, method string, params map[string]any) (json.RawMessage, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("mcp http url is empty")
	}
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range cfg.Headers {
		request.Header.Set(key, value)
	}
	response, err := httpClient(cfg).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("mcp http %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var envelope rpcEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return envelope.Result, nil
}

func (c *Client) callSSE(ctx context.Context, cfg ServerConfig, method string, params map[string]any) (json.RawMessage, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("mcp sse url is empty")
	}
	streamCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(streamCtx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/event-stream")
	for key, value := range cfg.Headers {
		request.Header.Set(key, value)
	}
	response, err := httpClient(cfg).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("mcp sse %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	reader := bufio.NewReader(response.Body)
	messageURL, err := discoverSSEMessageURL(streamCtx, cfg, reader)
	if err != nil {
		return nil, err
	}
	if err := c.postSSERequest(streamCtx, cfg, messageURL, 1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "personal-assistant",
			"version": "0.1.0",
		},
	}); err != nil {
		return nil, err
	}
	if _, err := readSSEEnvelope(streamCtx, reader, 1); err != nil {
		return nil, err
	}
	if err := c.postSSENotification(streamCtx, cfg, messageURL, "notifications/initialized", map[string]any{}); err != nil {
		return nil, err
	}
	if err := c.postSSERequest(streamCtx, cfg, messageURL, 2, method, params); err != nil {
		return nil, err
	}
	envelope, err := readSSEEnvelope(streamCtx, reader, 2)
	if err != nil {
		return nil, err
	}
	return envelope.Result, nil
}

func transportName(cfg ServerConfig) string {
	value := strings.ToLower(strings.TrimSpace(cfg.Transport))
	if value == "" {
		return "stdio"
	}
	return value
}

func statusEndpoint(cfg ServerConfig) string {
	switch transportName(cfg) {
	case "http", "sse":
		return strings.TrimSpace(cfg.URL)
	default:
		if strings.TrimSpace(cfg.Command) == "" {
			return ""
		}
		return strings.TrimSpace(cfg.Command)
	}
}

func statusDetail(cfg ServerConfig) string {
	switch transportName(cfg) {
	case "http", "sse":
		extras := make([]string, 0, 3)
		if len(cfg.Headers) > 0 {
			extras = append(extras, fmt.Sprintf("%d header(s)", len(cfg.Headers)))
		}
		if cfg.TimeoutMs > 0 {
			extras = append(extras, fmt.Sprintf("timeout %dms", cfg.TimeoutMs))
		}
		if transportName(cfg) == "sse" && strings.TrimSpace(cfg.MessageURL) != "" {
			extras = append(extras, "explicit messageUrl")
		}
		if transportName(cfg) == "sse" {
			if len(extras) > 0 {
				return "remote SSE stream · " + strings.Join(extras, " · ")
			}
			return "remote SSE stream"
		}
		if len(extras) > 0 {
			return "remote transport · " + strings.Join(extras, " · ")
		}
		return "remote transport"
	default:
		if len(cfg.Args) > 0 {
			return strings.Join(cfg.Args, " ")
		}
		return "local stdio process"
	}
}

func readEnvelope(reader *bufio.Reader) (rpcEnvelope, error) {
	payload, err := readMessage(reader)
	if err != nil {
		return rpcEnvelope{}, err
	}
	var envelope rpcEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return rpcEnvelope{}, err
	}
	if envelope.Error != nil {
		return rpcEnvelope{}, fmt.Errorf("mcp error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return envelope, nil
}

type sseEvent struct {
	Name string
	Data string
}

func discoverSSEMessageURL(ctx context.Context, cfg ServerConfig, reader *bufio.Reader) (string, error) {
	if strings.TrimSpace(cfg.MessageURL) != "" {
		return resolveSSEURL(cfg.URL, cfg.MessageURL), nil
	}
	fallback := strings.TrimSpace(cfg.URL)
	for {
		event, err := readSSEEvent(ctx, reader)
		if err != nil {
			if fallback != "" {
				return fallback, nil
			}
			return "", err
		}
		if strings.EqualFold(strings.TrimSpace(event.Name), "endpoint") {
			if resolved := resolveSSEURL(cfg.URL, event.Data); resolved != "" {
				return resolved, nil
			}
		}
		if strings.Contains(event.Data, `"jsonrpc"`) && fallback != "" {
			return fallback, nil
		}
	}
}

func readSSEEnvelope(ctx context.Context, reader *bufio.Reader, expectedID int64) (rpcEnvelope, error) {
	for {
		event, err := readSSEEvent(ctx, reader)
		if err != nil {
			return rpcEnvelope{}, err
		}
		if strings.TrimSpace(event.Data) == "" {
			continue
		}
		var envelope rpcEnvelope
		if err := json.Unmarshal([]byte(event.Data), &envelope); err != nil {
			continue
		}
		if envelope.Error != nil {
			return rpcEnvelope{}, fmt.Errorf("mcp error %d: %s", envelope.Error.Code, envelope.Error.Message)
		}
		if expectedID == 0 || envelope.ID == expectedID {
			return envelope, nil
		}
	}
}

func readSSEEvent(ctx context.Context, reader *bufio.Reader) (sseEvent, error) {
	event := sseEvent{Name: "message"}
	dataLines := make([]string, 0, 2)
	for {
		select {
		case <-ctx.Done():
			return sseEvent{}, ctx.Err()
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return sseEvent{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(dataLines) == 0 && event.Name == "message" {
				continue
			}
			event.Data = strings.Join(dataLines, "\n")
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event.Name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func resolveSSEURL(baseURL string, endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return strings.TrimSpace(baseURL)
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	return base.ResolveReference(parsed).String()
}

func (c *Client) postSSERequest(ctx context.Context, cfg ServerConfig, messageURL string, id int64, method string, params map[string]any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	return c.postSSEPayload(ctx, cfg, messageURL, payload)
}

func (c *Client) postSSENotification(ctx context.Context, cfg ServerConfig, messageURL string, method string, params map[string]any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	return c.postSSEPayload(ctx, cfg, messageURL, payload)
}

func (c *Client) postSSEPayload(ctx context.Context, cfg ServerConfig, messageURL string, payload []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, messageURL, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range cfg.Headers {
		request.Header.Set(key, value)
	}
	response, err := httpClient(cfg).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("mcp sse %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

func httpClient(cfg ServerConfig) *http.Client {
	timeout := 10 * time.Second
	if cfg.TimeoutMs > 0 {
		timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	}
	return &http.Client{Timeout: timeout}
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

func writeMessage(writer io.Writer, id int64, method string, params map[string]any) error {
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
