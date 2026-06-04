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
| Agent loop | Partial | Ollama chat loop now supports tool calls and repeated tool execution |
| Permission flow | Implemented | Shell, read, write, edit, and patch tools prompt for approval with path-aware previews |
| Edit/patch workflow | Partial | Exact edits, diff previews, multi-file patches, and `Begin Patch` update/add/delete envelopes are implemented; broader upstream semantics still remain |
| Tool activity | Partial | Tool lifecycle events now render with path, summary, status, and output details in the desktop activity panel |
| Repo path analysis | Implemented | Raw pasted paths and `/analyze` route into repo/file analysis |
| Repo indexing | Partial | File graph indexing now stores directories, file metadata, import-derived file references, and basic symbols; agent context also includes prompt-relevant snippets |
| Neo4j memory | Partial | Session/file linking, child-session links, session-linked memory nodes, lineage-aware retrieval, and ranked retrieval previews now exist; richer semantic retrieval still pending |
| LSP diagnostics | Partial | StdIO LSP diagnostics now support repo-wide diagnostics plus workspace symbol search in addition to document symbols, definitions, and references |
| MCP tools | Partial | MCP now supports transport-aware config, stdio + HTTP probing/invocation, and explicit status reporting; SSE is still not yet supported |
| Auto compact | Partial | Long sessions now summarize older turns into continuation summaries |
| Nested agent tasks | Partial | Child-session subtask execution is wired through commands, API, and tool loop |

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

1. Deeper workspace-aware LSP behavior beyond scanned document-symbol aggregation
2. MCP SSE transport support and broader remote-server compatibility
3. Summary continuation with stronger summary lineage + memory weighting
4. Richer repo graph edges for true symbol/reference relationships
5. Desktop command palette and activity stream closer to upstream UX
6. Diff preview polish and broader upstream patch semantics closer to upstream
