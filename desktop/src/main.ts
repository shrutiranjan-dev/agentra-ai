import { app, BrowserWindow, dialog, ipcMain } from "electron";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

let serviceProcess: ChildProcessWithoutNullStreams | null = null;
const authToken = process.env.APP_AUTH_TOKEN ?? "local-dev-token";
const apiBaseUrl = process.env.APP_API_BASE_URL ?? "http://127.0.0.1:8088";
const webUrl = process.env.WEB_URL ?? "http://localhost:5173";
const managedService = process.env.APP_MANAGED_SERVICE === "1";

async function createWindow(bootStatus: string, bootMessage: string) {
  const win = new BrowserWindow({
    width: 1400,
    height: 900,
    minWidth: 1100,
    minHeight: 720,
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false
    }
  });

  process.env.ASSISTANT_AUTH_TOKEN = authToken;
  process.env.ASSISTANT_API_BASE_URL = apiBaseUrl;
  process.env.ASSISTANT_BOOT_STATUS = bootStatus;
  process.env.ASSISTANT_BOOT_MESSAGE = bootMessage;

  const webReachable = await isHTTPReachable(webUrl);
  if (!webReachable) {
    await win.loadURL(`data:text/html,${encodeURIComponent(devServerUnavailableHTML(webUrl))}`);
    return;
  }

  await win.loadURL(webUrl);
}

function serviceCommand() {
  const rootDir = path.resolve(__dirname, "../..");
  const serviceDir = path.resolve(__dirname, "../../service");
  const localGo = path.join(rootDir, ".tools", "go", "bin", process.platform === "win32" ? "go.exe" : "go");
  const goCommand = fs.existsSync(localGo)
    ? localGo
    : process.platform === "win32"
      ? "go.exe"
      : "go";

  return {
    command: goCommand,
    args: ["run", "./cmd/service"],
    cwd: serviceDir
  };
}

function startService() {
  if (serviceProcess) {
    return;
  }

  const { command, args, cwd } = serviceCommand();
  serviceProcess = spawn(command, args, {
    cwd,
    env: {
      ...process.env,
      APP_AUTH_TOKEN: authToken
    },
    stdio: "pipe"
  });

  serviceProcess.stdout.on("data", (chunk) => {
    console.log(`[service] ${chunk.toString()}`);
  });
  serviceProcess.stderr.on("data", (chunk) => {
    console.error(`[service:error] ${chunk.toString()}`);
  });
  serviceProcess.on("error", (error) => {
    console.error(`[service:spawn-error] ${error.message}`);
  });
  serviceProcess.on("exit", (code) => {
    void (async () => {
      const stillReachable = await isHTTPReachable(`${apiBaseUrl}/health`);
      if (stillReachable) {
        console.log(`service helper exited with code ${code}, but an existing backend is already healthy`);
      } else {
        console.log(`service exited: ${code}`);
      }
    })();
    serviceProcess = null;
  });
}

function isHTTPReachable(url: string): Promise<boolean> {
  return new Promise((resolve) => {
    const request = http.get(url, (response) => {
      resolve(response.statusCode === 200);
      response.resume();
    });
    request.on("error", () => resolve(false));
    request.setTimeout(1500, () => {
      request.destroy();
      resolve(false);
    });
  });
}

function getServiceBootStatus(): Promise<{ status: string; message: string }> {
  return new Promise((resolve) => {
    const request = http.get(`${apiBaseUrl}/status`, (response) => {
      let body = "";
      response.on("data", (chunk) => {
        body += chunk.toString();
      });
      response.on("end", () => {
        if (response.statusCode !== 200) {
          resolve({ status: "offline", message: "Backend unavailable" });
          return;
        }
        try {
          const payload = JSON.parse(body) as { mode?: string; messages?: string[] };
          resolve({
            status: payload.mode ?? "healthy",
            message: payload.messages?.join(" | ") ?? ""
          });
        } catch {
          resolve({ status: "offline", message: "Backend status unreadable" });
        }
      });
    });
    request.on("error", () => resolve({ status: "offline", message: "Backend unavailable" }));
    request.setTimeout(1500, () => {
      request.destroy();
      resolve({ status: "offline", message: "Backend unavailable" });
    });
  });
}

async function waitForServiceHealth(attempts: number, delayMs: number): Promise<boolean> {
  for (let index = 0; index < attempts; index += 1) {
    if (await isHTTPReachable(`${apiBaseUrl}/health`)) {
      return true;
    }
    await new Promise((resolve) => setTimeout(resolve, delayMs));
  }
  return false;
}

function devServerUnavailableHTML(url: string) {
  return `
  <html>
    <body style="font-family: sans-serif; background: #020617; color: white; display:flex; align-items:center; justify-content:center; height:100vh; margin:0;">
      <div style="max-width:640px; padding:24px; border:1px solid #334155; border-radius:16px; background:#0f172a;">
        <h1 style="margin-top:0;">Web Dev Server Unavailable</h1>
        <p>Electron opened correctly, but the React dev server is not reachable.</p>
        <p>Expected URL: <code>${url}</code></p>
        <p>Start it with <code>cmd /c npm run dev:web</code> from the project root.</p>
      </div>
    </body>
  </html>`;
}

function stopService() {
  if (serviceProcess) {
    serviceProcess.kill();
    serviceProcess = null;
  }
}

app.whenReady().then(async () => {
  const reachable = await isHTTPReachable(`${apiBaseUrl}/health`);
  if (!reachable && !managedService) {
    startService();
    await waitForServiceHealth(12, 500);
  }
  const boot = await getServiceBootStatus();
  await createWindow(boot.status, boot.message);
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("before-quit", () => {
  stopService();
});

ipcMain.handle("dialog:pick-directory", async () => {
  const result = await dialog.showOpenDialog({
    properties: ["openDirectory"]
  });
  if (result.canceled || result.filePaths.length === 0) {
    return null;
  }
  return result.filePaths[0];
});
