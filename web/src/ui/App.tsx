import { useEffect, useMemo, useRef, useState } from "react";
import type { Message, PermissionRequest, ServiceStatus, Session } from "@assistant/shared";
import {
  connectEvents,
  createSession,
  decidePermission,
  getServiceStatus,
  indexRepo,
  listMessages,
  listModels,
  listSessions,
  ProtectedApiAuthError,
  runAgent,
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
      return "";
    })
    .join("");
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
  const [model, setModel] = useState("llama3.1");
  const [prompt, setPrompt] = useState("");
  const [shellCommand, setShellCommand] = useState("pwd");
  const [repoPath, setRepoPath] = useState("");
  const [pendingPermission, setPendingPermission] = useState<PermissionRequest | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [bootPhase, setBootPhase] = useState<BootPhase>(
    runtimeBootStatus === "healthy" ? "healthy" : runtimeBootStatus === "degraded" ? "degraded" : "loading"
  );
  const [authPhase, setAuthPhase] = useState<AuthPhase>("unknown");
  const [authMessage, setAuthMessage] = useState("");
  const [serviceStatus, setServiceStatus] = useState<ServiceStatus>(initialStatus(runtimeBootStatus, runtimeBootMessage));
  const eventSocketRef = useRef<WebSocket | null>(null);
  const safeSessions = safeArray(sessions);
  const safeMessages = safeArray(messages);
  const safeModels = safeArray(models);
  const safeStatusMessages = safeArray(serviceStatus.messages);
  const safeLogs = safeArray(logs);

  const selectedSession = useMemo(
    () => safeSessions.find((item) => item.id === selectedSessionId) ?? null,
    [safeSessions, selectedSessionId]
  );

  function pushLog(entry: string) {
    setLogs((current) => {
      const safeCurrent = Array.isArray(current) ? current : [];
      if (safeCurrent[0] === entry) {
        return safeCurrent;
      }
      return [entry, ...safeCurrent].slice(0, 100);
    });
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
          await Promise.all([loadSessionsIfAvailable(status), loadModelsIfAvailable(status)]);
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
  }, [authPhase]);

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
        setPendingPermission(event.data as PermissionRequest);
      }

      if (event.type === "repo.index.status") {
        const payload = event.data as { state: string; indexedFiles?: number; message?: string };
        pushLog(`repo index: ${payload.state} ${payload.indexedFiles ?? ""} ${payload.message ?? ""}`.trim());
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
    await runAgent(sessionId, nextPrompt, model);
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

  async function handlePickRepo() {
    const picked = await window.desktopApi?.pickDirectory?.();
    if (picked) setRepoPath(picked);
  }

  async function handleIndexRepo() {
    if (!repoPath || !serviceStatus.neo4jAvailable) return;
    await indexRepo(repoPath);
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
                  <pre>{extractText(message) || JSON.stringify(message.parts, null, 2)}</pre>
                </div>
              ))}
            </div>
            <div className="composer">
              <textarea
                value={prompt}
                onChange={(event) => setPrompt(event.target.value)}
                placeholder="Ask the assistant to inspect, edit, or explain your codebase..."
                disabled={!serviceStatus.serviceHealthy || !serviceStatus.ollamaReachable || !serviceStatus.mongoAvailable || authPhase !== "ready"}
              />
              <button
                onClick={handleSend}
                disabled={
                  !serviceStatus.serviceHealthy ||
                  !serviceStatus.ollamaReachable ||
                  !serviceStatus.mongoAvailable ||
                  authPhase !== "ready"
                }
              >
                Send
              </button>
            </div>
          </div>

          <div className="utility-panel">
            <div className="card">
              <h3>Repo</h3>
              <p>{repoPath || "No repo selected"}</p>
              {!serviceStatus.neo4jAvailable && <p>Graph indexing is unavailable until Neo4j comes back.</p>}
            </div>
            <div className="card">
              <h3>Shell Tool</h3>
              <input value={shellCommand} onChange={(event) => setShellCommand(event.target.value)} />
              <button onClick={handleShell} disabled={!selectedSessionId || !serviceStatus.serviceHealthy || authPhase !== "ready"}>
                Run shell command
              </button>
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
            <p>{pendingPermission.description}</p>
            <pre>{pendingPermission.params}</pre>
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

