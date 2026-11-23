import { afterEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { collectUnifiedSession, writeUnifiedSession } from "./unified_session.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

const req = require;
const { routedRun, routingOutcome, summarizeAgentOutput, detectionVerdict } = req("./routing_observation.cjs");
const fixtures = req("./fixtures/routing_analytics_runs.cjs");
const { readJsonlFile } = req("./jsonl_helpers.cjs");

const PROXY_LOGS = "sandbox/firewall/logs/api-proxy-logs";
// Routed run 39147795806 created its required report, but a later check failed its agent job.
const FAILED_CHECK_RUN = fixtures.runDir("39147795806");
const { routed, unrouted, routedClaude } = fixtures.RUNS;

/** @param {string} dir @param {string} name */
const proxyRecords = (dir, name) => [...readJsonlFile(path.join(dir, PROXY_LOGS, name), { strict: true })];
/** @param {string} reason */
const skip = reason => ({ outcome: "skip", reason });

describe("routedRun", () => {
  // Run 39508154330 routed gpt-5.6-luna; its declared haiku-whoami sub-agent ran claude-haiku-4.5.
  const dir = fixtures.runDir(routed.id);
  const tokenUsage = proxyRecords(dir, "token-usage.jsonl");
  const routing = proxyRecords(dir, "model-routing.jsonl");
  const subAgentModels = fixtures.awInfo(routed.id).sub_agent_models;
  const mainRequest = tokenUsage.find(record => !record.purpose && record.model === "gpt-5.6-luna").request_id;

  it("matches when the main requests ran as selected beside a declared sub-agent", () => {
    expect(routedRun({ tokenUsage, routing, subAgentModels })).toEqual({ routed: true, started: true, pair: "match", statuses: [] });
  });

  it("reports the statuses AWF recorded for the agent's failed requests, not the classifier's", () => {
    const classifierRequest = tokenUsage.find(record => record.purpose === "routing_classification").request_id;
    const upstreamErrors = [
      { request_id: mainRequest, status: 429 },
      { request_id: classifierRequest, status: 503 },
      // A failed classifier call can leave no usage or routing record at all.
      { request_id: "classifier-without-records", status: 503 },
    ];
    expect(routedRun({ tokenUsage, routing, upstreamErrors, subAgentModels }).statuses).toEqual([429]);
  });

  it("treats a model no declared sub-agent uses as a deviation from the selected pair", () => {
    expect(routedRun({ tokenUsage, routing }).pair).toBe("mismatch");
  });

  it("cannot attribute a request without AWF's routing record, or with conflicting ones", () => {
    expect(routedRun({ tokenUsage, routing: routing.filter(record => record.request_id !== mainRequest), subAgentModels }).pair).toBe("unknown");
    const conflicting = { ...routing.find(record => record.request_id === mainRequest), routed: "deviated" };
    expect(routedRun({ tokenUsage, routing: [...routing, conflicting], subAgentModels }).pair).toBe("unknown");
  });

  it("counts a request AWF routed but recorded no usage for", () => {
    const rejected = { stage: "request", request_id: "rejected-1", routed: "deviated", requested_model: "gpt-6-luna" };
    expect(routedRun({ tokenUsage, routing: [...routing, rejected], subAgentModels }).pair).toBe("mismatch");
  });

  it("needs AWF's selection to call a run routed, and an agent request to call it started", () => {
    expect(routedRun({ tokenUsage, routing: routing.filter(record => record.stage !== "selection"), subAgentModels }).routed).toBe(false);
    // Run 39499464895 recorded a selection and the router's classifier call, but no agent request.
    const claude = fixtures.runDir(routedClaude.id);
    expect(routedRun({ tokenUsage: proxyRecords(claude, "token-usage.jsonl"), routing: proxyRecords(claude, "model-routing.jsonl") })).toEqual({ routed: true, started: false, pair: "unknown", statuses: [] });
  });
});

describe("routingOutcome", () => {
  /** @param {string[]} types @param {Record<string, unknown>} [extra] */
  const output = (types, extra = {}) => summarizeAgentOutput({ items: types.map(type => ({ type })), errors: [], ...extra });
  /** @param {Record<string, unknown>} overrides */
  const evidence = overrides => ({
    pair: "match",
    agentResult: "success",
    harness: undefined,
    startupFailed: false,
    execution: { categories: [], errorCodes: [] },
    proxyStatuses: [],
    refused: false,
    output: output(["create_issue"]),
    detection: "clean",
    ...overrides,
  });
  const turnLimit = { outcome: "failure", failureClasses: ["max_turns"] };

  it.each([
    ["a successful agent with terminal output", {}, { outcome: "success" }],
    ["a valid noop", { output: output(["noop"]) }, { outcome: "success" }],
    ["a success that recovered from a rate limit", { execution: { categories: [], errorCodes: [429] }, proxyStatuses: [503] }, { outcome: "success" }],
    ["only a continuation request", { output: output(["request_continuation"]) }, skip("unknown_evidence")],
    ["a cancelled agent job, before the pair check", { agentResult: "cancelled", pair: "mismatch" }, skip("cancelled")],
    ["a harness attempt cancelled or timed out", { agentResult: "failure", harness: { outcome: "failure", failureClasses: ["cancelled_or_timed_out"] } }, skip("cancelled")],
    ["a startup failure before any harness attempt", { agentResult: "failure", startupFailed: true, output: undefined }, skip("infrastructure")],
    ["an interrupted harness attempt", { agentResult: "failure", harness: { outcome: "interrupted", failureClasses: [] } }, skip("infrastructure")],
    ["a provider 5xx", { agentResult: "failure", execution: { categories: [], errorCodes: ["503"] } }, skip("infrastructure")],
    ["an authentication failure", { agentResult: "failure", execution: { categories: ["authentication_failed"], errorCodes: [] } }, skip("infrastructure")],
    ["an incomplete report claiming an infrastructure error", { agentResult: "failure", output: summarizeAgentOutput({ items: [{ type: "report_incomplete", reason: "infrastructure_error" }], errors: [] }) }, skip("unknown_evidence")],
    [
      "a safe-output gateway that registered no tools",
      { agentResult: "failure", output: output(["report_incomplete"], { errors: ["gateway"], collectorFailureCause: "request_rejection", collectorInfrastructureCause: "safeoutputs_gateway_empty" }) },
      skip("infrastructure"),
    ],
    ["a 429 without terminal output", { agentResult: "failure", output: output([]), execution: { categories: [], errorCodes: [429] } }, skip("rate_limited")],
    ["a 429 that only AWF recorded", { agentResult: "failure", output: output(["report_incomplete"]), proxyStatuses: [429] }, skip("rate_limited")],
    ["requests that ran another model or effort", { pair: "mismatch" }, skip("pair_mismatch")],
    ["requests that could not be attributed", { pair: "unknown" }, skip("unknown_evidence")],
    ["an agent that reported it could not finish", { agentResult: "failure", output: output(["report_incomplete"]) }, { outcome: "failure" }],
    ["a turn limit", { agentResult: "failure", output: output([]), harness: turnLimit }, { outcome: "failure" }],
    ["a turn limit without agent output", { agentResult: "failure", output: undefined, harness: turnLimit }, { outcome: "failure" }],
    ["a turn limit beside an unclassified nonzero exit", { agentResult: "failure", output: output(["report_incomplete"], { collectorFailureCause: "driver_crash" }), harness: turnLimit }, { outcome: "failure" }],
    ["a credit limit wrapped as an authentication failure", { agentResult: "failure", output: output([]), execution: { categories: ["authentication_failed", "max_ai_credits_exceeded"], errorCodes: [] } }, { outcome: "failure" }],
    ["a refusal that left no terminal output", { agentResult: "failure", output: output([]), refused: true }, { outcome: "failure" }],
    ["a refusal the agent recovered from", { agentResult: "failure", refused: true }, skip("unknown_evidence")],
    ["outputs the validator rejected", { agentResult: "failure", output: summarizeAgentOutput({ items: [], errors: ["invalid"] }) }, { outcome: "failure" }],
    ["no terminal output", { agentResult: "failure", output: output(["report_incomplete"], { collectorFailureCause: "missing_terminal_safe_output" }) }, { outcome: "failure" }],
    ["a failed agent job after valid terminal output", { agentResult: "failure" }, skip("unknown_evidence")],
    ["a threat detection finding", { detection: "threat" }, skip("unknown_evidence")],
    ["rejected output that threat detection flagged", { agentResult: "failure", detection: "threat", output: summarizeAgentOutput({ items: [], errors: ["invalid"] }) }, skip("unknown_evidence")],
    ["detection that could not reach a result", { detection: "inconclusive" }, skip("unknown_evidence")],
    ["missing agent output", { agentResult: "failure", output: undefined }, skip("unknown_evidence")],
  ])("%s", (_name, overrides, expected) => {
    expect(routingOutcome(evidence(overrides))).toEqual(expected);
  });
});

describe("detectionVerdict", () => {
  it.each([
    ["no detection result", [], "clean"],
    ["a clean conclusion result", [{ job_result: "success", conclusion: "success", prompt_injection: false }], "clean"],
    ["skipped detection", [{ job_result: "skipped", conclusion: "skipped", reason: "detection_skipped" }], "clean"],
    ["a threat flag", [{ secretLeak: true }], "threat"],
    ["a threat reason without flags", [{ job_result: "failure", conclusion: "failure", reason: "threat_detected" }], "threat"],
    ["a detector that failed", [{ job_result: "success", conclusion: "warning", reason: "agent_failure" }], "inconclusive"],
    ["a warning without a verdict", [{ job_result: "success", conclusion: "warning" }], "inconclusive"],
    ["a cancelled detection job", [{ job_result: "cancelled" }], "inconclusive"],
  ])("%s", (_name, results, expected) => {
    expect(detectionVerdict(results)).toBe(expected);
  });
});

describe("routing.observation in the unified session", () => {
  const roots = [];
  afterEach(() => {
    for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
  });

  /** @param {string} source @param {unknown} [agentOutput] */
  function prepare(source, agentOutput = { items: [{ type: "create_issue" }], errors: [] }) {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "routing-observation-"));
    roots.push(root);
    fs.cpSync(source, root, { recursive: true, filter: file => path.basename(file) !== "aw_session.jsonl" });
    fs.writeFileSync(path.join(root, "agent_output.json"), JSON.stringify(agentOutput));
    return root;
  }
  /** @param {string} agent */
  const jobResults = agent => JSON.stringify({ activation: { result: "success" }, agent: { result: agent } });
  /** @param {string} root @param {{agent?: string, engine?: string, routingObservation?: boolean}} [options] */
  const collect = (root, { agent = "success", engine, routingObservation = true } = {}) =>
    collectUnifiedSession({ rootDir: root, engine, warn: () => {}, dailyAIC: {}, admission: {}, jobResults: jobResults(agent), routingObservation }).events;
  /** @param {any[]} events */
  const verdicts = events => events.filter(event => event.type === "routing.observation");

  it("writes one collector verdict for a routed run that validates against the session schema", () => {
    const root = prepare(fixtures.runDir(routed.id));
    const outputPath = path.join(root, "usage/aw_session.jsonl");
    writeUnifiedSession({ rootDir: root, outputPath, warn: () => {}, dailyAIC: {}, admission: {}, jobDurations: {}, jobResults: jobResults("success"), routingObservation: true });
    const content = fs.readFileSync(outputPath, "utf8");
    const events = [...readJsonlFile(outputPath, { strict: true })];
    expect(validateSession(content)).toBe(events.length);
    const [verdict, ...rest] = verdicts(events);
    expect(rest).toEqual([]);
    expect(verdict.data).toEqual({ outcome: "success" });
    expect(verdict.provenance).toMatchObject({ component: "collector", phase: "conclusion", path: "usage/aw_session.jsonl" });
  });

  it("judges the same AWF evidence the same way for every engine", () => {
    const root = prepare(fixtures.runDir(routed.id));
    for (const engine of ["copilot", "claude", "codex", "pi"]) {
      expect(verdicts(collect(root, { engine })).map(event => event.data)).toEqual([{ outcome: "success" }]);
    }
    expect(verdicts(collect(root, { agent: "failure" })).map(event => event.data)).toEqual([skip("unknown_evidence")]);
  });

  it("does not count run 39147795806, whose report succeeded but whose agent job failed a later check", () => {
    const root = prepare(FAILED_CHECK_RUN);
    expect(verdicts(collect(root, { agent: "failure" })).map(event => event.data)).toEqual([skip("unknown_evidence")]);
    expect(verdicts(collect(root)).map(event => event.data)).toEqual([{ outcome: "success" }]);
  });

  it("reads detection results, AWF's failed requests and content-filter refusals from the run", () => {
    const empty = { items: [], errors: [] };
    const threat = prepare(fixtures.runDir(routed.id));
    fs.mkdirSync(path.join(threat, "usage/detection"), { recursive: true });
    fs.writeFileSync(path.join(threat, "usage/detection/detection_result.json"), JSON.stringify({ job_result: "failure", conclusion: "failure", reason: "threat_detected" }));
    expect(verdicts(collect(threat)).map(event => event.data)).toEqual([skip("unknown_evidence")]);

    // A failed agent that left no output is unknown evidence, unless AWF recorded a provider outage.
    const outage = prepare(fixtures.runDir(routed.id), empty);
    expect(verdicts(collect(outage, { agent: "failure" })).map(event => event.data)).toEqual([skip("unknown_evidence")]);
    const mainRequest = proxyRecords(outage, "token-usage.jsonl").find(record => !record.purpose && record.model === "gpt-5.6-luna").request_id;
    fs.writeFileSync(path.join(outage, PROXY_LOGS, "upstream-errors.jsonl"), JSON.stringify({ event: "UPSTREAM_ERROR_RESPONSE", request_id: mainRequest, status: 503 }) + "\n");
    expect(verdicts(collect(outage, { agent: "failure" })).map(event => event.data)).toEqual([skip("infrastructure")]);

    const filtered = prepare(fixtures.runDir(routed.id), empty);
    fs.writeFileSync(path.join(filtered, "agent-session.jsonl"), JSON.stringify({ type: "assistant.refusal", data: { reason: "refusal" } }) + "\n");
    expect(verdicts(collect(filtered, { agent: "failure" })).map(event => event.data)).toEqual([{ outcome: "failure" }]);

    // A fallback detection file's reason is untrusted, as in the published session.
    const fallback = prepare(fixtures.runDir(routed.id));
    fs.rmSync(path.join(fallback, "usage/detection"), { recursive: true, force: true });
    fs.mkdirSync(path.join(fallback, "threat-detection"), { recursive: true });
    fs.writeFileSync(path.join(fallback, "threat-detection/detection_result.json"), JSON.stringify({ prompt_injection: false, secret_leak: false, malicious_patch: false, reason: "agent_failure" }));
    expect(verdicts(collect(fallback)).map(event => event.data)).toEqual([{ outcome: "success" }]);
  });

  it("has no verdict outside the conclusion job, for an unrouted run, or when the agent made no request", () => {
    expect(verdicts(collect(prepare(fixtures.runDir(routed.id)), { routingObservation: false }))).toEqual([]);
    expect(verdicts(collect(prepare(fixtures.runDir(unrouted.id))))).toEqual([]);
    expect(verdicts(collect(prepare(fixtures.runDir(routedClaude.id))))).toEqual([]);
  });

  it("drops a verdict that any other source supplies", () => {
    const root = prepare(fixtures.runDir(unrouted.id));
    fs.writeFileSync(path.join(root, "agent-session.jsonl"), JSON.stringify({ type: "routing.observation", data: { outcome: "success" } }) + "\n");
    expect(verdicts(collect(root))).toEqual([]);
  });
});
