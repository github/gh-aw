// @ts-check
"use strict";

const { parseDiagnostics } = require("./command_diagnostics.cjs");
const path = require("node:path");
const SHELL_TOOLS = new Set(["bash", "Bash", "read_bash", "powershell", "PowerShell", "go_repository"]);

/** @param {unknown} value @returns {string} */
function diagnosticOutput(value) {
  if (typeof value === "string") return value;
  if (Array.isArray(value))
    return value
      .filter(block => block?.type === "text" && typeof block.text === "string")
      .map(block => block.text)
      .join("\n");
  if (value && typeof value === "object") {
    if ("textResultForLlm" in value && typeof value.textResultForLlm === "string") return value.textResultForLlm;
    if ("content" in value) return diagnosticOutput(value.content);
    if ("message" in value && typeof value.message === "string") return value.message;
  }
  return "";
}

/**
 * Only text from observed shell/repository results is interpreted, never editor
 * output or assistant messages. This adapter does not authorize or execute commands.
 * @param {string} toolName
 * @param {unknown} args
 * @param {unknown} result
 * @param {import("./types/command_diagnostics").DiagnosticLanguage[]} languages
 * @param {{root?: string, cwd?: string, secrets?: string[]}} context
 */
function diagnoseToolResult(toolName, args, result, languages, context) {
  const name = toolName.replace(/^mcp__[^]+__/, "");
  if (!SHELL_TOOLS.has(name) || languages.length === 0) return undefined;
  const command = args && typeof args === "object" && "command" in args && typeof args.command === "string" ? args.command : undefined;
  const root = command && /(?:^|[;&|])\s*cd(?:\s|$)/.test(command) ? undefined : context.root;
  const argumentCwd = args && typeof args === "object" ? ("cwd" in args ? args.cwd : "workdir" in args ? args.workdir : undefined) : undefined;
  const paths = /^[A-Za-z]:[\\/]/.test(context.cwd ?? context.root ?? "") ? path.win32 : path.posix;
  const cwd = typeof argumentCwd === "string" ? (context.cwd ? paths.resolve(context.cwd, argumentCwd) : argumentCwd) : context.cwd;
  const stdout = result && typeof result === "object" && "stdout" in result && typeof result.stdout === "string" ? result.stdout : undefined;
  const stderr = result && typeof result === "object" && "stderr" in result && typeof result.stderr === "string" ? result.stderr : undefined;
  const report = parseDiagnostics({ command, ...context, root, cwd, stdout, stderr, ...(stdout === undefined && stderr === undefined ? { output: diagnosticOutput(result) } : {}) }, languages, { secrets: context.secrets });
  return report.diagnostics.length || report.issues.length || report.truncated ? report : undefined;
}

/** @param {NodeJS.ProcessEnv} env @returns {string[]} */
function diagnosticSecrets(env) {
  const configured = (env.GH_AW_SECRET_NAMES ?? "").split(",").flatMap(name => {
    const value = env[`SECRET_${name.trim()}`];
    return value ? [value] : [];
  });
  return configured.concat(["GITHUB_TOKEN", "GH_AW_GITHUB_TOKEN", "COPILOT_GITHUB_TOKEN", "OPENAI_API_KEY", "CODEX_API_KEY", "ANTHROPIC_API_KEY", "MCP_GATEWAY_API_KEY"].flatMap(name => (env[name] ? [env[name]] : [])));
}

module.exports = { diagnoseToolResult, diagnosticOutput, diagnosticSecrets };
