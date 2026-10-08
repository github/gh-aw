// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { parseDiagnosticLanguages, parseDiagnosticLanguagesJSON } = require("./command_diagnostics.cjs");

/** @param {string} value */
function quote(value) {
  return "'" + value.replaceAll("'", "'\\''") + "'";
}

/** @param {string[]} args @param {unknown} selected */
function withClaudeDiagnostics(args, selected) {
  const languages = parseDiagnosticLanguages(selected);
  if (languages.length === 0) return args;
  /** @type {Record<string, any>} */
  let settings = {};
  const remaining = [];
  for (let index = 0; index < args.length; index++) {
    const arg = args[index];
    if (arg === "--") {
      remaining.push(...args.slice(index));
      break;
    }
    if (arg !== "--settings" && !arg.startsWith("--settings=")) {
      remaining.push(arg);
      continue;
    }
    const value = arg === "--settings" ? args[++index] : arg.slice("--settings=".length);
    if (!value) throw new Error("Claude --settings requires a file or JSON object");
    let parsed;
    try {
      parsed = JSON.parse(value.trim().startsWith("{") ? value : fs.readFileSync(value, "utf8"));
    } catch (error) {
      throw new Error("Failed to read Claude --settings; provide a readable file or valid JSON", { cause: error });
    }
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Claude --settings must contain an object");
    if (parsed.hooks !== undefined && (!parsed.hooks || typeof parsed.hooks !== "object" || Array.isArray(parsed.hooks))) throw new Error("Claude settings.hooks must be an object");
    const hooks = { ...settings.hooks };
    for (const [event, handlers] of Object.entries(parsed.hooks ?? {})) {
      if (!Array.isArray(handlers) || (hooks[event] !== undefined && !Array.isArray(hooks[event]))) throw new Error(`Claude hooks.${event} must be an array`);
      hooks[event] = [...(hooks[event] ?? []), ...handlers];
    }
    settings = { ...settings, ...parsed, hooks };
  }
  if (settings.disableAllHooks === true) throw new Error("tools.diagnostics requires Claude hooks; remove disableAllHooks from --settings");
  const command = [process.execPath, path.join(__dirname, "command_diagnostics_hook.cjs"), "claude", JSON.stringify(languages)].map(quote).join(" ");
  for (const event of ["PostToolUse", "PostToolUseFailure"]) {
    const existing = settings.hooks?.[event] ?? [];
    if (!Array.isArray(existing)) throw new Error(`Claude hooks.${event} must be an array`);
    settings.hooks = { ...settings.hooks, [event]: [...existing, { matcher: "^(Bash|PowerShell)$", hooks: [{ type: "command", command, timeout: 10 }] }] };
  }
  const separator = remaining.indexOf("--");
  remaining.splice(separator < 0 ? remaining.length : separator, 0, "--settings", JSON.stringify(settings));
  return remaining;
}

function main() {
  const [, , command, ...args] = process.argv;
  if (!command) throw new Error("Claude diagnostic launcher requires a command");
  const updated = withClaudeDiagnostics(args, parseDiagnosticLanguagesJSON(process.env.GH_AW_DIAGNOSTICS));
  const child = spawn(command, updated, { stdio: "inherit", env: process.env });
  /** @type {NodeJS.Signals[]} */
  const signals = ["SIGTERM", "SIGINT"];
  for (const signal of signals) process.on(signal, () => child.kill(signal));
  child.on("error", () => {
    process.stderr.write("Failed to start Claude diagnostic command.\n");
    process.exitCode = 1;
  });
  child.on("exit", (code, signal) => {
    if (signal) {
      process.removeAllListeners(signal);
      process.kill(process.pid, signal);
    } else process.exitCode = code ?? 1;
  });
}

if (require.main === module) {
  try {
    main();
  } catch {
    process.stderr.write("Failed to configure Claude diagnostics; check --settings and tools.diagnostics.\n");
    process.exitCode = 1;
  }
}

module.exports = { withClaudeDiagnostics };
