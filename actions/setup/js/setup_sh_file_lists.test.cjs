import { describe, it, expect } from "vitest";
import { readFileSync, existsSync, mkdirSync, rmSync } from "fs";
import { execFileSync } from "child_process";
import { resolve, dirname } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const SETUP_SH = resolve(__dirname, "../setup.sh");
const SETUP_ACTION_INDEX = resolve(__dirname, "../index.js");
const INSTALL_COPILOT_CLI_SH = resolve(__dirname, "../sh/install_copilot_cli.sh");
const setupShContent = readFileSync(SETUP_SH, "utf8");
const setupActionIndexContent = readFileSync(SETUP_ACTION_INDEX, "utf8");
const installCopilotCliContent = readFileSync(INSTALL_COPILOT_CLI_SH, "utf8");

describe("setup action Windows support", () => {
  it("uses WSL on Windows and preserves Bash on other runners", () => {
    expect(setupActionIndexContent).toContain('spawnSync("bash", [path.join(__dirname, "setup.sh")]');
    expect(setupActionIndexContent).toContain('process.platform === "win32"');
    expect(setupActionIndexContent).toContain('process.env.GH_AW_WINDOWS_WSL === "true"');
    expect(setupActionIndexContent).toContain("setupWindowsRuntime();");
    expect(setupActionIndexContent).toContain('runWSLScript(path.join(__dirname, "setup.sh"), setupEnv)');
  });

  it("converts Windows paths for Bash without changing RUNNER_TEMP", () => {
    expect(setupShContent).toContain('RUNNER_TEMP_BASH="$(cygpath -u "${RUNNER_TEMP}")"');
    expect(setupShContent).toContain('DESTINATION="$(cygpath -u "${DESTINATION}")"');
  });

  it("installs the Windows Copilot CLI without sudo", () => {
    expect(installCopilotCliContent).toContain("MINGW*|MSYS*|CYGWIN*)");
    expect(installCopilotCliContent).toContain('TARBALL_NAME="copilot-${PLATFORM}-${ARCH_NAME}.zip"');
    expect(installCopilotCliContent).toContain("unzip or 7z is required for Windows installation");
    expect(installCopilotCliContent).toContain('unzip -qo "${TEMP_DIR}/${TARBALL_NAME}" -d "${INSTALL_DIR}"');
    expect(installCopilotCliContent).toContain('7z x -y "-o${INSTALL_DIR}" "${TEMP_DIR}/${TARBALL_NAME}"');
  });
});

describe("setup.sh Copilot runtime files", () => {
  it("copies the shared routing effort dependency alongside Copilot runtime scripts", () => {
    expect(setupShContent).toContain('for file in "${JS_SOURCE_DIR}"/*.cjs; do');
    expect(setupShContent).toContain('cp "$file" "${DESTINATION}/${filename}"');
    expect(getDirectLocalRequires("copilot_harness.cjs")).toContain("copilot_routing_effort.cjs");
    expect(getDirectLocalRequires("copilot_sdk_driver.cjs")).toContain("copilot_routing_effort.cjs");
  });
});

/**
 * Parse a bash array from setup.sh, e.g.:
 *   MCP_SCRIPTS_FILES=(
 *     "file1.cjs"
 *     "file2.cjs"
 *   )
 */
function parseSetupShArray(arrayName) {
  // Match the array declaration and capture everything inside the parentheses
  const pattern = new RegExp(`${arrayName}=\\(([\\s\\S]*?)\\)`, "m");
  const match = setupShContent.match(pattern);
  if (!match) throw new Error(`Could not find ${arrayName} in setup.sh`);
  return [...match[1].matchAll(/"([^"]+)"/g)].map(m => m[1]);
}

/**
 * Return all local ./relative requires from a .cjs file (non-recursive).
 * Only captures static string literals, ignores dynamic requires.
 */
function getDirectLocalRequires(filename) {
  const filePath = resolve(__dirname, filename);
  if (!existsSync(filePath)) return [];
  const content = readFileSync(filePath, "utf8");
  const deps = [];
  for (const match of content.matchAll(/require\(["']\.\/([\w\-./]+\.(?:cjs|json))["']\)/g)) {
    deps.push(match[1]);
  }
  return deps;
}

/**
 * Collect all transitive local dependencies starting from the given root files.
 * Returns a Set of all filenames reachable via require('./...') chains.
 */
function collectTransitiveDeps(rootFiles) {
  const visited = new Set();
  const queue = [...rootFiles];
  while (queue.length > 0) {
    const file = queue.shift();
    if (visited.has(file)) continue;
    visited.add(file);
    for (const dep of getDirectLocalRequires(file)) {
      if (!visited.has(dep)) queue.push(dep);
    }
  }
  return visited;
}

function filesCallingCoreSetSecret(files) {
  return files.filter(file => /\bcore\.setSecret\s*\(/.test(readFileSync(resolve(__dirname, file), "utf8")));
}

const mcpScriptsFiles = parseSetupShArray("MCP_SCRIPTS_FILES");
const safeOutputsFiles = parseSetupShArray("SAFE_OUTPUTS_FILES");

// safe-outputs-mcp-server.cjs is the entry point copied separately as mcp-server.cjs
// Its deps must also be covered by SAFE_OUTPUTS_FILES
const safeOutputsRoots = [...safeOutputsFiles, "safe-outputs-mcp-server.cjs"];

describe("work queue pool-install packaging", () => {
  it.each([
    ["MCP_SCRIPTS_FILES", mcpScriptsFiles],
    ["SAFE_OUTPUTS_FILES", safeOutputsFiles],
  ])("deploys the pool-install module in %s", (_name, files) => {
    expect(files).toContain("work_queue_pool_install.cjs");
  });

  it("embeds the pool-install module for standalone bundles", () => {
    const sources = readFileSync(resolve(__dirname, "../session_parsers.go"), "utf8");
    expect(sources).toMatch(/^\/\/go:embed .*js\/work_queue_pool_install\.cjs(?:\s|$)/m);
  });
});

describe("setup.sh isolated redaction dependencies", () => {
  it.each([
    ["MCP_SCRIPTS_FILES", mcpScriptsFiles],
    ["SAFE_OUTPUTS_FILES", safeOutputsRoots],
  ])("%s uses header collection without runner masking or telemetry export", (_name, roots) => {
    const dependencies = collectTransitiveDeps(roots);
    expect(dependencies.has("otlp_header_helpers.cjs")).toBe(true);
    expect(dependencies.has("actions_secret_masking.cjs")).toBe(false);
    expect(dependencies.has("detection_mask_state.cjs")).toBe(false);
    expect(dependencies.has("send_otlp_span.cjs")).toBe(false);
  });
});

describe("setup.sh MCP_SCRIPTS_FILES", () => {
  it("loads redaction consumers from the deployed bundle without source-directory dependencies", () => {
    const runnerRoot = resolve(__dirname, `.setup-mcp-bundle-${process.pid}`);
    mkdirSync(runnerRoot);
    try {
      // Run only the real MCP deployment block, not setup's runner-wide initialization.
      const deploymentBlock = setupShContent.slice(setupShContent.indexOf("# Copy mcp-scripts files"), setupShContent.indexOf("# Copy safe-outputs files"));
      execFileSync("bash", ["-ec", `create_dir() { mkdir -p "$1"; }\ndebug_log() { :; }\n${deploymentBlock}`], {
        env: { ...process.env, GH_AW_ROOT: runnerRoot, JS_SOURCE_DIR: __dirname },
      });
      execFileSync(
        process.execPath,
        [
          "-e",
          `
            const assert = require("node:assert/strict");
            const { createServer } = require("./mcp_server_core.cjs");
            const { normalizeSafeOutputName } = require("./safe_output_names.cjs");
            assert.equal(createServer({ name: "mcpscripts", version: "test" }).serverInfo.name, "mcpscripts");
            assert.equal(normalizeSafeOutputName("missing_tool"), "report_missing_tool");
            const manifest = require("./safe_output_manifest.cjs");
            const summary = require("./work_queue_summary_renderer.cjs");
            const native = require("./work_queue_native.cjs");
            const delivery = require("./work_queue_native_creation_delivery.cjs");
            const provisioning = require("./work_queue_provisioning.cjs");
            const { normalizeWebhookContext } = require("./webhook_context.cjs");
            const payload = { url: "https://api.github.com/orgs/org/projectsV2/1/items/123", type: ["edited"] };
            assert.equal(normalizeWebhookContext({ webhook: { event: "projects_v2_item", deliveries: [{ delivery_id: "d", payload }] } }).items[0].item_number, "123");
            assert.throws(() => normalizeWebhookContext({ webhook: { event: "projects_v2_item", deliveries: [{ delivery_id: "d", payload: { ...payload, body: "private" } }] } }), /accepts only/);
            assert.equal(typeof summary.renderWorkQueue, "function");
            assert.equal(typeof native.authenticatePublisher, "function");
            assert.equal(typeof delivery.captureDiscussionDelivery, "function");
            assert.equal(typeof provisioning.verifiedDefaultReference, "function");
            const token = "ghp_" + "a".repeat(36);
            assert.equal(
              manifest.redactManifestValue(token + " fixture-custom-secret", ["fixture-custom-secret"]),
              "***REDACTED*** ***REDACTED***"
            );
          `,
        ],
        { cwd: resolve(runnerRoot, "mcp-scripts") }
      );
    } finally {
      rmSync(runnerRoot, { recursive: true, force: true });
    }
  });

  it("contains all transitive local dependencies", () => {
    const listed = new Set(mcpScriptsFiles);
    const allDeps = collectTransitiveDeps(mcpScriptsFiles);
    const missing = [...allDeps].filter(f => !listed.has(f));
    expect(missing).toEqual([]);
  });

  it("deploys the queue workflow-dispatch transport", () => {
    expect(getDirectLocalRequires("work_queue_native.cjs")).toContain("dispatch_workflow_transport.cjs");
    expect(mcpScriptsFiles).toContain("dispatch_workflow_transport.cjs");
  });

  it("all listed files exist in js/", () => {
    const missing = mcpScriptsFiles.filter(f => !existsSync(resolve(__dirname, f)));
    expect(missing).toEqual([]);
  });

  it("does not deploy files that call core.setSecret", () => {
    expect(filesCallingCoreSetSecret(mcpScriptsFiles)).toEqual([]);
  });
});

describe("setup.sh SAFE_OUTPUTS_FILES", () => {
  it("renders Claim-scoped footers from the deployed bundle without source-directory dependencies", () => {
    const runnerRoot = resolve(__dirname, `.setup-safeoutputs-bundle-${process.pid}`);
    mkdirSync(runnerRoot);
    try {
      const deploymentBlock = setupShContent.slice(setupShContent.indexOf("# Copy safe-outputs files"), setupShContent.indexOf("# Copy configure_git_credentials.sh"));
      execFileSync("bash", ["-ec", `create_dir() { mkdir -p "$1"; }\ndebug_log() { :; }\n${deploymentBlock}`], {
        env: { ...process.env, GH_AW_ROOT: runnerRoot, JS_SOURCE_DIR: __dirname },
      });
      execFileSync(
        process.execPath,
        [
          "-e",
          `
            const assert = require("node:assert/strict");
            const { createServer, registerTool } = require("./mcp_server_core.cjs");
            const server = createServer({ name: "safeoutputs", version: "test" });
            registerTool(server, { name: "missing_tool", description: "", inputSchema: { type: "object", properties: {} } });
            assert.deepEqual(Object.keys(server.tools), ["report_missing_tool"]);
            const { getFooterMessage } = require("./messages_footer.cjs");
            const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
            const native = require("./work_queue_native.cjs");
            const delivery = require("./work_queue_native_creation_delivery.cjs");
            const provisioning = require("./work_queue_provisioning.cjs");
            const { normalizeWebhookContext } = require("./webhook_context.cjs");
            const payload = { url: "https://api.github.com/orgs/org/projectsV2/1/items/123", type: ["edited"] };
            assert.equal(normalizeWebhookContext({ webhook: { event: "projects_v2_item", deliveries: [{ delivery_id: "d", payload }] } }).items[0].item_number, "123");
            assert.throws(() => normalizeWebhookContext({ webhook: { event: "projects_v2_item", deliveries: [{ delivery_id: "d", payload: { ...payload, body: "private" } }] } }), /accepts only/);
            assert.equal(typeof native.authenticatePublisher, "function");
            assert.equal(typeof delivery.captureDiscussionDelivery, "function");
            assert.equal(typeof provisioning.verifiedDefaultReference, "function");
            const assignment = {
              version: 3, dispatch_id: "dispatch", request_id: "request", commit_id: "commit",
              policy_epoch: "policy", pool: "default", worker_profile: "default",
              claims: [{ handle: "h1", claim_id: "claim-one", work_id: "work-one", work: {}, result_refs: [] }],
            };
            const context = { workflowName: "Worker", runUrl: "https://github.com/owner/repo/actions/runs/123" };
            const footer = withClaimExecution({ assignment, claim_handle: "h1" }, () => getFooterMessage(context));
            assert.ok(footer.includes("> Work \`work-one\` · Claim \`claim-one\`"));
            assert.ok(!getFooterMessage(context).includes("> Work "));
          `,
        ],
        { cwd: resolve(runnerRoot, "safeoutputs") }
      );
    } finally {
      rmSync(runnerRoot, { recursive: true, force: true });
    }
  });

  it("deploys the queue workflow-dispatch transport", () => {
    expect(safeOutputsFiles).toContain("dispatch_workflow_transport.cjs");
  });

  it("prepares the isolated work queue finish-intent mount", () => {
    expect(setupShContent).toContain('create_dir "${SAFE_OUTPUTS_DEST}/work-queue"');
  });

  it("includes the work queue replay module", () => {
    expect(safeOutputsFiles).toContain("work_queue_replay.cjs");
    expect(safeOutputsFiles).toContain("work_queue_store.cjs");
    expect(safeOutputsFiles).toContain("work_queue_logging.cjs");
  });

  it("deploys checked transport and Issue binding dependencies with the queue runtime", () => {
    expect(getDirectLocalRequires("work_queue_store.cjs")).toContain("work_queue_checked_transport.cjs");
    expect(getDirectLocalRequires("work_queue_replay.cjs")).toContain("work_queue_issue_contract.cjs");
    expect(safeOutputsFiles).toContain("work_queue_checked_transport.cjs");
    expect(safeOutputsFiles).toContain("work_queue_issue_contract.cjs");
  });

  it("deploys Claim authority and worker route provisioning dependencies", () => {
    expect(getDirectLocalRequires("safe_outputs_handlers.cjs")).toContain("work_queue_claim_scope.cjs");
    expect(getDirectLocalRequires("work_queue_provisioning.cjs")).toContain("work_queue_yaml.cjs");
    for (const file of ["work_queue_claim_scope.cjs", "finish_work_queue_claim.cjs", "work_queue_resource_scope.cjs", "work_queue_effect_client.cjs", "work_queue_yaml.cjs"]) {
      expect(safeOutputsFiles).toContain(file);
    }
  });

  it("contains all transitive local dependencies (including entry point safe-outputs-mcp-server.cjs)", () => {
    const listed = new Set([...safeOutputsFiles, "safe_outputs_tools.json"]);
    // Entry point is also deployed (as mcp-server.cjs), so its deps must be covered too
    const allDeps = collectTransitiveDeps(safeOutputsRoots);
    const missing = [...allDeps]
      .filter(f => !listed.has(f))
      // The entry point itself is copied separately, not in the list
      .filter(f => f !== "safe-outputs-mcp-server.cjs");
    expect(missing).toEqual([]);
  });

  it("all listed files exist in js/", () => {
    const missing = safeOutputsFiles.filter(f => !existsSync(resolve(__dirname, f)));
    expect(missing).toEqual([]);
  });

  it("does not deploy files that call core.setSecret", () => {
    expect(filesCallingCoreSetSecret(safeOutputsRoots)).toEqual([]);
  });

  it("copies safe_outputs_tools.json to both safe_outputs_tools.json and tools.json", () => {
    expect(setupShContent).toContain('cp "${JS_SOURCE_DIR}/safe_outputs_tools.json" "${SAFE_OUTPUTS_DEST}/safe_outputs_tools.json"');
    expect(setupShContent).toContain('cp "${JS_SOURCE_DIR}/safe_outputs_tools.json" "${SAFE_OUTPUTS_DEST}/tools.json"');
  });
});
