# Final Parity Checklist

This checklist tracks the **remaining work** between the current project and a near-exact OpenCode-style desktop replica with Ollama-only model selection.

## Current estimate

- OpenCode-style behavioral parity: **80-85%**
- Desktop + Ollama target-product parity: **88-92%**

## Foundation status

- [x] Desktop shell
- [x] Go local runtime
- [x] Ollama model integration
- [x] Sessions and message history
- [x] Tool loop foundation
- [x] Permission flow
- [x] Edit and patch workflow foundation
- [x] Repo indexing and retrieval foundation
- [x] LSP foundation
- [x] MCP foundation
- [x] Subtask/session-lineage foundation
- [x] Auto-compact/continuation foundation
- [x] Upstream parity audit system

## Remaining parity work

### 1. Command parity refinement
- [ ] Improve custom-command ergonomics, aliases, and guidance density
- [ ] Make command discovery feel closer to upstream during active use
- [ ] Reduce audit classification from broad `partial` toward `desktop-adapted`

### 2. Tool-loop edge-case polish
- [x] Tighten malformed tool-call recovery
- [x] Tighten retry/stop semantics after tool failures
- [x] Keep denial recovery predictable across all risky tools

### 3. Continuation and compaction nuance
- [x] Further stabilize summary-head behavior in long sessions
- [x] Improve carry-forward weighting across recent turns, summary heads, and nested-task outputs
- [x] Reduce continuation drift after repeated compaction

### 4. Remote MCP compatibility polish
- [ ] Broaden compatibility across more remote MCP server implementations
- [ ] Improve endpoint/handshake fallbacks
- [ ] Keep transport-level errors clearer in UI and logs

### 5. Workspace-depth LSP polish
- [ ] Improve large-repo cross-file definition/reference quality
- [ ] Improve workspace-level intelligence consistency across languages
- [ ] Keep graph enrichment reliable when LSP coverage is partial

### 6. Desktop upstream-feel polish
- [ ] Increase command/runtime/activity density without making the UI noisy
- [ ] Make approvals, continuation, retrieval, and tool activity feel more cohesive
- [ ] Narrow the last presentation-layer gap from "good desktop clone" to "feels truly upstream-inspired"

## Recommended execution order

1. Tool-loop edge-case polish
2. Continuation and compaction nuance
3. Command parity refinement
4. Remote MCP compatibility polish
5. Workspace-depth LSP polish
6. Desktop upstream-feel polish

## Done criteria

We should call the project "effectively complete" for the target variant when:

- command, runtime, approval, and continuation behavior feel stable in day-to-day use
- LSP and MCP gaps are mostly edge-case compatibility rather than missing capability
- audit artifacts still show `partial`, but only for nuanced parity differences rather than broad missing behavior
- the desktop experience feels like a deliberate OpenCode adaptation rather than a parallel product
