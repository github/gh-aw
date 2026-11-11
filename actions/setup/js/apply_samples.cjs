#!/usr/bin/env node
// @ts-check
/// <reference types="@actions/github-script" />
// @safe-outputs-exempt SEC-005: target repo is used only for read-only PR head-ref lookups during deterministic sample replay; never derived from agent safe-output content and never used for a cross-repo write.

// apply_samples.cjs
//
// Deterministic replay driver for `gh aw compile --use-samples`.
//
// Reads `GH_AW_SAMPLES` (a JSON array of `{tool, arguments, sidecars}`
// entries produced by the compiler), spawns the safe-outputs MCP server
// (`safe_outputs_mcp_server.cjs`) as a child process, sends one JSON-RPC
// `tools/call` per sample over stdio, and writes a synthetic `agent-stdio.log`
// so downstream log-parsing / failure-handling steps continue to work.
//
// For samples whose tool is `create_pull_request` or `push_to_pull_request_branch`
// and whose sidecars include `patch`, the driver pre-stages a branch and commits
// the patch into the workspace BEFORE invoking the MCP tool. This lets the
// real `create_pull_request` MCP handler (which derives a git diff against the
// base branch) produce a meaningful transport payload.
//
// Env contract:
//   GH_AW_SAMPLES        — JSON array of replay entries (required)
//   GH_AW_SAMPLE_BINDINGS — static paths to runtime string values supplied
//                          separately in GH_AW_SAMPLE_VALUE_<n> env vars
//   GH_AW_AGENT_STDIO_LOG     — path where the synthetic stdio log is written
//   GH_AW_SAFE_OUTPUTS_CONFIG_PATH — path to the MCP server's config.json
//                               (also read to resolve the per-tool target-repo so
//                               cross-repo patches are staged in the right checkout)
//   GH_AW_SAFE_OUTPUTS        — path to the MCP server's outputs.jsonl
//   GITHUB_WORKSPACE          — git working directory for pre-staging (optional;
//                               falls back to cwd)

const { streamJsonlEvents } = require("./jsonl_helpers.cjs");
require("./shim.cjs");

const { spawn } = require("child_process");
const fs = require("fs");
const path = require("path");
const os = require("os");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_VALIDATION, ERR_PARSE, ERR_SYSTEM, ERR_API, ERR_CONFIG } = require("./error_codes.cjs");
const { findRepoCheckout } = require("./find_repo_checkout.cjs");
const { getSetupTimeoutMs } = require("./child_process_timeouts.cjs");

const DEFAULT_BASE_BRANCH = process.env.GH_AW_CUSTOM_BASE_BRANCH || process.env.GITHUB_BASE_REF || process.env.GITHUB_REF_NAME || "main";
const PATCH_SIDECAR_TOOLS = new Set(["create_pull_request", "push_to_pull_request_branch"]);
const FETCH_TIMEOUT_MS = getSetupTimeoutMs("applySamplesFetch");
const GIT_COMMAND_TIMEOUT_MS = getSetupTimeoutMs("applySamplesGit");

/**
 * @typedef {Object} SampleEntry
 * @property {string} tool
 * @property {Record<string, any>} arguments
 * @property {Record<string, any>} [sidecars]
 */

/**
 * Read and parse the GH_AW_SAMPLES env var. Returns an empty array (with a
 * warning) when unset or empty so the workflow can still complete cleanly.
 * @returns {SampleEntry[]}
 */
function loadSamples() {
  const raw = process.env.GH_AW_SAMPLES;
  if (!raw || !raw.trim()) {
    core.warning("apply_samples: GH_AW_SAMPLES is empty — no samples to replay.");
    return [];
  }
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new Error(`${ERR_PARSE}: apply_samples: failed to parse GH_AW_SAMPLES as JSON: ${getErrorMessage(err)}`, { cause: err });
  }
  // Tolerate a literal JSON `null` payload (older compiler emitted it for
  // workflows with --use-samples but no `samples:` entries). Treat as empty.
  if (parsed === null) {
    core.warning("apply_samples: GH_AW_SAMPLES is null — treating as no samples to replay.");
    return [];
  }
  if (!Array.isArray(parsed)) {
    throw new Error(`${ERR_VALIDATION}: apply_samples: GH_AW_SAMPLES must be a JSON array`);
  }
  for (const [i, entry] of parsed.entries()) {
    if (!entry || typeof entry !== "object" || typeof entry.tool !== "string") {
      throw new Error(`${ERR_VALIDATION}: apply_samples: entry ${i} is missing a string "tool" field`);
    }
    if (!entry.arguments || typeof entry.arguments !== "object") {
      throw new Error(`${ERR_VALIDATION}: apply_samples: entry ${i} (tool=${entry.tool}) is missing an "arguments" object`);
    }
  }
  return bindSampleRuntimeValues(parsed);
}

/**
 * Resolve compiler-authored string slots only after parsing replay control
 * data. Runtime values are assigned once as strings, never parsed or expanded.
 * @param {SampleEntry[]} samples
 * @returns {SampleEntry[]}
 */
function bindSampleRuntimeValues(samples) {
  const raw = process.env.GH_AW_SAMPLE_BINDINGS;
  if (!raw) return samples;
  let bindings;
  try {
    bindings = JSON.parse(raw);
  } catch (error) {
    throw new Error(`${ERR_PARSE}: apply_samples: failed to parse GH_AW_SAMPLE_BINDINGS as JSON: ${getErrorMessage(error)}`, { cause: error });
  }
  if (!Array.isArray(bindings)) {
    throw new Error(`${ERR_VALIDATION}: apply_samples: GH_AW_SAMPLE_BINDINGS must be a JSON array`);
  }
  for (const binding of bindings) {
    if (
      !binding ||
      !Number.isInteger(binding.entry) ||
      binding.entry < 0 ||
      binding.entry >= samples.length ||
      !Array.isArray(binding.path) ||
      binding.path.length < 2 ||
      !binding.path.every(key => typeof key === "string") ||
      !["arguments", "sidecars"].includes(binding.path[0]) ||
      typeof binding.env !== "string" ||
      !/^GH_AW_SAMPLE_VALUE_\d+$/.test(binding.env)
    ) {
      throw new Error(`${ERR_VALIDATION}: apply_samples: invalid runtime sample binding`);
    }
    let target = samples[binding.entry];
    for (const key of binding.path.slice(0, -1)) {
      if (!target || typeof target !== "object" || !Object.hasOwn(target, key)) {
        throw new Error(`${ERR_VALIDATION}: apply_samples: runtime sample binding path does not exist`);
      }
      target = target[key];
    }
    const key = binding.path[binding.path.length - 1];
    if (!target || typeof target !== "object" || !Object.hasOwn(target, key) || typeof target[key] !== "string") {
      throw new Error(`${ERR_VALIDATION}: apply_samples: runtime sample binding must target an authored string`);
    }
    const value = process.env[binding.env];
    if (value === undefined) {
      throw new Error(`${ERR_CONFIG}: apply_samples: missing runtime sample value ${binding.env}`);
    }
    target[key] = value;
  }
  return samples;
}

/**
 * Run a git subcommand synchronously and return stdout. Throws on non-zero exit.
 * @param {string[]} args
 * @param {string} cwd
 * @returns {string}
 */
function runGit(args, cwd) {
  const { spawnSync } = require("child_process");
  const result = spawnSync("git", args, { cwd, encoding: "utf8", timeout: GIT_COMMAND_TIMEOUT_MS });
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    throw new Error(`${ERR_SYSTEM}: git ${args.join(" ")} failed (exit ${result.status}): ${result.stderr || result.stdout}`);
  }
  return result.stdout;
}

/**
 * Ensure git user.email / user.name are configured so commits succeed in CI.
 * @param {string} cwd
 */
function ensureGitIdentity(cwd) {
  try {
    runGit(["config", "user.email"], cwd);
  } catch {
    runGit(["config", "user.email", "gh-aw-samples@github.com"], cwd);
  }
  try {
    runGit(["config", "user.name"], cwd);
  } catch {
    runGit(["config", "user.name", "gh-aw samples"], cwd);
  }
}

/**
 * Read and parse the GitHub Actions event payload from GITHUB_EVENT_PATH.
 * Returns null when the file is unset, missing, or not parseable.
 * @returns {Record<string, any>|null}
 */
function readEventPayload() {
  const p = process.env.GITHUB_EVENT_PATH;
  if (!p) return null;
  try {
    return JSON.parse(fs.readFileSync(p, "utf8"));
  } catch {
    return null;
  }
}

/**
 * Resolve the best token for a `owner/repo` API call.
 *
 * Multi-checkout workflows often need different tokens for different
 * repositories (e.g. a workflow runs in `owner/automation` but reaches into
 * `owner/product` via a separate `checkout:` entry that supplies its own
 * `github-token:` or `github-app:`). The compiler emits that mapping as
 * `GH_AW_REPO_TOKENS` (a JSON object keyed by `owner/repo`); this helper
 * looks the requested slug up there and falls back to GITHUB_TOKEN /
 * GH_TOKEN.
 *
 * @param {string} owner
 * @param {string} repo
 * @returns {string|undefined}
 */
function selectTokenForRepo(owner, repo) {
  const slug = `${owner}/${repo}`;
  const raw = process.env.GH_AW_REPO_TOKENS;
  if (raw && raw.trim()) {
    try {
      const map = JSON.parse(raw);
      if (map && typeof map === "object" && typeof map[slug] === "string" && map[slug].trim()) {
        return map[slug].trim();
      }
    } catch (err) {
      core.warning(`apply_samples: GH_AW_REPO_TOKENS is not valid JSON, ignoring: ${getErrorMessage(err)}`);
    }
  }
  return process.env.GITHUB_TOKEN || process.env.GH_TOKEN || undefined;
}

/**
 * Fetch a pull request via the REST API and return its head ref. Uses the
 * per-repo token from GH_AW_REPO_TOKENS when present, falling back to
 * GITHUB_TOKEN; falls back to anonymous (works for public repositories) when
 * no token is available. Returns null on any failure so the caller can decide
 * how to recover.
 * @param {{owner: string, repo: string, pullNumber: number}} args
 * @returns {Promise<string|null>}
 */
async function fetchPullRequestHeadRef({ owner, repo, pullNumber }) {
  const apiUrl = process.env.GITHUB_API_URL || "https://api.github.com";
  const url = `${apiUrl}/repos/${owner}/${repo}/pulls/${pullNumber}`;
  /** @type {Record<string, string>} */
  const headers = {
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
    "User-Agent": "gh-aw-apply-samples",
  };
  const token = selectTokenForRepo(owner, repo);
  if (token) headers["Authorization"] = `Bearer ${token}`;
  try {
    const resp = await fetch(url, { headers, signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) });
    if (!resp.ok) {
      core.warning(`apply_samples: GET ${url} returned HTTP ${resp.status}`);
      return null;
    }
    const body = await resp.json();
    // @ts-ignore - REST API response shape is well-defined; trust GitHub PR endpoint contract
    const ref = body && body.head && typeof body.head.ref === "string" ? body.head.ref : null;
    return ref || null;
  } catch (err) {
    core.warning(`apply_samples: failed to fetch PR ${owner}/${repo}#${pullNumber}: ${getErrorMessage(err)}`);
    return null;
  }
}

/**
 * Derive the pull request head ref for the triggering event.
 *
 * Resolution order:
 *   1. `pull_request.head.ref` from the event payload (pull_request and
 *      pull_request_target events).
 *   2. PR API lookup using `issue.number` when the payload describes an
 *      issue_comment on a pull request.
 *   3. PR API lookup using an explicit `pull_request_number` argument on the
 *      sample (covers workflow_dispatch driven by the agent).
 *
 * @param {SampleEntry} entry
 * @returns {Promise<string|null>}
 */
async function derivePrHeadRef(entry) {
  const payload = readEventPayload();

  // 1. pull_request* events expose the head ref directly.
  const directRef = payload?.pull_request?.head?.ref;
  if (typeof directRef === "string" && directRef.trim()) {
    return directRef.trim();
  }

  // Determine the target repo for any API lookups.
  // Resolution order:
  //   a. entry.arguments.repo — explicit per-sample override (cross-repo workflows).
  //   b. target-repo from the safe-outputs config file (GH_AW_SAFE_OUTPUTS_CONFIG_PATH)
  //      for the tool — covers siderepo workflow_dispatch where the sample arguments
  //      carry `pull_request_number` but not a `repo` override (issue #41292).
  //   c. GITHUB_REPOSITORY — host repo fallback.
  let repoSlug = "";
  if (typeof entry.arguments.repo === "string" && entry.arguments.repo.trim()) {
    repoSlug = entry.arguments.repo.trim();
  } else {
    repoSlug = readConfiguredTargetRepo(entry.tool) || process.env.GITHUB_REPOSITORY || "";
  }
  const [owner, repo] = repoSlug.split("/");
  if (!owner || !repo) return null;

  // 2. issue_comment / pull_request_review_comment with a PR-linked issue.
  if (payload?.issue?.pull_request && typeof payload.issue.number === "number") {
    const ref = await fetchPullRequestHeadRef({ owner, repo, pullNumber: payload.issue.number });
    if (ref) return ref;
  }

  // 3. PR number from sample arguments, workflow_dispatch inputs, or config target.
  const pullNumber =
    toPositivePullRequestNumber(entry.arguments.pull_request_number) ||
    toPositivePullRequestNumber(payload?.inputs?.pull_request_number) ||
    toPositivePullRequestNumber(payload?.client_payload?.pull_request_number) ||
    readConfiguredTargetPullRequestNumber(entry.tool);
  if (pullNumber) {
    const ref = await fetchPullRequestHeadRef({ owner, repo, pullNumber });
    if (ref) return ref;
  }

  return null;
}

/**
 * Convert unknown value to a positive pull request number, or null.
 * @param {any} value
 * @returns {number|null}
 */
function toPositivePullRequestNumber(value) {
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? n : null;
}

/**
 * Read the configured `target-repo` for a given safe-output tool from the
 * safe-outputs config file (GH_AW_SAFE_OUTPUTS_CONFIG_PATH). Returns an empty
 * string when no config is available or no target-repo is configured.
 * @param {string} tool
 * @returns {string}
 */
function readConfiguredTargetRepo(tool) {
  const configPath = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
  if (!configPath || !configPath.trim()) {
    return "";
  }

  const toolKey = typeof tool === "string" ? tool.replace(/-/g, "_") : "";

  try {
    const raw = fs.readFileSync(configPath, "utf8");
    const parsed = JSON.parse(raw);

    // Mirror safe_outputs_config.cjs behavior: normalize top-level keys by replacing '-' with '_'.
    const config = parsed && typeof parsed === "object" ? Object.fromEntries(Object.entries(parsed).map(([k, v]) => [String(k).replace(/-/g, "_"), v])) : {};

    const toolConfig = toolKey && config && typeof config === "object" ? config[toolKey] : null;
    const target = toolConfig && typeof toolConfig === "object" ? toolConfig["target-repo"] || toolConfig["target_repo"] : null;
    if (typeof target === "string") {
      return target.trim();
    }
  } catch (err) {
    core.debug(`apply_samples: could not read target-repo from ${configPath}: ${getErrorMessage(err)}`);
  }
  return "";
}

/**
 * Read configured `target` for a safe-output tool and coerce it into a PR number.
 * Supports plain numeric values and `${ENV_VAR}` placeholders.
 * @param {string} tool
 * @returns {number|null}
 */
function readConfiguredTargetPullRequestNumber(tool) {
  const configPath = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
  if (!configPath || !configPath.trim()) {
    return null;
  }

  const toolKey = typeof tool === "string" ? tool.replace(/-/g, "_") : "";

  try {
    const raw = fs.readFileSync(configPath, "utf8");
    const parsed = JSON.parse(raw);
    const config = parsed && typeof parsed === "object" ? Object.fromEntries(Object.entries(parsed).map(([k, v]) => [String(k).replace(/-/g, "_"), v])) : {};
    const toolConfig = toolKey && config && typeof config === "object" ? config[toolKey] : null;
    const target = toolConfig && typeof toolConfig === "object" ? toolConfig.target : null;

    if (typeof target === "number") {
      return toPositivePullRequestNumber(target);
    }
    if (typeof target === "string") {
      const trimmed = target.trim();
      const envMatch = /^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$/.exec(trimmed);
      if (envMatch) {
        return toPositivePullRequestNumber(process.env[envMatch[1]]);
      }
      return toPositivePullRequestNumber(trimmed);
    }
  } catch (err) {
    core.debug(`apply_samples: could not read target from ${configPath}: ${getErrorMessage(err)}`);
  }
  return null;
}

/**
 * Resolve the on-disk working directory in which a sample's patch should be
 * staged (branch created + patch committed).
 *
 * For cross-repo checkouts placed in a subdirectory (e.g. `path: github`), the
 * branch and commit must be created inside that subdirectory so the safe-outputs
 * MCP handler — which resolves the same checkout via the checkout manifest — can
 * find the branch. Staging in the main workspace root instead leaves the branch
 * invisible to the MCP server, producing "fatal: Needed a single revision" when
 * it tries to pin the branch (issue #40086).
 *
 * Resolution mirrors the MCP handler: the target repo comes from the sample's
 * `arguments.repo` override or the configured `target-repo`, and the checkout
 * directory is resolved via the manifest-first `findRepoCheckout`. Falls back to
 * the workspace root when no target repo is set or the checkout cannot be located.
 *
 * @param {SampleEntry} entry
 * @param {string} workspace
 * @returns {string}
 */
function resolvePatchWorkspace(entry, workspace) {
  let targetRepo = "";
  if (entry.arguments && typeof entry.arguments.repo === "string" && entry.arguments.repo.trim()) {
    targetRepo = entry.arguments.repo.trim();
  } else {
    targetRepo = readConfiguredTargetRepo(entry.tool);
  }
  if (!targetRepo) {
    return workspace;
  }
  try {
    const result = findRepoCheckout(targetRepo, workspace);
    if (result && result.success && result.path) {
      if (path.resolve(result.path) !== path.resolve(workspace)) {
        core.info(`apply_samples: staging patch for ${targetRepo} in checkout subdirectory ${result.path}`);
      }
      return result.path;
    }
    core.debug(`apply_samples: findRepoCheckout(${targetRepo}) did not locate a checkout; using workspace root`);
  } catch (err) {
    core.debug(`apply_samples: findRepoCheckout(${targetRepo}) failed: ${getErrorMessage(err)}; using workspace root`);
  }
  return workspace;
}

/**
 * Pre-stage a branch + patch for samples whose tool reads the workspace diff.
 *
 * - For `create_pull_request`, the sample creates a brand-new branch, so we
 *   accept the sample's declared `branch` or synthesize `gh-aw-sample-<i+1>`.
 * - For `push_to_pull_request_branch`, the destination is the triggering PR's
 *   head branch — we derive it from event/PR context and refuse to invent a
 *   synthetic branch (which would never exist on origin and would break the
 *   MCP server's incremental-patch generation, per issue #37835).
 *
 * @param {SampleEntry} entry
 * @param {number} index
 * @param {string} workspace
 * @returns {Promise<void>}
 */
async function preStagePatch(entry, index, workspace) {
  const patch = entry.sidecars && entry.sidecars.patch;
  if (typeof patch !== "string" || !patch.trim()) {
    return;
  }

  // Resolve the directory in which to create the branch and commit the patch.
  // For cross-repo checkouts in a subdirectory this is the checkout subdir, not
  // the main workspace root (issue #40086).
  const repoCwd = resolvePatchWorkspace(entry, workspace);

  let branch;
  if (entry.tool === "push_to_pull_request_branch") {
    // Source ref MUST match the PR's head ref so that
    // `git diff origin/<branch>..<branch>` in the MCP server resolves the
    // correct baseline. Synthetic `gh-aw-sample-N` names produce
    // `fatal: couldn't find remote ref` failures (issue #37835).
    branch = await derivePrHeadRef(entry);
    if (!branch) {
      throw new Error(
        `${ERR_VALIDATION}: apply_samples: cannot derive pull-request head branch for sample[${index}] (tool=${entry.tool}). ` +
          `Trigger the workflow from a pull_request event, or set arguments.pull_request_number on the sample entry, ` +
          `or provide GITHUB_TOKEN so the PR can be fetched.`
      );
    }
    // The agent input no longer carries `branch`; ensure stray sample-supplied
    // values do not leak through to the MCP tools/call payload.
    delete entry.arguments.branch;
  } else {
    branch = typeof entry.arguments.branch === "string" && entry.arguments.branch.trim() ? entry.arguments.branch.trim() : `gh-aw-sample-${index + 1}`;
    entry.arguments.branch = branch;
  }

  ensureGitIdentity(repoCwd);

  // Start from the base branch so the diff is meaningful. Tolerate the case
  // where the base ref doesn't exist locally — fall back to HEAD.
  try {
    runGit(["checkout", DEFAULT_BASE_BRANCH], repoCwd);
  } catch (err) {
    core.warning(`apply_samples: could not check out base branch ${DEFAULT_BASE_BRANCH}: ${getErrorMessage(err)}; staying on current HEAD`);
  }

  // Create the branch (or check it out if it already exists from a previous sample).
  try {
    runGit(["checkout", "-b", branch], repoCwd);
  } catch {
    runGit(["checkout", branch], repoCwd);
  }

  // Write patch to a temp file and apply it.
  const tmpPatch = path.join(os.tmpdir(), `gh-aw-sample-${index + 1}.patch`);
  try {
    fs.writeFileSync(tmpPatch, patch.endsWith("\n") ? patch : patch + "\n");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to write file ${tmpPatch}: ${getErrorMessage(err)}`, { cause: err });
  }
  try {
    runGit(["apply", "--whitespace=nowarn", tmpPatch], repoCwd);
  } catch (err) {
    // Fall back to --3way for patches that don't apply cleanly on top of an
    // empty working tree (uncommon but possible for synthetic samples).
    runGit(["apply", "--3way", "--whitespace=nowarn", tmpPatch], repoCwd);
  }

  runGit(["add", "-A"], repoCwd);
  runGit(["commit", "-m", `gh-aw sample ${index + 1}: ${entry.tool}`, "--allow-empty"], repoCwd);
}

/**
 * Send a single JSON-RPC request to the MCP server child process and resolve
 * with the parsed JSON response (or reject on timeout).
 * @param {import("child_process").ChildProcess} child
 * @param {NodeJS.WritableStream} stdin
 * @param {any} request
 * @param {AsyncIterableIterator<import("./jsonl_helpers.cjs").JsonlEvent>} responseIterator
 * @returns {Promise<any>}
 */
async function sendJsonRpc(child, stdin, request, responseIterator) {
  stdin.write(JSON.stringify(request) + "\n");
  while (true) {
    const { value, done } = await responseIterator.next();
    if (done) {
      throw new Error(`${ERR_API}: apply_samples: MCP server closed stdout before responding to request id=${request.id}`);
    }
    const line = value.rawLine.trim();
    if (!line) {
      continue;
    }
    if (!line.startsWith("{")) {
      core.debug(`apply_samples: ignoring non-JSON stdout line: ${line}`);
      continue;
    }
    if (value.type === "json") return value.value;
    const err = value.error;
    throw new Error(`${ERR_PARSE}: apply_samples: failed to parse MCP JSON-RPC response for request id=${request.id}: ${getErrorMessage(err)} (line: ${line})`, { cause: err });
  }
}

/**
 * Locate the safe_outputs_mcp_server.cjs script. The setup action copies it
 * into ${RUNNER_TEMP}/gh-aw/actions/ alongside this driver; fall back to
 * resolving via __dirname for local-execution / tests.
 * @returns {string}
 */
function resolveMcpServerPath() {
  const candidates = [
    path.join(__dirname, "safe_outputs_mcp_server.cjs"),
    process.env.RUNNER_TEMP ? path.join(process.env.RUNNER_TEMP, "gh-aw", "actions", "safe_outputs_mcp_server.cjs") : null,
    process.env.RUNNER_TEMP ? path.join(process.env.RUNNER_TEMP, "gh-aw", "safeoutputs", "safe_outputs_mcp_server.cjs") : null,
  ].filter(/** @returns {p is string} */ p => typeof p === "string");
  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }
  throw new Error(`${ERR_CONFIG}: apply_samples: could not locate safe_outputs_mcp_server.cjs. Looked in: ${candidates.join(", ")}`);
}

/**
 * Return true when an MCP tool-call result carries an application-level error
 * payload, regardless of whether the transport-level `isError` flag was set.
 *
 * Safe-output handlers encode errors as `{"result":"error", ...}` inside the
 * first content text item.  This helper provides a defense-in-depth check so
 * that the driver fails even when an older server version did not set `isError`.
 *
 * @param {any} result - The `result` object from a JSON-RPC tools/call response.
 * @returns {boolean}
 */
function sampleResultIsError(result) {
  if (!result) return false;
  if (result.isError) return true;
  const text = result.content && result.content[0] && result.content[0].text;
  if (!text) return false;
  try {
    const parsed = JSON.parse(text);
    return !!(parsed && parsed.result === "error");
  } catch {
    return false;
  }
}

/**
 * Append a synthetic terminal_reason: completed marker to the engine stdio log
 * so downstream parsers / handle_agent_failure recognize the replay as a
 * successful agent run.
 * @param {string} logPath
 * @param {number} sampleCount
 */
function writeSyntheticStdioLog(logPath, sampleCount) {
  if (!logPath) return;
  try {
    fs.mkdirSync(path.dirname(logPath), { recursive: true });
  } catch {
    /* ignore */
  }
  const lines = [
    `gh-aw samples replay: ${sampleCount} MCP tools/call invocation(s) completed deterministically.`,
    JSON.stringify({
      type: "result",
      subtype: "success",
      terminal_reason: "completed",
      num_turns: sampleCount,
      driver: "apply_samples",
    }),
    "",
  ];
  try {
    fs.appendFileSync(logPath, lines.join("\n"));
  } catch {
    /* ignore */
  }
}

async function main() {
  const samples = loadSamples();
  const workspace = process.env.GITHUB_WORKSPACE || process.cwd();
  const logPath = process.env.GH_AW_AGENT_STDIO_LOG || "";

  // Pre-stage branches/patches.
  for (let i = 0; i < samples.length; i++) {
    const sample = samples[i];
    if (PATCH_SIDECAR_TOOLS.has(sample.tool)) {
      await preStagePatch(sample, i, workspace);
    }
  }

  if (samples.length === 0) {
    core.info("apply_samples: nothing to replay; exiting cleanly.");
    writeSyntheticStdioLog(logPath, 0);
    return;
  }

  const serverPath = resolveMcpServerPath();
  core.info(`apply_samples: spawning MCP server ${serverPath}`);
  const child = spawn(process.execPath, [serverPath], {
    stdio: ["pipe", "pipe", "inherit"],
    env: process.env,
  });
  child.on("error", err => {
    core.error(`apply_samples: failed to launch MCP server: ${getErrorMessage(err)}`);
  });

  const stdoutIter = streamJsonlEvents(child.stdout);
  let nextId = 1;
  const failures = [];

  try {
    // Initialize handshake.
    const initRsp = await sendJsonRpc(
      child,
      child.stdin,
      {
        jsonrpc: "2.0",
        id: nextId++,
        method: "initialize",
        params: {
          protocolVersion: "2025-06-18",
          capabilities: {},
          clientInfo: { name: "apply_samples", version: "1.0.0" },
        },
      },
      stdoutIter
    );
    if (initRsp.error) {
      throw new Error(`${ERR_API}: MCP initialize failed: ${JSON.stringify(initRsp.error)}`);
    }

    // Send one tools/call per sample.
    for (const [i, sample] of samples.entries()) {
      const callRsp = await sendJsonRpc(
        child,
        child.stdin,
        {
          jsonrpc: "2.0",
          id: nextId++,
          method: "tools/call",
          params: { name: sample.tool, arguments: sample.arguments },
        },
        stdoutIter
      );
      if (callRsp.error) {
        failures.push(`sample[${i}] (tool=${sample.tool}): ${JSON.stringify(callRsp.error)}`);
        continue;
      }
      const result = callRsp.result;
      if (sampleResultIsError(result)) {
        const text = result && result.content && result.content[0] && result.content[0].text;
        failures.push(`sample[${i}] (tool=${sample.tool}): ${text || JSON.stringify(result)}`);
      } else {
        core.info(`apply_samples: sample[${i}] (tool=${sample.tool}) ok`);
      }
    }
  } finally {
    try {
      child.stdin.end();
    } catch {
      /* ignore */
    }
    // Give the server up to 2s to exit cleanly.
    await new Promise(resolve => {
      const timer = setTimeout(() => {
        try {
          child.kill("SIGTERM");
        } catch {
          /* ignore */
        }
        resolve(undefined);
      }, 2000);
      child.once("exit", () => {
        clearTimeout(timer);
        resolve(undefined);
      });
    });
  }

  writeSyntheticStdioLog(logPath, samples.length);

  if (failures.length > 0) {
    throw new Error(`${ERR_API}: apply_samples: ${failures.length} sample(s) failed:\n  - ${failures.join("\n  - ")}`);
  }
  core.info(`apply_samples: ${samples.length} sample(s) replayed successfully.`);
}

if (require.main === module) {
  main().catch(err => {
    core.setFailed(err && err.stack ? err.stack : getErrorMessage(err));
  });
}

module.exports = {
  main,
  loadSamples,
  preStagePatch,
  resolvePatchWorkspace,
  readConfiguredTargetRepo,
  resolveMcpServerPath,
  selectTokenForRepo,
  sendJsonRpc,
  // Exported for unit testing of the 3-tier PR head ref resolution logic.
  derivePrHeadRef,
  fetchPullRequestHeadRef,
  // Exported for unit testing of the defense-in-depth error detection.
  sampleResultIsError,
};
