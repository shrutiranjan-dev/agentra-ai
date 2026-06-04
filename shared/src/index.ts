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
  name?: string;
  path?: string;
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
  parentRunId?: string;
  summaryMessageId?: string;
  summaryParentMessageId?: string;
  summaryFromMessageId?: string;
  summaryToMessageId?: string;
  lastRunId?: string;
  lastRunStatus?: string;
  lastCompactedAt?: string;
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

export interface CommandDefinition {
  id: string;
  title: string;
  description: string;
  kind: "builtin" | "user" | "project";
  usage?: string;
  template?: string;
  sourcePath?: string;
  arguments?: CommandArgumentDefinition[];
}

export interface CommandArgumentDefinition {
  name: string;
  description?: string;
  required: boolean;
  defaultValue?: string;
}

export interface CommandInvocation {
  sessionId: string;
  model: string;
  input: string;
  repoPath?: string;
}

export interface CommandResult {
  commandId: string;
  message: Message;
}

export interface PermissionRequest {
  id: string;
  sessionId: string;
  runId?: string;
  toolCallId?: string;
  toolName: string;
  action: string;
  path?: string;
  paths?: string[];
  description: string;
  params?: string;
  preview?: string;
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
  repoPath?: string;
}

export interface RepoIndexStatus {
  repoPath: string;
  state: "idle" | "running" | "complete" | "failed";
  indexedFiles: number;
  message?: string;
}

export interface DiagnosticResult {
  path: string;
  language: string;
  source?: string;
  severity: "error" | "warning" | "information" | "hint";
  message: string;
  line: number;
  character: number;
}

export interface CodeIntelLocation {
  path: string;
  line: number;
  character: number;
  endLine?: number;
  endCharacter?: number;
  preview?: string;
}

export interface DocumentSymbolResult {
  name: string;
  kind: string;
  path: string;
  line: number;
  character: number;
}

export interface WorkspaceSymbolResult {
  name: string;
  kind: string;
  path: string;
  line: number;
  character: number;
  score?: number;
  reasons?: string[];
}

export interface MCPToolInfo {
  server: string;
  transport?: string;
  name: string;
  title?: string;
  description?: string;
}

export interface MCPServerStatus {
  server: string;
  transport: "stdio" | "http" | "sse";
  reachable: boolean;
  toolCount: number;
  error?: string;
}

export interface ToolActivity {
  toolCallId: string;
  toolName: string;
  status: "requested" | "awaiting_approval" | "approved" | "running" | "denied" | "failed" | "completed";
  output?: string;
  isError?: boolean;
  sessionId: string;
  runId?: string;
  path?: string;
  input?: string;
  summary?: string;
  startedAt?: number;
  completedAt?: number;
}

export interface ContinuationState {
  sessionId: string;
  summaryMessageId: string;
  summaryParentMessageId?: string;
  summaryFromMessageId?: string;
  summaryToMessageId?: string;
  compactedMessages: number;
  recentMessages: number;
  createdAt: string;
}

export interface SubtaskLink {
  parentSessionId: string;
  parentRunId?: string;
  childSessionId: string;
  title: string;
  prompt?: string;
  status: string;
}

export interface RepoGraphSummary {
  repoPath: string;
  indexedFiles: number;
  indexedDirectories: number;
  importEdges: number;
  referenceEdges: number;
  symbolCount: number;
  touchedFiles: string[];
  relatedFiles: string[];
  relatedFileMatches?: RepoFileMatch[];
  relatedMemoryMatches?: RepoMemoryMatch[];
  relatedSymbols?: RepoSymbolMatch[];
  childSessions: string[];
  lineageSessions: string[];
  memories: Array<{
    id: string;
    kind: string;
    label: string;
  }>;
}

export interface RepoFileMatch {
  path: string;
  score: number;
  reasons: string[];
  source?: string;
}

export interface RepoMemoryMatch {
  id: string;
  kind: string;
  label: string;
  score: number;
  reasons: string[];
}

export interface RepoSymbolMatch {
  filePath: string;
  name: string;
  kind: string;
  line: number;
  score?: number;
  reasons?: string[];
}

export interface RepoRetrievalPreview {
  repoPath: string;
  sessionId?: string;
  prompt: string;
  fileMatches: RepoFileMatch[];
  memoryMatches: RepoMemoryMatch[];
  symbolMatches: RepoSymbolMatch[];
  snippets: string[];
}

export interface SubtaskResult {
  session: Session;
  message: Message;
  link: SubtaskLink;
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
      type: "run.status";
      sessionId: string;
      runId: string;
      status:
        | "started"
        | "awaiting_tool"
        | "waiting_for_approval"
        | "tool_denied"
        | "tool_failed"
        | "assistant_resumed"
        | "completed"
        | "model_failed"
        | "compacted_then_continued"
        | "subtask_completed";
      message?: string;
      toolName?: string;
      continuation?: ContinuationState;
      subtask?: SubtaskLink;
      time: number;
    }
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
      type: "approval.updated";
      requestId: string;
      sessionId: string;
      runId?: string;
      toolCallId?: string;
      toolName: string;
      action: string;
      status: "requested" | "allow_once" | "allow_session" | "deny" | "timeout";
      path?: string;
      time: number;
    }
  | {
      type: "tool.lifecycle";
      sessionId: string;
      runId?: string;
      toolCallId: string;
      toolName: string;
      status: ToolActivity["status"];
      path?: string;
      input?: string;
      summary?: string;
      output?: string;
      isError?: boolean;
      time: number;
    }
  | {
      type: "tool.started";
      sessionId: string;
      toolCallId: string;
      toolName: string;
      runId?: string;
      path?: string;
      input?: string;
      summary?: string;
      startedAt?: number;
    }
  | {
      type: "tool.completed";
      sessionId: string;
      toolCallId: string;
      runId?: string;
      toolName?: string;
      path?: string;
      output: string;
      isError: boolean;
      summary?: string;
      completedAt?: number;
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
