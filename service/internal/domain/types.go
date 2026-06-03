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
	IsError    bool   `bson:"isError,omitempty" json:"isError,omitempty"`
	Reason     string `bson:"reason,omitempty" json:"reason,omitempty"`
	Time       int64  `bson:"time,omitempty" json:"time,omitempty"`
}

type Session struct {
	ID               string    `bson:"_id" json:"id"`
	Title            string    `bson:"title" json:"title"`
	ParentSessionID  string    `bson:"parentSessionId,omitempty" json:"parentSessionId,omitempty"`
	SummaryMessageID string    `bson:"summaryMessageId,omitempty" json:"summaryMessageId,omitempty"`
	PromptTokens     int64     `bson:"promptTokens" json:"promptTokens"`
	CompletionTokens int64     `bson:"completionTokens" json:"completionTokens"`
	Cost             float64   `bson:"cost" json:"cost"`
	CreatedAt        time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time `bson:"updatedAt" json:"updatedAt"`
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
	ToolName    string    `bson:"toolName" json:"toolName"`
	Action      string    `bson:"action" json:"action"`
	Path        string    `bson:"path,omitempty" json:"path,omitempty"`
	Description string    `bson:"description" json:"description"`
	Params      string    `bson:"params,omitempty" json:"params,omitempty"`
	Status      string    `bson:"status" json:"status"`
	CreatedAt   time.Time `bson:"createdAt" json:"createdAt"`
}

type ToolExecution struct {
	ID        string    `bson:"_id" json:"id"`
	SessionID string    `bson:"sessionId" json:"sessionId"`
	ToolName  string    `bson:"toolName" json:"toolName"`
	Input     string    `bson:"input" json:"input"`
	Output    string    `bson:"output" json:"output"`
	IsError   bool      `bson:"isError" json:"isError"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
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
