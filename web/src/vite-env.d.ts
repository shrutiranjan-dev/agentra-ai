/// <reference types="vite/client" />

declare global {
  interface Window {
    __APP_CONFIG__?: {
      apiBaseUrl: string;
      authToken: string;
    };
    desktopApi?: {
      pickDirectory: () => Promise<string | null>;
      runtimeConfig?: {
        apiBaseUrl: string;
        authToken: string;
        bootStatus: string;
        bootMessage: string;
        managedService: string;
      };
    };
  }
}

export {};
