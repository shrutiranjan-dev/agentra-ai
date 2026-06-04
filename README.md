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

## LSP, MCP, and auto-compact

- `LSP_CONFIG_PATH` defaults to `.opencode/lsp.json`
- `MCP_CONFIG_PATH` defaults to `.opencode/mcp.json`
- `.opencode/config.json.example` shows optional user/project config overlays
- example files are included at `.opencode/lsp.json.example` and `.opencode/mcp.json.example`
- `AUTO_COMPACT=true` enables conversation compaction
- `AUTO_COMPACT_MESSAGE_LIMIT` controls when older conversation turns are summarized
- `AUTO_COMPACT_PRESERVE_RECENT` controls how many recent messages stay uncompressed

Config precedence:

1. environment variables
2. `%USERPROFILE%\.opencode\config.json`
3. `<repo>\.opencode\config.json`

The config overlay can override:

- default model
- LSP config path
- MCP config path
- auto-compact behavior
- extra custom command directories

MCP transport support in this desktop runtime:

- `stdio` supported
- `http` supported for JSON-RPC style MCP endpoints
- `sse` reported clearly as not yet supported

New built-in commands:

- `/diagnostics <file-path>` runs LSP diagnostics for a file
- `/symbols <file-path>` lists LSP document symbols
- `/workspace-symbols [query]` searches symbols across the active repository
- `/definition <path> <line> <character>` finds an LSP definition
- `/references <path> <line> <character>` finds LSP references
- `/mcp` lists configured MCP tools
- `/mcp-status` shows MCP server reachability and transport details
- `/compact` forces a session compaction pass
- `/task [title] ::: <prompt>` runs a focused subtask in a child session
- `/edit <path> ::: <search> ::: <replace>` applies a single exact edit
- `/patch <path>` accepts SEARCH/REPLACE blocks on following lines
- `apply_patch` also accepts `*** Begin Patch` envelopes with `*** Update File:`, `*** Add File:`, and `*** Delete File:` sections
- the desktop utility column now shows diagnostics results, MCP-discovered tools, live tool activity, and repo graph context
- the desktop utility column now also shows workspace symbol search, MCP server status, LSP code-intel results, and a subtask runner
- the desktop utility column now also exposes direct file edit and patch testing
- patch approvals now include affected paths plus diff-style previews
- `apply_patch` can target multiple files when the patch body includes `*** FILE: <path>` sections
- the tool activity panel now shows richer per-tool status, path, summary, and output details
- the desktop utility column now also shows a retrieval preview with ranked files, symbols, memories, and snippets for the current prompt

The agent tool loop can also call:

- `diagnostics`
- configured MCP tools discovered from `.opencode/mcp.json`

## Custom command format

Custom commands load from:

- `%USERPROFILE%\.opencode\commands\`
- `<repo>\.opencode\commands\`
- any extra directories listed in `.opencode/config.json`

Supported template placeholders:

- `$ARGUMENTS`
- `$1`, `$2`, ...
- `{{path}}`
- `{{focus|default}}`
- `$path`
- `${path}`

Supported invocation styles:

- `/project:review --path src/app.ts --focus auth`
- `/project:review --path=src/app.ts`
- `/project:review path=src/app.ts`

Optional frontmatter:

```md
---
title: Review File
description: Review a file with extra context
arg: path|required|Path to inspect
arg: focus|optional|Focus area|tests
---
Please review {{path}} with focus {{focus|general}}.
```

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

This repo now delivers a stronger OpenCode-style desktop core:

- shared API/event contracts
- Electron + React desktop shell
- Go runtime with REST, WebSocket, session APIs, approval APIs, Ollama model listing, slash commands, tool-aware agent execution, and CLI prompt mode
- MongoDB persistence for sessions/messages/approvals/tool runs/settings
- Neo4j wiring for repo/memory indexing primitives
- Neo4j session graph links for parent/child subtasks and session-linked memories
- Docker Compose for MongoDB + Neo4j
- pasted-path analysis for local files and repositories
- desktop command discovery for built-in and `.opencode/commands` custom commands
- LSP diagnostics tool integration
- LSP document symbols, definitions, and references endpoints
- MCP stdio tool discovery and invocation
- auto-compact summary generation for long sessions
- child-session subtask execution for focused nested runs
- session-aware retrieval that blends lineage memories, touched files, and repo context into prompts
- ranked retrieval preview that explains why files, symbols, and memories were selected for prompt context
- desktop panels for tool activity, diagnostics visibility, MCP inventory, and repo graph summary
- repo indexing now stores file metadata, directory relationships, and simple import edges for better context injection
- repo indexing now also stores file-to-file reference edges resolved from local imports
- repo indexing now also stores simple symbols and injects prompt-relevant code snippets into agent context
- edit/apply-patch runtime for safer file mutations, multi-file patching, `Begin Patch`-style envelopes, diff previews, and richer tool result/activity rendering

See `PARITY.md` for the live parity matrix, including what is implemented, partially implemented, and still missing for deeper OpenCode behavioral parity.
