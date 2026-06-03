import type {
  ApiErrorResponse,
  Message,
  PermissionRequest,
  ServiceStatus,
  Session
} from "@assistant/shared";

export class StatusFetchError extends Error {
  kind: "network" | "http" | "invalid_json";

  constructor(kind: "network" | "http" | "invalid_json", message: string) {
    super(message);
    this.name = "StatusFetchError";
    this.kind = kind;
  }
}

export class ProtectedApiAuthError extends Error {
  kind: "missing" | "invalid";

  constructor(kind: "missing" | "invalid", message: string) {
    super(message);
    this.name = "ProtectedApiAuthError";
    this.kind = kind;
  }
}

function normalizeConfigValue(value?: string) {
  if (!value) {
    return "";
  }

  const trimmed = value.trim();
  if (!trimmed) {
    return "";
  }

  if (trimmed.startsWith("%VITE_") && trimmed.endsWith("%")) {
    return "";
  }

  return trimmed;
}

function runtimeConfig() {
  const desktopRuntime = window.desktopApi?.runtimeConfig;
  const browserRuntime = window.__APP_CONFIG__;

  return {
    apiBaseUrl:
      normalizeConfigValue(desktopRuntime?.apiBaseUrl) ||
      normalizeConfigValue(browserRuntime?.apiBaseUrl) ||
      "http://127.0.0.1:8088",
    authToken:
      normalizeConfigValue(desktopRuntime?.authToken) ||
      normalizeConfigValue(browserRuntime?.authToken)
  };
}

function requireProtectedAuthToken() {
  const config = runtimeConfig();
  if (!config.authToken) {
    throw new ProtectedApiAuthError(
      "missing",
      "Missing browser dev auth token. Set VITE_APP_AUTH_TOKEN to match APP_AUTH_TOKEN."
    );
  }
  return config.authToken;
}

function headers() {
  const authToken = requireProtectedAuthToken();
  return {
    "Content-Type": "application/json",
    Authorization: `Bearer ${authToken}`
  };
}

async function parseJSONOrThrow<T>(response: Response): Promise<T> {
  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    const apiError = payload as ApiErrorResponse | null;
    if (response.status === 401) {
      throw new ProtectedApiAuthError(
        "invalid",
        apiError?.error?.message ?? "Invalid browser dev auth token. Check VITE_APP_AUTH_TOKEN."
      );
    }
    throw new Error(apiError?.error?.message ?? response.statusText);
  }
  return payload as T;
}

export async function listSessions(): Promise<Session[]> {
  const response = await fetch(`${resolvedBaseUrl}/api/sessions`, { headers: headers() });
  return parseJSONOrThrow<Session[]>(response);
}

export async function createSession(title: string): Promise<Session> {
  const response = await fetch(`${resolvedBaseUrl}/api/sessions`, {
    method: "POST",
    headers: headers(),
    body: JSON.stringify({ title })
  });
  return parseJSONOrThrow<Session>(response);
}

export async function listMessages(sessionId: string): Promise<Message[]> {
  const response = await fetch(`${resolvedBaseUrl}/api/messages?sessionId=${encodeURIComponent(sessionId)}`, {
    headers: headers()
  });
  return parseJSONOrThrow<Message[]>(response);
}

export async function runAgent(sessionId: string, prompt: string, model: string) {
  const response = await fetch(`${resolvedBaseUrl}/api/agent/run`, {
    method: "POST",
    headers: headers(),
    body: JSON.stringify({ sessionId, prompt, model })
  });
  return parseJSONOrThrow<Message>(response);
}

export async function listModels(): Promise<Array<{ name: string }>> {
  const response = await fetch(`${resolvedBaseUrl}/api/models`, {
    headers: headers()
  });
  return parseJSONOrThrow<Array<{ name: string }>>(response);
}

export async function decidePermission(requestId: string, decision: "allow_once" | "allow_session" | "deny") {
  const response = await fetch(`${resolvedBaseUrl}/api/permissions/decide`, {
    method: "POST",
    headers: headers(),
    body: JSON.stringify({ requestId, decision })
  });
  await parseJSONOrThrow<{ ok: boolean }>(response);
}

export async function runShell(sessionId: string, command: string, cwd: string) {
  const response = await fetch(`${resolvedBaseUrl}/api/tools/shell`, {
    method: "POST",
    headers: headers(),
    body: JSON.stringify({ sessionId, command, cwd })
  });
  return parseJSONOrThrow<{ output?: string; error?: string }>(response);
}

export async function indexRepo(repoPath: string) {
  const response = await fetch(`${resolvedBaseUrl}/api/repo/index`, {
    method: "POST",
    headers: headers(),
    body: JSON.stringify({ repoPath })
  });
  return parseJSONOrThrow<{ started: boolean }>(response);
}

export function connectEvents(onEvent: (event: { type: string; data: any }) => void) {
  const { apiBaseUrl } = runtimeConfig();
  const authToken = requireProtectedAuthToken();
  const socket = new WebSocket(`${apiBaseUrl.replace("http", "ws")}/ws?token=${encodeURIComponent(authToken)}`);
  socket.onmessage = (event) => {
    onEvent(JSON.parse(event.data));
  };
  return socket;
}

export async function getServiceStatus(): Promise<ServiceStatus> {
  const { apiBaseUrl } = runtimeConfig();
  let response: Response;
  try {
    response = await fetch(`${apiBaseUrl}/status`);
  } catch {
    throw new StatusFetchError("network", "Backend unavailable");
  }

  if (!response.ok) {
    const payload = await response.json().catch(() => null);
    const apiError = payload as ApiErrorResponse | null;
    throw new StatusFetchError("http", apiError?.error?.message ?? response.statusText);
  }

  const payload = await response.json().catch(() => null);
  if (!payload) {
    throw new StatusFetchError("invalid_json", "Backend status unreadable");
  }

  return payload as ServiceStatus;
}

const resolvedBaseUrl = runtimeConfig().apiBaseUrl;

export type { PermissionRequest };
