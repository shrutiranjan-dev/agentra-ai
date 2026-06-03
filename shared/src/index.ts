export type Role = "system" | "user" | "assistant" | "tool";

export type ContentPartType =
  | "text"
  | "reasoning"
  | "tool_call"
  | "tool_result"
  | "finish";

export interface TextContentPart {
  type: "text";
  text: string;
}

export interface ReasoningContentPart {
  type: "reasoning";
  text: string;
}

export interface ToolCallPart {
  type: "tool_call";
  id: string;
  name: string;
  input: string;
  status: "pending" | "running" | "completed" | "failed";
}

export interface ToolResultPart {
  type: "tool_result";
  toolCallId: string;
  content: string;
  isError: boolean;
}

export interface FinishPart {
  type: "finish";
  reason:
    | "stop"
    | "tool_use"
    | "permission_denied"
    | "cancelled"
    | "length";
  time: number;
}

export type ContentPart =
  | TextContentPart
  | ReasoningContentPart
  | ToolCallPart
  | ToolResultPart
  | FinishPart;

export interface Session {
  id: string;
  title: string;
  parentSessionId?: string;
  summaryMessageId?: string;
  promptTokens: number;
  completionTokens: number;
  cost: number;
  createdAt: string;
  updatedAt: string;
}

export interface Message {
  id: string;
  sessionId: string;
  role: Role;
  model?: string;
  parts: ContentPart[];
  createdAt: string;
  updatedAt: string;
}

export interface ToolDefinition {
  name: string;
  description: string;
  risky: boolean;
}

export interface PermissionRequest {
  id: string;
  sessionId: string;
  toolName: string;
  action: string;
  path?: string;
  description: string;
  params?: string;
  createdAt: string;
}

export interface PermissionDecision {
  requestId: string;
  decision: "allow_once" | "allow_session" | "deny";
}

export interface ModelInfo {
  id: string;
  name: string;
  provider: "ollama";
}

export interface ProviderInfo {
  id: "ollama";
  name: "Ollama";
}

export interface AgentRunRequest {
  sessionId: string;
  prompt: string;
  model: string;
  attachments?: string[];
}

export interface RepoIndexStatus {
  repoPath: string;
  state: "idle" | "running" | "complete" | "failed";
  indexedFiles: number;
  message?: string;
}

export interface MemoryNode {
  id: string;
  label: string;
  kind: string;
  metadata?: Record<string, string>;
}

export interface MemoryEdge {
  from: string;
  to: string;
  kind: string;
}

export interface ServiceStatus {
  serviceHealthy: boolean;
  mongoAvailable: boolean;
  neo4jAvailable: boolean;
  ollamaReachable: boolean;
  mode: "healthy" | "degraded" | "offline";
  messages: string[];
}

export interface ApiErrorResponse {
  error: {
    code: string;
    message: string;
    dependency?: "mongo" | "neo4j" | "ollama" | "service";
  };
}

export type AgentEvent =
  | {
      type: "message.delta";
      sessionId: string;
      messageId: string;
      delta: string;
    }
  | {
      type: "message.completed";
      sessionId: string;
      message: Message;
    }
  | {
      type: "permission.requested";
      request: PermissionRequest;
    }
  | {
      type: "tool.started";
      sessionId: string;
      toolCallId: string;
      toolName: string;
    }
  | {
      type: "tool.completed";
      sessionId: string;
      toolCallId: string;
      output: string;
      isError: boolean;
    }
  | {
      type: "repo.index.status";
      payload: RepoIndexStatus;
    }
  | {
      type: "log";
      level: "info" | "warn" | "error";
      message: string;
    };
