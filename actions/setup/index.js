// Setup Activation Action - Main Entry Point
// Invokes setup.sh to copy activation job files to the agent environment

const { spawnSync } = require("child_process");
const path = require("path");
const fs = require("fs");
const { getActionInput } = require("./js/action_input_utils.cjs");

// Record start time for the OTLP span before any setup work begins.
const setupStartMs = Date.now();

// GitHub Actions converts input names to INPUT_<UPPER_UNDERSCORE>, but some
// runner versions preserve the original hyphen form. getActionInput() handles
// both forms automatically.
const safeOutputCustomTokens = getActionInput("SAFE_OUTPUT_CUSTOM_TOKENS") || "false";
const inputTraceId = getActionInput("TRACE_ID");
const inputParentSpanId = getActionInput("PARENT_SPAN_ID");
const inputJobName = getActionInput("JOB_NAME");
const inputOTLPOIDCToken = getActionInput("OTLP_OIDC_TOKEN");

const result = spawnSync("bash", [path.join(__dirname, "setup.sh")], {
  stdio: "inherit",
  env: Object.assign({}, process.env, {
    INPUT_SAFE_OUTPUT_CUSTOM_TOKENS: safeOutputCustomTokens,
    INPUT_TRACE_ID: inputTraceId,
    INPUT_PARENT_SPAN_ID: inputParentSpanId,
    INPUT_JOB_NAME: inputJobName,
    INPUT_OTLP_OIDC_TOKEN: inputOTLPOIDCToken,
    // Tell setup.sh to skip the OTLP span: in action mode index.js sends it
    // after setup.sh returns so that the startMs captured here is used.
    GH_AW_SKIP_SETUP_OTLP: "1",
  }),
});

if (result.error) {
  console.error(`Failed to run setup.sh: ${result.error.message}`);
  process.exit(1);
}

if (result.status !== 0) {
  process.exit(result.status ?? 1);
}

// setup.sh runs in a child process; import its allocation record for JS work
// performed by this action before the runner applies GITHUB_ENV to later steps.
if (process.env.GITHUB_STATE) {
  for (const line of fs.readFileSync(process.env.GITHUB_STATE, "utf8").split("\n")) {
    const separator = line.indexOf("=");
    const key = line.slice(0, separator);
    if (/^GH_AW_(?:RUNTIME_(?:DIR|ID|DEV|INO|UID|OWNER)|(?:CLI_PROXY|MCP_GATEWAY|MCP_CONTROL|MCP_SCRIPTS|SAFE_OUTPUTS|COPILOT_SDK)_PORT)$/.test(key)) {
      process.env[key] = line.slice(separator + 1);
    }
  }
}

// Send a gh-aw.<jobName>.setup span to the OTLP endpoint when configured.
// Delegates to action_setup_otlp.cjs so that script mode (setup.sh) and
// dev/release mode share the same implementation.
// Explicitly set INPUT_TRACE_ID (normalized above) so action_setup_otlp.cjs
// always reads the underscore form regardless of runner version.
// The IIFE keeps the event loop alive until the fetch completes.
// Errors are swallowed: trace export failures must never break the workflow.
(async () => {
  try {
    process.env.SETUP_START_MS = String(setupStartMs);
    process.env.INPUT_TRACE_ID = inputTraceId;
    process.env.INPUT_PARENT_SPAN_ID = inputParentSpanId;
    process.env.INPUT_JOB_NAME = inputJobName;
    process.env.INPUT_OTLP_OIDC_TOKEN = inputOTLPOIDCToken;
    const { run } = require(path.join(__dirname, "js", "action_setup_otlp.cjs"));
    await run();
  } catch {
    // Non-fatal: silently ignore any OTLP export or output-write errors.
  }
})();
