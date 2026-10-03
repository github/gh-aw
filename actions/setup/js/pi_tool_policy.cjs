// @ts-check
"use strict";

const { splitOnPipelineOperators, extractCommandName } = require("./bash_command_parser.cjs");
const { parseMaxToolCalls } = require("./copilot_sdk_tool_budget.cjs");

/** @param {unknown} command @param {unknown} allowed @returns {boolean} */
function isPiBashAllowed(command, allowed) {
  if (allowed === false || (Array.isArray(allowed) && allowed.length === 0)) return false;
  if (!Array.isArray(allowed) || allowed.includes("*") || allowed.includes(":*")) return true;
  if (typeof command !== "string" || !command.trim()) return false;
  // Dynamic expansions, redirections, grouping, and background execution are
  // deliberately refused rather than attempting to infer their executable scope.
  if (/[$`\\()<>{}]/.test(command) || /(^|[^&])&([^&]|$)/.test(command)) return false;
  const segments = splitOnPipelineOperators(command);
  return (
    segments.length > 0 &&
    segments.every(segment => {
      const executable = extractCommandName(segment);
      if (!executable) return false;
      return allowed.some(pattern => {
        if (typeof pattern !== "string") return false;
        const prefix = pattern.replace(/:\*$/, "").trim();
        return segment === prefix || (segment.startsWith(prefix + " ") && extractCommandName(prefix) === executable);
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
    callCount += 1;
    let reason;
    if (limit !== undefined && callCount > limit) reason = `Workflow tool-call budget exhausted (${limit} calls)`;
    else if (event.toolName === "bash" && !isPiBashAllowed(event.input?.command, policy.bash)) reason = "Command is outside the workflow tools.bash allowlist";
    else if (policy.edit === false && ["edit", "write"].includes(event.toolName)) reason = "File editing is disabled by the workflow";
    if (!reason) return;
    denialCount += 1;
    process.stderr.write(`[gh-aw/pi-tool-policy] denied tool=${event.toolName} calls=${callCount} reason=${reason}\n`);
    if (denialLimit !== undefined && denialCount >= denialLimit) {
      ctx.abort();
      process.exitCode = 1;
    }
    return { block: true, reason };
  });
}

module.exports = Object.assign(piToolPolicy, { isPiBashAllowed });
