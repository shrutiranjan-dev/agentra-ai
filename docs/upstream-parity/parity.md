# Upstream Parity Audit

Generated: 2026-06-04T18:17:13.638Z

## Baselines

- Current project: `my-allrounder-agent`
- Archived baseline: `opencode-archive` from https://github.com/opencode-ai/opencode
- Live successor baseline: `crush-live` from https://github.com/charmbracelet/crush

## Comparison Matrix

| Capability | Status | Source of Truth | Current | Archived OpenCode | Live Crush | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| Repo Structure and Entrypoints | desktop-adapted | both | Electron + React + Go sidecar + CLI workspace | Single Go CLI/TUI application | Single terminal-first application | Current project intentionally replaces terminal-first shell with desktop UI while keeping a CLI/debug surface. |
| Runtime Architecture | desktop-adapted | both | Local Go sidecar with REST + WebSocket loopback | In-process terminal runtime | In-process terminal runtime | Intentional divergence for desktop-first packaging. |
| Provider and Model Layer | intentionally-different | both | Ollama-only local models | Multi-provider | Multi-model / provider-flexible | Intentional product choice; parity target is behavior, not provider matrix. |
| Commands and Command Discovery | partial | archive | 23 built-in commands plus project/user command loading | Custom commands with named arguments | Terminal command workflows and extensible interaction model | Current custom commands are simpler than upstream named-argument behavior. |
| Tool Loop and Approvals | partial | both | 18 tool schemas with approval broker and patch/edit flow | Tool execution with approvals and file-change tracking | Tool-driven coding flow with richer terminal integration | Core loop exists, but deeper parity around orchestration and tool semantics is still pending. |
| Sessions, History, and Compaction | partial | both | Mongo-backed sessions with degraded fallback and auto-compact summaries | SQLite-backed sessions with auto compact | Session-based workflow | Summary lineage and continuation weighting still need work. |
| LSP Code Intelligence | partial | crush | Diagnostics, document symbols, definitions, and references | LSP integration | LSP-enhanced workspace assistance | Current implementation is file-scoped; deeper workspace parity is still missing. |
| MCP Integration | partial | crush | stdio MCP discovery and invocation | n/a or early-era tooling | http, stdio, and sse MCP support | Current project needs broader transport parity and richer error semantics. |
| Nested Task / Agent Behavior | partial | archive | Child-session subtask execution | Task-oriented agent model | Session-based workflows with composable context | Subtasks exist, but not yet full upstream-depth orchestration. |
| Repo Graph and Memory Retrieval | partial | current+crush | Neo4j graph, lineage retrieval, prompt-relevant files/symbols/memories | SQLite + tracked changes + LSP context | LSP- and context-enhanced session workflow | Current project goes beyond archived storage choices but still needs stronger semantic ranking. |
| Presentation Layer UX | partial | both | Desktop shell with command suggestions, panels, approvals, and activity cards | Bubble Tea terminal UI | Terminal-first Charm UX | Desktop adaptation is correct, but upstream-like polish and interaction density still lag. |

## Current Project Signals

- Built-in commands detected: 23
- Tool schemas detected: 18
- Desktop shell present: true
- Local Go service present: true
- CLI entrypoint present: true

## Baseline Metadata

- OpenCode archive commit: unknown
- Crush live commit: unknown
- Current project commit: e55e1c308c4e93e387917e67fc87b55412fd5aec