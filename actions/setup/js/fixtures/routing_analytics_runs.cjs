// @ts-check
"use strict";

/**
 * Routing-analytics fixtures for github/gh-aw#70668.
 *
 * Each `routing-analytics/run-<id>/` directory keeps, verbatim, the collector
 * inputs from the run's original `agent` artifact (collector rootDir layout):
 * `runtime/aw_info.json`, `agent/model-resolution.json`,
 * `sandbox/firewall/awf-reflect.json` and the api-proxy `token-usage.jsonl` /
 * `model-routing.jsonl` under `sandbox/firewall/logs/api-proxy-logs/`.
 * `aw_session.jsonl` keeps, verbatim and in order, the `session.format`,
 * `workflow.info`, `firewall.model_routing`, `firewall.token_usage`,
 * `model.resolution`, `model_routing.outcome`, `guardrail.daily_aic` and
 * `session.collection` records of the published `usage/aw_session.jsonl`.
 * All other files and records are dropped.
 *
 * - run-39508154330: Smoke Copilot SDK Routed on main at 70e15c7ff6, AWF v0.28.70,
 *   https://github.com/github/gh-aw/actions/runs/39508154330 (usage 12303814398,
 *   agent 12304048825). Selected gpt-5.6-luna; four main requests, one
 *   claude-haiku-4.5 sub-agent request, one classifier request. The detection
 *   artifact's api-proxy `token-usage.jsonl` (two detection requests) is kept at
 *   `threat-detection/sandbox/firewall/logs/api-proxy-logs/`.
 * - run-39506475007: Smoke Copilot (CLI, unrouted) at 9c8c823adb, AWF v0.28.62,
 *   https://github.com/github/gh-aw/actions/runs/39506475007 (usage 12304237333,
 *   agent 12303457750). Main `auto` resolved to claude-sonnet-5.5.
 * - run-39499464895: Smoke Copilot SDK Routed Claude at b9f7f48a3b, AWF v0.28.62,
 *   https://github.com/github/gh-aw/actions/runs/39499464895 (usage 12300520074,
 *   agent 12298393979). Selected claude-haiku-4.5, then failed closed:
 *   `mini-whoami` requested unavailable gpt-5.4-mini.
 *
 * `routing-analytics/awf-10211/` holds records produced by the gh-aw-firewall
 * writers at b5c683e116470e0aa579ec9bf30a675fcdb66f76 (github/gh-aw-firewall#10211)
 * for the inputs of its tests, because none of the three runs has an
 * upstream-error record: `upstream-errors.jsonl` from `createLogUpstreamErrorResponse`
 * + `auditUpstreamErrorResponse` (upstream-log.test.js captured Copilot
 * `invalid_reasoning_effort`, provider-error-fields.test.js code/type/param and
 * type-only envelopes, and a non-JSON body without identifiers), and
 * `model-routing.jsonl` from `writeRoutingRecord` (upstream-response.test.js
 * `local-req-5` with `error_source: upstream`; routing-persistence.test.js
 * `request-1` with `error_source: awf`). `ts`/`timestamp` are the writer's clock.
 */

const fs = require("fs");
const path = require("path");
const { readJsonlFile } = require("../jsonl_helpers.cjs");

const DIR = path.join(__dirname, "routing-analytics");
const REPO_ROOT = path.join(__dirname, "../../../..");

const RUNS = {
  routed: { id: "39508154330", workflow: "smoke-copilot-sdk-routed" },
  unrouted: { id: "39506475007", workflow: "smoke-copilot" },
  routedClaude: { id: "39499464895", workflow: "smoke-copilot-sdk-routed-claude" },
};

/** @param {string} id */
const runDir = id => path.join(DIR, `run-${id}`);

/** @param {string} id @returns {Record<string, any>} */
const awInfo = id => JSON.parse(fs.readFileSync(path.join(runDir(id), "runtime/aw_info.json"), "utf8"));

/** @param {string} id @returns {any[]} The recorded session subset. */
const session = id => [...readJsonlFile(path.join(runDir(id), "aw_session.jsonl"), { strict: true })];

/**
 * Evaluate the compiled `GH_AW_INFO_*` env of the generate_aw_info step, as the
 * runner would with no repository variables set (`vars.X || 'literal'` -> literal).
 * @param {string} workflow
 * @param {Record<string, any>} info Recorded aw_info supplying `job.workflow_ref`.
 * @returns {Record<string, string>}
 */
function compiledInfoEnv(workflow, info) {
  const lines = fs.readFileSync(path.join(REPO_ROOT, ".github/workflows", `${workflow}.lock.yml`), "utf8").split("\n");
  const start = lines.findIndex(line => /^\s+GH_AW_INFO_ENGINE_NAME:/.test(line));
  if (start < 0) throw new Error(`No generate_aw_info step in ${workflow}`);
  let first = start;
  while (first > 0 && /^\s+GH_AW_INFO_[A-Z_]+:/.test(lines[first - 1])) first--;
  /** @type {Record<string, string>} */
  const env = {};
  for (let index = first; index < lines.length; index++) {
    const match = /^\s+(GH_AW_INFO_[A-Z_]+): (.*)$/.exec(lines[index]);
    if (!match) break;
    const raw = match[2].trim();
    let value;
    if (raw === "${{ job.workflow_ref }}") value = info.workflow_id;
    else if (raw === "${{ job.workflow_sha }}") value = info.sha;
    else if (raw.startsWith("${{")) value = /'([^']*)' }}$/.exec(raw)?.[1] ?? "";
    else if (raw.startsWith("'")) value = raw.slice(1, -1).replace(/''/g, "'");
    else if (raw.startsWith('"')) value = JSON.parse(raw);
    else value = raw;
    env[match[1]] = value;
  }
  return env;
}

/**
 * GitHub Actions context of the recorded run.
 * @param {Record<string, any>} info
 */
function runContext(info) {
  const [owner, repo] = info.repository.split("/");
  return { runId: info.run_id, runNumber: info.run_number, sha: info.sha, ref: info.ref, actor: info.actor, eventName: info.event_name, repo: { owner, repo }, serverUrl: "https://github.com", payload: {} };
}

module.exports = { DIR, RUNS, runDir, awInfo, session, compiledInfoEnv, runContext };
