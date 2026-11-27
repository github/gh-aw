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
    [
      "a category-only model endpoint mismatch with no terminal output",
      { agentResult: "failure", output: output(["report_incomplete"], { collectorFailureCause: "missing_terminal_safe_output" }), execution: { categories: ["model_endpoint_mismatch_error"], errorCodes: [] } },
      skip("infrastructure"),
    ],
    [
      "a category-only model policy violation with no terminal output",
      { agentResult: "failure", output: output(["report_incomplete"], { collectorFailureCause: "missing_terminal_safe_output" }), execution: { categories: ["model_policy_violation"], errorCodes: [] } },
      skip("infrastructure"),
    ],
    [
      "a model endpoint mismatch beside a confirmed limit",
      { agentResult: "failure", output: output([]), execution: { categories: ["model_endpoint_mismatch_error", "effective_tokens_limit_exceeded"], errorCodes: [] } },
      skip("infrastructure"),
    ],
    ["an API challenge the harness retries could not clear", { agentResult: "failure", output: output([]), harness: { outcome: "failure", failureClasses: ["transient_error"] } }, skip("infrastructure")],
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
    [
      "an effective token limit beside a model policy violation",
      { agentResult: "failure", output: output([]), execution: { categories: ["model_policy_violation", "effective_tokens_limit_exceeded"], errorCodes: [] } },
      { outcome: "failure" },
    ],
    [
      "a permission denial limit beside a model policy violation",
      { agentResult: "failure", output: output([]), execution: { categories: ["model_policy_violation", "permission_denied_limit_exceeded"], errorCodes: [] } },
      { outcome: "failure" },
    ],
    ["a turn limit beside a retried API challenge", { agentResult: "failure", output: output([]), harness: { outcome: "failure", failureClasses: ["transient_error", "max_turns"] } }, { outcome: "failure" }],
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
  /** @param {string} agent @param {string} [detection] */
  const jobResults = (agent, detection) => JSON.stringify({ activation: { result: "success" }, agent: { result: agent }, ...(detection ? { detection: { result: detection } } : {}) });
  /** @param {string} root @param {{agent?: string, engine?: string, routingObservation?: boolean}} [options] */
  const collect = (root, { agent = "success", detection, engine, routingObservation = true } = {}) =>
    collectUnifiedSession({ rootDir: root, engine, warn: () => {}, dailyAIC: {}, admission: {}, jobResults: jobResults(agent, detection), routingObservation }).events;
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
    // A detector that failed before writing any result leaves no clean verdict either.
    const failedDetector = prepare(fixtures.runDir(routed.id));
    fs.rmSync(path.join(failedDetector, "usage/detection"), { recursive: true, force: true });
    expect(verdicts(collect(failedDetector, { detection: "failure" })).map(event => event.data)).toEqual([skip("unknown_evidence")]);

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

  describe("agent-output-fallback parity with the full agent artifact", () => {
    const { persistUpstreamErrorFallback } = req("./parse_token_usage.cjs");
    const { persistRoutingRefusalIndicator } = req("./log_parser_bootstrap.cjs");
    const LAYOUTS = ["sandbox/firewall/logs/api-proxy-logs", "sandbox/firewall/audit/api-proxy-logs", "sandbox/firewall-audit-logs/api-proxy-logs"];

    /**
     * Build a full-artifact run directory and a fallback-only recovery directory containing only
     * the files uploaded by `agent-output-fallback` (no `agent-session.jsonl`, no `agent-stdio.log`,
     * no native session logs, and no raw `upstream-errors.jsonl`).
     * @param {{layout?: string, agentOutput?: unknown, upstreamError?: Record<string, unknown>, sessionEvents?: unknown[]}} options
     */
    function buildFullAndFallback({ layout = PROXY_LOGS, agentOutput = { items: [], errors: [] }, upstreamError, sessionEvents = [], agentExecution }) {
      const full = fs.mkdtempSync(path.join(os.tmpdir(), "routing-full-"));
      const fallback = fs.mkdtempSync(path.join(os.tmpdir(), "routing-fallback-"));
      roots.push(full, fallback);
      const source = fixtures.runDir(routed.id);
      const awInfo = JSON.stringify(fixtures.awInfo(routed.id));
      // Write activation metadata (from the info artifact) and proxy files into the selected layout.
      fs.writeFileSync(path.join(full, "aw_info.json"), awInfo);
      fs.writeFileSync(path.join(fallback, "aw_info.json"), awInfo);
      fs.writeFileSync(path.join(full, "agent_output.json"), JSON.stringify(agentOutput));
      fs.writeFileSync(path.join(fallback, "agent_output.json"), JSON.stringify(agentOutput));
      const fullProxyDir = path.join(full, layout);
      const fallbackProxyDir = path.join(fallback, layout);
      fs.mkdirSync(fullProxyDir, { recursive: true });
      fs.mkdirSync(fallbackProxyDir, { recursive: true });
      for (const name of ["token-usage.jsonl", "model-routing.jsonl"]) {
        fs.copyFileSync(path.join(source, PROXY_LOGS, name), path.join(fullProxyDir, name));
        fs.copyFileSync(path.join(source, PROXY_LOGS, name), path.join(fallbackProxyDir, name));
      }
      if (upstreamError) {
        fs.writeFileSync(path.join(fullProxyDir, "upstream-errors.jsonl"), JSON.stringify(upstreamError) + "\n");
      }
      if (sessionEvents.length > 0) {
        fs.writeFileSync(path.join(full, "agent-session.jsonl"), sessionEvents.map(e => JSON.stringify(e)).join("\n") + "\n");
      }
      if (agentExecution) {
        fs.writeFileSync(path.join(full, "agent-errors.jsonl"), JSON.stringify({ type: "agent.execution", data: agentExecution }) + "\n");
      }
      // Run the agent-job producers on the full directory, then copy ONLY fallback-manifest files.
      persistUpstreamErrorFallback(full);
      persistRoutingRefusalIndicator(full, sessionEvents);
      for (const rel of ["agent-errors.jsonl", "agent/upstream-errors-fallback.jsonl", "agent/routing-refusal.json"]) {
        const src = path.join(full, rel);
        if (fs.existsSync(src)) {
          fs.mkdirSync(path.join(fallback, path.dirname(rel)), { recursive: true });
          fs.copyFileSync(src, path.join(fallback, rel));
        }
      }
      return { full, fallback };
    }

    const mainRequest = proxyRecords(fixtures.runDir(routed.id), "token-usage.jsonl").find(record => !record.purpose && record.model === "gpt-5.6-luna").request_id;

    it.each(LAYOUTS)("matches full-artifact verdicts in fallback-only recovery across layout %s", layout => {
      const scenarios = [
        {
          name: "report_incomplete with AWF-only HTTP 429",
          agent: "failure",
          agentOutput: { items: [{ type: "report_incomplete" }], errors: [] },
          upstreamError: { event: "UPSTREAM_ERROR_RESPONSE", request_id: mainRequest, status: 429, response_headers: { "x-debug": "secret-header" }, response_body: "rate limited" },
          expected: skip("rate_limited"),
        },
        {
          name: "failed agent with no terminal output and AWF-only HTTP 503",
          agent: "failure",
          agentOutput: { items: [], errors: [] },
          upstreamError: { event: "UPSTREAM_ERROR_RESPONSE", request_id: mainRequest, status: 503, response_body: "upstream outage" },
          expected: skip("infrastructure"),
        },
        {
          name: "failed agent with no terminal output and main-agent assistant.refusal",
          agent: "failure",
          agentOutput: { items: [], errors: [] },
          sessionEvents: [{ type: "assistant.refusal", data: { reason: "content_filter", content: "Private refusal content" } }],
          expected: { outcome: "failure" },
        },
        {
          name: "failed agent with no terminal output and nested subagent refusal only",
          agent: "failure",
          agentOutput: { items: [], errors: [] },
          sessionEvents: [{ type: "assistant.refusal", data: { reason: "refusal", agentId: "subagent-1", content: "Subagent refusal" } }],
          expected: skip("unknown_evidence"),
        },
        {
          name: "failed agent with agentic_engine_timeout in agent-errors.jsonl",
          agent: "failure",
          agentOutput: { items: [], errors: [] },
          agentExecution: { categories: ["agentic_engine_timeout"], errorCodes: [], errorTypes: [], exitCode: 124 },
          expected: skip("infrastructure"),
        },
        {
          name: "failed agent with effective_tokens_limit_exceeded in agent-errors.jsonl",
          agent: "failure",
          agentOutput: { items: [], errors: [] },
          agentExecution: { categories: ["effective_tokens_limit_exceeded"], errorCodes: [], errorTypes: [], exitCode: 1 },
          expected: { outcome: "failure" },
        },
        {
          name: "report_incomplete with HTTP 429 and space-containing errorType in agent-errors.jsonl",
          agent: "failure",
          agentOutput: { items: [{ type: "report_incomplete" }], errors: [] },
          agentExecution: { categories: [], errorCodes: [429], errorTypes: ["Too Many Requests"], exitCode: 1 },
          expected: skip("rate_limited"),
        },
      ];

      const { main: validateRecovery } = req("./validate_recovery_envelopes.cjs");
      for (const scenario of scenarios) {
        const { full, fallback } = buildFullAndFallback({
          layout,
          agentOutput: scenario.agentOutput,
          upstreamError: scenario.upstreamError,
          sessionEvents: scenario.sessionEvents,
          agentExecution: scenario.agentExecution,
        });
        // Run the pre-upload recovery validator on the fallback root before collection.
        validateRecovery({ roots: [fallback] });
        const fullOut = path.join(full, "usage/aw_session.jsonl");
        const fallbackOut = path.join(fallback, "usage/aw_session.jsonl");
        writeUnifiedSession({ rootDir: full, outputPath: fullOut, warn: () => {}, dailyAIC: {}, admission: {}, jobDurations: {}, jobResults: jobResults(scenario.agent), routingObservation: true });
        writeUnifiedSession({ rootDir: fallback, outputPath: fallbackOut, warn: () => {}, dailyAIC: {}, admission: {}, jobDurations: {}, jobResults: jobResults(scenario.agent), routingObservation: true });

        const fullContent = fs.readFileSync(fullOut, "utf8");
        const fallbackContent = fs.readFileSync(fallbackOut, "utf8");
        const fullEvents = [...readJsonlFile(fullOut, { strict: true })];
        const fallbackEvents = [...readJsonlFile(fallbackOut, { strict: true })];
        expect(validateSession(fullContent)).toBe(fullEvents.length);
        expect(validateSession(fallbackContent)).toBe(fallbackEvents.length);
        expect(verdicts(fullEvents).map(e => e.data)).toEqual([scenario.expected]);
        expect(verdicts(fallbackEvents).map(e => e.data)).toEqual([scenario.expected]);
        // Fallback recovery must never copy response headers, bodies, or refusal prose.
        expect(fallbackContent).not.toContain("secret-header");
        expect(fallbackContent).not.toContain("Private refusal content");
      }
    });

    it("does not duplicate upstream errors or override full session evidence when both artifacts merge", () => {
      const { full } = buildFullAndFallback({
        agentOutput: { items: [], errors: [] },
        upstreamError: { event: "UPSTREAM_ERROR_RESPONSE", request_id: mainRequest, status: 503, provider: "copilot", model: "gpt-5.6-luna", error_code: "server_error" },
        sessionEvents: [{ type: "assistant.message", data: { content: "no refusal here" } }],
      });
      // Simulate a stale/conflicting refusal indicator beside a merged full agent-session.jsonl.
      fs.mkdirSync(path.join(full, "agent"), { recursive: true });
      fs.writeFileSync(path.join(full, "agent/routing-refusal.json"), '{"refused":true}\n');

      const events = collect(full, { agent: "failure" });
      const upstreamEvents = events.filter(event => event.type === "firewall.upstream_error");
      expect(upstreamEvents).toHaveLength(1);
      expect(upstreamEvents[0].data).toMatchObject({ requestId: mainRequest, httpStatus: 503, provider: "copilot", model: "gpt-5.6-luna", errorCode: "server_error" });
      expect(upstreamEvents[0].provenance.path).toContain("upstream-errors.jsonl");
      expect(verdicts(events).map(e => e.data)).toEqual([skip("infrastructure")]);

      // Remove upstream errors to confirm full agent-session.jsonl takes precedence over routing-refusal.json.
      fs.rmSync(path.join(full, PROXY_LOGS, "upstream-errors.jsonl"));
      fs.rmSync(path.join(full, "agent/upstream-errors-fallback.jsonl"));
      expect(verdicts(collect(full, { agent: "failure" })).map(e => e.data)).toEqual([skip("unknown_evidence")]);
    });

    it("rejects over-64-char fallback request_ids and ignores fallback upstream errors in protected session capture", () => {
      const { fallback } = buildFullAndFallback({
        agentOutput: { items: [], errors: [] },
      });
      const overBoundaryId = "r".repeat(65);
      fs.mkdirSync(path.join(fallback, "agent"), { recursive: true });
      fs.writeFileSync(path.join(fallback, "agent/upstream-errors-fallback.jsonl"), JSON.stringify({ event: "UPSTREAM_ERROR_RESPONSE", request_id: overBoundaryId, status: 503 }) + "\n");
      const overBoundaryEvents = collect(fallback, { agent: "failure" });
      expect(overBoundaryEvents.filter(event => event.type === "firewall.upstream_error")).toEqual([]);
      expect(overBoundaryEvents.some(event => event.type === "session.collection_warning" && event.data.code === "invalid_upstream_error_fallback")).toBe(true);

      // In protected session capture, sandbox-side fallback upstream-error files are ignored.
      fs.writeFileSync(path.join(fallback, "agent-session.jsonl"), JSON.stringify({ type: "assistant.message", data: { content: "verified" } }) + "\n");
      fs.writeFileSync(path.join(fallback, "agent/upstream-errors-fallback.jsonl"), JSON.stringify({ event: "UPSTREAM_ERROR_RESPONSE", request_id: mainRequest, status: 503 }) + "\n");
      const protectedEvents = collectUnifiedSession({
        rootDir: fallback,
        protectedSessionCapture: true,
        warn: () => {},
        dailyAIC: {},
        admission: {},
        jobResults: jobResults("failure"),
        routingObservation: true,
      }).events;
      expect(protectedEvents.filter(event => event.type === "firewall.upstream_error")).toEqual([]);
    });
  });
});
