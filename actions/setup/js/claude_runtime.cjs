// @ts-check
"use strict";

const { iterateJsonl, iterateJsonlEvents } = require("./jsonl_helpers.cjs");
const fs = require("fs");
const path = require("path");
const { countPermissionDeniedIssues, extractDeniedCommands } = require("./permission_denied_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { getSafeOutputsByteOffset } = require("./harness_safe_outputs.cjs");

const CLAUDE_RESUME_PROMPT = "Continue the interrupted task using the existing session. Preserve completed work and staged safe outputs; do not repeat them. Complete only unfinished work.";

/** @param {string} output @returns {Array<Record<string, any>>} */
function claudeRecords(output) {
  const records = [];
  for (const record of iterateJsonl(output)) {
    if (record && typeof record === "object" && !Array.isArray(record)) records.push(record);
  }
  return records;
}

/** @param {string} output @returns {string} */
function claudeFailureEvidence(output) {
  const lines = [];
  for (const event of iterateJsonlEvents(output)) {
    const line = event.rawLine.replace(/\r?\n$/, "");
    if (event.type === "invalid" || event.value === null) {
      lines.push(line);
      continue;
    }
    const record = event.value;
    if (record.type === "user") continue;
    if (record.type === "assistant") {
      if (record.is_api_error_message !== true && record.error == null) continue;
    } else if (record.type === "system" && record.error == null) continue;
    else if (record.type !== "result" && record.type !== "system" && record.error == null) continue;
    if (record.type === "result") {
      const { result, permission_denials, ...metadata } = record;
      if (record.is_error === true) metadata.result = result;
      lines.push(JSON.stringify(metadata));
    } else {
      lines.push(line);
    }
  }
  if (output.endsWith("\n")) lines.push("");
  return lines.join("\n");
}

/** @param {string} output @returns {boolean} */
function hasClaudeSessionProgress(output) {
  for (const record of iterateJsonl(output)) {
    if (record?.type === "assistant" && record.error == null && record.is_api_error_message !== true) return true;
  }
  return false;
}

/** @param {string} output @returns {string | undefined} */
function claudeSessionId(output) {
  for (const record of iterateJsonl(output)) {
    if (record && !record.parent_tool_use_id && typeof record.session_id === "string" && record.session_id.length > 0) return record.session_id;
  }
  return undefined;
}

/** @param {string} output @returns {{count: number, commands: string[]}} */
function claudePermissionDenials(output) {
  const denials = new Map();
  for (const record of claudeRecords(output)) {
    if (record.type !== "result" || !Array.isArray(record.permission_denials)) continue;
    for (const denial of record.permission_denials) {
      if (!denial || typeof denial !== "object") continue;
      denials.set(denial.tool_use_id ?? JSON.stringify(denial), denial);
    }
  }
  const evidence = claudeFailureEvidence(output);
  return {
    count: denials.size || countPermissionDeniedIssues(evidence),
    commands: [
      ...new Set(
        [...denials.values()]
          .map(denial => denial.tool_input?.command)
          .filter(command => typeof command === "string")
          .concat(extractDeniedCommands(evidence))
      ),
    ],
  };
}

/** @param {string[]} args @param {string} [managedDir] @param {string} [tempDir] */
function claudeBareCapabilities(args, managedDir = "/tmp/gh-aw/.claude", tempDir = "/tmp/gh-aw/agent") {
  if (!args.includes("--bare")) return { args, pluginDir: undefined };
  const directories = ["skills", "agents"].filter(name => {
    try {
      return fs.statSync(path.join(managedDir, name)).isDirectory();
    } catch (error) {
      if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") return false;
      throw error;
    }
  });
  if (directories.length === 0) return { args, pluginDir: undefined };
  let pluginDir;
  try {
    fs.mkdirSync(tempDir, { recursive: true, mode: 0o700 });
    pluginDir = fs.mkdtempSync(path.join(tempDir, "gh-aw-claude-workflow-"));
    fs.mkdirSync(path.join(pluginDir, ".claude-plugin"));
    fs.writeFileSync(path.join(pluginDir, ".claude-plugin/plugin.json"), JSON.stringify({ name: "gh-aw-workflow", version: "1.0.0", description: "Workflow-declared skills and subagents" }), { mode: 0o600 });
    for (const name of directories) fs.symlinkSync(path.join(managedDir, name), path.join(pluginDir, name), "dir");
    return { args: [...args, "--plugin-dir", pluginDir], pluginDir };
  } catch (error) {
    removeClaudePlugin(pluginDir);
    throw new Error(`Failed to prepare Claude workflow capabilities: ${getErrorMessage(error)}`, { cause: error });
  }
}

/** @param {string[]} args @param {NodeJS.ProcessEnv} env */
function claudeRepositoryEditPolicy(args, env) {
  if (env.GH_AW_CLAUDE_DISABLE_REPO_EDITS !== "true") return args;
  const workspace = env.GITHUB_WORKSPACE;
  if (!workspace || !path.isAbsolute(workspace)) throw new Error("tools.edit: false requires an absolute GITHUB_WORKSPACE");
  // Claude consults Edit(path) for file permissions, not Write/NotebookEdit/MultiEdit(path).
  // Remove those native editors entirely, in sync with claudeDisabledTools in Go.
  const rules = [`Edit(//${workspace.replace(/^\/+|\/+$/g, "")}/**)`, "Write", "MultiEdit", "NotebookEdit"];
  const result = [...args];
  const flag = result.indexOf("--disallowed-tools");
  if (flag >= 0 && result[flag + 1]) result[flag + 1] = [...new Set([...result[flag + 1].split(","), ...rules])].join(",");
  else result.push("--disallowed-tools", rules.join(","));
  return result;
}

/** @param {string} outputPath @returns {number} */
function claudeSafeOutputsOffset(outputPath) {
  return getSafeOutputsByteOffset(outputPath, { strict: true, errorLabel: "Claude safe-outputs file" });
}

/** @param {string | undefined} pluginDir */
function removeClaudePlugin(pluginDir) {
  if (!pluginDir) return;
  try {
    fs.rmSync(pluginDir, { recursive: true, force: true });
  } catch (error) {
    throw new Error(`Failed to remove temporary Claude workflow plugin: ${getErrorMessage(error)}`, { cause: error });
  }
}

module.exports = {
  CLAUDE_RESUME_PROMPT,
  claudeRecords,
  claudeFailureEvidence,
  hasClaudeSessionProgress,
  claudeSessionId,
  claudePermissionDenials,
  claudeBareCapabilities,
  claudeRepositoryEditPolicy,
  claudeSafeOutputsOffset,
  removeClaudePlugin,
};
