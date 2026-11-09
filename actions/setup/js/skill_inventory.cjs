// @ts-check

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { readMarkdownMetadata } = require("./markdown_metadata.cjs");

const INVENTORY_PATH = "/tmp/gh-aw/skill_inventory.json";
const INSTALL_HISTORY_PATH = "/tmp/gh-aw/skill_install_history.json";

function declaredPurpose(text) {
  const frontmatter = text.match(/^---[ \t]*\r?\n([\s\S]*?)^---[ \t]*(?:\r?\n|$)/m)?.[1];
  if (frontmatter === undefined) return undefined;
  const metadata = readMarkdownMetadata("agent", text).value;
  if (!metadata || typeof metadata !== "object" || Array.isArray(metadata) || typeof metadata.description !== "string") return undefined;
  return metadata.description.trim() || undefined;
}

/**
 * Inventory only regular skill files in the staged engine directory and the
 * checked-out repository. Never follow links outside either tree.
 * @param {string} directory
 * @param {string} scope
 * @param {string} source
 */
function inventoryDirectory(directory, scope, source) {
  if (!fs.existsSync(directory) || fs.lstatSync(directory).isSymbolicLink()) return [];
  const skills = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))) {
    if (!entry.isDirectory()) continue;
    const file = path.join(directory, entry.name, "SKILL.md");
    if (!fs.existsSync(file) || !fs.lstatSync(file).isFile()) continue;
    const content = fs.readFileSync(file);
    const purpose = declaredPurpose(content.toString("utf8"));
    skills.push({ name: entry.name, source: `${source}/${entry.name}`, scope, digest: `sha256:${crypto.createHash("sha256").update(content).digest("hex")}`, ...(purpose ? { purpose } : {}) });
  }
  return skills;
}

/** @param {Array<{name: string, source: string, scope: string, digest: string}>} skills */
function detectSkillConflicts(skills) {
  const groups = new Map();
  for (const skill of skills) groups.set(skill.name, [...(groups.get(skill.name) || []), skill]);
  return [...groups]
    .filter(([, entries]) => entries.length > 1 && (new Set(entries.map(entry => entry.digest)).size > 1 || new Set(entries.map(entry => entry.scope)).size > 1))
    .map(([name, entries]) => ({
      name,
      kind: new Set(entries.map(entry => entry.scope)).size > 1 ? "scope_collision" : "name_collision",
      sources: entries.map(entry => entry.source).sort(),
    }));
}

function main({ stagedRoot = "/tmp/gh-aw", inventoryPath = INVENTORY_PATH, historyPath = INSTALL_HISTORY_PATH } = {}) {
  const skillDir = process.env.GH_AW_SKILL_DIR || "";
  const workspace = process.env.GITHUB_WORKSPACE || "";
  const resolvedStagedRoot = path.resolve(stagedRoot);
  const staged = path.resolve(resolvedStagedRoot, skillDir);
  if (!skillDir || (staged !== resolvedStagedRoot && !staged.startsWith(resolvedStagedRoot + path.sep))) throw new Error("Invalid staged skill directory");
  const skills = inventoryDirectory(staged, "engine", `staged/${skillDir}`);
  if (workspace) {
    for (const directory of ["skills", ".github/skills"]) {
      skills.push(...inventoryDirectory(path.join(workspace, directory), "repository", directory));
    }
  }
  skills.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0) || (a.source < b.source ? -1 : a.source > b.source ? 1 : 0));
  const conflicts = detectSkillConflicts(skills);
  if (fs.existsSync(historyPath)) {
    const history = JSON.parse(fs.readFileSync(historyPath, "utf8"));
    if (!Array.isArray(history)) throw new Error("Invalid skill installation history");
    for (const conflict of detectSkillConflicts(history.filter(entry => entry && typeof entry.name === "string" && typeof entry.digest === "string" && typeof entry.source === "string").map(entry => ({ ...entry, scope: "engine" })))) {
      const existing = conflicts.find(item => item.name === conflict.name && item.kind === conflict.kind);
      if (existing) existing.sources = [...new Set([...existing.sources, ...conflict.sources])].sort();
      else conflicts.push(conflict);
    }
  }
  fs.mkdirSync(path.dirname(inventoryPath), { recursive: true });
  fs.writeFileSync(inventoryPath, JSON.stringify({ skills, conflicts }) + "\n");
  for (const conflict of conflicts) core.warning(`Skill conflict (${conflict.kind}): ${conflict.name} in ${conflict.sources.join(", ")}`);
  core.info(`Inventoried ${skills.length} skills; ${conflicts.length} conflict(s)`);
}

module.exports = { main, inventoryDirectory, detectSkillConflicts, declaredPurpose };
