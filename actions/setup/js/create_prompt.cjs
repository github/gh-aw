// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_CONFIG, ERR_PARSE, ERR_SYSTEM } = require("./error_codes.cjs");
const { readInboundAwContext } = require("./aw_context.cjs");
const LEDGER_REPLAY_PROMPT = "/tmp/gh-aw/ledgers/replay-prompt.txt";

/**
 * @typedef {Object} PromptRenderItem
 * @property {string} [content_env]
 * @property {string} [file]
 * @property {string} [condition_env]
 * @property {boolean} [ledger_replay]
 */

/**
 * @typedef {Object} PromptRenderConfig
 * @property {PromptRenderItem[]} items
 * @property {number} [system_item_count]
 */

/**
 * Parse and validate the compiler-generated prompt render configuration.
 * @param {string} value
 * @returns {PromptRenderConfig}
 */
function parseConfig(value) {
  let parsed;
  try {
    parsed = JSON.parse(value);
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Invalid GH_AW_PROMPT_CONFIG: ${getErrorMessage(error)}`, { cause: error });
  }
  if (!parsed || !Array.isArray(parsed.items)) {
    throw new Error(`${ERR_CONFIG}: GH_AW_PROMPT_CONFIG must contain an items array`);
  }
  if (parsed.system_item_count !== undefined && (!Number.isInteger(parsed.system_item_count) || parsed.system_item_count < 0 || parsed.system_item_count > parsed.items.length)) {
    throw new Error(`${ERR_CONFIG}: GH_AW_PROMPT_CONFIG has an invalid system_item_count`);
  }
  return parsed;
}

/**
 * Resolve a compiler-owned prompt file without allowing traversal outside the
 * setup action's prompt directory.
 * @param {string} promptsDir
 * @param {string} filename
 * @returns {string}
 */
function resolvePromptFile(promptsDir, filename) {
  if (!filename || path.isAbsolute(filename)) {
    throw new Error(`${ERR_CONFIG}: Prompt file must be a relative path`);
  }
  const root = fs.realpathSync(promptsDir);
  const unresolved = path.resolve(root, filename);
  const unresolvedRelative = path.relative(root, unresolved);
  if (unresolvedRelative === ".." || unresolvedRelative.startsWith(`..${path.sep}`) || path.isAbsolute(unresolvedRelative)) {
    throw new Error(`${ERR_CONFIG}: Prompt file must stay within the prompt directory`);
  }
  const resolved = fs.realpathSync(unresolved);
  const relative = path.relative(root, resolved);
  if (relative === ".." || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    throw new Error(`${ERR_CONFIG}: Prompt file must stay within the prompt directory`);
  }
  return resolved;
}

/**
 * Check that a path stays inside a directory.
 * @param {string} root
 * @param {string} candidate
 */
function assertPathWithin(root, candidate) {
  const relative = path.relative(root, candidate);
  if (relative === ".." || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    throw new Error(`${ERR_CONFIG}: Prompt output must stay within the runner temp directory`);
  }
}

/**
 * Write a prompt without exposing its contents through pre-existing permissions.
 * @param {string} promptPath
 * @param {string} content
 */
function writePromptFile(promptPath, content) {
  const flags = fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_TRUNC | (fs.constants.O_NOFOLLOW || 0);
  const fd = fs.openSync(promptPath, flags, 0o600);
  try {
    fs.fchmodSync(fd, 0o600);
    try {
      fs.writeFileSync(fd, content, "utf8");
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to write prompt file ${promptPath}: ${getErrorMessage(error)}`, { cause: error });
    }
  } finally {
    fs.closeSync(fd);
  }
}

/**
 * Render prompt items. All workflow-authored content and expression results are
 * read from environment variables and appended as data.
 * @param {PromptRenderConfig} config
 * @param {NodeJS.ProcessEnv} env
 * @param {string} promptsDir
 * @param {string} [replayPromptPath]
 * @returns {string}
 */
function renderPrompt(config, env, promptsDir, replayPromptPath = LEDGER_REPLAY_PROMPT) {
  let result = "";

  for (const item of config.items) {
    if (!item || typeof item !== "object") {
      throw new Error(`${ERR_CONFIG}: Prompt render item must be an object`);
    }
    if (item.condition_env && env[item.condition_env] !== "true") {
      continue;
    }
    if (item.ledger_replay === true && item.content_env === undefined && item.file === undefined) {
      const stat = fs.lstatSync(replayPromptPath);
      if (!stat.isFile() || stat.isSymbolicLink() || stat.size > 65536) throw new Error(`${ERR_CONFIG}: Invalid ledger replay prompt`);
      result += fs.readFileSync(replayPromptPath, "utf8");
      continue;
    }

    const contentEnv = item.content_env;
    const filename = item.file;
    const hasContent = typeof contentEnv === "string";
    const hasFile = typeof filename === "string";
    if (hasContent === hasFile) {
      throw new Error(`${ERR_CONFIG}: Prompt render item must specify exactly one content_env or file`);
    }

    if (typeof contentEnv === "string") {
      if (!Object.prototype.hasOwnProperty.call(env, contentEnv)) {
        throw new Error(`${ERR_CONFIG}: Prompt content environment variable is missing: ${contentEnv}`);
      }
      result += env[contentEnv] || "";
      continue;
    }

    if (typeof filename !== "string") {
      throw new Error(`${ERR_CONFIG}: Prompt render item file must be a string`);
    }
    const promptFile = resolvePromptFile(promptsDir, filename);
    try {
      result += fs.readFileSync(promptFile, "utf8");
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to read prompt file: ${getErrorMessage(error)}`, { cause: error });
    }
  }

  return result;
}

/**
 * @param {unknown} value
 * @returns {value is Record<string, unknown>}
 */
function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function renderWorkClaim(payload) {
  const assignment = readInboundAwContext(payload)?.work_claim;
  if (!isRecord(assignment) || typeof assignment.work_id !== "string" || !assignment.work_id || typeof assignment.claim_id !== "string" || !assignment.claim_id || !isRecord(assignment.work)) {
    return "";
  }

  const work = JSON.stringify({ id: assignment.work_id, payload: assignment.work }).replace(/</g, "\\u003c");
  const claim = JSON.stringify({ id: assignment.claim_id, work_id: assignment.work_id }).replace(/</g, "\\u003c");
  return `<work-claim>\n${work}\n${claim}\nWhen the work is complete, call dispatch_claim_finish with outcome "completed" to record the finish intent. If you cannot complete the work, call it with outcome "cancelled". The claim is only complete after trusted reconciliation verifies it; recording intent alone does not authorize safe outputs.\n</work-claim>\n`;
}

/**
 * @param {typeof import('@actions/core')} core - GitHub Actions core library
 * @returns {Promise<void>}
 */
async function main(core) {
  try {
    const promptPath = process.env.GH_AW_PROMPT;
    const configValue = process.env.GH_AW_PROMPT_CONFIG;
    const runnerTemp = process.env.RUNNER_TEMP;
    if (!promptPath) {
      throw new Error(`${ERR_CONFIG}: GH_AW_PROMPT environment variable is not set`);
    }
    if (!configValue) {
      throw new Error(`${ERR_CONFIG}: GH_AW_PROMPT_CONFIG environment variable is not set`);
    }
    if (!runnerTemp) {
      throw new Error(`${ERR_CONFIG}: RUNNER_TEMP environment variable is not set`);
    }

    const config = parseConfig(configValue);
    const promptsDir = path.join(runnerTemp, "gh-aw", "prompts");
    const promptOutputDir = path.join(runnerTemp, "gh-aw", "aw-prompts");
    const systemItemCount = config.system_item_count ?? 0;
    const systemContent = renderPrompt({ items: config.items.slice(0, systemItemCount) }, process.env, promptsDir);
    const userContent = renderWorkClaim(typeof context === "undefined" ? null : context.payload) + renderPrompt({ items: config.items.slice(systemItemCount) }, process.env, promptsDir);
    const content = systemContent + userContent;

    fs.mkdirSync(promptOutputDir, { recursive: true, mode: 0o700 });
    const unresolvedOutputDir = path.resolve(promptOutputDir);
    const unresolvedPromptPath = path.resolve(promptPath);
    assertPathWithin(unresolvedOutputDir, unresolvedPromptPath);
    const resolvedOutputDir = fs.realpathSync(promptOutputDir);
    assertPathWithin(fs.realpathSync(runnerTemp), resolvedOutputDir);
    const resolvedPromptPath = path.resolve(resolvedOutputDir, path.relative(unresolvedOutputDir, unresolvedPromptPath));
    writePromptFile(path.join(resolvedOutputDir, "system.txt"), systemContent);
    writePromptFile(path.join(resolvedOutputDir, "user.txt"), userContent);
    writePromptFile(resolvedPromptPath, content);
    core.info(`Created prompt at ${resolvedPromptPath} (${Buffer.byteLength(content, "utf8")} bytes)`);
  } catch (error) {
    core.setFailed(`${ERR_CONFIG}: ${getErrorMessage(error)}`);
  }
}

module.exports = { main, parseConfig, renderPrompt, resolvePromptFile };
