import { contextBridge, ipcRenderer } from "electron";

contextBridge.exposeInMainWorld("desktopApi", {
  pickDirectory: () => ipcRenderer.invoke("dialog:pick-directory"),
  runtimeConfig: {
    authToken: process.env.ASSISTANT_AUTH_TOKEN ?? "local-dev-token",
    apiBaseUrl: process.env.ASSISTANT_API_BASE_URL ?? "http://127.0.0.1:8088",
    bootStatus: process.env.ASSISTANT_BOOT_STATUS ?? "offline",
    bootMessage: process.env.ASSISTANT_BOOT_MESSAGE ?? "",
    managedService: process.env.APP_MANAGED_SERVICE ?? "0"
  }
});
