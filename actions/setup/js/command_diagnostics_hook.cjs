// @ts-check
"use strict";

const { parseDiagnosticLanguages, parseDiagnosticLanguagesJSON } = require("./command_diagnostics.cjs");
const { diagnoseToolResult, diagnosticSecrets } = require("./command_diagnostics_tools.cjs");
const { renderDiagnostics } = require("./command_diagnostics_render.cjs");

/**
 * Claude and Codex use the same JSON command-hook envelope, but only Claude has
 * PostToolUseFailure. Never return permission decisions or replacement results.
 * @param {"claude" | "codex"} engine
 * @param {unknown} input
 * @param {unknown} selected
 * @param {NodeJS.ProcessEnv} [env]
 */
function diagnosticHookOutput(engine, input, selected, env = process.env) {
  const languages = parseDiagnosticLanguages(selected);
  if (!input || typeof input !== "object" || !("hook_event_name" in input)) throw new Error("Invalid diagnostic hook input");
  const event = input.hook_event_name;
  if (event !== "PostToolUse" && !(engine === "claude" && event === "PostToolUseFailure")) return undefined;
  if (!("tool_name" in input) || typeof input.tool_name !== "string") throw new Error("Diagnostic hook input requires tool_name");
  const result = event === "PostToolUseFailure" ? ("error" in input ? input.error : undefined) : "tool_response" in input ? input.tool_response : undefined;
  const cwd = "cwd" in input && typeof input.cwd === "string" ? input.cwd : undefined;
  const report = diagnoseToolResult(input.tool_name, "tool_input" in input ? input.tool_input : undefined, result, languages, {
    root: env.GITHUB_WORKSPACE,
    cwd,
    secrets: diagnosticSecrets(env),
  });
  return report ? { hookSpecificOutput: { hookEventName: event, additionalContext: renderDiagnostics(report) } } : undefined;
}

async function main() {
  const engine = process.argv[2];
  if (engine !== "claude" && engine !== "codex") throw new Error("Diagnostic hook requires claude or codex");
  const selected = parseDiagnosticLanguagesJSON(process.argv[3]);
  const chunks = [];
  let size = 0;
  for await (const chunk of process.stdin) {
    size += Buffer.byteLength(chunk);
    if (size > 512 * 1024) throw new Error("Diagnostic hook input exceeds 512 KiB; parsing withheld");
    chunks.push(chunk);
  }
  let input;
  try {
    input = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch (error) {
    throw new Error("Invalid command diagnostic hook input", { cause: error });
  }
  const output = diagnosticHookOutput(engine, input, selected);
  if (output) process.stdout.write(JSON.stringify(output) + "\n");
}

if (require.main === module) {
  main().catch(() => {
    // Do not echo exceptions that could include the untrusted JSON or credentials.
    process.stderr.write("Command diagnostics hook failed; native tool results are unchanged.\n");
    process.exitCode = 1;
  });
}

module.exports = { diagnosticHookOutput };
