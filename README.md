# Personal OpenCode-Style Desktop Assistant

Desktop-first AI coding assistant inspired by OpenCode, rebuilt around:

- `desktop/`: Electron shell
- `web/`: React renderer
- `service/`: Go sidecar runtime + CLI
- `shared/`: shared TypeScript contracts

## Architecture

- Electron launches the Go sidecar locally.
- The sidecar exposes REST + WebSocket APIs on localhost.
- Ollama provides chat/model execution.
- MongoDB stores sessions, messages, approvals, tool runs, and settings.
- Neo4j stores repo graph + memory graph relationships.

## Development

### Recommended Windows flow

Use the scripts as the canonical local workflow:

```bat
stop-dev.bat
start-dev.bat
```

`start-dev.bat` starts Docker first, waits for MongoDB and Neo4j, starts the Go service, waits for `/health`, starts Vite, waits for `http://localhost:5173/`, and only then launches Electron with `APP_MANAGED_SERVICE=1` so Electron does not start a second backend.
It also sets `VITE_API_BASE_URL` and `VITE_APP_AUTH_TOKEN` for the browser-rendered dev UI so the web client can authenticate to the local backend.

### Manual flow

If you need to start pieces by hand, use this order:

```bash
docker compose up -d
cd service
go run ./cmd/service
cd ..
npm install
set VITE_API_BASE_URL=http://127.0.0.1:8088
set VITE_APP_AUTH_TOKEN=local-dev-token
npm run dev:web
npm run dev:desktop
```

## Supported dev modes

- Full mode: Docker + service + web + desktop
- Degraded mode: service + web + desktop without Docker
- UI-only diagnostics: web + desktop without backend

## Health checks

- Backend health: `http://127.0.0.1:8088/health`
- Backend status: `http://127.0.0.1:8088/status`
- Web UI: `http://localhost:5173/`

## Browser dev auth

Direct browser access to `http://localhost:5173/` is supported, but it must be configured explicitly.

- `VITE_API_BASE_URL` should point to the local backend, typically `http://127.0.0.1:8088`
- `VITE_APP_AUTH_TOKEN` must match the backend `APP_AUTH_TOKEN`
- Electron does not rely on these browser env vars because it receives auth through preload/runtime config

## Troubleshooting

- `bind: Only one usage of each socket address...`
  - A second backend tried to start on `127.0.0.1:8088`.
  - Use `stop-dev.bat`, then `start-dev.bat`.
  - When using `start-dev.bat`, do not manually run `go run ./cmd/service` in parallel.

- `Web Dev Server Unavailable`
  - Electron opened, but Vite is not reachable at `http://localhost:5173/`.
  - Restart with `start-dev.bat` or run `npm run dev:web` from the repo root.

- UI shows offline mode
  - Check `http://127.0.0.1:8088/status`.
  - If `/health` is up and `/status` is healthy, the renderer should recover automatically after its retry loop.
  - If not, restart with `stop-dev.bat` followed by `start-dev.bat`.

- `models unavailable: Unauthorized` or `sessions unavailable: Unauthorized`
  - The backend is reachable, but the browser dev token is missing or wrong.
  - Set `VITE_APP_AUTH_TOKEN` to the same value as `APP_AUTH_TOKEN`.
  - Restart Vite after changing the env vars.

- Degraded mode without Docker
  - The desktop app still opens, but MongoDB-backed session history and Neo4j-backed graph features are limited until Docker services return.

## Environment

The Go service reads:

- `APP_PORT` default `8088`
- `APP_HOST` default `127.0.0.1`
- `APP_AUTH_TOKEN` optional fixed token
- `OLLAMA_BASE_URL` default `http://127.0.0.1:11434`
- `MONGODB_URI` default `mongodb://localhost:27017`
- `MONGODB_DATABASE` default `assistant`
- `NEO4J_URI` default `neo4j://localhost:7687`
- `NEO4J_USERNAME` default `neo4j`
- `NEO4J_PASSWORD` default `password12345`

## Current implementation status

This first pass delivers the platform scaffold and working core:

- shared API/event contracts
- Electron + React desktop shell
- Go runtime with REST, WebSocket, session APIs, approval APIs, Ollama model listing, chat streaming, CLI prompt mode
- MongoDB persistence for sessions/messages/approvals/tool runs/settings
- Neo4j wiring for repo/memory indexing primitives
- Docker Compose for MongoDB + Neo4j

Advanced tool execution, full OpenCode-level patching flows, and deep LSP execution are scaffolded for extension rather than fully complete in this initial pass.
