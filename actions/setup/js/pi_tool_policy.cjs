// @ts-check
"use strict";

const { splitOnPipelineOperators, extractCommandName } = require("./bash_command_parser.cjs");
const { parseMaxToolCalls } = require("./copilot_sdk_tool_budget.cjs");
const fs = require("node:fs");
const path = require("node:path");
const { getErrorMessage } = require("./error_helpers.cjs");

/** @param {string} kind @param {number} count @param {number|undefined} limit @returns {number} */
function reservePiBudget(kind, count, limit) {
  const root = process.env.GH_AW_PI_TOOL_BUDGET_DIR;
  if (!root || limit === undefined) return count + 1;
  const dir = path.join(root, kind);
  try {
    fs.mkdirSync(dir, { recursive: true });
  } catch (error) {
    throw new Error(`Cannot initialize shared Pi ${kind} budget: ${getErrorMessage(error)}`, { cause: error });
  }
  // Exclusive creation reserves a slot atomically across the parent and all child processes.
  for (let next = count + 1; next <= limit; next++) {
    try {
      const fd = fs.openSync(path.join(dir, String(next)), "wx", 0o600);
      fs.closeSync(fd);
      return next;
    } catch (error) {
      if (!(error instanceof Error) || !("code" in error) || error.code !== "EEXIST") throw error;
    }
  }
  return limit + 1;
}

/** @param {unknown} command @param {unknown} allowed @returns {boolean} */
function isPiBashAllowed(command, allowed) {
  if (allowed === false || (Array.isArray(allowed) && allowed.length === 0)) return false;
  if (!Array.isArray(allowed) || allowed.includes("*") || allowed.includes(":*")) return true;
  if (typeof command !== "string" || !command.trim()) return false;
  // Dynamic expansions, redirections, grouping, and background execution are
  // deliberately refused rather than attempting to infer their executable scope.
  if (/[$`\\()<>{}#]/.test(command) || /(^|[^&])&([^&]|$)/.test(command)) return false;
  const segments = splitOnPipelineOperators(command);
  return (
    segments.length > 0 &&
    segments.every(segment => {
      const executable = extractCommandName(segment);
      if (!executable) return false;
      return allowed.some(pattern => {
        if (typeof pattern !== "string") return false;
        const wildcard = /(?::\*| \*)$/.test(pattern);
        const prefix = pattern.replace(/(?::\*| \*)$/, "").trim();
        if (segment === prefix) return true;
        if (!prefix.includes(" ") && !wildcard) return executable === prefix;
        return wildcard && segment.startsWith(prefix + " ") && extractCommandName(prefix) === executable;
      });
    })
  );
}

/** @param {any} pi */
function piToolPolicy(pi) {
  const policy = JSON.parse(process.env.GH_AW_PI_TOOL_POLICY || "{}");
  const limit = process.env.GH_AW_MAX_TOOL_CALLS ? parseMaxToolCalls(process.env.GH_AW_MAX_TOOL_CALLS) : undefined;
  const denialLimit = process.env.GH_AW_MAX_TOOL_DENIALS ? parseMaxToolCalls(process.env.GH_AW_MAX_TOOL_DENIALS) : undefined;
  let callCount = 0;
  let denialCount = 0;
  pi.on("tool_call", (event, ctx) => {
    try {
      callCount = reservePiBudget("calls", callCount, limit);
      let reason;
      if (limit !== undefined && callCount > limit) reason = `Workflow tool-call budget exhausted (${limit} calls)`;
      else if (event.toolName === "bash" && !isPiBashAllowed(event.input?.command, policy.bash)) reason = "Command is outside the workflow tools.bash allowlist";
      else if (policy.edit === false && ["edit", "write"].includes(event.toolName)) reason = "File editing is disabled by the workflow";
      if (!reason) return;
      denialCount = reservePiBudget("denials", denialCount, denialLimit);
      process.stderr.write(`[gh-aw/pi-tool-policy] denied tool=${event.toolName} calls=${callCount} reason=${reason}\n`);
      if (denialLimit !== undefined && denialCount >= denialLimit) {
        ctx.abort();
        process.exitCode = 1;
      }
      return { block: true, reason };
    } catch (error) {
      const reason = `Cannot enforce shared Pi tool budget: ${getErrorMessage(error)}`;
      process.stderr.write(`[gh-aw/pi-tool-policy] denied tool=${event.toolName} reason=${reason}\n`);
      ctx.abort();
      process.exitCode = 1;
      return { block: true, reason };
    }
  });
}

module.exports = Object.assign(piToolPolicy, { isPiBashAllowed, reservePiBudget });
