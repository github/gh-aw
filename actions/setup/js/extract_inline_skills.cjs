// @ts-check
/// <reference types="@actions/github-script" />

// extract_inline_skills.cjs
//
// Parses ## skill: `name` markers from workflow markdown and writes each skill
// block to the engine-appropriate skills folder.
//
// This step runs AFTER {{#runtime-import}} macros have been fully inlined by
// processRuntimeImports() in interpolate_prompt.cjs, ensuring that any imports
// inside a skill block are resolved before the skill file is written.
//
// Marker syntax
// ─────────────
//   ## skill: `name`       Opens a skill block.  name must start with a
//                          lowercase letter and contain only lowercase letters,
//                          digits, hyphens, or underscores (safe for filenames).
//   ## end skill: `name`   Optional. Explicitly closes the block opened by the
//                          matching "## skill: `name`" marker.
//
// A skill block ends at a matching "## end skill: `name`" marker if one is
// present, or otherwise at the next level-2 Markdown heading (## ...) or EOF.
// The explicit end marker lets a skill block be embedded in the middle of a
// document (for example via an import) without swallowing unrelated content
// that follows it, and lets the block itself contain nested "##" headings.
//
// Supported frontmatter fields (all others are stripped with a warning)
// ─────────────────────────────────────────────────────────────────────
//   description   Human-readable description of the skill's role.
//
// If no ## skill: markers are present the content is returned unchanged and no
// files are written.

const fs = require("fs");
const path = require("path");
const { collectInlineEndMarkers, unknownInlineEndMarkerError } = require("./inline_marker_helpers.cjs");

// Supported frontmatter fields for inline skills.
// Any other field is stripped with a warning.
const SUPPORTED_FRONTMATTER_FIELDS = ["description"];

// Regex for the start marker: ## skill: `name` (lowercase identifier)
const START_MARKER_RE = /^##[ \t]+skill:[ \t]+`([a-z][a-z0-9_-]*)`[ \t]*$/gm;

// Regex for the optional explicit end marker: ## end skill: `name`
const END_MARKER_RE = /^##[ \t]+end[ \t]+skill:[ \t]+`([a-z][a-z0-9_-]*)`[ \t]*$/gm;

// Regex for an inline sub-agent marker exactly at an implicit H2 boundary.
const AGENT_START_BOUNDARY_RE = /^##[ \t]+agent:[ \t]+`(?:[a-z][a-z0-9_-]*)`[ \t]*(?:\n|$)/;

// Regex that matches the start of any level-2 Markdown heading (## ).
// Used to find the boundary where each skill block ends when no explicit end
// marker is present.
const H2_HEADING_RE = /^##[ \t]/gm;

/**
 * Filters skill frontmatter to only retain supported fields.
 *
 * Only `description` is valid in a skill frontmatter block. Any other
 * top-level key is stripped and a warning is emitted.
 *
 * When no YAML frontmatter delimiter (`---`) is found at the start of the
 * content, the content is returned unchanged.
 *
 * @param {string} content   - Raw skill block content (frontmatter + prompt).
 * @param {string} skillName - Skill name used in log messages.
 * @returns {string} Content with only supported frontmatter fields retained.
 */
function filterInlineSkillFrontmatter(content, skillName) {
  // A YAML frontmatter block must start immediately at the beginning of the
  // content (after trimming performed by the caller).
  if (!content.startsWith("---\n")) {
    return content;
  }

  // Locate the closing delimiter.  We search for "\n---" starting after the
  // complete opening "---\n" (offset 4) to avoid matching the opening itself.
  const closeIdx = content.indexOf("\n---", 4);
  if (closeIdx === -1) {
    return content;
  }

  // Lines between the opening and closing "---".
  const fmLines = content.slice(4, closeIdx).split("\n");
  // Everything after the closing "\n---" (including the optional newline).
  const body = content.slice(closeIdx + 4);

  const kept = [];
  const stripped = [];

  for (const line of fmLines) {
    // Match a simple scalar YAML key at the start of the line.
    // YAML keys are plain identifiers (no hyphens).
    const keyMatch = line.match(/^([a-z_][a-z0-9_]*)[ \t]*:/);
    if (keyMatch) {
      const key = keyMatch[1];
      if (SUPPORTED_FRONTMATTER_FIELDS.includes(key)) {
        kept.push(line);
      } else {
        stripped.push(key);
      }
    } else {
      // Continuation / comment / blank line — keep only when at least one
      // supported key has already been accepted, so multi-line values (e.g.
      // `description: |`) are preserved correctly.
      if (kept.length > 0) {
        kept.push(line);
      }
    }
  }

  if (stripped.length > 0) {
    core.warning(`[extractInlineSkills] skill "${skillName}": unsupported frontmatter field(s) stripped: ${stripped.join(", ")} (only "description" is supported)`);
  }

  // If no supported fields remain, omit the frontmatter block entirely.
  if (kept.length === 0) {
    return body.replace(/^\n/, "");
  }

  return `---\n${kept.join("\n")}\n---${body}`;
}

/**
 * Extracts inline skills from markdown content.
 *
 * Returns the reassembled main content (with skill blocks removed) and an
 * array of extracted skills.
 *
 * A skill block extends from its start marker to a matching "## end skill:
 * `name`" marker if present, or otherwise to the next H2 heading or EOF. When
 * a skill is closed by an explicit end marker, any text following it (up to
 * the next start marker or EOF) is preserved in the main content rather than
 * discarded.
 *
 * Throws if an end marker's name does not correspond to any start marker of
 * the same name found within its search window (an "orphan" end marker),
 * which is almost always an authoring mistake such as a typo.
 *
 * @param {string} content - Markdown with potential inline skill blocks.
 * @returns {{ mainContent: string, skills: Array<{name: string, content: string}> }}
 */
function extractInlineSkills(content) {
  const startMatches = [...content.matchAll(START_MARKER_RE)];
  const endMarkers = collectInlineEndMarkers(content, END_MARKER_RE);

  if (startMatches.length === 0) {
    if (endMarkers.length > 0) {
      throw unknownInlineEndMarkerError(content, endMarkers[0], "skill");
    }
    return { mainContent: content, skills: [] };
  }

  // Collect all H2 heading positions for the implicit block boundary fallback.
  const h2Positions = [...content.matchAll(H2_HEADING_RE)].map(m => m.index).filter(i => i !== undefined);

  // Track explicit end markers so unused markers can be reported as orphans.
  const usedEnd = new Array(endMarkers.length).fill(false);

  /** @type {Array<{name: string, content: string}>} */
  const skills = [];
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

    let skillContent;
    let newCursor;
    const explicit = matchedEnd !== undefined;
    let preserveAfterImplicitBoundary = false;
    if (explicit) {
      skillContent = content.slice(lineEnd, matchedEnd.start).trim();
      newCursor = matchedEnd.end;
    } else {
      const contentEnd = h2Positions.find(pos => pos >= lineEnd) ?? content.length;
      skillContent = content.slice(lineEnd, contentEnd).trim();
      newCursor = contentEnd;
      if (AGENT_START_BOUNDARY_RE.test(content.slice(contentEnd))) {
        preserveAfterImplicitBoundary = true;
      }
    }

    skills.push({ name, content: skillContent });
    cursor = newCursor;
    prevExplicit = explicit || preserveAfterImplicitBoundary;
  }

  if (prevExplicit) {
    mainParts.push(content.slice(cursor));
  }

  const orphan = endMarkers.find((_, ei) => !usedEnd[ei]);
  if (orphan) {
    throw unknownInlineEndMarkerError(content, orphan, "skill");
  }

  const mainContent = mainParts
    .join("")
    .replace(/\n{3,}/g, "\n\n")
    .replace(/\n+$/, "");

  return { mainContent, skills };
}

/**
 * Ensures every "## skill: `name`" start marker in content has a matching
 * explicit "## end skill: `name`" marker, inserting one at the block's
 * implicit boundary (next H2 heading or EOF) for any start marker that
 * doesn't already have one.
 *
 * This is intended for content that is about to be spliced into a larger
 * document (for example a runtime-imported file). Without it, a skill block
 * that relies on implicit closing would expand to swallow whatever content
 * follows it once spliced in — including unrelated content from subsequent
 * imports or the main workflow body — instead of stopping where it would
 * have stopped when the file was considered on its own.
 *
 * Blocks that already have an explicit end marker are left untouched. The
 * boundary computed here matches exactly what {@link extractInlineSkills}
 * would already infer for implicit blocks, so this is a no-op with respect to
 * extraction results when the content is not spliced elsewhere.
 *
 * @param {string} content - Markdown that may contain "## skill:" blocks.
 * @returns {string} Content with implicit end markers made explicit.
 */
function closeUnterminatedSkillMarkers(content) {
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
    const marker = `${needsLeadingNewline ? "\n" : ""}\n## end skill: \`${name}\`\n`;
    result = result.slice(0, pos) + marker + result.slice(pos);
  }
  return result;
}

/**
 * Returns the target directory (relative to skillsBaseDir) and filename extension
 * for inline skill files based on the engine ID.
 *
 * Each AI engine stores its skill definitions in a different location:
 *   claude   → .claude/skills/<name>.md
 *   codex    → .codex/skills/<name>/SKILL.md
 *   gemini   → .gemini/skills/<name>.md
 *   copilot  → .github/skills/<name>/SKILL.md  (default)
 *   others   → .github/skills/<name>/SKILL.md  (fallback)
 *
 * @param {string} [engineId] - The engine identifier (e.g. "claude", "copilot").
 * @returns {{ dir: string, ext: string }}
 */
function getEngineSkillTarget(engineId) {
  switch ((engineId || "").toLowerCase()) {
    case "claude":
      return { dir: ".claude/skills", ext: ".md" };
    case "codex":
      return { dir: ".codex/skills", ext: "/SKILL.md" };
    case "gemini":
      return { dir: ".gemini/skills", ext: ".md" };
    default:
      return { dir: ".github/skills", ext: "/SKILL.md" };
  }
}

/**
 * Extracts inline skills from content and writes each one to the
 * engine-appropriate location under skillsBaseDir.
 *
 * The target directory and filename extension are determined by engineId:
 *   - claude  → <base>/.claude/skills/<name>.md
 *   - codex   → <base>/.codex/skills/<name>/SKILL.md
 *   - gemini  → <base>/.gemini/skills/<name>.md
 *   - default → <base>/.github/skills/<name>/SKILL.md
 *
 * Returns the main content (before the first ## skill: marker) after stripping
 * all skill blocks.  When no skill markers are found the original content is
 * returned unchanged.
 *
 * Skill files are written relative to `skillsBaseDir` (defaults to `workspaceDir`).
 * Pass the gh-aw tmp directory (`/tmp/gh-aw`) as `agentsBaseDir` in production so
 * the files land under `/tmp/gh-aw/<engine-dir>/` — which is included in the
 * activation artifact and therefore available to the downstream agent job.
 *
 * @param {string} content - Markdown with potential inline skill blocks.
 * @param {string} workspaceDir - GITHUB_WORKSPACE (repository root).
 * @param {string} [skillsBaseDir] - Root directory for skill output.
 *   Defaults to `workspaceDir` when omitted (for tests and legacy callers).
 * @param {string} [engineId] - The engine ID (e.g. "claude", "copilot").
 *   Defaults to "copilot" behavior when omitted.
 * @returns {string} Main content with skill sections removed.
 */
function writeInlineSkills(content, workspaceDir, skillsBaseDir, engineId) {
  const { mainContent, skills } = extractInlineSkills(content);

  if (skills.length === 0) {
    return content;
  }

  const baseDir = skillsBaseDir || workspaceDir;
  const { dir, ext } = getEngineSkillTarget(engineId);
  const skillsDir = path.join(baseDir, dir);
  core.info(`[extractInlineSkills] Engine: "${engineId || "(default)"}" → dir="${dir}" ext="${ext}"`);
  core.info(`[extractInlineSkills] Writing ${skills.length} skill(s) to: ${skillsDir}`);
  try {
    fs.mkdirSync(skillsDir, { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create directory ${skillsDir}: ${String(err)}`, { cause: err });
  }

  for (const skill of skills) {
    const skillPath = path.join(skillsDir, skill.name + ext);
    try {
      fs.mkdirSync(path.dirname(skillPath), { recursive: true });
    } catch (err) {
      throw new Error(`Failed to create directory ${path.dirname(skillPath)}: ${String(err)}`, { cause: err });
    }
    const filteredContent = filterInlineSkillFrontmatter(skill.content, skill.name);
    const skillContent = filteredContent.endsWith("\n") ? filteredContent : filteredContent + "\n";
    try {
      fs.writeFileSync(skillPath, skillContent, "utf8");
    } catch (err) {
      throw new Error(`Failed to write file ${skillPath}: ${String(err)}`, { cause: err });
    }
    core.info(`[extractInlineSkills] Written skill: ${skillPath} (${skillContent.length} bytes)`);
  }

  core.info(`[extractInlineSkills] Done — ${skills.length} file(s) written to ${skillsDir}`);
  return mainContent;
}

module.exports = { extractInlineSkills, writeInlineSkills, getEngineSkillTarget, filterInlineSkillFrontmatter, closeUnterminatedSkillMarkers };
