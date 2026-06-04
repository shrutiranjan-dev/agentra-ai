package domain

import "time"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentPart struct {
	Type       string `bson:"type" json:"type"`
	Text       string `bson:"text,omitempty" json:"text,omitempty"`
	ID         string `bson:"id,omitempty" json:"id,omitempty"`
	Name       string `bson:"name,omitempty" json:"name,omitempty"`
	Input      string `bson:"input,omitempty" json:"input,omitempty"`
	Status     string `bson:"status,omitempty" json:"status,omitempty"`
	ToolCallID string `bson:"toolCallId,omitempty" json:"toolCallId,omitempty"`
	Content    string `bson:"content,omitempty" json:"content,omitempty"`
	Path       string `bson:"path,omitempty" json:"path,omitempty"`
	IsError    bool   `bson:"isError,omitempty" json:"isError,omitempty"`
	Reason     string `bson:"reason,omitempty" json:"reason,omitempty"`
	Time       int64  `bson:"time,omitempty" json:"time,omitempty"`
}

type Session struct {
	ID                     string    `bson:"_id" json:"id"`
	Title                  string    `bson:"title" json:"title"`
	ParentSessionID        string    `bson:"parentSessionId,omitempty" json:"parentSessionId,omitempty"`
	ParentRunID            string    `bson:"parentRunId,omitempty" json:"parentRunId,omitempty"`
	SummaryMessageID       string    `bson:"summaryMessageId,omitempty" json:"summaryMessageId,omitempty"`
	SummaryParentMessageID string    `bson:"summaryParentMessageId,omitempty" json:"summaryParentMessageId,omitempty"`
	SummaryFromMessageID   string    `bson:"summaryFromMessageId,omitempty" json:"summaryFromMessageId,omitempty"`
	SummaryToMessageID     string    `bson:"summaryToMessageId,omitempty" json:"summaryToMessageId,omitempty"`
	LastRunID              string    `bson:"lastRunId,omitempty" json:"lastRunId,omitempty"`
	LastRunStatus          string    `bson:"lastRunStatus,omitempty" json:"lastRunStatus,omitempty"`
	LastCompactedAt        time.Time `bson:"lastCompactedAt,omitempty" json:"lastCompactedAt,omitempty"`
	PromptTokens           int64     `bson:"promptTokens" json:"promptTokens"`
	CompletionTokens       int64     `bson:"completionTokens" json:"completionTokens"`
	Cost                   float64   `bson:"cost" json:"cost"`
	CreatedAt              time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt              time.Time `bson:"updatedAt" json:"updatedAt"`
}

type Message struct {
	ID        string        `bson:"_id" json:"id"`
	SessionID string        `bson:"sessionId" json:"sessionId"`
	Role      Role          `bson:"role" json:"role"`
	Model     string        `bson:"model,omitempty" json:"model,omitempty"`
	Parts     []ContentPart `bson:"parts" json:"parts"`
	CreatedAt time.Time     `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time     `bson:"updatedAt" json:"updatedAt"`
}

type PermissionRequest struct {
	ID          string    `bson:"_id" json:"id"`
	SessionID   string    `bson:"sessionId" json:"sessionId"`
	RunID       string    `bson:"runId,omitempty" json:"runId,omitempty"`
	ToolCallID  string    `bson:"toolCallId,omitempty" json:"toolCallId,omitempty"`
	ToolName    string    `bson:"toolName" json:"toolName"`
	Action      string    `bson:"action" json:"action"`
	Path        string    `bson:"path,omitempty" json:"path,omitempty"`
	Paths       []string  `bson:"paths,omitempty" json:"paths,omitempty"`
	Description string    `bson:"description" json:"description"`
	Params      string    `bson:"params,omitempty" json:"params,omitempty"`
	Preview     string    `bson:"preview,omitempty" json:"preview,omitempty"`
	Status      string    `bson:"status" json:"status"`
	CreatedAt   time.Time `bson:"createdAt" json:"createdAt"`
}

type ToolExecution struct {
	ID          string    `bson:"_id" json:"id"`
	RunID       string    `bson:"runId,omitempty" json:"runId,omitempty"`
	SessionID   string    `bson:"sessionId" json:"sessionId"`
	ToolName    string    `bson:"toolName" json:"toolName"`
	Path        string    `bson:"path,omitempty" json:"path,omitempty"`
	Summary     string    `bson:"summary,omitempty" json:"summary,omitempty"`
	Status      string    `bson:"status,omitempty" json:"status,omitempty"`
	Input       string    `bson:"input" json:"input"`
	Output      string    `bson:"output" json:"output"`
	IsError     bool      `bson:"isError" json:"isError"`
	StartedAt   time.Time `bson:"startedAt,omitempty" json:"startedAt,omitempty"`
	CompletedAt time.Time `bson:"completedAt,omitempty" json:"completedAt,omitempty"`
	CreatedAt   time.Time `bson:"createdAt" json:"createdAt"`
}

type CommandDefinition struct {
	ID          string                      `json:"id"`
	Title       string                      `json:"title"`
	Description string                      `json:"description"`
	Kind        string                      `json:"kind"`
	Usage       string                      `json:"usage,omitempty"`
	Template    string                      `json:"template,omitempty"`
	SourcePath  string                      `json:"sourcePath,omitempty"`
	Arguments   []CommandArgumentDefinition `json:"arguments,omitempty"`
}

type CommandArgumentDefinition struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Required     bool   `json:"required"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

type Setting struct {
	Key       string    `bson:"_id" json:"key"`
	Value     string    `bson:"value" json:"value"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
}

type RepoIndexStatus struct {
	RepoPath     string `json:"repoPath"`
	State        string `json:"state"`
	IndexedFiles int    `json:"indexedFiles"`
	Message      string `json:"message,omitempty"`
}

type ContinuationState struct {
	SessionID              string    `json:"sessionId"`
	SummaryMessageID       string    `json:"summaryMessageId"`
	SummaryParentMessageID string    `json:"summaryParentMessageId,omitempty"`
	SummaryFromMessageID   string    `json:"summaryFromMessageId,omitempty"`
	SummaryToMessageID     string    `json:"summaryToMessageId,omitempty"`
	CompactedMessages      int       `json:"compactedMessages"`
	RecentMessages         int       `json:"recentMessages"`
	CreatedAt              time.Time `json:"createdAt"`
}

type SubtaskLink struct {
	ParentSessionID string `json:"parentSessionId"`
	ParentRunID     string `json:"parentRunId,omitempty"`
	ChildSessionID  string `json:"childSessionId"`
	Title           string `json:"title"`
	Prompt          string `json:"prompt,omitempty"`
	Status          string `json:"status"`
}

type SubtaskResult struct {
	Session Session     `json:"session"`
	Message Message     `json:"message"`
	Link    SubtaskLink `json:"link"`
}
