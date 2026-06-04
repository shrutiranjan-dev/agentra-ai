# Implementation Backlog

Generated: 2026-06-04T18:17:13.638Z

This backlog now reflects the **remaining high-value parity work** after the major foundation and runtime tranches already landed.

## Command and Config Parity
- Target behavior: Match the last layer of upstream-feel command ergonomics, including richer aliases, denser discovery flow, and clearer template guidance.
- Current gap: Named placeholders, metadata, and config precedence now exist, but command discovery and custom-command ergonomics are still less dense than upstream.
- Subsystem: Go command loader, shared command contracts, desktop command UI, parity audit classifier
- Acceptance criteria: Command palette flow feels desktop-adapted rather than partial, advanced custom commands remain predictable, and audit notes this as desktop-adapted or matched.
- Source of truth: OpenCode archive + Crush live

## Runtime and Tool Loop Parity
- Target behavior: Match upstream tool orchestration, stop/continue semantics, malformed-call recovery, and richer tool result shaping.
- Current gap: Run-state events, denials, failures, and continuation states now exist, but a few retry/stop edge cases and malformed tool-call recovery paths still need tightening.
- Subsystem: Go agent loop, tool result modeling, WebSocket event stream, desktop runtime cards
- Acceptance criteria: Tool calls, denials, malformed inputs, retries, and completions render consistently and audit no longer treats orchestration as a broad partial gap.
- Source of truth: OpenCode archive + Crush live

## Approval and Edit-Flow Parity
- Target behavior: Provide upstream-grade approval semantics and diff-aware mutation review for all risky actions.
- Current gap: Approvals already expose intent, paths, summaries, and previews, but a final layer of mutation-preview polish and edge-case consistency still remains.
- Subsystem: Approval broker, desktop approval UI, patch/edit engine, tool result rendering
- Acceptance criteria: Risky mutations always show clear intent and preview context, and audit can treat approval behavior as implemented or desktop-adapted.
- Source of truth: OpenCode archive

## LSP and MCP Parity
- Target behavior: Expand current LSP and MCP behavior to deeper workspace quality and broader remote compatibility closer to upstream behavior.
- Current gap: Workspace symbols/definitions/references, graph enrichment, and stdio/http/sse MCP are implemented, but larger-workspace LSP quality and broader remote MCP edge compatibility are still partial.
- Subsystem: LSP client, MCP client, tool schema exposure, desktop diagnostics panels, remote transport handling
- Acceptance criteria: Parity audit documents broad transport support and stronger workspace behavior, and the remaining gap is narrowed to minor compatibility polish.
- Source of truth: Crush live

## Graph, Memory, and Retrieval Parity
- Target behavior: Use graph and session lineage to influence answers with stronger semantic ranking, cross-file symbol/reference relationships, and summary-aware carry-forward.
- Current gap: Ranked retrieval, summary weighting, and symbol-reference edges are now implemented, but deeper semantic ranking polish still remains.
- Subsystem: Neo4j store, repo indexer, prompt context builder, retrieval preview UI
- Acceptance criteria: Follow-up answers stay strong across long sessions, retrieval reasons remain inspectable, and audit can narrow this to a smaller polish gap.
- Source of truth: Current project + Crush live

## Nested Task and Continuation Parity
- Target behavior: Bring subtask and compaction behavior closer to upstream continuation semantics across long-running coding sessions.
- Current gap: Child sessions, parent summaries, recent subtasks, and summary heads now exist, but continuation nuance and long-session lineage polish are still partial.
- Subsystem: Session model, compaction flow, memory/session linking, repo graph summaries
- Acceptance criteria: Parent/subtask context continues predictably after compaction, summary heads stay stable, and audit narrows this to final nuance cleanup.
- Source of truth: OpenCode archive

## Desktop UX Parity
- Target behavior: Preserve upstream information architecture in a desktop-native interface with denser command, approval, runtime, and activity flows.
- Current gap: Desktop UX is solid and now exposes dedicated approvals, continuation, retrieval, MCP, and runtime cards, but the overall interaction density still trails upstream.
- Subsystem: React desktop renderer, command palette, activity stream, session panels, diagnostic/retrieval sidebars
- Acceptance criteria: Audit classifies presentation parity as desktop-adapted with no major missing interaction patterns and the UI feels closer to upstream during active tool use.
- Source of truth: OpenCode archive + Crush live
