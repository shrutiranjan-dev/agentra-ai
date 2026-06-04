import { useEffect, useMemo, useRef, useState } from "react";
import type {
  CodeIntelLocation,
  CommandDefinition,
  DiagnosticResult,
  DocumentSymbolResult,
  MCPServerStatus,
  MCPToolInfo,
  Message,
  PermissionRequest,
  RepoGraphSummary,
  RepoRetrievalPreview,
  ServiceStatus,
  Session,
  ToolActivity,
  WorkspaceSymbolResult
} from "@assistant/shared";
import {
  applyPatch,
  connectEvents,
  createSession,
  decidePermission,
  editFile,
  getDefinitions,
  getDiagnostics,
  getDocumentSymbols,
  getMCPServerStatuses,
  getRepoContext,
  getRepoRetrievalPreview,
  getReferences,
  getServiceStatus,
  getWorkspaceDefinitions,
  getWorkspaceReferences,
  getWorkspaceSymbols,
  indexRepo,
  listMCPTools,
  listCommands,
  listMessages,
  listModels,
  listSessions,
  ProtectedApiAuthError,
  runAgent,
  runCommand,
  runSubtask,
  runShell
} from "../api/client";

type BootPhase = "loading" | "healthy" | "degraded" | "offline";
type AuthPhase = "unknown" | "ready" | "missing" | "invalid";

const STATUS_RETRY_DELAYS = [0, 500, 1000, 2000, 3000];

function safeArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function extractText(message: Message) {
  return message.parts
    .map((part: Message["parts"][number]) => {
      if (part.type === "text" || part.type === "reasoning") {
        return part.text;
      }
      if (part.type === "tool_result") {
        return part.content;
      }
      return "";
    })
    .join("");
}

function renderMessageParts(message: Message) {
  return message.parts.map((part, index) => {
    if (part.type === "text" || part.type === "reasoning") {
      return (
        <pre key={`${message.id}-${index}`} className="message-part">
          {part.text}
        </pre>
      );
    }
    if (part.type === "tool_call") {
      return (
        <div key={`${message.id}-${index}`} className="message-tool message-tool-call">
          <strong>Tool call: {part.name}</strong>
          <span>Status: {part.status}</span>
          <pre>{part.input}</pre>
        </div>
      );
    }
    if (part.type === "tool_result") {
      return (
        <div key={`${message.id}-${index}`} className={`message-tool ${part.isError ? "error" : "success"}`}>
          <strong>Tool result: {part.name ?? part.toolCallId}</strong>
          {part.path && <span>Path: {part.path}</span>}
          <pre>{part.content}</pre>
        </div>
      );
    }
    if (part.type === "finish") {
      return (
        <div key={`${message.id}-${index}`} className="message-finish">
          finish: {part.reason}
        </div>
      );
    }
    return null;
  });
}

function safePaths(request: PermissionRequest | null) {
  if (!request) {
    return [];
  }
  const fromList = Array.isArray(request.paths) ? request.paths : [];
  if (fromList.length > 0) {
    return fromList;
  }
  return request.path ? [request.path] : [];
}

function formatTimestamp(epochSeconds?: number) {
  if (!epochSeconds) {
    return "";
  }
  return new Date(epochSeconds * 1000).toLocaleTimeString();
}

function decodeToolName(toolName: string) {
  if (toolName.startsWith("mcp_") && toolName.includes("__")) {
    const trimmed = toolName.replace(/^mcp_/, "");
    const [server, name] = trimmed.split("__", 2);
    return { label: `MCP ${server}/${name}`, source: "mcp", server, name };
  }
  return { label: toolName, source: "core", server: "", name: toolName };
}

function renderToolActivity(item: ToolActivity) {
  const decoded = decodeToolName(item.toolName);
  const statusClass =
    item.status === "failed" || item.status === "denied"
      ? "error"
      : item.status === "completed" || item.status === "approved"
        ? "success"
        : "pending";
  return (
    <div key={item.toolCallId} className={`status-row ${item.isError ? "error" : statusClass}`}>
      <strong>{decoded.label}</strong>
      <span>
        {item.status.toUpperCase()}
        {decoded.source === "mcp" ? " · remote tool" : " · local tool"}
        {item.path ? ` · ${item.path}` : ""}
        {item.startedAt ? ` · ${formatTimestamp(item.completedAt ?? item.startedAt)}` : ""}
      </span>
      {decoded.source === "mcp" && decoded.server && <small>Server: {decoded.server}</small>}
      {item.summary && <pre>{item.summary}</pre>}
      {item.output && <pre>{item.output}</pre>}
    </div>
  );
}

function statusTone(status: string) {
  if (["failed", "denied", "tool_failed", "tool_denied", "model_failed", "offline"].includes(status)) {
    return "error";
  }
  if (["completed", "approved", "assistant_resumed", "subtask_completed", "healthy"].includes(status)) {
    return "success";
  }
  if (["awaiting_approval", "waiting_for_approval", "awaiting_tool", "running", "requested", "started", "loading", "degraded"].includes(status)) {
    return "pending";
  }
  return "neutral";
}

function formatRuntimeHeadline(entry: string) {
  const [headline, ...detail] = entry.split(" · ");
  return {
    headline: headline ?? "event",
    detail: detail.join(" · ")
  };
}

function buildCommandPrompt(command: CommandDefinition) {
  const requiredArgs = safeArray(command.arguments).filter((item) => item.required);
  if (requiredArgs.length === 0) {
    return `/${command.id} `;
  }
  return `/${command.id} ${requiredArgs.map((item) => `--${item.name} `).join("")}`;
}

function initialStatus(runtimeBootStatus: string, runtimeBootMessage: string): ServiceStatus {
  if (runtimeBootStatus === "healthy") {
    return {
      serviceHealthy: true,
      mongoAvailable: true,
      neo4jAvailable: true,
      ollamaReachable: true,
      mode: "healthy",
      messages: runtimeBootMessage ? [runtimeBootMessage] : []
    };
  }

  if (runtimeBootStatus === "degraded") {
    return {
      serviceHealthy: true,
      mongoAvailable: false,
      neo4jAvailable: false,
      ollamaReachable: true,
      mode: "degraded",
      messages: runtimeBootMessage ? [runtimeBootMessage] : ["Backend is running with limited dependencies."]
    };
  }

  return {
    serviceHealthy: false,
    mongoAvailable: false,
    neo4jAvailable: false,
    ollamaReachable: false,
    mode: "offline",
    messages: runtimeBootMessage ? [runtimeBootMessage] : ["Checking backend availability..."]
  };
}

function delay(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export function App() {
  const runtimeBootStatus = window.desktopApi?.runtimeConfig?.bootStatus ?? "offline";
  const runtimeBootMessage = window.desktopApi?.runtimeConfig?.bootMessage ?? "";
  const [sessions, setSessions] = useState<Session[]>([]);
  const [selectedSessionId, setSelectedSessionId] = useState<string>("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [models, setModels] = useState<Array<{ name: string }>>([]);
  const [commands, setCommands] = useState<CommandDefinition[]>([]);
  const [model, setModel] = useState("llama3.1");
  const [prompt, setPrompt] = useState("");
  const [shellCommand, setShellCommand] = useState("pwd");
  const [editPath, setEditPath] = useState("");
  const [editSearch, setEditSearch] = useState("");
  const [editReplace, setEditReplace] = useState("");
  const [patchPath, setPatchPath] = useState("");
  const [patchText, setPatchText] = useState("");
  const [diagnosticPath, setDiagnosticPath] = useState("");
  const [symbolPath, setSymbolPath] = useState("");
  const [workspaceSymbolQuery, setWorkspaceSymbolQuery] = useState("");
  const [codeIntelLine, setCodeIntelLine] = useState("1");
  const [codeIntelCharacter, setCodeIntelCharacter] = useState("1");
  const [subtaskTitle, setSubtaskTitle] = useState("Subtask");
  const [subtaskPrompt, setSubtaskPrompt] = useState("");
  const [repoPath, setRepoPath] = useState("");
  const [pendingPermission, setPendingPermission] = useState<PermissionRequest | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [runtimeEvents, setRuntimeEvents] = useState<string[]>([]);
  const [toolActivities, setToolActivities] = useState<ToolActivity[]>([]);
  const [latestContinuation, setLatestContinuation] = useState<{ summaryMessageId?: string; compactedMessages?: number; recentMessages?: number; summaryDepth?: number } | null>(null);
  const [latestApproval, setLatestApproval] = useState<{ toolName: string; status: string; path?: string; requestId?: string } | null>(null);
  const [diagnostics, setDiagnostics] = useState<DiagnosticResult[]>([]);
  const [documentSymbols, setDocumentSymbols] = useState<DocumentSymbolResult[]>([]);
  const [workspaceSymbols, setWorkspaceSymbols] = useState<WorkspaceSymbolResult[]>([]);
  const [definitions, setDefinitions] = useState<CodeIntelLocation[]>([]);
  const [references, setReferences] = useState<CodeIntelLocation[]>([]);
  const [mcpTools, setMCPTools] = useState<MCPToolInfo[]>([]);
  const [mcpServerStatuses, setMCPServerStatuses] = useState<MCPServerStatus[]>([]);
  const [repoContext, setRepoContext] = useState<RepoGraphSummary | null>(null);
  const [retrievalPreview, setRetrievalPreview] = useState<RepoRetrievalPreview | null>(null);
  const [selectedCommandIndex, setSelectedCommandIndex] = useState(0);
  const [bootPhase, setBootPhase] = useState<BootPhase>(
    runtimeBootStatus === "healthy" ? "healthy" : runtimeBootStatus === "degraded" ? "degraded" : "loading"
  );
  const [authPhase, setAuthPhase] = useState<AuthPhase>("unknown");
  const [authMessage, setAuthMessage] = useState("");
  const [serviceStatus, setServiceStatus] = useState<ServiceStatus>(initialStatus(runtimeBootStatus, runtimeBootMessage));
  const eventSocketRef = useRef<WebSocket | null>(null);
  const composerRef = useRef<HTMLTextAreaElement | null>(null);
  const safeSessions = safeArray(sessions);
  const safeMessages = safeArray(messages);
  const safeModels = safeArray(models);
  const safeCommands = safeArray(commands);
  const safeStatusMessages = safeArray(serviceStatus.messages);
  const safeLogs = safeArray(logs);
  const safeRuntimeEvents = safeArray(runtimeEvents);
  const safeToolActivities = safeArray(toolActivities);
  const safeDiagnostics = safeArray(diagnostics);
  const safeDocumentSymbols = safeArray(documentSymbols);
  const safeWorkspaceSymbols = safeArray(workspaceSymbols);
  const safeDefinitions = safeArray(definitions);
  const safeReferences = safeArray(references);
  const safeMCPTools = safeArray(mcpTools);
  const safeMCPServerStatuses = safeArray(mcpServerStatuses);
  const permissionPaths = safePaths(pendingPermission);
  const safeRetrievalFileMatches = safeArray(retrievalPreview?.fileMatches);
  const safeRetrievalMemoryMatches = safeArray(retrievalPreview?.memoryMatches);
  const safeRetrievalSymbolMatches = safeArray(retrievalPreview?.symbolMatches);
  const safeRetrievalSnippets = safeArray(retrievalPreview?.snippets);

  const selectedSession = useMemo(
    () => safeSessions.find((item) => item.id === selectedSessionId) ?? null,
    [safeSessions, selectedSessionId]
  );

  const commandMatches = useMemo(() => {
    const trimmed = prompt.trim();
    if (!trimmed.startsWith("/")) {
      return [];
    }
    const query = trimmed.slice(1).toLowerCase();
    return safeCommands
      .filter((command: CommandDefinition) =>
        command.id.toLowerCase().includes(query) ||
        command.title.toLowerCase().includes(query) ||
        command.description.toLowerCase().includes(query) ||
        safeArray(command.arguments).some((item) => item.name.toLowerCase().includes(query))
      )
      .slice(0, 8);
  }, [prompt, safeCommands]);
  const selectedCommandMatch = commandMatches[selectedCommandIndex] ?? commandMatches[0] ?? null;

  function pushLog(entry: string) {
    setLogs((current) => {
      const safeCurrent = Array.isArray(current) ? current : [];
      if (safeCurrent[0] === entry) {
        return safeCurrent;
      }
      return [entry, ...safeCurrent].slice(0, 100);
    });
  }

  function pushRuntimeEvent(entry: string) {
    setRuntimeEvents((current) => {
      const safeCurrent = Array.isArray(current) ? current : [];
      if (safeCurrent[0] === entry) {
        return safeCurrent;
      }
      return [entry, ...safeCurrent].slice(0, 40);
    });
  }

  useEffect(() => {
    setSelectedCommandIndex(0);
  }, [prompt]);

  function applyCommandSuggestion(command: CommandDefinition) {
    setPrompt(buildCommandPrompt(command));
    setSelectedCommandIndex(0);
    window.setTimeout(() => composerRef.current?.focus(), 0);
  }

  function handlePromptKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (commandMatches.length === 0) {
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setSelectedCommandIndex((current) => (current + 1) % commandMatches.length);
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setSelectedCommandIndex((current) => (current - 1 + commandMatches.length) % commandMatches.length);
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      setPrompt((current) => (current.trim().startsWith("/") ? current.split(" ")[0] : current));
      return;
    }
    if (event.key === "Tab" || (event.key === "Enter" && !event.shiftKey && prompt.trim().startsWith("/"))) {
      event.preventDefault();
      if (selectedCommandMatch) {
        applyCommandSuggestion(selectedCommandMatch);
      }
    }
  }

  useEffect(() => {
    let disposed = false;

    async function loadSessionsIfAvailable(status: ServiceStatus) {
      if (!status.serviceHealthy || !status.mongoAvailable) {
        setSessions([]);
        setSelectedSessionId("");
        return;
      }

      try {
        const sessionData = safeArray(await listSessions());
        if (disposed) return;
        setAuthPhase("ready");
        setAuthMessage("");
        setSessions(sessionData);
        setSelectedSessionId((current) => current || sessionData[0]?.id || "");
      } catch (error) {
        if (disposed) return;
        if (error instanceof ProtectedApiAuthError) {
          setAuthPhase(error.kind);
          setAuthMessage(
            error.kind === "missing"
              ? "Set VITE_APP_AUTH_TOKEN to match APP_AUTH_TOKEN before using the browser UI."
              : "The browser dev token does not match APP_AUTH_TOKEN. Update VITE_APP_AUTH_TOKEN and restart Vite."
          );
          setSessions([]);
          setSelectedSessionId("");
          return;
        }
        pushLog(`sessions unavailable: ${(error as Error).message}`);
      }
    }

    async function loadModelsIfAvailable(status: ServiceStatus) {
      if (!status.serviceHealthy || !status.ollamaReachable) {
        setModels([]);
        return;
      }

      try {
        const modelData = safeArray(await listModels());
        if (disposed) return;
        setAuthPhase("ready");
        setAuthMessage("");
        setModels(modelData);
        if (modelData[0]?.name) {
          setModel((current) => {
            if (modelData.some((item) => item.name === current)) {
              return current;
            }
            return modelData[0].name;
          });
        }
      } catch (error) {
        if (disposed) return;
        if (error instanceof ProtectedApiAuthError) {
          setAuthPhase(error.kind);
          setAuthMessage(
            error.kind === "missing"
              ? "Set VITE_APP_AUTH_TOKEN to match APP_AUTH_TOKEN before loading Ollama models in the browser."
              : "The browser dev token is invalid for protected API routes. Update VITE_APP_AUTH_TOKEN and restart Vite."
          );
          setModels([]);
          return;
        }
        pushLog(`models unavailable: ${(error as Error).message}`);
      }
    }

    async function loadCommandsIfAvailable(status: ServiceStatus) {
      if (!status.serviceHealthy) {
        setCommands([]);
        return;
      }

      try {
        const commandData = safeArray(await listCommands(repoPath));
        if (disposed) return;
        setCommands(commandData);
      } catch (error) {
        if (disposed) return;
        if (error instanceof ProtectedApiAuthError) {
          return;
        }
        pushLog(`commands unavailable: ${(error as Error).message}`);
      }
    }

    async function refreshStatusLoop() {
      setBootPhase((current) => (current === "offline" ? "loading" : current));

      for (const retryDelay of STATUS_RETRY_DELAYS) {
        if (retryDelay > 0) {
          await delay(retryDelay);
        }

        try {
          const status = await getServiceStatus();
          if (disposed) return;

          setServiceStatus(status);
          setBootPhase(status.mode === "healthy" ? "healthy" : "degraded");
          if (status.serviceHealthy && authPhase === "unknown") {
            setAuthMessage("");
          }
          await Promise.all([loadSessionsIfAvailable(status), loadModelsIfAvailable(status), loadCommandsIfAvailable(status)]);
          return;
        } catch (error) {
          if (disposed) return;
          const message = (error as Error).message;
          pushLog(`status retry failed: ${message}`);
        }
      }

      if (disposed) return;
      setBootPhase("offline");
      setServiceStatus({
        serviceHealthy: false,
        mongoAvailable: false,
        neo4jAvailable: false,
        ollamaReachable: false,
        mode: "offline",
        messages: ["Backend unavailable after retries. The UI will keep retrying in the background."]
      });
    }

    void refreshStatusLoop();
    const poll = window.setInterval(() => {
      void refreshStatusLoop();
    }, 10000);

    return () => {
      disposed = true;
      window.clearInterval(poll);
    };
  }, [authPhase, repoPath]);

  useEffect(() => {
    if (!repoPath || !serviceStatus.serviceHealthy || authPhase !== "ready") {
      setDiagnostics([]);
      setWorkspaceSymbols([]);
      setMCPTools([]);
      setMCPServerStatuses([]);
      setRepoContext(null);
      setRetrievalPreview(null);
      return;
    }

    let disposed = false;
    void (async () => {
      try {
        const [tools, statuses, context] = await Promise.all([
          listMCPTools(repoPath).catch(() => []),
          getMCPServerStatuses(repoPath).catch(() => []),
          getRepoContext(repoPath, selectedSessionId || undefined).catch(() => null)
        ]);
        if (disposed) return;
        setMCPTools(safeArray<MCPToolInfo>(tools));
        setMCPServerStatuses(safeArray<MCPServerStatus>(statuses));
        setRepoContext(context);
      } catch (error) {
        if (disposed) return;
        pushLog(`repo metadata unavailable: ${(error as Error).message}`);
      }
    })();

    return () => {
      disposed = true;
    };
  }, [authPhase, repoPath, selectedSessionId, serviceStatus.serviceHealthy]);

  useEffect(() => {
    if (!repoPath || !prompt.trim() || !serviceStatus.serviceHealthy || authPhase !== "ready") {
      setRetrievalPreview(null);
      return;
    }

    let disposed = false;
    const timer = window.setTimeout(() => {
      void (async () => {
        try {
          const preview = await getRepoRetrievalPreview(repoPath, prompt.trim(), selectedSessionId || undefined);
          if (disposed) return;
          setRetrievalPreview(preview);
        } catch (error) {
          if (disposed) return;
          pushLog(`retrieval preview unavailable: ${(error as Error).message}`);
          setRetrievalPreview(null);
        }
      })();
    }, 350);

    return () => {
      disposed = true;
      window.clearTimeout(timer);
    };
  }, [authPhase, prompt, repoPath, selectedSessionId, serviceStatus.serviceHealthy]);

  useEffect(() => {
    if ((bootPhase !== "healthy" && bootPhase !== "degraded") || authPhase !== "ready") {
      if (eventSocketRef.current) {
        eventSocketRef.current.close();
        eventSocketRef.current = null;
      }
      return;
    }

    if (eventSocketRef.current) {
      return;
    }

    const socket = connectEvents((event) => {
      if (event.type === "run.status") {
        const payload = event.data as {
          sessionId: string;
          runId: string;
          status: string;
          message?: string;
          toolName?: string;
          continuation?: { summaryMessageId?: string; compactedMessages?: number; recentMessages?: number; summaryDepth?: number } | null;
          subtask?: { title?: string; childSessionId?: string; status?: string } | null;
          time: number;
        };
        const details = [
          payload.status,
          payload.toolName ? `tool=${payload.toolName}` : "",
          payload.message ?? "",
          payload.continuation?.summaryMessageId
            ? `summary=${payload.continuation.summaryMessageId} depth=${payload.continuation.summaryDepth ?? 1} compacted=${payload.continuation.compactedMessages ?? 0} recent=${payload.continuation.recentMessages ?? 0}`
            : "",
          payload.subtask?.childSessionId ? `subtask=${payload.subtask.title ?? payload.subtask.childSessionId}` : ""
        ]
          .filter(Boolean)
          .join(" · ");
        pushRuntimeEvent(details);
        if (payload.continuation?.summaryMessageId) {
          setLatestContinuation(payload.continuation);
        }
      }

      if (event.type === "message.delta") {
        const payload = event.data as { messageId: string; delta: string };
        setMessages((current) =>
          current.map((message) =>
            message.id === payload.messageId
              ? {
                  ...message,
                  parts: [{ type: "text", text: extractText(message) + payload.delta }]
                }
              : message
          )
        );
      }

      if (event.type === "message.completed") {
        const message = event.data as Message;
        setMessages((current) => {
          const exists = current.some((item) => item.id === message.id);
          return exists
            ? current.map((item) => (item.id === message.id ? message : item))
            : [...current, message];
        });
      }

      if (event.type === "permission.requested") {
        const request = event.data as PermissionRequest;
        setPendingPermission(request);
        setLatestApproval({
          toolName: request.toolName,
          status: "requested",
          path: request.path,
          requestId: request.id
        });
      }

      if (event.type === "approval.updated") {
        const payload = event.data as {
          requestId: string;
          sessionId: string;
          runId?: string;
          toolCallId?: string;
          toolName: string;
          status: ToolActivity["status"] | "allow_once" | "allow_session" | "timeout" | "deny";
          path?: string;
        };
        const normalizedStatus =
          payload.status === "allow_once" || payload.status === "allow_session"
            ? "approved"
            : payload.status === "deny"
              ? "denied"
              : payload.status === "timeout"
                ? "failed"
                : payload.status;
        if (payload.toolCallId) {
          setToolActivities((current) => {
            const existing = safeArray(current);
            const found = existing.find((item) => item.toolCallId === payload.toolCallId);
            const updated: ToolActivity = {
              sessionId: payload.sessionId,
              runId: payload.runId,
              toolCallId: payload.toolCallId!,
              toolName: payload.toolName,
              status: normalizedStatus as ToolActivity["status"],
              path: payload.path ?? found?.path,
              input: found?.input,
              summary: found?.summary,
              output: found?.output,
              isError: normalizedStatus === "denied" || normalizedStatus === "failed",
              startedAt: found?.startedAt,
              completedAt: found?.completedAt
            };
            return [updated, ...existing.filter((item) => item.toolCallId !== payload.toolCallId)].slice(0, 30);
          });
        }
        setLatestApproval({
          toolName: payload.toolName,
          status: normalizedStatus,
          path: payload.path,
          requestId: payload.requestId
        });
        pushRuntimeEvent(`${payload.toolName} approval ${normalizedStatus}${payload.path ? ` · ${payload.path}` : ""}`);
      }

      if (event.type === "tool.lifecycle") {
        const payload = event.data as {
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
        };
        setToolActivities((current) => {
          const existing = safeArray(current);
          const found = existing.find((item) => item.toolCallId === payload.toolCallId);
          const updated: ToolActivity = {
            sessionId: payload.sessionId,
            runId: payload.runId,
            toolCallId: payload.toolCallId,
            toolName: payload.toolName,
            status: payload.status,
            path: payload.path ?? found?.path,
            input: payload.input ?? found?.input,
            summary: payload.summary ?? found?.summary,
            output: payload.output ?? found?.output,
            isError: payload.isError ?? found?.isError,
            startedAt: found?.startedAt ?? payload.time,
            completedAt: payload.status === "completed" || payload.status === "failed" || payload.status === "denied" ? payload.time : found?.completedAt
          };
          return [updated, ...existing.filter((item) => item.toolCallId !== payload.toolCallId)].slice(0, 30);
        });
      }

      if (event.type === "tool.started") {
        const payload = event.data as {
          sessionId: string;
          runId?: string;
          toolCallId: string;
          toolName: string;
          path?: string;
          input?: string;
          summary?: string;
          startedAt?: number;
        };
        setToolActivities((current) => {
          const started: ToolActivity = {
            sessionId: payload.sessionId,
            runId: payload.runId,
            toolCallId: payload.toolCallId,
            toolName: payload.toolName,
            status: "running",
            path: payload.path,
            input: payload.input,
            summary: payload.summary,
            startedAt: payload.startedAt
          };
          return [started, ...safeArray<ToolActivity>(current).filter((item) => item.toolCallId !== payload.toolCallId)].slice(0, 30);
        });
      }

      if (event.type === "tool.completed") {
        const payload = event.data as {
          sessionId: string;
          runId?: string;
          toolCallId: string;
          toolName?: string;
          path?: string;
          output: string;
          isError: boolean;
          summary?: string;
          completedAt?: number;
        };
        setToolActivities((current) => {
          const existing = safeArray(current);
          const found = existing.find((item) => item.toolCallId === payload.toolCallId);
          const updated: ToolActivity = {
            sessionId: payload.sessionId,
            runId: payload.runId,
            toolCallId: payload.toolCallId,
            toolName: payload.toolName ?? found?.toolName ?? "tool",
            status: payload.isError ? "failed" : "completed",
            path: payload.path ?? found?.path,
            input: found?.input,
            summary: payload.summary ?? found?.summary,
            output: payload.output,
            isError: payload.isError,
            startedAt: found?.startedAt,
            completedAt: payload.completedAt
          };
          return [updated, ...existing.filter((item) => item.toolCallId !== payload.toolCallId)].slice(0, 30);
        });
      }

      if (event.type === "repo.index.status") {
        const payload = event.data as { state: string; indexedFiles?: number; message?: string };
        pushLog(`repo index: ${payload.state} ${payload.indexedFiles ?? ""} ${payload.message ?? ""}`.trim());
        if (repoPath && payload.state === "complete") {
          void getRepoContext(repoPath)
            .then((context) => setRepoContext(context))
            .catch((error) => pushLog(`repo context unavailable: ${(error as Error).message}`));
        }
      }

      if (event.type === "log") {
        pushLog(JSON.stringify(event.data));
      }
    });

    socket.onclose = () => {
      if (eventSocketRef.current === socket) {
        eventSocketRef.current = null;
      }
    };

    eventSocketRef.current = socket;

    return () => {
      socket.close();
      if (eventSocketRef.current === socket) {
        eventSocketRef.current = null;
      }
    };
  }, [authPhase, bootPhase]);

  useEffect(() => {
    if (!selectedSessionId || !serviceStatus.serviceHealthy || !serviceStatus.mongoAvailable) {
      return;
    }

    void (async () => {
      try {
        const data = safeArray(await listMessages(selectedSessionId));
        setMessages(data);
      } catch (error) {
        pushLog(`load messages error: ${(error as Error).message}`);
      }
    })();
  }, [selectedSessionId, serviceStatus.mongoAvailable, serviceStatus.serviceHealthy]);

  async function handleNewSession() {
    if (!serviceStatus.serviceHealthy || !serviceStatus.mongoAvailable || authPhase !== "ready") return;
    const created = await createSession(`Session ${new Date().toLocaleTimeString()}`);
    if (!created?.id) {
      pushLog("create session failed: backend returned an invalid session");
      return;
    }
    setSessions((current) => [created, ...safeArray(current)]);
    setSelectedSessionId(created.id);
    setMessages([]);
  }

  async function handleSend() {
    if (!serviceStatus.serviceHealthy || !prompt.trim() || authPhase !== "ready") return;
    let sessionId = selectedSessionId;
    if (!sessionId) {
      const created = await createSession(`Session ${new Date().toLocaleTimeString()}`);
      if (!created?.id) {
        pushLog("auto-create session failed: backend returned an invalid session");
        return;
      }
      sessionId = created.id;
      setSessions((current) => [created, ...safeArray(current)]);
      setSelectedSessionId(created.id);
      setMessages([]);
    }
    const nextPrompt = prompt;
    setPrompt("");
    if (nextPrompt.trim().startsWith("/")) {
      await runCommand(sessionId, model, nextPrompt, repoPath);
    } else {
      await runAgent(sessionId, nextPrompt, model, repoPath);
    }
    if (!serviceStatus.mongoAvailable) {
      return;
    }
    const refreshed = safeArray(await listMessages(sessionId));
    setMessages(refreshed);
  }

  async function handleShell() {
    if (!serviceStatus.serviceHealthy || !selectedSessionId || authPhase !== "ready") return;
    const result = await runShell(selectedSessionId, shellCommand, repoPath);
    pushLog(`shell: ${JSON.stringify(result)}`);
  }

  async function handleEditFile() {
    if (!serviceStatus.serviceHealthy || !selectedSessionId || authPhase !== "ready" || !editPath.trim()) return;
    const result = await editFile(selectedSessionId, editPath.trim(), editSearch, editReplace, false);
    pushLog(`edit: ${result.output}`);
    const refreshed = safeArray(await listMessages(selectedSessionId));
    setMessages(refreshed);
  }

  async function handleApplyPatch() {
    if (!serviceStatus.serviceHealthy || !selectedSessionId || authPhase !== "ready" || !patchText.trim()) return;
    const result = await applyPatch(selectedSessionId, patchPath.trim() || undefined, patchText);
    pushLog(`patch: ${result.output}`);
    const refreshed = safeArray(await listMessages(selectedSessionId));
    setMessages(refreshed);
  }

  async function handlePickRepo() {
    const picked = await window.desktopApi?.pickDirectory?.();
    if (picked) {
      setRepoPath(picked);
      try {
        const commandData = safeArray(await listCommands(picked));
        setCommands(commandData);
      } catch (error) {
        pushLog(`commands unavailable: ${(error as Error).message}`);
      }
    }
  }

  async function handleIndexRepo() {
    if (!repoPath || !serviceStatus.neo4jAvailable) return;
    await indexRepo(repoPath);
  }

  async function handleDiagnostics() {
    if (!serviceStatus.serviceHealthy || authPhase !== "ready") return;
    const target = diagnosticPath.trim() || repoPath;
    if (!target) return;
    try {
      const items = safeArray(await getDiagnostics(target, repoPath));
      setDiagnostics(items);
      pushLog(`diagnostics loaded: ${items.length}`);
    } catch (error) {
      pushLog(`diagnostics unavailable: ${(error as Error).message}`);
      setDiagnostics([]);
    }
  }

  async function handleSymbols() {
    if (!serviceStatus.serviceHealthy || authPhase !== "ready") return;
    const target = symbolPath.trim() || diagnosticPath.trim() || repoPath;
    if (!target) return;
    try {
      const items = safeArray(await getDocumentSymbols(target, repoPath));
      setDocumentSymbols(items);
      pushLog(`symbols loaded: ${items.length}`);
    } catch (error) {
      pushLog(`symbols unavailable: ${(error as Error).message}`);
      setDocumentSymbols([]);
    }
  }

  async function handleWorkspaceSymbols() {
    if (!serviceStatus.serviceHealthy || authPhase !== "ready" || !repoPath) return;
    try {
      const items = safeArray(await getWorkspaceSymbols(repoPath, workspaceSymbolQuery.trim() || undefined));
      setWorkspaceSymbols(items);
      pushLog(`workspace symbols loaded: ${items.length}`);
    } catch (error) {
      pushLog(`workspace symbols unavailable: ${(error as Error).message}`);
      setWorkspaceSymbols([]);
    }
  }

  async function handleWorkspaceDefinitions() {
    if (!serviceStatus.serviceHealthy || authPhase !== "ready" || !repoPath) return;
    try {
      const items = safeArray(await getWorkspaceDefinitions(repoPath, workspaceSymbolQuery.trim() || undefined));
      setDefinitions(items);
      pushLog(`workspace definitions loaded: ${items.length}`);
    } catch (error) {
      pushLog(`workspace definitions unavailable: ${(error as Error).message}`);
      setDefinitions([]);
    }
  }

  async function handleWorkspaceReferences() {
    if (!serviceStatus.serviceHealthy || authPhase !== "ready" || !repoPath) return;
    try {
      const items = safeArray(await getWorkspaceReferences(repoPath, workspaceSymbolQuery.trim() || undefined));
      setReferences(items);
      pushLog(`workspace references loaded: ${items.length}`);
    } catch (error) {
      pushLog(`workspace references unavailable: ${(error as Error).message}`);
      setReferences([]);
    }
  }

  async function handleDefinitions() {
    if (!serviceStatus.serviceHealthy || authPhase !== "ready") return;
    const target = symbolPath.trim() || diagnosticPath.trim() || repoPath;
    if (!target) return;
    try {
      const items = safeArray(await getDefinitions(target, Number(codeIntelLine) || 1, Number(codeIntelCharacter) || 1, repoPath));
      setDefinitions(items);
      pushLog(`definitions loaded: ${items.length}`);
    } catch (error) {
      pushLog(`definitions unavailable: ${(error as Error).message}`);
      setDefinitions([]);
    }
  }

  async function handleReferences() {
    if (!serviceStatus.serviceHealthy || authPhase !== "ready") return;
    const target = symbolPath.trim() || diagnosticPath.trim() || repoPath;
    if (!target) return;
    try {
      const items = safeArray(await getReferences(target, Number(codeIntelLine) || 1, Number(codeIntelCharacter) || 1, repoPath));
      setReferences(items);
      pushLog(`references loaded: ${items.length}`);
    } catch (error) {
      pushLog(`references unavailable: ${(error as Error).message}`);
      setReferences([]);
    }
  }

  async function handleSubtask() {
    if (!serviceStatus.serviceHealthy || !selectedSessionId || authPhase !== "ready" || !subtaskPrompt.trim()) return;
    try {
      const result = await runSubtask(selectedSessionId, model, subtaskPrompt, subtaskTitle, repoPath);
      setSessions((current) => [result.session, ...safeArray(current).filter((item) => item.id !== result.session.id)]);
      pushLog(`subtask created: ${result.session.title}`);
    } catch (error) {
      pushLog(`subtask failed: ${(error as Error).message}`);
    }
  }

  async function handlePermission(decision: "allow_once" | "allow_session" | "deny") {
    if (!pendingPermission) return;
    await decidePermission(pendingPermission.id, decision);
    setPendingPermission(null);
  }

  const statusLabel =
    bootPhase === "loading"
      ? "Checking backend"
      : serviceStatus.mode === "healthy"
        ? "Healthy"
        : serviceStatus.mode === "degraded"
          ? "Degraded mode"
          : "Offline mode";

  const statusClass =
    bootPhase === "loading" ? "degraded" : serviceStatus.mode === "healthy" ? "healthy" : serviceStatus.mode;
  const browserAuthNeedsSetup = !window.desktopApi?.runtimeConfig && (authPhase === "missing" || authPhase === "invalid");
  const modelSelectDisabled = !serviceStatus.ollamaReachable || authPhase !== "ready";
  const composerDisabled =
    !serviceStatus.serviceHealthy || !serviceStatus.ollamaReachable || !serviceStatus.mongoAvailable || authPhase !== "ready";

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="sidebar-header">
          <h2>Sessions</h2>
          <button
            onClick={handleNewSession}
            disabled={!serviceStatus.mongoAvailable || !serviceStatus.serviceHealthy || authPhase !== "ready"}
          >
            New
          </button>
        </div>
        <div className="session-list">
          {safeSessions.length === 0 && (
            <span>
              {browserAuthNeedsSetup
                ? "Session history is locked until browser auth is configured."
                : serviceStatus.mongoAvailable
                  ? "No saved sessions yet."
                  : "Session history unavailable."}
            </span>
          )}
          {safeSessions.map((session) => (
            <button
              key={session.id}
              className={session.id === selectedSessionId ? "session active" : "session"}
              onClick={() => setSelectedSessionId(session.id)}
            >
              <span>{session.title}</span>
            </button>
          ))}
        </div>
      </aside>

      <main className="main-panel">
        <header className="topbar">
          <div>
            <h1>Personal Assistant</h1>
            <p>
              {selectedSession?.title ??
                (browserAuthNeedsSetup
                  ? "Browser auth setup required for protected API access"
                  : serviceStatus.mongoAvailable
                    ? "No session selected"
                    : "Persistence unavailable in current mode")}
            </p>
          </div>
          <div className="toolbar">
            <select value={model} onChange={(event) => setModel(event.target.value)} disabled={modelSelectDisabled}>
              {safeModels.length === 0 && <option value={model}>{browserAuthNeedsSetup ? "Configure browser auth" : "No models loaded"}</option>}
              {safeModels.map((item) => (
                <option key={item.name} value={item.name}>
                  {item.name}
                </option>
              ))}
            </select>
            <button onClick={handlePickRepo}>Pick Repo</button>
            <button onClick={handleIndexRepo} disabled={!repoPath || !serviceStatus.neo4jAvailable}>
              Index Repo
            </button>
          </div>
        </header>

        <section className={`status-banner ${statusClass}`}>
          <strong>{statusLabel}</strong>
          <span>
            MongoDB: {serviceStatus.mongoAvailable ? "up" : "down"} - Neo4j: {serviceStatus.neo4jAvailable ? "up" : "down"} - Ollama:
            {serviceStatus.ollamaReachable ? "up" : "down"}
          </span>
          {safeStatusMessages.length > 0 && <span>{safeStatusMessages.join(" | ")}</span>}
          {bootPhase === "loading" && <span>Waiting for the backend to report its final state...</span>}
          {browserAuthNeedsSetup && <span>{authMessage}</span>}
        </section>

        <section className="content-grid">
          <div className="chat-panel">
            <div className="messages">
              {safeMessages.length === 0 && (
                <div className="message assistant">
                  <div className="message-role">assistant</div>
                  <pre>
                    {bootPhase === "offline"
                      ? "Backend is offline right now. We'll reconnect automatically when it comes back."
                      : browserAuthNeedsSetup
                        ? "Backend is up, but browser auth is missing or invalid. Set VITE_APP_AUTH_TOKEN and restart Vite."
                      : "Ask the assistant to inspect, explain, or edit your codebase."}
                  </pre>
                </div>
              )}
              {safeMessages.map((message) => (
                <div key={message.id} className={`message ${message.role}`}>
                  <div className="message-role">{message.role}</div>
                  {renderMessageParts(message)}
                </div>
              ))}
            </div>
            <div className="composer">
              <textarea
                ref={composerRef}
                value={prompt}
                onChange={(event) => setPrompt(event.target.value)}
                onKeyDown={handlePromptKeyDown}
                placeholder="Ask the assistant to inspect, edit, or explain your codebase..."
                disabled={composerDisabled}
              />
              {commandMatches.length > 0 && (
                <div className="command-palette">
                  {commandMatches.map((command: CommandDefinition, index) => (
                    <button
                      key={command.id}
                      className={index === selectedCommandIndex ? "command-option active" : "command-option"}
                      onClick={() => applyCommandSuggestion(command)}
                      disabled={composerDisabled}
                    >
                      <strong>/{command.id}</strong>
                      <span>{command.description}</span>
                      {command.usage && <code>{command.usage}</code>}
                      {safeArray(command.arguments).length > 0 && (
                        <small>
                          {safeArray(command.arguments)
                            .map((item) => `${item.required ? "*" : ""}${item.name}`)
                            .join(" · ")}
                        </small>
                      )}
                    </button>
                  ))}
                </div>
              )}
              <button
                onClick={handleSend}
                disabled={composerDisabled}
              >
                Send
              </button>
            </div>
          </div>

          <div className="utility-panel">
            <div className="card">
              <h3>Repo</h3>
              <p>{repoPath || "No repo selected"}</p>
              {repoPath && <p>Paste this path or use `/analyze`, `/ls`, `/read`, `/index`, or `/shell` for repo-aware actions.</p>}
              {repoContext && (
                <div className="repo-summary">
                  <p>Indexed files: {repoContext.indexedFiles}</p>
                  <p>Reference edges: {repoContext.referenceEdges}</p>
                  <p>Symbol reference edges: {repoContext.symbolReferenceEdges}</p>
                  <p>Touched files tracked: {repoContext.touchedFiles.length}</p>
                  <p>Child sessions: {repoContext.childSessions.length}</p>
                  <p>Lineage sessions: {repoContext.lineageSessions.length}</p>
                  <p>Recent subtasks: {safeArray(repoContext.recentSubtasks).length}</p>
                  <p>Memory nodes: {repoContext.memories.length}</p>
                </div>
              )}
              {!serviceStatus.neo4jAvailable && <p>Graph indexing is unavailable until Neo4j comes back.</p>}
            </div>
            <div className="card">
              <h3>Commands</h3>
              {selectedCommandMatch && (
                <div className="command-detail">
                  <strong>/{selectedCommandMatch.id}</strong>
                  <span>{selectedCommandMatch.description}</span>
                  {selectedCommandMatch.usage && <code>{selectedCommandMatch.usage}</code>}
                  {safeArray(selectedCommandMatch.arguments).length > 0 && (
                    <div className="command-arguments">
                      {safeArray(selectedCommandMatch.arguments).map((item) => (
                        <span key={item.name} className={item.required ? "required" : ""}>
                          {item.required ? "*" : ""}{item.name}
                        </span>
                      ))}
                    </div>
                  )}
                  <small>Keyboard: ↑ ↓ to browse · Tab or Enter to apply · Esc to collapse</small>
                </div>
              )}
              <div className="command-list">
                {safeCommands.slice(0, 10).map((command: CommandDefinition) => (
                  <button key={command.id} className="command-list-item" onClick={() => applyCommandSuggestion(command)}>
                    <strong>/{command.id}</strong>
                    <span>{command.description}</span>
                    {command.usage && <code>{command.usage}</code>}
                  </button>
                ))}
                {safeCommands.length === 0 && <p>No commands loaded.</p>}
              </div>
            </div>
            <div className="card">
              <h3>Shell Tool</h3>
              <input value={shellCommand} onChange={(event) => setShellCommand(event.target.value)} />
              <button onClick={handleShell} disabled={!selectedSessionId || !serviceStatus.serviceHealthy || authPhase !== "ready"}>
                Run shell command
              </button>
            </div>
            <div className="card">
              <h3>File Edit</h3>
              <input value={editPath} onChange={(event) => setEditPath(event.target.value)} placeholder="File path" />
              <textarea value={editSearch} onChange={(event) => setEditSearch(event.target.value)} placeholder="Search text" />
              <textarea value={editReplace} onChange={(event) => setEditReplace(event.target.value)} placeholder="Replace text" />
              <button onClick={handleEditFile} disabled={!selectedSessionId || !serviceStatus.serviceHealthy || authPhase !== "ready"}>
                Apply edit
              </button>
            </div>
            <div className="card">
              <h3>Apply Patch</h3>
              <input
                value={patchPath}
                onChange={(event) => setPatchPath(event.target.value)}
                placeholder="Optional file path (blank when patch includes file markers)"
              />
              <textarea
                value={patchText}
                onChange={(event) => setPatchText(event.target.value)}
                placeholder={"*** FILE: src/example.ts\n<<<<<<< SEARCH\nold text\n=======\nnew text\n>>>>>>> REPLACE"}
              />
              <button onClick={handleApplyPatch} disabled={!selectedSessionId || !serviceStatus.serviceHealthy || authPhase !== "ready"}>
                Apply patch
              </button>
            </div>
            <div className="card">
              <h3>Diagnostics</h3>
              <input
                value={diagnosticPath}
                onChange={(event) => setDiagnosticPath(event.target.value)}
                placeholder={repoPath ? "Leave blank to use selected repo/file path" : "Enter file path"}
              />
              <button onClick={handleDiagnostics} disabled={!serviceStatus.serviceHealthy || authPhase !== "ready"}>
                Run diagnostics
              </button>
              <div className="mini-list">
                {safeDiagnostics.length === 0 && <p>No diagnostics loaded.</p>}
                {safeDiagnostics.slice(0, 10).map((item, index) => (
                  <pre key={`${item.path}-${item.line}-${index}`}>{`${item.severity.toUpperCase()} ${item.path}:${item.line}:${item.character} ${item.message}`}</pre>
                ))}
              </div>
            </div>
            <div className="card">
              <h3>LSP Code Intel</h3>
              <input
                value={symbolPath}
                onChange={(event) => setSymbolPath(event.target.value)}
                placeholder={repoPath ? "Leave blank to use repo/file path" : "Enter file path"}
              />
              <div className="inline-fields">
                <input value={codeIntelLine} onChange={(event) => setCodeIntelLine(event.target.value)} placeholder="Line" />
                <input value={codeIntelCharacter} onChange={(event) => setCodeIntelCharacter(event.target.value)} placeholder="Character" />
              </div>
              <div className="inline-actions">
                <button onClick={handleSymbols} disabled={!serviceStatus.serviceHealthy || authPhase !== "ready"}>
                  Symbols
                </button>
                <button onClick={handleWorkspaceSymbols} disabled={!serviceStatus.serviceHealthy || authPhase !== "ready" || !repoPath}>
                  Workspace
                </button>
                <button onClick={handleWorkspaceDefinitions} disabled={!serviceStatus.serviceHealthy || authPhase !== "ready" || !repoPath}>
                  WS Defs
                </button>
                <button onClick={handleWorkspaceReferences} disabled={!serviceStatus.serviceHealthy || authPhase !== "ready" || !repoPath}>
                  WS Refs
                </button>
                <button onClick={handleDefinitions} disabled={!serviceStatus.serviceHealthy || authPhase !== "ready"}>
                  Definition
                </button>
                <button onClick={handleReferences} disabled={!serviceStatus.serviceHealthy || authPhase !== "ready"}>
                  References
                </button>
              </div>
              <input
                value={workspaceSymbolQuery}
                onChange={(event) => setWorkspaceSymbolQuery(event.target.value)}
                placeholder="Workspace symbol query"
              />
              <div className="mini-list">
                {safeDocumentSymbols.slice(0, 8).map((item, index) => (
                  <pre key={`${item.path}-${item.name}-${index}`}>{`${item.kind} ${item.name}\n${item.path}:${item.line}:${item.character}`}</pre>
                ))}
                {safeWorkspaceSymbols.slice(0, 10).map((item, index) => (
                  <pre key={`${item.path}-${item.name}-${index}`}>{`WS ${item.kind} ${item.name}\n${item.path}:${item.line}:${item.character}`}</pre>
                ))}
                {safeDefinitions.slice(0, 5).map((item, index) => (
                  <pre key={`${item.path}-${item.line}-${index}`}>{`DEF ${item.path}:${item.line}:${item.character}\n${item.preview ?? ""}`}</pre>
                ))}
                {safeReferences.slice(0, 5).map((item, index) => (
                  <pre key={`${item.path}-${item.line}-${index}`}>{`REF ${item.path}:${item.line}:${item.character}\n${item.preview ?? ""}`}</pre>
                ))}
                {safeDocumentSymbols.length === 0 && safeWorkspaceSymbols.length === 0 && safeDefinitions.length === 0 && safeReferences.length === 0 && <p>No code-intel results loaded.</p>}
              </div>
            </div>
            <div className="card">
              <h3>MCP Tools</h3>
              <div className="mini-list">
                {safeMCPServerStatuses.slice(0, 8).map((item, index) => (
                  <div key={`${item.server}-${index}`} className={`status-row ${item.reachable ? "healthy" : "offline"}`}>
                    <strong>{item.server}</strong>
                    <span>{item.transport.toUpperCase()} · {item.reachable ? "reachable" : "offline"} · {item.toolCount} tools{item.latencyMs ? ` · ${item.latencyMs}ms` : ""}</span>
                    {item.endpoint && <code>{item.endpoint}</code>}
                    {item.detail && <small>{item.detail}</small>}
                    {safeArray(item.toolNames).length > 0 && <small>{safeArray(item.toolNames).join(" · ")}</small>}
                    {item.error && <pre>{item.error}</pre>}
                  </div>
                ))}
                {safeMCPTools.length === 0 && <p>No MCP tools discovered.</p>}
                {safeMCPTools.slice(0, 12).map((item, index) => (
                  <div key={`${item.server}-${item.name}-${index}`} className="status-row neutral">
                    <strong>{decodeToolName(`mcp_${item.server}__${item.name}`).label}</strong>
                    <span>{item.transport ? `${item.transport.toUpperCase()} transport` : "MCP tool"}</span>
                    {item.description && <pre>{item.description}</pre>}
                  </div>
                ))}
              </div>
            </div>
            <div className="card">
              <h3>Tool Activity</h3>
              <div className="mini-list">
                {safeToolActivities.length === 0 && <p>No recent tool activity.</p>}
                {safeToolActivities.map((item) => renderToolActivity(item))}
              </div>
            </div>
            <div className="card">
              <h3>Approvals</h3>
              <div className="mini-list">
                {!latestApproval && <p>No approval activity yet.</p>}
                {latestApproval && (
                  <div className={`status-row ${statusTone(latestApproval.status)}`}>
                    <strong>{latestApproval.toolName}</strong>
                    <span>{latestApproval.status.replaceAll("_", " ")}</span>
                    {latestApproval.path && <code>{latestApproval.path}</code>}
                    {pendingPermission?.description && latestApproval.requestId === pendingPermission.id && <small>{pendingPermission.description}</small>}
                  </div>
                )}
              </div>
            </div>
            <div className="card">
              <h3>Runtime</h3>
              <div className="mini-list">
                {safeRuntimeEvents.length === 0 && <p>No runtime state yet.</p>}
                {safeRuntimeEvents.map((entry, index) => (
                  <div key={`${entry}-${index}`} className={`status-row ${statusTone(formatRuntimeHeadline(entry).headline)}`}>
                    <strong>{formatRuntimeHeadline(entry).headline ?? "event"}</strong>
                    {formatRuntimeHeadline(entry).detail && <span>{formatRuntimeHeadline(entry).detail}</span>}
                    <pre>{entry}</pre>
                  </div>
                ))}
              </div>
            </div>
            <div className="card">
              <h3>Continuation</h3>
              <div className="mini-list">
                {!latestContinuation && <p>No continuation summary in use yet.</p>}
                {latestContinuation && (
                  <div className="status-row neutral">
                    <strong>{latestContinuation.summaryMessageId ?? "summary"}</strong>
                    <span>Depth: {latestContinuation.summaryDepth ?? 1} · Compacted: {latestContinuation.compactedMessages ?? 0} · Recent: {latestContinuation.recentMessages ?? 0}</span>
                  </div>
                )}
              </div>
            </div>
            <div className="card">
              <h3>Subtask</h3>
              <input value={subtaskTitle} onChange={(event) => setSubtaskTitle(event.target.value)} placeholder="Subtask title" />
              <textarea value={subtaskPrompt} onChange={(event) => setSubtaskPrompt(event.target.value)} placeholder="Focused subtask prompt" />
              <button onClick={handleSubtask} disabled={!selectedSessionId || !serviceStatus.serviceHealthy || authPhase !== "ready"}>
                Run subtask
              </button>
            </div>
            {repoContext && (
              <div className="card">
                <h3>Repo Graph</h3>
                <div className="mini-list">
                  <pre>{`Repo: ${repoContext.repoPath}\nIndexed: ${repoContext.indexedFiles}\nReferences: ${repoContext.referenceEdges}\nSymbol refs: ${repoContext.symbolReferenceEdges}`}</pre>
                  {repoContext.touchedFiles.slice(0, 8).map((item, index) => (
                    <pre key={`${item}-${index}`}>{item}</pre>
                  ))}
                  {repoContext.childSessions.slice(0, 6).map((item, index) => (
                    <pre key={`${item}-${index}`}>{`child session: ${item}`}</pre>
                  ))}
                  {safeArray(repoContext.recentSubtasks).slice(0, 4).map((item, index) => (
                    <pre key={`${item.sessionId}-${index}`}>{`subtask: ${item.title}\n${item.summary}`}</pre>
                  ))}
                  {repoContext.lineageSessions.slice(0, 6).map((item, index) => (
                    <pre key={`${item}-${index}`}>{`lineage session: ${item}`}</pre>
                  ))}
                  {repoContext.memories.slice(0, 6).map((item) => (
                    <pre key={item.id}>{`${item.kind}: ${item.label}`}</pre>
                  ))}
                </div>
              </div>
            )}
            <div className="card">
              <h3>Retrieval Preview</h3>
              <div className="mini-list">
                {!retrievalPreview && <p>Type a repo-aware prompt to preview ranked context.</p>}
                {safeRetrievalFileMatches.slice(0, 8).map((item, index) => (
                  <pre key={`${item.path}-${index}`}>{`FILE ${item.score} ${item.path}\n${safeArray(item.reasons).join(" · ")}`}</pre>
                ))}
                {safeRetrievalSymbolMatches.slice(0, 6).map((item, index) => (
                  <pre key={`${item.filePath}-${item.name}-${index}`}>{`SYMBOL ${item.score ?? 0} ${item.name} (${item.kind})\n${item.filePath}:${item.line}\n${safeArray(item.reasons).join(" · ")}`}</pre>
                ))}
                {safeRetrievalMemoryMatches.slice(0, 5).map((item) => (
                  <pre key={item.id}>{`MEMORY ${item.score} ${item.kind}: ${item.label}\n${safeArray(item.reasons).join(" · ")}`}</pre>
                ))}
                {safeRetrievalSnippets.slice(0, 3).map((item, index) => (
                  <pre key={`snippet-${index}`}>{item}</pre>
                ))}
              </div>
            </div>
            <div className="card logs-card">
              <h3>Logs</h3>
              <div className="logs">
                {safeLogs.map((entry, index) => (
                  <pre key={`${entry}-${index}`}>{entry}</pre>
                ))}
              </div>
            </div>
          </div>
        </section>
      </main>

      {pendingPermission && (
        <div className="modal-backdrop">
          <div className="modal">
            <h3>Permission required</h3>
            <p className="permission-meta">
              <strong>{pendingPermission.toolName}</strong> · {pendingPermission.action}
            </p>
            <p>{pendingPermission.description}</p>
            {permissionPaths.length > 0 && (
              <div className="permission-paths">
                <strong>Affected paths</strong>
                <ul>
                  {permissionPaths.map((item, index) => (
                    <li key={`${item}-${index}`}>{item}</li>
                  ))}
                </ul>
              </div>
            )}
            {pendingPermission.preview && <pre className="permission-preview">{pendingPermission.preview}</pre>}
            {pendingPermission.params && <pre>{pendingPermission.params}</pre>}
            <div className="modal-actions">
              <button onClick={() => handlePermission("allow_once")}>Allow once</button>
              <button onClick={() => handlePermission("allow_session")}>Allow session</button>
              <button onClick={() => handlePermission("deny")}>Deny</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}


