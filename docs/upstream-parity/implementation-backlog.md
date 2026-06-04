# Implementation Backlog

Generated: 2026-06-04T09:25:35.352Z

## Command and Config Parity
- Target behavior: Support upstream-style named command arguments, richer command templates, and config parity across local/project/user scopes.
- Current gap: Custom commands load, but argument semantics and config parity are simpler than upstream.
- Subsystem: Go command loader, shared command contracts, desktop command UI
- Acceptance criteria: Named placeholders work end-to-end, config precedence is documented, and parity audit marks command system as matched or desktop-adapted.
- Source of truth: OpenCode archive + Crush live

## Runtime and Tool Loop Parity
- Target behavior: Match upstream tool orchestration, stop/continue semantics, and richer tool result shaping.
- Current gap: Core loop exists but deeper orchestration parity is still partial.
- Subsystem: Go agent loop, tool result modeling, WebSocket event stream
- Acceptance criteria: Tool calls, denials, retries, and completions render consistently and match audit expectations.
- Source of truth: OpenCode archive + Crush live

## Approval and Edit-Flow Parity
- Target behavior: Provide upstream-grade approval semantics and diff-aware mutation review.
- Current gap: Approvals and patching are strong but not fully upstream-equivalent.
- Subsystem: Approval broker, desktop approval UI, patch/edit engine
- Acceptance criteria: Approvals expose intent, affected files, and diff previews for all risky mutations.
- Source of truth: OpenCode archive

## LSP and MCP Parity
- Target behavior: Expand from current file-scoped LSP and stdio-only MCP to richer upstream behavior.
- Current gap: Definitions/references exist, but workspace depth and MCP transport breadth are still partial.
- Subsystem: LSP client, MCP client, tool schema exposure, desktop diagnostics panels
- Acceptance criteria: Parity audit upgrades LSP and MCP classifications and documents supported transports/capabilities.
- Source of truth: Crush live

## Graph, Memory, and Retrieval Parity
- Target behavior: Use graph and session lineage to influence answers with stronger semantic ranking and symbol/reference relationships.
- Current gap: Lineage-aware retrieval exists, but symbol/reference graph semantics remain shallow.
- Subsystem: Neo4j store, repo indexer, prompt context builder
- Acceptance criteria: Repo graph stores true symbol/reference relationships and retrieval is traceably better across follow-up tasks.
- Source of truth: Current project + Crush live

## Nested Task and Continuation Parity
- Target behavior: Bring subtask and compaction behavior closer to upstream continuation semantics.
- Current gap: Child sessions exist, but summary lineage and nested-task behavior are still partial.
- Subsystem: Session model, compaction flow, memory/session linking
- Acceptance criteria: Parent/subtask context continues predictably after compaction and is surfaced in audit outputs.
- Source of truth: OpenCode archive

## Desktop UX Parity
- Target behavior: Preserve upstream information architecture in a desktop-native interface.
- Current gap: Desktop UX is solid but still less polished and less dense than upstream terminal flows.
- Subsystem: React desktop renderer, command palette, activity stream, session panels
- Acceptance criteria: Audit classifies presentation parity as desktop-adapted with no major missing interaction patterns.
- Source of truth: OpenCode archive + Crush live
