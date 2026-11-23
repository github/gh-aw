import { afterEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { collectUnifiedSession, writeUnifiedSession } from "./unified_session.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

const req = require;
const { routedRun, routingOutcome, summarizeAgentOutput } = req("./routing_observation.cjs");
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
    expect(routedRun({ tokenUsage, routing, subAgentModels })).toEqual({ routed: true, started: true, pair: "match" });
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
    expect(routedRun({ tokenUsage: proxyRecords(claude, "token-usage.jsonl"), routing: proxyRecords(claude, "model-routing.jsonl") })).toEqual({ routed: true, started: false, pair: "unknown" });
  });
});

describe("routingOutcome", () => {
  /** @param {string[]} types @param {Record<string, unknown>} [extra] */
  const output = (types, extra = {}) => summarizeAgentOutput({ items: types.map(type => ({ type })), errors: [], ...extra });
  /** @param {Record<string, unknown>} overrides */
  const evidence = overrides => ({ pair: "match", agentResult: "success", harness: undefined, startupFailed: false, execution: { categories: [], errorCodes: [] }, output: output(["create_issue"]), threat: false, ...overrides });

  it.each([
    ["a successful agent with terminal output", {}, { outcome: "success" }],
    ["a valid noop", { output: output(["noop"]) }, { outcome: "success" }],
    ["a success that recovered from a rate limit", { execution: { categories: [], errorCodes: [429] } }, { outcome: "success" }],
    ["a cancelled agent job, before the pair check", { agentResult: "cancelled", pair: "mismatch" }, skip("cancelled")],
    ["a harness attempt cancelled or timed out", { agentResult: "failure", harness: { outcome: "failure", failureClasses: ["cancelled_or_timed_out"] } }, skip("cancelled")],
    ["a startup failure before any harness attempt", { agentResult: "failure", startupFailed: true, output: undefined }, skip("infrastructure")],
    ["an interrupted harness attempt", { agentResult: "failure", harness: { outcome: "interrupted", failureClasses: [] } }, skip("infrastructure")],
    ["a provider 5xx", { agentResult: "failure", execution: { categories: [], errorCodes: ["503"] } }, skip("infrastructure")],
    ["a 429 without terminal output", { agentResult: "failure", output: output([]), execution: { categories: [], errorCodes: [429] } }, skip("rate_limited")],
    ["requests that ran another model or effort", { pair: "mismatch" }, skip("pair_mismatch")],
    ["requests that could not be attributed", { pair: "unknown" }, skip("unknown_evidence")],
    ["an agent that reported it could not finish", { agentResult: "failure", output: output(["report_incomplete"]) }, { outcome: "failure" }],
    ["a turn limit", { agentResult: "failure", output: output([]), harness: { outcome: "failure", failureClasses: ["max_turns"] } }, { outcome: "failure" }],
    ["outputs the validator rejected", { agentResult: "failure", output: summarizeAgentOutput({ items: [], errors: ["invalid"] }) }, { outcome: "failure" }],
    ["no terminal output", { agentResult: "failure", output: output(["report_incomplete"], { collectorFailureCause: "missing_terminal_safe_output" }) }, { outcome: "failure" }],
    ["a failed agent job after valid terminal output", { agentResult: "failure" }, skip("unknown_evidence")],
    ["a threat detection finding", { threat: true }, skip("unknown_evidence")],
    ["missing agent output", { agentResult: "failure", output: undefined }, skip("unknown_evidence")],
  ])("%s", (_name, overrides, expected) => {
    expect(routingOutcome(evidence(overrides))).toEqual(expected);
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
