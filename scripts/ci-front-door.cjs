"use strict";

const fs = require("node:fs");
const path = require("node:path");
const https = require("node:https");
const { summarizeDiff } = require("./ci-front-door-diff.cjs");

// Keep this independent of the catalog: corrupt metadata must never remove jobs.
const SUITE_IDS = Object.freeze(["go-tests", "security", "native-javascript-tests", "build-wasm", "mcp-server-compile-test", "cross-platform-build", "alpine-container-test", "actions-build"]);
const MANDATORY = Object.freeze(["go-tests", "security", "native-javascript-tests"]);
const LIMITS = Object.freeze({ files: 200, patch: 24000, git: 2 * 1024 * 1024, response: 16384, request: 262144, timeout: 10000 });

function validateCatalog(catalog) {
  if (catalog?.version !== 1 || !Array.isArray(catalog.suites) || catalog.suites.length !== SUITE_IDS.length) throw new Error("invalid-catalog");
  const seen = new Set();
  for (const suite of catalog.suites) {
    if (!SUITE_IDS.includes(suite.id) || seen.has(suite.id) || typeof suite.description !== "string" || !suite.description || suite.description.length > 500 || typeof suite.mandatory !== "boolean") throw new Error("invalid-catalog");
    if (MANDATORY.includes(suite.id) && !suite.mandatory) throw new Error("invalid-catalog");
    if (suite.historical_duration_seconds !== null && (!Number.isFinite(suite.historical_duration_seconds) || suite.historical_duration_seconds < 0)) throw new Error("invalid-catalog");
    seen.add(suite.id);
  }
  return catalog;
}

function readBounded(file, limit) {
  const fd = fs.openSync(file, "r");
  try {
    const bytes = Buffer.alloc(limit + 1);
    const length = fs.readSync(fd, bytes, 0, bytes.length, 0);
    if (length > limit) throw new Error("input-too-large");
    return bytes.subarray(0, length).toString("utf8");
  } finally {
    fs.closeSync(fd);
  }
}

function postDecision(endpoint, input, { request = https.request, onRequest = () => {} } = {}) {
  return new Promise((resolve, reject) => {
    let url;
    let body;
    try {
      url = new URL(endpoint);
      if (url.protocol !== "https:" || url.username || url.password || url.hash) throw new Error("invalid-endpoint");
      const hostname = url.hostname.replace(/\.$/, "");
      if (hostname === "api.github.com" || hostname === "github.com" || hostname.endsWith(".github.com")) throw new Error("github-api-forbidden");
      body = JSON.stringify(input);
      if (Buffer.byteLength(body) > LIMITS.request) throw new Error("input-too-large");
    } catch (error) {
      reject(error);
      return;
    }
    // One HTTP request, no redirect following, retries, discovery, or SDK calls.
    onRequest();
    const req = request(url, { method: "POST", headers: { "content-type": "application/json", "content-length": Buffer.byteLength(body) } }, res => {
      let size = 0;
      const chunks = [];
      res.on("data", chunk => {
        size += chunk.length;
        if (size > LIMITS.response) {
          req.destroy(new Error("response-too-large"));
          return;
        }
        chunks.push(chunk);
      });
      res.on("error", reject);
      res.on("end", () => {
        if (res.statusCode !== 200 || size > LIMITS.response) {
          reject(new Error("decision-unavailable"));
          return;
        }
        try {
          resolve(JSON.parse(Buffer.concat(chunks).toString("utf8")));
        } catch {
          reject(new Error("invalid-response"));
        }
      });
    });
    const timer = setTimeout(() => req.destroy(new Error("decision-timeout")), LIMITS.timeout);
    req.on("close", () => clearTimeout(timer));
    req.on("error", reject);
    req.end(body);
  });
}

function selectSuites(response, catalog) {
  if (!response || !Array.isArray(response.suite_ids) || !Number.isFinite(response.confidence) || response.confidence < 0.9 || response.confidence > 1) throw new Error("invalid-or-low-confidence");
  if (response.suite_ids.length > SUITE_IDS.length || new Set(response.suite_ids).size !== response.suite_ids.length || response.suite_ids.some(id => typeof id !== "string" || !SUITE_IDS.includes(id))) throw new Error("invalid-suite-ids");
  const selected = new Set([...response.suite_ids, ...catalog.suites.filter(suite => suite.mandatory).map(suite => suite.id), ...MANDATORY]);
  return SUITE_IDS.filter(id => selected.has(id));
}

async function frontDoor({ catalog, diff, prompt, endpoint, fullRun = false, decide = postDecision }) {
  let validCatalog;
  try {
    validCatalog = validateCatalog(catalog);
  } catch {
    // Still make the one configured decision request, but never trust its result.
  }
  let decisionCalls = 0;
  let response;
  let reason = fullRun ? "full-run" : !validCatalog ? "invalid-catalog" : diff.unavailable ? "diff-unavailable" : diff.truncated ? "diff-truncated" : null;
  if (!reason && diff.files.some(file => file.startsWith(".github/ci/") || file.startsWith("scripts/ci-front-door") || file === ".github/workflows/cgo.yml")) reason = "policy-changed";
  if (!fullRun) {
    try {
      response = await decide(
        endpoint,
        {
          diff_summary: diff,
          suite_catalog: validCatalog || { version: 1, suites: SUITE_IDS.map(id => ({ id, description: "Catalog unavailable; run all suites.", historical_duration_seconds: null, mandatory: MANDATORY.includes(id) })) },
          prompt,
        },
        {
          onRequest: () => {
            decisionCalls++;
          },
        }
      );
    } catch {
      reason ||= "decision-unavailable";
    }
  }
  let suiteIds = [...SUITE_IDS];
  if (!reason) {
    try {
      suiteIds = selectSuites(response, validCatalog);
    } catch {
      reason = "invalid-or-low-confidence";
    }
  }
  const omitted = validCatalog?.suites.filter(suite => !suiteIds.includes(suite.id));
  const knownDurations = omitted?.every(suite => suite.historical_duration_seconds !== null);
  return {
    suite_ids: suiteIds,
    fallback: !fullRun && reason !== null,
    reason: reason || "selected",
    metrics: {
      selected_suite_count: suiteIds.length,
      estimated_test_minutes_saved: knownDurations ? omitted.reduce((sum, suite) => sum + suite.historical_duration_seconds, 0) / 60 : null,
      missed_regressions: null,
      decision_api_requests: decisionCalls,
      other_github_api_requests: 0,
    },
  };
}

async function main() {
  const cwd = path.resolve(__dirname, "..");
  let catalog;
  let prompt = "";
  try {
    catalog = JSON.parse(readBounded(path.join(cwd, ".github/ci/suite-catalog.json"), 65536));
    prompt = readBounded(path.join(cwd, ".github/ci/decision-prompt.txt"), 8192);
    if (!prompt.trim()) throw new Error("missing-prompt");
  } catch {
    catalog = null;
  }
  const result = await frontDoor({
    catalog,
    prompt,
    diff: summarizeDiff({ cwd }),
    endpoint: process.env.CI_DECISION_URL,
    fullRun: process.env.GITHUB_EVENT_NAME !== "pull_request",
  });
  const json = JSON.stringify(result);
  if (process.env.GITHUB_OUTPUT) fs.appendFileSync(process.env.GITHUB_OUTPUT, `suite_ids=${JSON.stringify(result.suite_ids)}\nfallback=${result.fallback}\nmetrics=${JSON.stringify(result.metrics)}\n`);
  if (process.env.GITHUB_STEP_SUMMARY) fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, `### CI Front Door\n\n${json}\n`);
  process.stdout.write(json + "\n");
}

module.exports = { SUITE_IDS, MANDATORY, LIMITS, validateCatalog, summarizeDiff, postDecision, selectSuites, frontDoor };
if (require.main === module)
  main().catch(() => {
    process.stderr.write("CI Front Door failed; downstream jobs must fail open.\n");
    process.exitCode = 1;
  });
