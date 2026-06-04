import fs from "node:fs";
import path from "node:path";

const root = process.cwd();
const outDir = path.join(root, "docs", "upstream-parity");
const baselineManifestPath = path.join(root, "upstream", "baselines.json");

const baselineManifest = JSON.parse(fs.readFileSync(baselineManifestPath, "utf8"));

function exists(target) {
  return fs.existsSync(target);
}

function readText(target) {
  try {
    return fs.readFileSync(target, "utf8");
  } catch {
    return "";
  }
}

function readJson(target) {
  try {
    return JSON.parse(fs.readFileSync(target, "utf8"));
  } catch {
    return null;
  }
}

function ensureDir(target) {
  fs.mkdirSync(target, { recursive: true });
}

function normalizeGitDir(repoRoot) {
  const dotGit = path.join(repoRoot, ".git");
  if (!exists(dotGit)) {
    return "";
  }
  const stat = fs.statSync(dotGit);
  if (stat.isDirectory()) {
    return dotGit;
  }
  const pointer = readText(dotGit).trim();
  if (!pointer.startsWith("gitdir:")) {
    return "";
  }
  const relative = pointer.slice("gitdir:".length).trim();
  return path.resolve(repoRoot, relative);
}

function readGitHead(repoRoot) {
  const gitDir = normalizeGitDir(repoRoot);
  if (!gitDir) {
    return "";
  }
  const head = readText(path.join(gitDir, "HEAD")).trim();
  if (!head) {
    return "";
  }
  if (!head.startsWith("ref:")) {
    return head;
  }
  const ref = head.slice("ref:".length).trim();
  const refPath = path.join(gitDir, ...ref.split("/"));
  const refValue = readText(refPath).trim();
  if (refValue) {
    return refValue;
  }
  const packedRefs = readText(path.join(gitDir, "packed-refs"));
  for (const line of packedRefs.split(/\r?\n/)) {
    if (!line || line.startsWith("#") || line.startsWith("^")) {
      continue;
    }
    const [sha, packedRef] = line.split(" ");
    if (packedRef === ref) {
      return sha;
    }
  }
  return "";
}

function listTopLevelNames(repoRoot) {
  try {
    return fs
      .readdirSync(repoRoot, { withFileTypes: true })
      .filter((entry) => !entry.name.startsWith(".gocache") && entry.name !== "node_modules")
      .map((entry) => entry.name)
      .sort();
  } catch {
    return [];
  }
}

function detectCurrentProject(repoRoot) {
  const packageJson = readJson(path.join(repoRoot, "package.json")) ?? {};
  const readme = readText(path.join(repoRoot, "README.md"));
  const parity = readText(path.join(repoRoot, "PARITY.md"));
  const commandsFile = readText(path.join(repoRoot, "service", "internal", "app", "commands.go"));
  const integrationsFile = readText(path.join(repoRoot, "service", "internal", "app", "integrations.go"));
  const appFile = readText(path.join(repoRoot, "service", "internal", "app", "app.go"));
  const lspFile = readText(path.join(repoRoot, "service", "internal", "lsp", "client.go"));
  const webFile = readText(path.join(repoRoot, "web", "src", "ui", "App.tsx"));
  const neo4jFile = readText(path.join(repoRoot, "service", "internal", "store", "neo4j.go"));

  const commandMatches = [...commandsFile.matchAll(/\{ID: "([^"]+)"/g)].map((match) => match[1]);
  const toolMatches = [...integrationsFile.matchAll(/toolSchema\("([^"]+)"/g)].map((match) => match[1]);

  return {
    id: "current-project",
    name: packageJson.name ?? "current-project",
    repoRoot,
    readmeSummary: readme.split(/\r?\n/).slice(0, 12).join("\n"),
    topLevel: listTopLevelNames(repoRoot),
    commit: readGitHead(repoRoot),
    commands: commandMatches,
    tools: toolMatches,
    facts: {
      desktopShell: exists(path.join(repoRoot, "desktop")) && exists(path.join(repoRoot, "web")),
      localService: exists(path.join(repoRoot, "service", "cmd", "service")),
      cliParity: exists(path.join(repoRoot, "service", "cmd", "cli")),
      ollamaOnly: readme.includes("Ollama-only") || readme.includes("Ollama provides"),
      mongoNeo4j: readme.includes("MongoDB") && readme.includes("Neo4j"),
      slashCommands: commandsFile.includes('ID: "help"'),
      customCommands: commandsFile.includes(".opencode") && parity.includes("Custom commands"),
      approvals: appFile.includes("RequestPermission"),
      autoCompact: integrationsFile.includes("AutoCompact") || appFile.includes("maybeCompactHistory"),
      lspDiagnostics: lspFile.includes("Diagnostics("),
      lspDefinitions: lspFile.includes("Definition(") && lspFile.includes("References("),
      mcpStdio: readme.includes("MCP stdio") || integrationsFile.includes("listMCPTools"),
      nestedTasks: appFile.includes("RunSubtask"),
      desktopCommandPalette: webFile.includes("commandMatches"),
      graphRetrieval: neo4jFile.includes("RelevantMemorySummaries") && neo4jFile.includes("RelevantTouchedFiles"),
      patchWorkflow: appFile.includes("ApplyPatch(") && webFile.includes("Apply Patch")
    }
  };
}

function detectUpstream(repoRoot, id, role, repoUrl, ref, notes) {
  const readme = readText(path.join(repoRoot, "README.md"));
  const topLevel = listTopLevelNames(repoRoot);
  const opencodeConfig = readText(path.join(repoRoot, ".opencode.json"));
  const schema = readText(path.join(repoRoot, "opencode-schema.json"));

  return {
    id,
    role,
    repoUrl,
    ref,
    notes,
    repoRoot,
    commit: readGitHead(repoRoot),
    topLevel,
    readmeSummary: readme.split(/\r?\n/).slice(0, 20).join("\n"),
    facts: {
      archived: readme.includes("Archived: Project has Moved"),
      terminalFirst: readme.includes("terminal") || readme.includes("TUI"),
      sqlite: readme.includes("SQLite") || topLevel.includes("sqlc.yaml"),
      multiProvider: readme.includes("Multiple AI Providers") || readme.includes("Multi-Model"),
      lsp: readme.includes("LSP"),
      mcp: readme.includes("MCP"),
      autoCompact: readme.includes("Auto Compact") || readme.includes("auto compact"),
      customCommandsNamedArgs: readme.includes("Named Arguments for Custom Commands"),
      localConfig: opencodeConfig.length > 0 || schema.length > 0,
      commandSchema: schema.includes("command") || readme.includes("command")
    }
  };
}

function classifyCapabilities(current, archive, crush) {
  return [
    {
      capability: "Repo Structure and Entrypoints",
      status: "desktop-adapted",
      sourceOfTruth: "both",
      current: "Electron + React + Go sidecar + CLI workspace",
      archive: "Single Go CLI/TUI application",
      crush: "Single terminal-first application",
      notes: "Current project intentionally replaces terminal-first shell with desktop UI while keeping a CLI/debug surface."
    },
    {
      capability: "Runtime Architecture",
      status: "desktop-adapted",
      sourceOfTruth: "both",
      current: current.facts.localService ? "Local Go sidecar with REST + WebSocket loopback" : "missing",
      archive: "In-process terminal runtime",
      crush: "In-process terminal runtime",
      notes: "Intentional divergence for desktop-first packaging."
    },
    {
      capability: "Provider and Model Layer",
      status: "intentionally-different",
      sourceOfTruth: "both",
      current: current.facts.ollamaOnly ? "Ollama-only local models" : "custom",
      archive: archive.facts.multiProvider ? "Multi-provider" : "unknown",
      crush: crush.facts.multiProvider ? "Multi-model / provider-flexible" : "unknown",
      notes: "Intentional product choice; parity target is behavior, not provider matrix."
    },
    {
      capability: "Commands and Command Discovery",
      status: current.facts.slashCommands && current.facts.customCommands ? "partial" : "missing",
      sourceOfTruth: "archive",
      current: `${current.commands.length} built-in commands plus project/user command loading`,
      archive: archive.facts.customCommandsNamedArgs ? "Custom commands with named arguments" : "Custom commands",
      crush: "Terminal command workflows and extensible interaction model",
      notes: "Current custom commands are simpler than upstream named-argument behavior."
    },
    {
      capability: "Tool Loop and Approvals",
      status: current.facts.approvals && current.facts.patchWorkflow ? "partial" : "missing",
      sourceOfTruth: "both",
      current: `${current.tools.length} tool schemas with approval broker and patch/edit flow`,
      archive: "Tool execution with approvals and file-change tracking",
      crush: "Tool-driven coding flow with richer terminal integration",
      notes: "Core loop exists, but deeper parity around orchestration and tool semantics is still pending."
    },
    {
      capability: "Sessions, History, and Compaction",
      status: current.facts.autoCompact ? "partial" : "missing",
      sourceOfTruth: "both",
      current: "Mongo-backed sessions with degraded fallback and auto-compact summaries",
      archive: archive.facts.sqlite && archive.facts.autoCompact ? "SQLite-backed sessions with auto compact" : "Session management",
      crush: crush.facts.autoCompact ? "Session-based context management" : "Session-based workflow",
      notes: "Summary lineage and continuation weighting still need work."
    },
    {
      capability: "LSP Code Intelligence",
      status: current.facts.lspDefinitions ? "partial" : current.facts.lspDiagnostics ? "partial" : "missing",
      sourceOfTruth: "crush",
      current: "Diagnostics, document symbols, definitions, and references",
      archive: archive.facts.lsp ? "LSP integration" : "limited",
      crush: crush.facts.lsp ? "LSP-enhanced workspace assistance" : "unknown",
      notes: "Current implementation is file-scoped; deeper workspace parity is still missing."
    },
    {
      capability: "MCP Integration",
      status: current.facts.mcpStdio ? "partial" : "missing",
      sourceOfTruth: "crush",
      current: "stdio MCP discovery and invocation",
      archive: archive.facts.mcp ? "n/a or early-era tooling" : "not emphasized",
      crush: crush.facts.mcp ? "http, stdio, and sse MCP support" : "unknown",
      notes: "Current project needs broader transport parity and richer error semantics."
    },
    {
      capability: "Nested Task / Agent Behavior",
      status: current.facts.nestedTasks ? "partial" : "missing",
      sourceOfTruth: "archive",
      current: "Child-session subtask execution",
      archive: "Task-oriented agent model",
      crush: "Session-based workflows with composable context",
      notes: "Subtasks exist, but not yet full upstream-depth orchestration."
    },
    {
      capability: "Repo Graph and Memory Retrieval",
      status: current.facts.graphRetrieval ? "partial" : "missing",
      sourceOfTruth: "current+crush",
      current: "Neo4j graph, lineage retrieval, prompt-relevant files/symbols/memories",
      archive: "SQLite + tracked changes + LSP context",
      crush: "LSP- and context-enhanced session workflow",
      notes: "Current project goes beyond archived storage choices but still needs stronger semantic ranking."
    },
    {
      capability: "Presentation Layer UX",
      status: current.facts.desktopCommandPalette ? "partial" : "missing",
      sourceOfTruth: "both",
      current: "Desktop shell with command suggestions, panels, approvals, and activity cards",
      archive: archive.facts.terminalFirst ? "Bubble Tea terminal UI" : "terminal UI",
      crush: crush.facts.terminalFirst ? "Terminal-first Charm UX" : "unknown",
      notes: "Desktop adaptation is correct, but upstream-like polish and interaction density still lag."
    }
  ];
}

function backlogItems() {
  return [
    {
      tranche: "Command and Config Parity",
      targetBehavior: "Support upstream-style named command arguments, richer command templates, and config parity across local/project/user scopes.",
      currentGap: "Custom commands load, but argument semantics and config parity are simpler than upstream.",
      subsystem: "Go command loader, shared command contracts, desktop command UI",
      acceptanceCriteria: "Named placeholders work end-to-end, config precedence is documented, and parity audit marks command system as matched or desktop-adapted.",
      sourceOfTruth: "OpenCode archive + Crush live"
    },
    {
      tranche: "Runtime and Tool Loop Parity",
      targetBehavior: "Match upstream tool orchestration, stop/continue semantics, and richer tool result shaping.",
      currentGap: "Core loop exists but deeper orchestration parity is still partial.",
      subsystem: "Go agent loop, tool result modeling, WebSocket event stream",
      acceptanceCriteria: "Tool calls, denials, retries, and completions render consistently and match audit expectations.",
      sourceOfTruth: "OpenCode archive + Crush live"
    },
    {
      tranche: "Approval and Edit-Flow Parity",
      targetBehavior: "Provide upstream-grade approval semantics and diff-aware mutation review.",
      currentGap: "Approvals and patching are strong but not fully upstream-equivalent.",
      subsystem: "Approval broker, desktop approval UI, patch/edit engine",
      acceptanceCriteria: "Approvals expose intent, affected files, and diff previews for all risky mutations.",
      sourceOfTruth: "OpenCode archive"
    },
    {
      tranche: "LSP and MCP Parity",
      targetBehavior: "Expand from current file-scoped LSP and stdio-only MCP to richer upstream behavior.",
      currentGap: "Definitions/references exist, but workspace depth and MCP transport breadth are still partial.",
      subsystem: "LSP client, MCP client, tool schema exposure, desktop diagnostics panels",
      acceptanceCriteria: "Parity audit upgrades LSP and MCP classifications and documents supported transports/capabilities.",
      sourceOfTruth: "Crush live"
    },
    {
      tranche: "Graph, Memory, and Retrieval Parity",
      targetBehavior: "Use graph and session lineage to influence answers with stronger semantic ranking and symbol/reference relationships.",
      currentGap: "Lineage-aware retrieval exists, but symbol/reference graph semantics remain shallow.",
      subsystem: "Neo4j store, repo indexer, prompt context builder",
      acceptanceCriteria: "Repo graph stores true symbol/reference relationships and retrieval is traceably better across follow-up tasks.",
      sourceOfTruth: "Current project + Crush live"
    },
    {
      tranche: "Nested Task and Continuation Parity",
      targetBehavior: "Bring subtask and compaction behavior closer to upstream continuation semantics.",
      currentGap: "Child sessions exist, but summary lineage and nested-task behavior are still partial.",
      subsystem: "Session model, compaction flow, memory/session linking",
      acceptanceCriteria: "Parent/subtask context continues predictably after compaction and is surfaced in audit outputs.",
      sourceOfTruth: "OpenCode archive"
    },
    {
      tranche: "Desktop UX Parity",
      targetBehavior: "Preserve upstream information architecture in a desktop-native interface.",
      currentGap: "Desktop UX is solid but still less polished and less dense than upstream terminal flows.",
      subsystem: "React desktop renderer, command palette, activity stream, session panels",
      acceptanceCriteria: "Audit classifies presentation parity as desktop-adapted with no major missing interaction patterns.",
      sourceOfTruth: "OpenCode archive + Crush live"
    }
  ];
}

function markdownReport({ generatedAt, current, archive, crush, capabilities }) {
  const lines = [];
  lines.push("# Upstream Parity Audit");
  lines.push("");
  lines.push(`Generated: ${generatedAt}`);
  lines.push("");
  lines.push("## Baselines");
  lines.push("");
  lines.push(`- Current project: \`${current.name}\``);
  lines.push(`- Archived baseline: \`${archive.id}\` from ${archive.repoUrl}`);
  lines.push(`- Live successor baseline: \`${crush.id}\` from ${crush.repoUrl}`);
  lines.push("");
  lines.push("## Comparison Matrix");
  lines.push("");
  lines.push("| Capability | Status | Source of Truth | Current | Archived OpenCode | Live Crush | Notes |");
  lines.push("| --- | --- | --- | --- | --- | --- | --- |");
  for (const item of capabilities) {
    lines.push(`| ${item.capability} | ${item.status} | ${item.sourceOfTruth} | ${escapePipe(item.current)} | ${escapePipe(item.archive)} | ${escapePipe(item.crush)} | ${escapePipe(item.notes)} |`);
  }
  lines.push("");
  lines.push("## Current Project Signals");
  lines.push("");
  lines.push(`- Built-in commands detected: ${current.commands.length}`);
  lines.push(`- Tool schemas detected: ${current.tools.length}`);
  lines.push(`- Desktop shell present: ${current.facts.desktopShell}`);
  lines.push(`- Local Go service present: ${current.facts.localService}`);
  lines.push(`- CLI entrypoint present: ${current.facts.cliParity}`);
  lines.push("");
  lines.push("## Baseline Metadata");
  lines.push("");
  lines.push(`- OpenCode archive commit: ${archive.commit || "unknown"}`);
  lines.push(`- Crush live commit: ${crush.commit || "unknown"}`);
  lines.push(`- Current project commit: ${current.commit || "unknown"}`);
  return lines.join("\n");
}

function backlogMarkdown({ generatedAt, items }) {
  const lines = [];
  lines.push("# Implementation Backlog");
  lines.push("");
  lines.push(`Generated: ${generatedAt}`);
  lines.push("");
  for (const item of items) {
    lines.push(`## ${item.tranche}`);
    lines.push(`- Target behavior: ${item.targetBehavior}`);
    lines.push(`- Current gap: ${item.currentGap}`);
    lines.push(`- Subsystem: ${item.subsystem}`);
    lines.push(`- Acceptance criteria: ${item.acceptanceCriteria}`);
    lines.push(`- Source of truth: ${item.sourceOfTruth}`);
    lines.push("");
  }
  return lines.join("\n");
}

function escapePipe(value) {
  return String(value ?? "").replace(/\|/g, "\\|").replace(/\n/g, "<br />");
}

const generatedAt = new Date().toISOString();
const current = detectCurrentProject(root);
const archiveBaseline = baselineManifest.baselines.find((item) => item.id === "opencode-archive");
const crushBaseline = baselineManifest.baselines.find((item) => item.id === "crush-live");

const archive = detectUpstream(
  path.join(root, archiveBaseline.localPath),
  archiveBaseline.id,
  archiveBaseline.role,
  archiveBaseline.repoUrl,
  archiveBaseline.ref,
  archiveBaseline.notes
);
const crush = detectUpstream(
  path.join(root, crushBaseline.localPath),
  crushBaseline.id,
  crushBaseline.role,
  crushBaseline.repoUrl,
  crushBaseline.ref,
  crushBaseline.notes
);

const capabilities = classifyCapabilities(current, archive, crush);
const backlog = backlogItems();

const parityJson = {
  generatedAt,
  baselines: [
    {
      ...archiveBaseline,
      commit: archive.commit || "",
      topLevel: archive.topLevel
    },
    {
      ...crushBaseline,
      commit: crush.commit || "",
      topLevel: crush.topLevel
    }
  ],
  currentProject: {
    name: current.name,
    commit: current.commit || "",
    topLevel: current.topLevel,
    commands: current.commands,
    tools: current.tools,
    facts: current.facts
  },
  capabilities,
  backlog
};

ensureDir(outDir);
fs.writeFileSync(path.join(outDir, "parity.json"), JSON.stringify(parityJson, null, 2));
fs.writeFileSync(path.join(outDir, "parity.md"), markdownReport({ generatedAt, current, archive, crush, capabilities }));
fs.writeFileSync(path.join(outDir, "implementation-backlog.md"), backlogMarkdown({ generatedAt, items: backlog }));

console.log(`Generated parity audit in ${path.relative(root, outDir)}`);
