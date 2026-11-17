// @ts-check
"use strict";

// pi_workflow_skills.cjs
//
// Derives the explicit skill load list for bare Pi runs from the inline skills
// that activation actually extracted and staged (see extract_inline_skills.cjs).
// Bare mode disables ambient skill discovery, so only these workflow-owned skills
// are loaded, through one list shared by the CLI, sub-agents, and SDK/RPC drivers.

const fs = require("node:fs");
const path = require("node:path");

// Written by activation next to the extracted skills; listed in extraction order.
const INLINE_SKILL_MANIFEST = ".gh-aw-inline-skills.json";
// Runtime list consumed by the CLI command line and the sub-agent extension.
const WORKFLOW_SKILL_ARGS_FILE = "workflow-skill-args";
const SKILL_NAME_RE = /^[a-z][a-z0-9_-]*$/;
// Paths are word-split by the shell command line, so only allow characters that are inert there.
const SAFE_PATH_RE = /^[A-Za-z0-9._/-]+$/;

/**
 * Returns the staged SKILL.md paths of the workflow inline skills listed in the
 * activation manifest. Missing manifests yield an empty list.
 * @param {string} agentDir - Managed Pi agent directory holding the staged skills.
 * @param {string} [stagingDir] - Activation artifact Pi directory.
 * @returns {string[]}
 */
function resolveWorkflowSkillPaths(agentDir, stagingDir = process.env.GH_AW_PI_STAGING_DIR || require("./runtime_paths.cjs").runtimePath(".pi")) {
  const manifestPath = path.join(stagingDir, "skills", INLINE_SKILL_MANIFEST);
  let names;
  try {
    names = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return [];
    throw new Error(`Invalid Pi inline skill manifest ${manifestPath}`, { cause: error });
  }
  if (!Array.isArray(names)) throw new Error(`Invalid Pi inline skill manifest ${manifestPath}`);
  const paths = [];
  for (const name of new Set(names)) {
    if (typeof name !== "string" || !SKILL_NAME_RE.test(name)) throw new Error(`Invalid Pi inline skill name in manifest: ${JSON.stringify(name)}`);
    const skillPath = path.join(agentDir, "skills", name, "SKILL.md");
    if (!SAFE_PATH_RE.test(skillPath)) throw new Error(`Unsafe Pi skill path: ${skillPath}`);
    let stat;
    try {
      stat = fs.lstatSync(skillPath);
    } catch {
      continue;
    }
    if (stat.isFile()) paths.push(skillPath);
  }
  return paths;
}

/** @param {string[]} paths @returns {string[]} */
function skillArgs(paths) {
  return paths.flatMap(skillPath => ["--skill", skillPath]);
}

/**
 * Writes the explicit --skill arguments, one token per line, for the CLI and sub-agents.
 * @param {string} agentDir
 * @param {string[]} paths
 */
function writeWorkflowSkillArgs(agentDir, paths) {
  fs.mkdirSync(agentDir, { recursive: true });
  const args = skillArgs(paths);
  fs.writeFileSync(path.join(agentDir, WORKFLOW_SKILL_ARGS_FILE), args.length ? args.join("\n") + "\n" : "", { mode: 0o600 });
}

/**
 * Reads the explicit --skill arguments written by {@link writeWorkflowSkillArgs}.
 * Only well-formed `--skill <agentDir>/skills/<name>/SKILL.md` pairs are accepted.
 * @param {string} [agentDir]
 * @returns {string[]}
 */
function readWorkflowSkillArgs(agentDir = process.env.PI_CODING_AGENT_DIR || require("./runtime_paths.cjs").runtimePath("pi-agent-dir")) {
  let raw;
  try {
    raw = fs.readFileSync(path.join(agentDir, WORKFLOW_SKILL_ARGS_FILE), "utf8");
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return [];
    throw error;
  }
  const tokens = raw.split("\n").filter(Boolean);
  const skillsDir = path.join(agentDir, "skills");
  for (let i = 0; i < tokens.length; i += 2) {
    const skillPath = tokens[i + 1] || "";
    const name = path.relative(skillsDir, path.dirname(skillPath));
    if (tokens[i] !== "--skill" || path.join(skillsDir, name, "SKILL.md") !== skillPath || !SKILL_NAME_RE.test(name)) {
      throw new Error("Invalid Pi workflow skill arguments");
    }
  }
  return tokens;
}

module.exports = { INLINE_SKILL_MANIFEST, WORKFLOW_SKILL_ARGS_FILE, resolveWorkflowSkillPaths, skillArgs, writeWorkflowSkillArgs, readWorkflowSkillArgs };
