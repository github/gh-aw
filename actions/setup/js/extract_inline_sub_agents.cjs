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
 * @returns {string} Main content with sub-agent sections removed.
 */
function writeInlineSubAgents(content, workspaceDir, agentsBaseDir, engineId) {
  const { mainContent, agents } = extractInlineSubAgents(content);

  if (agents.length === 0) {
    return content;
  }

  const baseDir = agentsBaseDir || workspaceDir;
  const { dir, ext } = inlineArtifactTarget("SUB_AGENT", getEngineSubAgentTarget(engineId));
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
    const filteredContent = preserveSubAgentFrontmatter(agent.content);
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

module.exports = { extractInlineSubAgents, writeInlineSubAgents, getEngineSubAgentTarget, preserveSubAgentFrontmatter, closeUnterminatedSubAgentMarkers };
