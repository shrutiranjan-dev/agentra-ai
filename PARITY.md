# OpenCode Parity Matrix

This project now uses a dual-baseline parity model:

- archived OpenCode for historical provenance
- live Crush for current upstream successor behavior

This project targets OpenCode/Crush-style behavior with two deliberate differences:

- desktop UI instead of terminal-first TUI
- Ollama-only local model selection

Detailed generated audit artifacts live in `docs/upstream-parity/`.

## Current parity status

| Capability | Status | Notes |
| --- | --- | --- |
| Session history | Partial | Mongo-backed with degraded in-memory fallback |
| Slash commands | Implemented | Built-ins plus `.opencode/commands` loading from user/project dirs |
| Custom commands | Partial | Markdown templates now support positional placeholders, named placeholders, frontmatter metadata, and config-driven extra command directories |
| Agent loop | Partial | Ollama chat loop now supports repeated tool execution, malformed-call validation, denial recovery, and stop-after-stall protection for repeated unsuccessful tool loops |
| Permission flow | Implemented | Shell, read, write, edit, and patch tools prompt for approval with path-aware previews |
| Edit/patch workflow | Partial | Exact edits, diff previews, multi-file patches, and `Begin Patch` update/add/delete envelopes are implemented; broader upstream semantics still remain |
| Tool activity | Partial | Tool lifecycle events now render with path, summary, status, output details, clearer runtime-state cards, and dedicated approval/continuation visibility in the desktop panel |
| Repo path analysis | Implemented | Raw pasted paths and `/analyze` route into repo/file analysis |
| Repo indexing | Partial | File graph indexing now stores directories, file metadata, import-derived file references, symbol-reference edges from LSP lookups when available, and prompt-relevant snippets |
| Neo4j memory | Partial | Session/file linking, child-session links, session-linked memory nodes, lineage-aware retrieval, ranked retrieval previews, and summary-weighted continuation retrieval now exist; richer semantic retrieval still pending |
| LSP diagnostics | Partial | StdIO LSP diagnostics now support repo-wide diagnostics, workspace symbol search, workspace-level definition/reference expansion, and graph enrichment |
| MCP tools | Partial | MCP now supports transport-aware config, stdio + HTTP + SSE probing/invocation, explicit status reporting, latency/endpoint metadata, discovered tool-name surfacing, and timeout/message-endpoint overrides; broader remote compatibility still remains |
| Auto compact | Partial | Long sessions now summarize older turns into stable continuation heads, keep summary-depth metadata, and continue surfacing active continuation state even when no fresh compaction happens on a given run |
| Nested agent tasks | Partial | Child-session subtask execution is wired through commands, API, and tool loop, and completed subtasks now feed summaries back into parent-session memory and repo lineage context |

## Implemented commands

- `/help`
- `/status`
- `/models`
- `/new [title]`
- `/repo [path]`
- `/index [path]`
- `/read <path>`
- `/ls [path]`
- `/shell <command>`
- `/analyze <path>`
- `/diagnostics <path>`
- `/symbols <path>`
- `/workspace-symbols [query]`
- `/definition <path> <line> <character>`
- `/references <path> <line> <character>`
- `/mcp`
- `/mcp-status`
- `/compact`
- `/edit <path> ::: <search> ::: <replace>`
- `/patch <path>` + multiline SEARCH/REPLACE blocks
- `/task [title] ::: <prompt>`

## Custom command locations and config

- User: `%USERPROFILE%\\.opencode\\commands\\`
- Project: `<repo>\\.opencode\\commands\\`

Each Markdown file becomes a command:

- `git/commit.md` -> `/user:git:commit` or `/project:git:commit`

Custom commands can also declare frontmatter metadata and named args, and extra command directories can be added through `.opencode/config.json`.

## Next parity targets

1. Richer custom-command semantics and a denser command-palette flow closer to upstream behavior
2. Final tool-loop edge-case polish for malformed tool calls, denial recovery, and retry/stop nuance
3. Stronger continuation semantics around summary heads, long-session carry-forward, and nested-task reuse
4. Broader remote MCP compatibility and protocol edge-case handling across more server implementations
5. Deeper workspace-level LSP intelligence and cross-file reference quality in larger repos
6. Final desktop UX density/polish so command, runtime, approvals, and activity views feel closer to upstream
