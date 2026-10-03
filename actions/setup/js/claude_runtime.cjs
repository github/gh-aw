// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { countPermissionDeniedIssues, extractDeniedCommands } = require("./permission_denied_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/** @param {string} output @returns {Array<Record<string, any>>} */
function claudeRecords(output) {
  const records = [];
  for (const line of output.split(/\r?\n/)) {
    try {
      const record = JSON.parse(line);
      if (record && typeof record === "object" && !Array.isArray(record)) records.push(record);
    } catch {
      // stderr diagnostics are not part of the JSONL event stream.
      continue;
    }
  }
  return records;
}

/** @param {string} output @returns {string} */
function claudeFailureEvidence(output) {
  return output
    .split(/\r?\n/)
    .filter(line => {
      try {
        const record = JSON.parse(line);
        if (record.type === "user") return false;
        if (record.type === "assistant") return record.is_api_error_message === true || record.error != null;
        if (record.type === "system" && record.error == null) return false;
        return record.type === "result" || record.type === "system" || record.error != null;
      } catch {
        return true;
      }
    })
    .map(line => {
      try {
        const record = JSON.parse(line);
        if (record.type !== "result") return line;
        const { result, permission_denials, ...metadata } = record;
        if (record.is_error === true) metadata.result = result;
        return JSON.stringify(metadata);
      } catch {
        return line;
      }
    })
    .join("\n");
}

/** @param {string} output @returns {boolean} */
function hasClaudeSessionProgress(output) {
  return claudeRecords(output).some(record => record.type === "assistant" && record.error == null && record.is_api_error_message !== true);
}

/** @param {string} output @returns {string | undefined} */
function claudeSessionId(output) {
  return claudeRecords(output).find(record => !record.parent_tool_use_id && typeof record.session_id === "string" && record.session_id.length > 0)?.session_id;
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
function claudeBareCapabilities(args, managedDir = "/tmp/gh-aw/.claude", tempDir = path.dirname(managedDir)) {
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
  const scope = `//${workspace.replace(/^\/+|\/+$/g, "")}/**`;
  const rules = ["Edit", "Write", "MultiEdit", "NotebookEdit"].map(tool => `${tool}(${scope})`).join(",");
  const result = [...args];
  const flag = result.indexOf("--disallowed-tools");
  if (flag >= 0 && result[flag + 1]) result[flag + 1] += `,${rules}`;
  else result.push("--disallowed-tools", rules);
  return result;
}

/** @param {string} outputPath @returns {number} */
function claudeSafeOutputsOffset(outputPath) {
  if (!outputPath) return 0;
  try {
    return fs.statSync(outputPath).size;
  } catch (error) {
    if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") return 0;
    throw new Error(`Failed to inspect Claude safe-outputs file: ${getErrorMessage(error)}`, { cause: error });
  }
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

module.exports = { claudeRecords, claudeFailureEvidence, hasClaudeSessionProgress, claudeSessionId, claudePermissionDenials, claudeBareCapabilities, claudeRepositoryEditPolicy, claudeSafeOutputsOffset, removeClaudePlugin };
