// @ts-check
/// <reference types="@actions/github-script" />

// extract_inline_sub_agents.cjs
//
// Parses ## agent: `name` markers from workflow markdown and writes each agent
// block as a separate .agent.md file under .github/agents/ (Copilot) or the
// engine-appropriate directory.
//
// This step runs AFTER {{#runtime-import}} macros have been fully inlined by
// processRuntimeImports() in interpolate_prompt.cjs, ensuring that any imports
// inside an agent block are resolved before the agent file is written.
//
// Marker syntax
// ─────────────
//   ## agent: `name`       Opens an agent block.  name must start with a
//                          lowercase letter and contain only lowercase letters,
//                          digits, hyphens, or underscores (safe for filenames).
//   ## end agent: `name`   Optional. Explicitly closes the block opened by the
//                          matching "## agent: `name`" marker.
//
// An agent block ends at a matching "## end agent: `name`" marker if one is
// present, or otherwise at the next level-2 Markdown heading (## ...) or EOF.
// The explicit end marker lets an agent block be embedded in the middle of a
// document (for example via an import) without swallowing unrelated content
// that follows it, and lets the block itself contain nested "##" headings.
//
// Sub-agent frontmatter keys and their order are preserved without filtering;
// boundary whitespace is trimmed.
//
// If no ## agent: markers are present the content is returned unchanged and no
// files are written.

const fs = require("fs");
const path = require("path");
const { collectInlineEndMarkers, unknownInlineEndMarkerError } = require("./inline_marker_helpers.cjs");
const { inlineArtifactTarget } = require("./inline_artifact_target.cjs");
const { ERR_CONFIG } = require("./error_codes.cjs");

// Regex for the start marker: ## agent: `name` (lowercase identifier)
const START_MARKER_RE = /^##[ \t]+agent:[ \t]+`([a-z][a-z0-9_-]*)`[ \t]*$/gm;

// Regex for the optional explicit end marker: ## end agent: `name`
const END_MARKER_RE = /^##[ \t]+end[ \t]+agent:[ \t]+`([a-z][a-z0-9_-]*)`[ \t]*$/gm;

// Regex for an inline skill marker exactly at an implicit H2 boundary.
const SKILL_START_BOUNDARY_RE = /^##[ \t]+skill:[ \t]+`(?:[a-z][a-z0-9_-]*)`[ \t]*(?:\n|$)/;

// Regex that matches the start of any level-2 Markdown heading (## ).
// Used to find the boundary where each agent block ends when no explicit end
// marker is present.
const H2_HEADING_RE = /^##[ \t]/gm;

/**
 * Preserves sub-agent frontmatter exactly as authored.
 *
 * This helper is kept to preserve the write-path structure used by the inline
 * skills/sub-agents extractors and to provide a single hook if the runtime ever
 * needs sub-agent-specific frontmatter normalization again.
 *
 * @param {string} content - Raw agent block content (frontmatter + prompt).
 * @returns {string} Unchanged content.
 */
function preserveSubAgentFrontmatter(content) {
  return content;
}

// Experiment references allowed in sub-agent frontmatter: the direct
// ${{ experiments.<name> }} form (runtime-imported content keeps it verbatim)
// and the __GH_AW_EXPERIMENTS_<NAME>__ placeholder (compile-time inlined content).
// Compound expressions are rejected at compile time and are not evaluated here.
const EXPERIMENT_REFERENCE_RE = /\$\{\{[ \t]*experiments\.([A-Za-z_][A-Za-z0-9_-]*)[ \t]*\}\}|__(GH_AW_EXPERIMENTS_[A-Z0-9_]+)__/g;

// Selected variants are written into YAML frontmatter, so only plain scalars
// such as model IDs and aliases (small, large, provider/model-1.2) are accepted.
const SAFE_EXPERIMENT_VALUE_RE = /^[A-Za-z0-9][A-Za-z0-9._/@+:?=&%-]*$/;

/**
 * Env var holding the selected variant for an experiment, matching
 * ExperimentEnvVarName in pkg/workflow/expression_extraction.go.
 * @param {string} experimentName
 * @returns {string}
 */
function experimentEnvVarName(experimentName) {
  return "GH_AW_EXPERIMENTS_" + experimentName.toUpperCase().replace(/-/g, "_");
}

/**
 * Replaces experiment references in a sub-agent's frontmatter with the
 * variant already selected by the pick-experiment step (exposed to the
 * activation job as GH_AW_EXPERIMENTS_* env vars). No variant is selected and
 * no other expression is evaluated here.
 *
 * Throws when a referenced experiment has no assignment or its value is not a
 * plain scalar, so a sub-agent is never written with an unresolved expression.
 *
 * @param {string} agentName - Sub-agent name (for error messages).
 * @param {string} content - Raw agent block content (frontmatter + prompt).
 * @param {NodeJS.ProcessEnv} [env] - Environment with GH_AW_EXPERIMENTS_* values.
 * @returns {string} Content with frontmatter experiment references resolved.
 */
function resolveSubAgentExperimentReferences(agentName, content, env = process.env) {
  const match = /^---[ \t]*\r?\n([\s\S]*?)\r?\n---[ \t]*\r?(?=\n|$)/.exec(content);
  if (!match) return content;
  const frontmatter = match[1];
  const resolved = frontmatter
    .split("\n")
    .map(line =>
      line.replace(EXPERIMENT_REFERENCE_RE, (reference, experimentName, placeholderEnvVar) => {
        const envVar = experimentName ? experimentEnvVarName(experimentName) : placeholderEnvVar;
        const field = /^[ \t]*([A-Za-z0-9_-]+)[ \t]*:/.exec(line)?.[1] ?? "frontmatter";
        const value = env[envVar];
        if (!value) {
          throw new Error(
            `${ERR_CONFIG}: inline sub-agent "${agentName}" field "${field}" references ${reference}, but no experiment assignment is available (${envVar} is not set). Declare the experiment under "experiments:" in the workflow frontmatter.`
          );
        }
        if (!SAFE_EXPERIMENT_VALUE_RE.test(value)) {
          throw new Error(`${ERR_CONFIG}: inline sub-agent "${agentName}" field "${field}" references ${reference}, whose selected variant ${JSON.stringify(value)} is not a plain model or alias value.`);
        }
        core.info(`[extractInlineSubAgents] Sub-agent "${agentName}" ${field}: experiment expression ${reference} → selected variant "${value}"`);
        return value;
      })
    )
    .join("\n");
  if (/^[ \t]*model[ \t]*:[^\n]*(?:\$\{\{|__GH_AW_)/m.test(resolved)) {
    throw new Error(`${ERR_CONFIG}: inline sub-agent "${agentName}" field "model" contains an unresolved expression or placeholder. Use a literal model selector or a declared experiment assignment.`);
  }
  if (resolved === frontmatter) return content;
  const start = match.index + match[0].indexOf("\n") + 1;
  return content.slice(0, start) + resolved + content.slice(start + frontmatter.length);
}

/**
 * Claude Code skips a project agent file without a frontmatter `name`, so the
 * generated Claude copy gets the declared block name when the author omitted it.
 * An authored `name` (detected by the YAML parser, so quoted keys count) is kept.
 * A flow mapping gets the key inside its braces, so mapping styles are never mixed.
 * Frontmatter that does not parse as a mapping is left unchanged; the model
 * resolution step then rejects it before Claude starts.
 *
 * @param {string} agentName - Declared sub-agent name from the `## agent:` marker.
 * @param {string} content - Agent block content (frontmatter + prompt).
 * @returns {string} Content with a top-level `name:` in its frontmatter.
 */
function ensureClaudeSubAgentName(agentName, content) {
  const { parseDocument } = require("./work_queue_yaml.cjs");
  const nameEntry = `name: ${JSON.stringify(agentName)}`;
  const match = /^---[ \t]*\r?\n(?:([\s\S]*?)\r?\n)?---[ \t]*\r?(?=\n|$)/.exec(content);
  if (!match) return `---\n${nameEntry}\n---\n${content}`;
  const frontmatter = match[1] ?? "";
  const start = content.indexOf("\n") + 1;
  /** @type {any} */
  const document = parseDocument(frontmatter);
  if (!document || document.errors.length) return content;
  const map = document.contents;
  if (map === null || map === undefined) return content.slice(0, start) + nameEntry + "\n" + content.slice(start);
  if (!Array.isArray(map.items) || !Array.isArray(map.range)) return content;
  if (map.items.some((/** @type {any} */ pair) => pair?.key?.value === "name")) return content;
  const position = start + map.range[0];
  if (map.flow) {
    return content.slice(0, position + 1) + nameEntry + (map.items.length > 0 ? ", " : "") + content.slice(position + 1);
  }
  const indent = content.slice(content.lastIndexOf("\n", position - 1) + 1, position);
  return content.slice(0, position) + nameEntry + "\n" + indent + content.slice(position);
}

/**
 * Extracts inline sub-agents from markdown content.
 *
 * Returns the reassembled main content (with agent blocks removed) and an
 * array of extracted agents.
 *
 * An agent block extends from its start marker to a matching "## end agent:
 * `name`" marker if present, or otherwise to the next H2 heading or EOF. When
 * an agent is closed by an explicit end marker, any text following it (up to
 * the next start marker or EOF) is preserved in the main content rather than
 * discarded.
 *
 * Throws if an end marker's name does not correspond to any start marker of
 * the same name found within its search window (an "orphan" end marker),
 * which is almost always an authoring mistake such as a typo.
 *
 * @param {string} content - Markdown with potential inline sub-agent blocks.
 * @returns {{ mainContent: string, agents: Array<{name: string, content: string}> }}
 */
function extractInlineSubAgents(content) {
  const startMatches = [...content.matchAll(START_MARKER_RE)];
  const endMarkers = collectInlineEndMarkers(content, END_MARKER_RE);

  if (startMatches.length === 0) {
    if (endMarkers.length > 0) {
      throw unknownInlineEndMarkerError(content, endMarkers[0], "agent");
    }
    return { mainContent: content, agents: [] };
  }

  // Collect all H2 heading positions for the implicit block boundary fallback.
  const h2Positions = [...content.matchAll(H2_HEADING_RE)].map(m => m.index).filter(i => i !== undefined);

  // Track explicit end markers so unused markers can be reported as orphans.
  const usedEnd = new Array(endMarkers.length).fill(false);

  /** @type {Array<{name: string, content: string}>} */
  const agents = [];
  /** @type {string[]} */
  const mainParts = [];
  let cursor = 0;
  let prevExplicit = true; // text before the first marker is always kept

  for (let i = 0; i < startMatches.length; i++) {
    const m = startMatches[i];
    if (m.index === undefined) continue;

    if (prevExplicit) {
      mainParts.push(content.slice(cursor, m.index));
    }

    const name = m[1];

    // Content starts on the line after the start marker.
    let lineEnd = m.index + m[0].length;
    if (lineEnd < content.length && content[lineEnd] === "\n") lineEnd++;

    const windowEnd = i + 1 < startMatches.length ? /** @type {number} */ startMatches[i + 1].index : content.length;

    let matchedEnd;
    for (let ei = 0; ei < endMarkers.length; ei++) {
      const e = endMarkers[ei];
      if (usedEnd[ei] || e.name !== name || e.start < lineEnd || e.start >= windowEnd) continue;
      matchedEnd = e;
      usedEnd[ei] = true;
      break;
    }

    let agentContent;
    let newCursor;
    const explicit = matchedEnd !== undefined;
    let preserveAfterImplicitBoundary = false;
    if (explicit) {
      agentContent = content.slice(lineEnd, matchedEnd.start).trim();
      newCursor = matchedEnd.end;
    } else {
      const contentEnd = h2Positions.find(pos => pos >= lineEnd) ?? content.length;
      agentContent = content.slice(lineEnd, contentEnd).trim();
      newCursor = contentEnd;
      if (SKILL_START_BOUNDARY_RE.test(content.slice(contentEnd))) {
        preserveAfterImplicitBoundary = true;
      }
    }

    agents.push({ name, content: agentContent });
    cursor = newCursor;
    prevExplicit = explicit || preserveAfterImplicitBoundary;
  }

  if (prevExplicit) {
    mainParts.push(content.slice(cursor));
  }

  const orphan = endMarkers.find((_, ei) => !usedEnd[ei]);
  if (orphan) {
    throw unknownInlineEndMarkerError(content, orphan, "agent");
  }

  const mainContent = mainParts
    .join("")
    .replace(/\n{3,}/g, "\n\n")
    .replace(/\n+$/, "");

  return { mainContent, agents };
}

/**
 * Ensures every "## agent: `name`" start marker in content has a matching
 * explicit "## end agent: `name`" marker, inserting one at the block's
 * implicit boundary (next H2 heading or EOF) for any start marker that
 * doesn't already have one.
 *
 * This is intended for content that is about to be spliced into a larger
 * document (for example a runtime-imported file). Without it, an agent block
 * that relies on implicit closing would expand to swallow whatever content
 * follows it once spliced in — including unrelated content from subsequent
 * imports or the main workflow body — instead of stopping where it would
 * have stopped when the file was considered on its own.
 *
 * Blocks that already have an explicit end marker are left untouched. The
 * boundary computed here matches exactly what {@link extractInlineSubAgents}
 * would already infer for implicit blocks, so this is a no-op with respect to
 * extraction results when the content is not spliced elsewhere.
 *
 * @param {string} content - Markdown that may contain "## agent:" blocks.
 * @returns {string} Content with implicit end markers made explicit.
 */
function closeUnterminatedSubAgentMarkers(content) {
  const startMatches = [...content.matchAll(START_MARKER_RE)];
  if (startMatches.length === 0) return content;

  const h2Positions = [...content.matchAll(H2_HEADING_RE)].map(m => m.index).filter(i => i !== undefined);
  const endMarkers = collectInlineEndMarkers(content, END_MARKER_RE);
  const usedEnd = new Array(endMarkers.length).fill(false);

  /** @type {Array<{pos: number, name: string}>} */
  const insertions = [];

  for (let i = 0; i < startMatches.length; i++) {
    const m = startMatches[i];
    if (m.index === undefined) continue;

    const name = m[1];
    let lineEnd = m.index + m[0].length;
    if (lineEnd < content.length && content[lineEnd] === "\n") lineEnd++;

    const windowEnd = i + 1 < startMatches.length ? /** @type {number} */ startMatches[i + 1].index : content.length;

    let matchedEnd;
    for (let ei = 0; ei < endMarkers.length; ei++) {
      const e = endMarkers[ei];
      if (usedEnd[ei] || e.name !== name || e.start < lineEnd || e.start >= windowEnd) continue;
      matchedEnd = e;
      usedEnd[ei] = true;
      break;
    }

    if (matchedEnd === undefined) {
      const contentEnd = h2Positions.find(pos => pos >= lineEnd) ?? content.length;
      insertions.push({ pos: contentEnd, name });
    }
  }

  if (insertions.length === 0) return content;

  // Insert from the end of the string backwards so earlier offsets stay valid.
  insertions.sort((a, b) => b.pos - a.pos);
  let result = content;
  for (const { pos, name } of insertions) {
    const needsLeadingNewline = pos > 0 && result[pos - 1] !== "\n";
    const marker = `${needsLeadingNewline ? "\n" : ""}\n## end agent: \`${name}\`\n`;
    result = result.slice(0, pos) + marker + result.slice(pos);
  }
  return result;
}

/**
 * Returns the target directory (relative to agentsBaseDir) and filename extension
 * for inline sub-agent files based on the engine ID.
 *
 * Each AI engine stores its sub-agent definitions in a different location:
 *   claude   → .claude/agents/<name>.md
 *   codex    → .codex/agents/<name>.md
 *   gemini   → .gemini/agents/<name>.md
 *   copilot  → .github/agents/<name>.agent.md  (default)
 *   others   → .github/agents/<name>.agent.md  (fallback)
 *
 * @param {string} [engineId] - The engine identifier (e.g. "claude", "copilot").
 * @returns {{ dir: string, ext: string }}
 */
function getEngineSubAgentTarget(engineId) {
  switch ((engineId || "").toLowerCase()) {
    case "claude":
      return { dir: ".claude/agents", ext: ".md" };
    case "codex":
      return { dir: ".codex/agents", ext: ".md" };
    case "gemini":
      return { dir: ".gemini/agents", ext: ".md" };
    case "pi":
      return { dir: ".pi/agents", ext: ".md" };
    default:
      return { dir: ".github/agents", ext: ".agent.md" };
  }
}

/**
 * Extracts inline sub-agents from content and writes each one to the
 * engine-appropriate location under agentsBaseDir.
 *
 * The target directory and filename extension are determined by engineId:
 *   - claude  → <base>/.claude/agents/<name>.md
 *   - codex   → <base>/.codex/agents/<name>.md
 *   - gemini  → <base>/.gemini/agents/<name>.md
 *   - default → <base>/.github/agents/<name>.agent.md
 *
 * Returns the main content (before the first ## agent: marker) after stripping
 * all agent blocks.  When no agent markers are found the original content is
 * returned unchanged.
 *
 * Agent files are written relative to `agentsBaseDir` (defaults to `workspaceDir`).
 * Pass the gh-aw tmp directory (`/tmp/gh-aw`) as `agentsBaseDir` in production so
 * the files land under `/tmp/gh-aw/<engine-dir>/` — which is included in the
 * activation artifact and therefore available to the downstream agent job.
 *
 * @param {string} content - Markdown with potential inline sub-agent blocks.
 * @param {string} workspaceDir - GITHUB_WORKSPACE (repository root).
 * @param {string} [agentsBaseDir] - Root directory for agent output.
 *   Defaults to `workspaceDir` when omitted (for tests and legacy callers).
 * @param {string} [engineId] - The engine ID (e.g. "claude", "copilot").
 *   Defaults to "copilot" behavior when omitted.
 * @param {(content: string) => string} [restoreValues] - Restore literal context after extracting author-owned markers
 * @returns {string} Main content with sub-agent sections removed.
 */
function writeInlineSubAgents(content, workspaceDir, agentsBaseDir, engineId, restoreValues = content => content) {
  const { mainContent, agents } = extractInlineSubAgents(content);

  if (agents.length === 0) {
    return content;
  }

  const baseDir = agentsBaseDir || workspaceDir;
  // Normalise once so the target directory and the Claude `name:` injection always agree:
  // every id that stages into .claude/agents also gets the name Claude Code registers by.
  const normalizedEngineId = (engineId || "").trim().toLowerCase();
  const { dir, ext } = inlineArtifactTarget("SUB_AGENT", getEngineSubAgentTarget(normalizedEngineId));
  const agentsDir = path.join(baseDir, dir);
  core.info(`[extractInlineSubAgents] Engine: "${engineId || "(default)"}" → dir="${dir}" ext="${ext}"`);
  core.info(`[extractInlineSubAgents] Writing ${agents.length} sub-agent(s) to: ${agentsDir}`);
  try {
    fs.mkdirSync(agentsDir, { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create directory ${agentsDir}: ${String(err)}`, { cause: err });
  }

  for (const agent of agents) {
    const agentPath = path.join(agentsDir, agent.name + ext);
    let filteredContent = resolveSubAgentExperimentReferences(agent.name, preserveSubAgentFrontmatter(restoreValues(agent.content)));
    if (normalizedEngineId === "claude") filteredContent = ensureClaudeSubAgentName(agent.name, filteredContent);
    const agentContent = filteredContent.endsWith("\n") ? filteredContent : filteredContent + "\n";
    try {
      fs.writeFileSync(agentPath, agentContent, "utf8");
    } catch (err) {
      throw new Error(`Failed to write file ${agentPath}: ${String(err)}`, { cause: err });
    }
    core.info(`[extractInlineSubAgents] Written sub-agent: ${agentPath} (${agentContent.length} bytes)`);
  }

  core.info(`[extractInlineSubAgents] Done — ${agents.length} file(s) written to ${agentsDir}`);
  return mainContent;
}

/**
 * Read repository and workflow-written Copilot agents when the SDK session starts.
 * Workflow definitions override repository definitions with the same name.
 *
 * @param {string} workspaceDir
 * @param {string} agentsBaseDir
 * @param {(message: string) => void} logger
 * @returns {import("@github/copilot-sdk").CustomAgentConfig[]}
 */
function loadCopilotSDKCustomAgents(workspaceDir, agentsBaseDir, logger) {
  const { extractFrontmatterAndBody } = require("./frontmatter_hash_pure.cjs");
  const { parseDocument } = require("./work_queue_yaml.cjs");
  const target = inlineArtifactTarget("SUB_AGENT", getEngineSubAgentTarget("copilot"));
  const directories = new Set([path.join(workspaceDir, ".github/agents"), path.join(agentsBaseDir, target.dir)]);
  const agents = new Map();
  for (const directory of directories) {
    let entries;
    try {
      entries = fs.readdirSync(directory, { withFileTypes: true });
    } catch (error) {
      if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) logger(`warning: cannot read custom agents in ${directory}: ${String(error)}`);
      continue;
    }
    for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
      if (!entry.isFile() || !entry.name.endsWith(".agent.md")) continue;
      const agentPath = path.join(directory, entry.name);
      try {
        const { frontmatterText, markdown } = extractFrontmatterAndBody(fs.readFileSync(agentPath, "utf8"));
        /** @type {any} */
        const document = parseDocument(frontmatterText);
        if (!document) throw new Error("cannot parse agent frontmatter");
        if (document.errors.length) throw new Error(document.errors.map(error => error.message).join("; "));
        const frontmatter = document.toJS() || {};
        if (typeof frontmatter !== "object" || Array.isArray(frontmatter)) throw new Error("agent frontmatter must be a mapping");
        const name = typeof frontmatter.name === "string" && frontmatter.name.trim() ? frontmatter.name.trim() : entry.name.slice(0, -".agent.md".length);
        let tools = frontmatter.tools;
        if (typeof tools === "string")
          tools = tools
            .split(",")
            .map(tool => tool.trim())
            .filter(Boolean);
        if (tools !== undefined && tools !== null && (!Array.isArray(tools) || !tools.every(tool => typeof tool === "string"))) throw new Error("agent tools must be a list of names or a comma-separated string");
        agents.set(name, {
          name,
          displayName: name,
          prompt: markdown.trim(),
          ...(typeof frontmatter.description === "string" ? { description: frontmatter.description } : {}),
          ...(typeof frontmatter.model === "string" ? { model: frontmatter.model.trim() } : {}),
          ...(tools !== undefined ? { tools } : {}),
        });
      } catch (error) {
        logger(`warning: cannot load custom agent ${agentPath}: ${String(error)}`);
      }
    }
  }
  return [...agents.values()];
}

module.exports = {
  extractInlineSubAgents,
  writeInlineSubAgents,
  getEngineSubAgentTarget,
  preserveSubAgentFrontmatter,
  resolveSubAgentExperimentReferences,
  ensureClaudeSubAgentName,
  closeUnterminatedSubAgentMarkers,
  loadCopilotSDKCustomAgents,
};
