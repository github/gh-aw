import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { checkDetectionEvidence, checkModelRoutingEvidence, checkSessionContract, detectionMain, evaluateDetectionEvidence, main, normalizeEndpoint, servedModel, sessionContractMain } from "./smoke_model_routing_assertions.cjs";
import { piRoutedSession, postFixCopilotRoutedSession, postFixCopilotSession, preFixCopilotSession } from "./fixtures/session_contract_sessions.cjs";

const TOKEN_USAGE = "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl";
const ROUTED_ALLOWED = ["gpt-5.4-mini", "gpt-5.6-luna", "claude-haiku-4.5"];

// Fixtures mirror the record shapes from runs 38097181601 (Smoke Copilot Routed),
// 38096990663 (Smoke Pi Routed) and 38100426674 (Smoke Copilot SDK Routed):
// requests come from the agent artifact's api-proxy token-usage.jsonl, where only
// the classifier carries `purpose`; agent and sub-agent requests carry none.
const workflowInfo = (status = "selected", model = "gpt-5.6-luna", engineId = "copilot") => ({
  type: "workflow.info",
  data: {
    engineId,
    model,
    modelRouting: { status, source: "awf-routing", provider: "copilot", wireModel: model, model: `github-copilot/${model}`, effectiveEndpoint: "/responses", selectedEndpoint: "/responses", mode: "economy" },
  },
  provenance: { component: "workflow", phase: "activation", path: "aw_info.json", index: 0 },
});
const selection = (model = "gpt-5.6-luna", endpoint = "/responses") => ({
  type: "firewall.model_routing",
  data: { schema: "model-routing/v0.28.50", stage: "selection", selectedProvider: "copilot", selectedModel: `github-copilot/${model}`, wireModel: model, endpoint },
  provenance: { component: "firewall", phase: "agent", path: "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl", index: 1 },
});
const harnessOutcome = (status = "selected") => ({
  type: "model_routing.outcome",
  data: { status, wireModel: "gpt-5.6-luna", effectiveEndpoint: "/responses", selectedEndpoint: "/responses" },
  provenance: { component: "agent", phase: "agent", path: "agent/awf-routing-outcome.json", index: 0 },
});
let nextRequestId = 0;
const usage = (model, endpoint, status = 200, extra = {}) => ({
  _schema: "token-usage/v0.28.50",
  event: "token_usage",
  request_id: `req-${++nextRequestId}`,
  provider: "copilot",
  model,
  path: endpoint,
  status,
  streaming: true,
  x_initiator: "agent",
  ...extra,
});
const classifier = (model = "gpt-5.6-luna") => usage(model, "/responses", 200, { streaming: false, purpose: "routing_classification" });
const luna = (status = 200, endpoint = "/responses", extra = {}) => usage("gpt-5.6-luna", endpoint, status, extra);
const reflect = {
  endpoints: [
    { provider: "openai", routing_models: [] },
    {
      provider: "copilot",
      routing_models: [
        { model_id: "claude-haiku-4.5", supported_endpoints: ["/chat/completions", "/v1/messages"] },
        { model_id: "gpt-5.4-mini", supported_endpoints: ["/responses", "ws:/responses"] },
        { model_id: "gpt-5.6-luna", supported_endpoints: ["/responses", "ws:/responses"] },
      ],
    },
  ],
};

// Pi: sub-agent lifecycle events keyed by invocationId (== agentId).
const piEvent = (type, invocationId, data = {}) => ({
  type,
  agentId: invocationId,
  data: { invocationId, ...data },
  provenance: { component: "agent", phase: "agent", path: "agent-session.jsonl", index: 0 },
});
const HAIKU_ID = "6d6644df-c25f-400f-a2fc-39badc717c9b";
const MINI_ID = "089f851c-786e-468f-914e-0dd9a6f38362";
const piSession = (overrides = {}) => [
  workflowInfo("selected", "gpt-5.6-luna", "pi"),
  selection(),
  piEvent("subagent.started", HAIKU_ID, { agentName: "haiku-whoami", model: "copilot/claude-haiku-4.5", resolvedModel: "claude-haiku-4.5" }),
  piEvent("subagent.configured", HAIKU_ID, { model: "claude-haiku-4.5" }),
  piEvent("subagent.request", HAIKU_ID, { agentName: "haiku-whoami", model: overrides.haikuModel ?? "claude-haiku-4-5-20251001" }),
  piEvent("subagent.completed", HAIKU_ID, { outcome: "completed", agentName: "haiku-whoami" }),
  piEvent("subagent.started", MINI_ID, { agentName: "mini-whoami", model: "copilot/gpt-5.4-mini", resolvedModel: "gpt-5.4-mini" }),
  piEvent("subagent.configured", MINI_ID, { model: "gpt-5.4-mini" }),
  piEvent("subagent.request", MINI_ID, { agentName: "mini-whoami", model: "gpt-5.4-mini" }),
  piEvent(overrides.miniFailed ? "subagent.failed" : "subagent.completed", MINI_ID, { outcome: overrides.miniFailed ? "failed" : "completed", agentName: "mini-whoami" }),
  harnessOutcome(),
];
const piHaiku = (status = 200, endpoint = "/v1/messages?beta=true") => usage("claude-haiku-4-5-20251001", endpoint, status);
const piMini = (status = 200, endpoint = "/responses") => usage("gpt-5.4-mini-2026-03-17", endpoint, status);
const piRequests = ({ haiku = [piHaiku()], mini = [piMini()] } = {}) => [classifier(), luna(), ...haiku, luna(), ...mini, luna(), luna(), luna()];

// Copilot SDK: lifecycle events keyed by agentId with a toolCallId and no
// invocationId; a second subagent.completed marked cancelled follows the first.
const SDK_AGENT_ID = "b0f874cf-5674-489c-96c8-5dabcfc14499";
const sdkEvent = (type, agentName, data = {}, agentId = SDK_AGENT_ID) => ({
  type,
  agentId,
  data: { toolCallId: "call_d67p12Dpt9jri5ih0qxHcZjb", agentName, agentDisplayName: agentName, ...data },
  provenance: { component: "agent", phase: "agent", path: "sandbox/agent/logs/copilot-session-state/events.jsonl", index: 0 },
});
const sdkSession = (agentName = "haiku-whoami") => [
  workflowInfo(),
  selection(),
  sdkEvent("subagent.started", agentName, { agentType: agentName, model: "copilot-completions/claude-haiku-4.5", modelSelectionSource: "agent_definition_default" }),
  { type: "subagent.configured", agentId: SDK_AGENT_ID, data: { model: "copilot-completions/claude-haiku-4.5", multiTurn: true }, provenance: { component: "agent", phase: "agent", path: "events.jsonl", index: 1 } },
  sdkEvent("subagent.selected", agentName, { tools: null }),
  sdkEvent("subagent.completed", agentName, { model: "copilot-completions/claude-haiku-4.5", firstDispatchedModel: "claude-haiku-4.5" }),
  sdkEvent("subagent.completed", agentName, { model: "copilot-completions/claude-haiku-4.5", firstDispatchedModel: "claude-haiku-4.5", cancelled: true }),
  harnessOutcome(),
];
const sdkHaiku = (status = 200, endpoint = "/chat/completions") => usage("claude-haiku-4.5", endpoint, status);
const sdkRequests = (haiku = [sdkHaiku()]) => [classifier(), luna(200, "/responses", { x_initiator: "user" }), ...haiku, luna(), luna()];

const copilotRequests = () => [classifier(), luna(200, "/responses", { x_initiator: "user" }), luna(), luna(), luna()];

const copilotExpectations = { engine: "copilot", allowedModels: ROUTED_ALLOWED };
const piExpectations = {
  engine: "pi",
  allowedModels: ["gpt-5.6-luna"],
  subAgents: [
    { name: "haiku-whoami", model: "claude-haiku-4.5", endpoint: "/v1/messages" },
    { name: "mini-whoami", model: "gpt-5.4-mini", endpoint: "/responses" },
  ],
};
const sdkExpectations = {
  engine: "copilot",
  allowedModels: ["gpt-5.6-luna"],
  mainEndpoint: "/responses",
  subAgents: [{ name: "haiku-whoami", model: "claude-haiku-4.5", endpoint: "/chat/completions" }],
  requireDeclaredAgentNames: true,
};
const R3_ENDPOINTS = "(/responses, ws:/responses)";

describe("smoke_model_routing_assertions", () => {
  let root;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-smoke-routing-"));
  });
  afterEach(() => {
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  function write(file, entries) {
    const target = path.join(root, file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, typeof entries === "string" ? entries : file.endsWith(".json") ? JSON.stringify(entries) : entries.map(entry => JSON.stringify(entry)).join("\n") + "\n");
  }

  function fixture({ session, requests, withReflect = true }) {
    write("usage/aw_session.jsonl", session);
    if (requests) write(TOKEN_USAGE, requests);
    if (withReflect) write("sandbox/firewall/awf-reflect.json", reflect);
  }

  function check(expectations) {
    return checkModelRoutingEvidence({ rootDir: root, buildSession: false, ...expectations });
  }

  const checkIds = lines => lines.map(line => line.split(" ")[1]);
  const copilotPass = () => ({ session: [workflowInfo(), selection(), harnessOutcome()], requests: copilotRequests() });

  describe("normalization", () => {
    it.each([
      ["copilot/claude-haiku-4.5", "claude-haiku-4.5"],
      ["copilot-completions/claude-haiku-4.5", "claude-haiku-4.5"],
      ["github-copilot/gpt-5.6-luna", "gpt-5.6-luna"],
      ["claude-haiku-4-5-20251001", "claude-haiku-4.5"],
      ["gpt-5.4-mini-2026-03-17", "gpt-5.4-mini"],
      ["gpt-4o-mini-2024-07-18", "gpt-4o-mini"],
      ["gpt-5.6-luna", "gpt-5.6-luna"],
    ])("serves %s as %s", (input, expected) => {
      expect(servedModel(input)).toBe(expected);
    });

    it("drops endpoint query strings", () => {
      expect(normalizeEndpoint("/v1/messages?beta=true")).toBe("/v1/messages");
      expect(normalizeEndpoint("/responses/")).toBe("/responses");
    });
  });

  describe("passing variants (real record shapes)", () => {
    it("passes routed Copilot CLI evidence with agent requests that carry no purpose", () => {
      fixture(copilotPass());
      const results = check(copilotExpectations);
      expect(results.failures).toEqual([]);
      expect(checkIds(results.passes)).toEqual(["E2", "R1", "R2", "R3", "R4"]);
      expect(results.passes).toContain("PASS R4 all 4 main-agent request(s) used allowed models");
    });

    it("passes routed pi evidence and attributes dated sub-agent models and query-string endpoints by model", () => {
      fixture({ session: piSession(), requests: piRequests() });
      const results = check({ ...piExpectations, executionOutcome: "success" });
      expect(results.failures).toEqual([]);
      expect(checkIds(results.passes)).toEqual(["E2", "C1", "E1", "R1", "R2", "R3", "R4", "S1", "S2", "S3", "S1", "S2", "S3"]);
      expect(results.passes).toEqual(
        expect.arrayContaining(["PASS R4 all 5 main-agent request(s) used allowed models", "PASS S3 sub-agent haiku-whoami request claude-haiku-4.5 /v1/messages 200", "PASS S3 sub-agent mini-whoami request gpt-5.4-mini /responses 200"])
      );
    });

    it("passes routed Copilot SDK evidence, including a second cancelled completion", () => {
      fixture({ session: sdkSession(), requests: sdkRequests() });
      const results = check({ ...sdkExpectations, executionOutcome: "success" });
      expect(results.failures).toEqual([]);
      expect(checkIds(results.passes)).toEqual(["E2", "C1", "E1", "R1", "R2", "R3", "R4", "M1", "S1", "S2", "S3", "S4"]);
      expect(results.passes).toEqual(expect.arrayContaining(["PASS M1 all 3 main-agent request(s) used /responses", "PASS S3 sub-agent haiku-whoami request claude-haiku-4.5 /chat/completions 200"]));
    });

    it("falls back to the selection endpoint when /reflect has no metadata", () => {
      fixture({ ...copilotPass(), withReflect: false });
      expect(check(copilotExpectations).failures).toEqual([]);
    });
  });

  describe("configuration", () => {
    it("fails C1 when a declared sub-agent model is also a routing candidate", () => {
      fixture({ session: piSession(), requests: piRequests() });
      expect(check({ ...piExpectations, allowedModels: ROUTED_ALLOWED }).failures).toEqual([
        "FAIL C1 declared sub-agent model(s) also in allowed-models [gpt-5.4-mini, gpt-5.6-luna, claude-haiku-4.5]: haiku-whoami=claude-haiku-4.5, mini-whoami=gpt-5.4-mini; sub-agent models must be disjoint from the routing candidates",
      ]);
    });

    it("fails C1 when two declared sub-agents share a normalized model", () => {
      fixture({ session: piSession(), requests: piRequests() });
      const subAgents = [
        { name: "haiku-whoami", model: "claude-haiku-4.5", endpoint: "/v1/messages" },
        { name: "mini-whoami", model: "copilot/claude-haiku-4-5-20251001", endpoint: "/v1/messages" },
      ];
      const { failures } = check({ ...piExpectations, subAgents });
      expect(failures.filter(line => line.startsWith("FAIL C1 "))).toEqual(["FAIL C1 declared sub-agents share a model: claude-haiku-4.5 (haiku-whoami, mini-whoami); each sub-agent must declare a distinct model"]);
    });
  });

  describe("wrong model", () => {
    it("fails R1 when the selected model is outside allowed-models", () => {
      fixture({
        session: [workflowInfo("selected", "gpt-5.5"), selection("gpt-5.5")],
        requests: [classifier(), usage("gpt-5.5", "/responses")],
      });
      const { failures } = check(copilotExpectations);
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL R1 .*selected model gpt-5\.5 is outside allowed-models \[gpt-5\.4-mini, gpt-5\.6-luna, claude-haiku-4\.5\]/));
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL R4 .*gpt-5\.5 \/responses 200/));
    });

    it("fails R3 and R4 when main-agent traffic ran on a model other than the selected one", () => {
      fixture({ session: [workflowInfo(), selection()], requests: [classifier(), usage("gpt-5.5", "/responses")] });
      expect(check(copilotExpectations).failures).toEqual([
        `FAIL R3 no 200 request for gpt-5.6-luna on a supported endpoint ${R3_ENDPOINTS}; observed: no gpt-5.6-luna requests; main-agent requests: gpt-5.5 /responses 200`,
        "FAIL R4 main-agent request(s) on models outside allowed-models [gpt-5.4-mini, gpt-5.6-luna, claude-haiku-4.5]: gpt-5.5 /responses 200",
      ]);
    });

    it("fails R4 for a main-agent request on an out-of-policy model", () => {
      const evidence = copilotPass();
      evidence.requests.push(usage("gpt-5.5", "/responses"));
      fixture(evidence);
      expect(check(copilotExpectations).failures).toEqual(["FAIL R4 main-agent request(s) on models outside allowed-models [gpt-5.4-mini, gpt-5.6-luna, claude-haiku-4.5]: gpt-5.5 /responses 200"]);
    });

    it("fails S3 when a sub-agent runs on a model other than its declared model", () => {
      fixture({ session: piSession({ haikuModel: "gpt-5.4-mini-2026-03-17" }), requests: piRequests({ haiku: [piMini()] }) });
      const luna200 = "gpt-5.6-luna /responses 200";
      const mini200 = "gpt-5.4-mini /responses 200";
      expect(check(piExpectations).failures).toEqual([
        `FAIL S3 sub-agent haiku-whoami: no 200 request for claude-haiku-4.5 on /v1/messages; recorded model(s) [gpt-5.4-mini-2026-03-17] differ from declared claude-haiku-4.5; observed: no claude-haiku-4.5 requests; agent requests: ${[luna200, mini200, luna200, mini200, luna200, luna200, luna200].join(", ")}`,
      ]);
    });
  });

  describe("wrong endpoint", () => {
    it("fails R3 when the selected model only ran on an unsupported endpoint", () => {
      fixture({ session: [workflowInfo(), selection()], requests: [classifier(), luna(200, "/v1/messages")] });
      expect(check(copilotExpectations).failures).toEqual([`FAIL R3 no 200 request for gpt-5.6-luna on a supported endpoint ${R3_ENDPOINTS}; observed: gpt-5.6-luna /v1/messages 200`]);
    });

    it("fails M1 when a main-agent request is not on /responses", () => {
      fixture({ session: sdkSession(), requests: [...sdkRequests(), luna(200, "/chat/completions")] });
      expect(check(sdkExpectations).failures).toEqual(["FAIL M1 main-agent request(s) not on /responses; observed: gpt-5.6-luna /chat/completions 200"]);
    });

    it("fails S3 and names the sub-agent, model and endpoint when the SDK sub-agent only hit /responses with 400", () => {
      fixture({ session: sdkSession(), requests: sdkRequests([sdkHaiku(400, "/responses")]) });
      expect(check(sdkExpectations).failures).toEqual(["FAIL S3 sub-agent haiku-whoami: no 200 request for claude-haiku-4.5 on /chat/completions; observed: /responses 400"]);
    });

    it("fails S3 when the pi sub-agent used an endpoint other than its declared one", () => {
      fixture({ session: piSession(), requests: piRequests({ haiku: [piHaiku(200, "/chat/completions")] }) });
      expect(check(piExpectations).failures).toEqual(["FAIL S3 sub-agent haiku-whoami: no 200 request for claude-haiku-4.5 on /v1/messages; observed: /chat/completions 200"]);
    });
  });

  describe("non-200 status", () => {
    it("fails R3 when every selected-model request failed", () => {
      fixture({ session: [workflowInfo(), selection()], requests: [classifier(), luna(500), luna(429)] });
      expect(check(copilotExpectations).failures).toEqual([`FAIL R3 no 200 request for gpt-5.6-luna on a supported endpoint ${R3_ENDPOINTS}; observed: gpt-5.6-luna /responses 500, gpt-5.6-luna /responses 429`]);
    });

    it("fails S3 when the pi sub-agent request returned 400", () => {
      fixture({ session: piSession(), requests: piRequests({ haiku: [piHaiku(400)] }) });
      expect(check(piExpectations).failures).toEqual(["FAIL S3 sub-agent haiku-whoami: no 200 request for claude-haiku-4.5 on /v1/messages; observed: /v1/messages 400"]);
    });
  });

  describe("no agent requests", () => {
    it("fails R3 and R4 when only the classifier request was recorded", () => {
      fixture({ session: [workflowInfo(), selection()], requests: [classifier()] });
      expect(check(copilotExpectations).failures).toEqual([
        `FAIL R3 no 200 request for gpt-5.6-luna on a supported endpoint ${R3_ENDPOINTS}; observed: no gpt-5.6-luna requests; main-agent requests: none (agent requests: none)`,
        "FAIL R4 no main-agent requests to check against allowed-models [gpt-5.4-mini, gpt-5.6-luna, claude-haiku-4.5]; observed agent requests: none",
      ]);
    });

    it("fails R3, R4, M1 and S3 for the SDK smoke when only the classifier request was recorded", () => {
      fixture({ session: sdkSession(), requests: [classifier()] });
      const { failures } = check(sdkExpectations);
      expect(checkIds(failures)).toEqual(["R3", "R4", "M1", "S3"]);
      expect(failures).toContain("FAIL M1 no main-agent requests to check for /responses; observed agent requests: none");
      expect(failures).toContain("FAIL S3 sub-agent haiku-whoami: no 200 request for claude-haiku-4.5 on /chat/completions; observed: no claude-haiku-4.5 requests; agent requests: none");
    });

    it("fails R3 and R4 when all agent traffic is attributed to sub-agents", () => {
      fixture({ session: piSession(), requests: [classifier(), piHaiku(), piMini()] });
      const observed = "claude-haiku-4.5 /v1/messages 200, gpt-5.4-mini /responses 200";
      expect(check(piExpectations).failures).toEqual([
        `FAIL R3 no 200 request for gpt-5.6-luna on a supported endpoint ${R3_ENDPOINTS}; observed: no gpt-5.6-luna requests; main-agent requests: none (agent requests: ${observed})`,
        `FAIL R4 no main-agent requests to check against allowed-models [gpt-5.6-luna]; observed agent requests: ${observed}`,
      ]);
    });
  });

  describe("request correlation", () => {
    it("prefers lifecycle request IDs over model attribution when the session records them", () => {
      const correlated = usage("gpt-5.5", "/responses", 500);
      const session = sdkSession();
      session[2].data.requestId = correlated.request_id;
      fixture({ session, requests: [classifier(), luna(), correlated, sdkHaiku()] });
      const { failures } = check(sdkExpectations);
      expect(failures).toContain("FAIL R4 main-agent request(s) on models outside allowed-models [gpt-5.6-luna]: claude-haiku-4.5 /chat/completions 200");
      expect(failures).toContain("FAIL S3 sub-agent haiku-whoami: no 200 request for claude-haiku-4.5 on /chat/completions; observed: no claude-haiku-4.5 requests; requests correlated to sub-agent haiku-whoami: gpt-5.5 /responses 500");
    });

    it("attributes proxy records that carry the sub-agent ID and keeps other traffic on the main agent", () => {
      fixture({ session: sdkSession(), requests: [classifier(), luna(), usage("claude-haiku-4.5", "/chat/completions", 200, { agent_id: SDK_AGENT_ID }), sdkHaiku()] });
      const { passes, failures } = check({ ...sdkExpectations, mainEndpoint: undefined });
      expect(failures).toEqual(["FAIL R4 main-agent request(s) on models outside allowed-models [gpt-5.6-luna]: claude-haiku-4.5 /chat/completions 200"]);
      expect(passes).toContain("PASS S3 sub-agent haiku-whoami request claude-haiku-4.5 /chat/completions 200");
    });
  });

  describe("sub-agent lifecycle", () => {
    it("fails S2 when a sub-agent failed", () => {
      fixture({ session: piSession({ miniFailed: true }), requests: piRequests() });
      expect(check(piExpectations).failures).toEqual([expect.stringMatching(/^FAIL S2 sub-agent mini-whoami: completed=0 failed=1; observed events: .*subagent\.failed\(agentName=mini-whoami outcome=failed\)/)]);
    });

    it("fails S1 and S2 when a sub-agent never ran", () => {
      fixture({ session: piSession().filter(event => event.agentId !== MINI_ID), requests: piRequests() });
      const { failures } = check(piExpectations);
      expect(failures).toContainEqual("FAIL S1 sub-agent mini-whoami: expected exactly 1 subagent.started, observed 0; observed agent names: [haiku-whoami]");
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL S2 sub-agent mini-whoami: never started/));
    });

    it("fails S1 when a sub-agent ran twice", () => {
      const session = [...sdkSession(), sdkEvent("subagent.started", "haiku-whoami", { model: "claude-haiku-4.5" }, "agent-2"), sdkEvent("subagent.completed", "haiku-whoami", {}, "agent-2")];
      fixture({ session, requests: sdkRequests() });
      expect(check(sdkExpectations).failures).toEqual(["FAIL S1 sub-agent haiku-whoami: expected exactly 1 subagent.started, observed 2; observed agent names: [haiku-whoami]"]);
    });

    it("fails S4 when SDK events carry a per-call display name instead of the declared agent name", () => {
      fixture({ session: sdkSession("Haiku-whoami call 1"), requests: sdkRequests() });
      const { failures } = check(sdkExpectations);
      expect(failures).toContainEqual("FAIL S4 sub-agent haiku-whoami: no events carry the declared agent name; observed agent names: [Haiku-whoami call 1]");
      expect(failures).toContainEqual("FAIL S4 sub-agent events carry undeclared agent name(s); declared: [haiku-whoami]; observed: subagent.started(agentName=Haiku-whoami call 1 model=copilot-completions/claude-haiku-4.5)");
    });
  });

  describe("routing evidence", () => {
    it("fails R1 and R3 when the firewall selection event is missing", () => {
      fixture({ session: [workflowInfo()], requests: copilotRequests() });
      const { failures } = check(copilotExpectations);
      expect(failures).toContain("FAIL R1 routing not selected: no firewall model_routing selection record with a selected model (observed 0 selection record(s))");
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL R3 no selected model to verify; observed main-agent requests: gpt-5\.6-luna \/responses 200/));
    });

    it("fails R1 when the runner-written routing event is missing", () => {
      fixture({ session: [selection()], requests: copilotRequests() });
      expect(check(copilotExpectations).failures).toEqual(["FAIL R1 routing not selected: runner-written routing status is missing, expected selected"]);
    });

    it("fails R1 when only the agent-written routing outcome reports selection", () => {
      const forgedSelection = { ...selection(), provenance: { component: "agent", phase: "agent", path: "agent/model-routing.jsonl", index: 0 } };
      const agentInfo = { ...workflowInfo(), provenance: { component: "workflow", phase: "agent", path: "agent/aw_info.json", index: 0 } };
      fixture({ session: [agentInfo, forgedSelection, harnessOutcome("selected")], requests: copilotRequests() });
      const { failures } = check(copilotExpectations);
      expect(failures).toContain(
        "FAIL R1 routing not selected: runner-written routing status is missing, expected selected; no firewall model_routing selection record with a selected model (observed 0 selection record(s)); ignored agent-written model_routing.outcome (status=selected)"
      );
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL R3 no selected model to verify/));
    });

    it("fails R1 when the runner reports a routing failure", () => {
      fixture({ session: [workflowInfo("failed"), selection()], requests: copilotRequests() });
      expect(check(copilotExpectations).failures).toEqual(["FAIL R1 routing not selected: runner-written routing status is failed, expected selected"]);
    });

    it.each([
      [0, () => []],
      [2, () => [classifier(), classifier("gpt-5.4-mini")]],
    ])("fails R2 when the classifier ran %i times", (count, classifiers) => {
      fixture({ session: [workflowInfo(), selection()], requests: [...classifiers(), luna()] });
      const { failures } = check(copilotExpectations);
      expect(failures).toHaveLength(1);
      expect(failures[0]).toMatch(new RegExp(`^FAIL R2 expected exactly 1 routing_classification request, observed ${count}`));
    });

    it("ignores the classifier request when checking out-of-policy models", () => {
      fixture({ session: [workflowInfo(), selection()], requests: [classifier("gpt-5-mini"), luna()] });
      expect(check(copilotExpectations).failures).toEqual([]);
    });
  });

  describe("evidence files", () => {
    it("fails E2 when token-usage.jsonl is missing", () => {
      fixture({ session: [workflowInfo(), selection()] });
      const { failures } = check(copilotExpectations);
      expect(failures[0]).toMatch(/^FAIL E2 evidence: missing api-proxy token-usage\.jsonl/);
      expect(failures).toContain("FAIL R2 expected exactly 1 routing_classification request, observed 0: none");
    });

    it("fails E2 when the unified session is missing or broken", () => {
      write(TOKEN_USAGE, copilotRequests());
      expect(check(copilotExpectations).failures[0]).toBe("FAIL E2 evidence: missing usage/aw_session.jsonl");
      write("usage/aw_session.jsonl", "{not json\n");
      expect(check(copilotExpectations).failures[0]).toMatch(/^FAIL E2 evidence: .*aw_session\.jsonl:1 is not valid JSON/);
    });

    it("fails E1 when agent execution did not succeed", () => {
      fixture(copilotPass());
      expect(check({ ...copilotExpectations, executionOutcome: "failure" }).failures).toEqual(["FAIL E1 agent execution outcome is failure, expected success"]);
    });

    it("builds the unified session from runner and proxy sources and ignores the harness outcome", () => {
      write("aw_info.json", { engine_id: "copilot", model_routing: { status: "selected", source: "awf-routing", wire_model: "gpt-5.6-luna" } });
      write("sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl", [{ _schema: "model-routing/v0.28.50", event: "model_routing", stage: "selection", selected_model: "github-copilot/gpt-5.6-luna", endpoint: "/responses" }]);
      write(TOKEN_USAGE, copilotRequests());
      expect(checkModelRoutingEvidence({ rootDir: root, engine: "copilot", allowedModels: ROUTED_ALLOWED }).failures).toEqual([]);
      expect(fs.existsSync(path.join(root, "usage/aw_session.jsonl"))).toBe(true);

      fs.rmSync(path.join(root, "aw_info.json"));
      fs.rmSync(path.join(root, "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl"));
      write("agent/awf-routing-outcome.json", { status: "selected", wire_model: "gpt-5.6-luna" });
      const { failures } = checkModelRoutingEvidence({ rootDir: root, engine: "copilot", allowedModels: ROUTED_ALLOWED });
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL R1 routing not selected: runner-written routing status is missing.*ignored agent-written model_routing\.outcome \(status=selected\)$/));
    });
  });

  // Claude engine: real records from pkg/cli/testdata/model_routing_golden/claude-awf-selected-messages
  // (run 37971713032). AWF selects /chat/completions; Claude Code calls /v1/messages.
  describe("claude engine (real record shapes)", () => {
    const goldenDir = fileURLToPath(new URL("../../../pkg/cli/testdata/model_routing_golden/claude-awf-selected-messages/", import.meta.url));
    const claudeExpectations = { engine: "claude", allowedModels: ["claude-sonnet-5"], mainEndpoint: "/v1/messages", executionOutcome: "success" };
    // /reflect routing_models from sandbox run 38003537540 (sandbox/firewall/awf-reflect.json).
    const claudeReflect = {
      endpoints: [
        {
          provider: "copilot",
          routing_models: [
            { model_id: "claude-haiku-4.5", supported_endpoints: ["/chat/completions", "/v1/messages"] },
            { model_id: "claude-sonnet-5", supported_endpoints: ["/v1/messages", "/chat/completions"] },
            { model_id: "claude-opus-5", supported_endpoints: ["/v1/messages", "/chat/completions"] },
          ],
        },
      ],
    };
    const ROUTING = "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl";
    const golden = file => fs.readFileSync(path.join(goldenDir, file), "utf8");
    // The golden capture dropped `event`; AWF v0.28.50 writes `"event":"model_routing"`
    // on every record (see the unredacted copilot-routed-gpt-responses golden).
    const routingRecords = () =>
      golden(ROUTING)
        .split("\n")
        .filter(Boolean)
        .map(line => ({ event: "model_routing", ...JSON.parse(line) }));

    function claudeFixture({ awInfo = JSON.parse(golden("aw_info.json")), tokenUsage = golden(TOKEN_USAGE) } = {}) {
      write("aw_info.json", awInfo);
      write(ROUTING, routingRecords());
      write(TOKEN_USAGE, tokenUsage);
      write("agent/awf-routing-outcome.json", golden("agent/awf-routing-outcome.json"));
      write("sandbox/firewall/awf-reflect.json", claudeReflect);
    }

    const claudeCheck = () => checkModelRoutingEvidence({ rootDir: root, ...claudeExpectations });

    it("passes every check for a routed Claude run on /v1/messages", () => {
      claudeFixture();
      const results = claudeCheck();
      expect(results.failures).toEqual([]);
      expect(results.passes).toEqual([
        "PASS E2 evidence: unified session and api-proxy token usage present",
        "PASS E1 agent execution outcome is success",
        "PASS R1 routing selected claude-sonnet-5 (runner status=selected, endpoint=/chat/completions)",
        "PASS R2 one routing_classification request: claude-opus-5 /chat/completions 200",
        "PASS R3 main-agent request on selected model: claude-sonnet-5 /v1/messages 200",
        "PASS R4 all 3 main-agent request(s) used allowed models",
        "PASS M1 all 3 main-agent request(s) used /v1/messages",
      ]);
    });

    it("fails only M1 when main-agent traffic ran on /chat/completions", () => {
      claudeFixture({ tokenUsage: golden(TOKEN_USAGE).replaceAll('"path":"/v1/messages?beta=true"', '"path":"/chat/completions"') });
      const { passes, failures } = claudeCheck();
      expect(failures).toEqual(["FAIL M1 main-agent request(s) not on /v1/messages; observed: claude-sonnet-5 /chat/completions 200, claude-sonnet-5 /chat/completions 200, claude-sonnet-5 /chat/completions 200"]);
      expect(passes).toContain("PASS R3 main-agent request on selected model: claude-sonnet-5 /chat/completions 200");
    });

    it("fails R1 when the runner records the #67012 rejected shape and the agent-written outcome says selected", () => {
      const awInfo = JSON.parse(golden("aw_info.json"));
      awInfo.model = "agent";
      awInfo.model_routing = { status: "rejected", failure_code: "unsupported_endpoint", detail: "AWF model routing endpoint /chat/completions is not supported by this engine" };
      claudeFixture({ awInfo });
      expect(claudeCheck().failures).toEqual(["FAIL R1 routing not selected: runner-written routing status is rejected, expected selected; ignored agent-written model_routing.outcome (status=selected)"]);
    });
  });

  // Routed Codex: raw records from pkg/cli/testdata/model_routing_golden/copilot-routed-gpt-responses
  // (run 37886511291, gpt-5.6-luna on /responses). Derived fixture: the selection and request
  // records' raw `selected_effort` / `requested_effort` are changed from high to the effort under
  // test, aw_info.json names the codex engine, and the harness outcome records `effort` /
  // `applied_effort` as codex_harness.cjs writes them. No recorded routed Codex run exists yet.
  describe("routed codex smoke (derived from copilot-routed-gpt-responses)", () => {
    const goldenDir = fileURLToPath(new URL("../../../pkg/cli/testdata/model_routing_golden/copilot-routed-gpt-responses/", import.meta.url));
    const ROUTING = "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl";
    const golden = file => fs.readFileSync(path.join(goldenDir, file), "utf8");
    const GPT_56 = ["gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.6-sol"];
    const codexExpectations = { engine: "codex", allowedModels: GPT_56, mainEndpoint: "/responses", executionOutcome: "success", expectAppliedEffort: true, expectSelectedEffort: "max" };
    const lunaReflect = {
      endpoints: [{ provider: "copilot", routing_models: [{ model_id: "gpt-5.6-luna", supported_endpoints: ["/responses", "ws:/responses"], supported_reasoning_efforts: ["none", "low", "medium", "high", "xhigh", "max"] }] }],
    };

    function codexFixture({ selectedEffort = "max", outcome = { status: "selected", wire_model: "gpt-5.6-luna", endpoint: "/responses", selected_endpoint: "/responses", effort: selectedEffort, applied_effort: selectedEffort } } = {}) {
      const awInfo = JSON.parse(golden("aw_info.json"));
      awInfo.engine_id = "codex";
      awInfo.model_routing = { ...awInfo.model_routing, effort: selectedEffort };
      write("aw_info.json", awInfo);
      const records = golden(ROUTING)
        .split("\n")
        .filter(Boolean)
        .map(line => JSON.parse(line))
        .map(record => (record.stage === "selection" || record.stage === "request" ? { ...record, selected_effort: selectedEffort, ...(record.stage === "request" ? { requested_effort: selectedEffort } : {}) } : record));
      write(ROUTING, records);
      write(TOKEN_USAGE, golden(TOKEN_USAGE));
      write("sandbox/firewall/awf-reflect.json", lunaReflect);
      if (outcome) write("agent/awf-routing-outcome.json", outcome);
    }

    const codexCheck = (expectations = {}) => checkModelRoutingEvidence({ rootDir: root, ...codexExpectations, ...expectations });

    it.each(["max", "none"])("routed_codex_smoke_records_selected_effort_as_applied (%s)", effort => {
      codexFixture({ selectedEffort: effort });
      const results = codexCheck({ expectSelectedEffort: effort });
      expect(results.failures).toEqual([]);
      expect(checkIds(results.passes)).toEqual(["E2", "E1", "R1", "R2", "R3", "R4", "M1", "H1", "H2"]);
      expect(results.passes).toContain(`PASS H2 proxy-selected effort ${effort} is the covered effort`);
      expect(results.passes).toContain(`PASS H1 harness-written outcome applied_effort=${effort} matches the proxy-selected effort for gpt-5.6-luna (harness record, not upstream wire evidence)`);
      expect(results.passes).toContain("PASS R3 main-agent request on selected model: gpt-5.6-luna /responses 200");
    });

    it("routed_codex_smoke_records_selected_effort_as_applied: the workflow configures routed Codex on GitHub with gpt-5.6 candidates and H1", () => {
      const workflow = fs.readFileSync(new URL("../../../.github/workflows/smoke-codex-routed.md", import.meta.url), "utf8");
      const engine = workflow
        .split("\nengine:\n")[1]
        .split("\n")
        .filter(line => line.startsWith("  "))
        .join("\n");
      expect(engine).toContain("  id: codex");
      expect(engine).toContain("  model-provider: github");
      expect(engine).toContain("    mode: robust");
      expect(engine).toMatch(/\n {2}model-routing:\n(?: {4}.*\n)* {4}allowed-models: \[gpt-5\.6-luna, gpt-5\.6-terra, gpt-5\.6-sol\]/);
      expect(workflow).toContain('engine: "codex"');
      expect(workflow).toContain('allowedModels: ["gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.6-sol"]');
      expect(workflow).toContain('mainEndpoint: "/responses"');
      expect(workflow).toContain("expectAppliedEffort: true");
      expect(workflow).toContain('expectSelectedEffort: "max"');
    });

    it("fails only H2 when the proxy selected high instead of the covered max", () => {
      codexFixture({ selectedEffort: "high" });
      expect(codexCheck().failures).toEqual(["FAIL H2 proxy-selected effort high, expected max: this run does not cover the max path"]);
    });

    it("fails only H1 when the harness applied a different effort than the proxy selected", () => {
      codexFixture({ outcome: { status: "selected", wire_model: "gpt-5.6-luna", endpoint: "/responses", effort: "max", applied_effort: "xhigh" } });
      expect(codexCheck().failures).toEqual(["FAIL H1 harness outcome applied_effort xhigh differs from the proxy-selected effort max"]);
    });

    it("fails H1 for the pre-fix rejected harness outcome", () => {
      codexFixture({ outcome: { status: "rejected", failure_code: "unsupported_effort", detail: 'AWF model routing effort "max" is not supported by the codex engine' } });
      expect(codexCheck().failures).toEqual([
        "FAIL H1 harness outcome status is rejected (unsupported_effort), expected selected; harness outcome wire_model missing differs from the proxy-selected model gpt-5.6-luna; harness outcome applied_effort null differs from the proxy-selected effort max",
      ]);
    });

    it("fails H1, not E2, when the harness outcome is missing", () => {
      codexFixture({ outcome: null });
      const results = codexCheck();
      expect(results.failures).toEqual(["FAIL H1 missing harness-written agent/awf-routing-outcome.json"]);
      expect(checkIds(results.passes)).toContain("E2");
    });

    it("does not read the harness outcome or add H1 unless expectAppliedEffort is set", () => {
      codexFixture({ outcome: null });
      const results = codexCheck({ expectAppliedEffort: undefined, expectSelectedEffort: undefined });
      expect(results.failures).toEqual([]);
      expect(checkIds(results.passes)).not.toContain("H1");
      expect(checkIds(results.passes)).not.toContain("H2");
    });
  });

  // Steering: redacted copies of the agent-phase records from sandbox run
  // 38174216426 (AWF v0.28.51, gpt-5.6-sol, max-ai-credits 30). The totals
  // before each notice (24.0052, 27.00336, 29.16304) and the final total
  // (30.1812) are the run's; per-response values between them are redacted.
  // Request IDs and token counts are replaced.
  describe("AI-credit steering (run 38174216426 record shapes)", () => {
    const SOL = "gpt-5.6-sol";
    const ROUTING_LOG = "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl";
    // [ai_credits_this_response, ai_credits_total, steering]
    const RUN_CREDITS = [
      [0.06512, 0.06512],
      [6.81204, 6.87716],
      [4.10377, 10.98093],
      [3.88214, 14.86307],
      [4.95021, 19.81328],
      [4.19192, 24.0052],
      [1.40917, 25.41437, { type: "ai_credit", threshold: 80 }],
      [1.58899, 27.00336],
      [0.93158, 27.93494, { type: "ai_credit", threshold: 90 }],
      [0.71262, 28.64756],
      [0.51548, 29.16304],
      [1.01816, 30.1812, { type: "ai_credit", threshold: 95 }],
    ];
    const tokenRecord = ([thisResponse, total, steering], index) => ({
      _schema: "token-usage/v0.28.51",
      timestamp: `2026-10-12T09:${String(10 + index).padStart(2, "0")}:00.000Z`,
      event: "token_usage",
      request_id: `run-38174216426-req-${index + 1}`,
      ...(index === 0 ? { purpose: "routing_classification" } : {}),
      provider: "copilot",
      model: SOL,
      path: "/responses",
      status: 200,
      streaming: index !== 0,
      x_initiator: index === 1 ? "user" : "agent",
      input_tokens: 1000,
      output_tokens: 100,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
      ai_credits_this_response: thisResponse,
      ai_credits_total: total,
      ...(steering ? { steering } : {}),
    });
    const sessionUsage = (record, index) => ({
      type: "firewall.token_usage",
      timestamp: record.timestamp,
      data: {
        provider: record.provider,
        model: record.model,
        ...(record.purpose ? { purpose: record.purpose } : {}),
        path: record.path,
        requestId: record.request_id,
        status: record.status,
        aic: record.ai_credits_this_response,
        totalAic: record.ai_credits_total,
        ...(record.steering ? { steering: record.steering } : {}),
      },
      provenance: { component: "firewall", phase: "agent", path: TOKEN_USAGE, index },
    });
    const budgetFailureRecord = { _schema: "model-routing/v0.28.51", timestamp: "2026-10-12T09:22:00.000Z", event: "model_routing", stage: "failure", phase: "primary", code: "ai_credits_limit_exceeded" };
    const budgetFailureEvent = {
      type: "firewall.model_routing",
      data: { schema: "model-routing/v0.28.51", stage: "failure", code: "ai_credits_limit_exceeded" },
      provenance: { component: "firewall", phase: "agent", path: ROUTING_LOG, index: 3 },
    };
    const copilotSubagent = (agentName, toolCallId) => ({
      type: "subagent.started",
      agentId: toolCallId,
      data: { toolCallId, agentName, agentDisplayName: agentName },
      provenance: { component: "agent", phase: "agent", path: "sandbox/agent/logs/copilot-session-state/events.jsonl", index: 0 },
    });

    /** Write steering evidence; `credits` defaults to the run's first 11 records (the agent stopped before 95%). */
    function steeringFixture({ credits = RUN_CREDITS.slice(0, 11), status = "selected", subagents = [], failure = false, sessionRecords } = {}) {
      const requests = credits.map(tokenRecord);
      const session = [workflowInfo(status, SOL), selection(SOL), ...(sessionRecords ?? requests).map(sessionUsage), ...subagents, ...(failure ? [budgetFailureEvent] : [])];
      write("usage/aw_session.jsonl", session);
      write(TOKEN_USAGE, requests);
      write(ROUTING_LOG, [{ _schema: "model-routing/v0.28.51", event: "model_routing", stage: "selection", selected_model: `github-copilot/${SOL}`, wire_model: SOL, endpoint: "/responses" }, ...(failure ? [budgetFailureRecord] : [])]);
      return requests;
    }
    const steeringCheck = (overrides = {}) => check({ engine: "copilot", allowedModels: [SOL], executionOutcome: "success", steering: { maxAiCredits: 30 }, ...overrides });
    const lineFor = (lines, id) => lines.find(line => line.split(" ")[1] === id);

    it("passes T1–T4 when the main agent received the 80% notice and stopped under budget", () => {
      steeringFixture();
      const { passes, failures } = steeringCheck();
      expect(failures).toEqual([]);
      expect(checkIds(passes)).toEqual(["E2", "E1", "R1", "R2", "R3", "R4", "T1", "T2", "T3", "T4"]);
      expect(lineFor(passes, "T1")).toBe("PASS T1 main agent received ai_credit@80 request=run-38174216426-req-7 status=200 total_before=24.0052 total_after=25.41437; session event matches; no sub-agents");
      expect(lineFor(passes, "T3")).toBe("PASS T3 final ai_credits_total 29.16304 is below the budget 30; no budget rejection; agent execution succeeded");
      expect(lineFor(passes, "T4")).toBe("PASS T4 all 6 existing check(s) passed");
    });

    it("reports the real run's failures: sub-agent notices, budget overshoot and rejection", () => {
      const subagents = [copilotSubagent("task", "call_task"), copilotSubagent("research", "call_research")];
      steeringFixture({ credits: RUN_CREDITS, status: "rejected", subagents, failure: true });
      const { passes, failures } = steeringCheck({ executionOutcome: "failure" });
      expect(checkIds(failures)).toEqual(["E1", "R1", "T1", "T3", "T4"]);
      expect(lineFor(failures, "T1")).toMatch(
        /^FAIL T1 2 subagent\.started event\(s\), so a notice may have gone to a sub-agent: subagent\.started\(agentName=task\), subagent\.started\(agentName=research\); observed token-usage notices: ai_credit@80 /
      );
      expect(lineFor(passes, "T2")).toMatch(/^PASS T2 3 notice\(s\), each once and at or above its threshold of 30: /);
      expect(lineFor(failures, "T3")).toBe(
        "FAIL T3 final ai_credits_total 30.1812 is at or above the budget 30; 1 ai_credits_limit_exceeded failure record(s) in model-routing.jsonl; 1 ai_credits_limit_exceeded firewall.model_routing failure event(s) in the session; agent execution outcome is failure, expected success"
      );
      expect(lineFor(failures, "T4")).toBe("FAIL T4 existing check(s) failed: E1, R1");
    });

    it("fails T1 and T2 when no record carries steering", () => {
      steeringFixture({ credits: RUN_CREDITS.slice(0, 11).map(([thisResponse, total]) => [thisResponse, total]) });
      const { failures } = steeringCheck();
      expect(checkIds(failures)).toEqual(["T1", "T2"]);
      expect(lineFor(failures, "T1")).toBe("FAIL T1 no ai_credit@80 notice in token-usage.jsonl; observed token-usage notices: none; observed session notices: none");
      expect(lineFor(failures, "T2")).toBe("FAIL T2 no steering notices to check; observed notices: none");
    });

    it("fails T1 when the session event's request ID differs from the token-usage record", () => {
      const requests = RUN_CREDITS.slice(0, 11).map(tokenRecord);
      requests[6] = { ...requests[6], request_id: "other-request" };
      steeringFixture({ sessionRecords: requests });
      const { failures } = steeringCheck();
      expect(checkIds(failures)).toEqual(["T1"]);
      expect(failures[0]).toMatch(
        /^FAIL T1 no session firewall\.token_usage event for notice ai_credit@80 request=run-38174216426-req-7; session notice\(s\) with no matching token-usage\.jsonl record: ai_credit@80 request=other-request; observed token-usage notices: /
      );
    });

    it("fails T1 when the session event carries a different notice", () => {
      const requests = RUN_CREDITS.slice(0, 11).map(tokenRecord);
      requests[6] = { ...requests[6], steering: { type: "ai_credit", threshold: 90 } };
      steeringFixture({ sessionRecords: requests });
      expect(lineFor(steeringCheck().failures, "T1")).toMatch(/^FAIL T1 session event for request=run-38174216426-req-7 carries steering ai_credit@90, expected ai_credit@80;/);
    });

    it("fails T1 when a session notice shares a request ID but has different steering", () => {
      const requests = RUN_CREDITS.slice(0, 11).map(tokenRecord);
      steeringFixture({
        sessionRecords: [...requests, { ...requests[6], steering: { type: "ai_credit", threshold: 90 } }],
      });
      expect(lineFor(steeringCheck().failures, "T1")).toMatch(/^FAIL T1 session notice\(s\) with no matching token-usage\.jsonl record: ai_credit@90 request=run-38174216426-req-7; observed token-usage notices: /);
    });

    it("fails T1 when the 80% notice was only on a rejected request", () => {
      const credits = RUN_CREDITS.slice(0, 11);
      steeringFixture({ credits });
      const requests = credits.map(tokenRecord);
      requests[6] = { ...requests[6], status: 403 };
      write(TOKEN_USAGE, requests);
      expect(lineFor(steeringCheck().failures, "T1")).toMatch(/^FAIL T1 ai_credit@80 notice only on non-200 response\(s\), so it never reached the model;/);
    });

    it("fails T2 when a threshold repeats", () => {
      const credits = RUN_CREDITS.slice(0, 11).map(entry => [...entry]);
      credits[10][2] = { type: "ai_credit", threshold: 80 };
      steeringFixture({ credits });
      const { failures } = steeringCheck();
      expect(checkIds(failures)).toEqual(["T2"]);
      expect(failures[0]).toMatch(/^FAIL T2 repeated notice\(s\): ai_credit@80 x2; observed notices: /);
    });

    it("passes T2 when the 90% notice arrives before the 80% notice", () => {
      // One response crossed both 80 and 90: AWF sends the higher threshold first.
      const credits = RUN_CREDITS.slice(0, 11).map(([thisResponse, total]) => [thisResponse, total]);
      credits[8][2] = { type: "ai_credit", threshold: 90 };
      credits[9][2] = { type: "ai_credit", threshold: 80 };
      steeringFixture({ credits });
      const { passes, failures } = steeringCheck();
      expect(failures).toEqual([]);
      expect(lineFor(passes, "T2")).toBe(
        "PASS T2 2 notice(s), each once and at or above its threshold of 30: ai_credit@90 request=run-38174216426-req-9 status=200 total_before=27.00336 total_after=27.93494, ai_credit@80 request=run-38174216426-req-10 status=200 total_before=27.93494 total_after=28.64756"
      );
    });

    it("fails T2 for an early notice and an unknown threshold", () => {
      const credits = RUN_CREDITS.slice(0, 11).map(entry => [...entry]);
      credits[5][2] = { type: "ai_credit", threshold: 85 };
      credits[8][2] = { type: "ai_credit", threshold: 95 };
      steeringFixture({ credits });
      const t2 = lineFor(steeringCheck().failures, "T2");
      expect(t2).toContain("threshold(s) outside [80, 90, 95, 99]: ai_credit@85");
      expect(t2).toContain("early notice ai_credit@95 request=run-38174216426-req-9 status=200 total_before=27.00336 total_after=27.93494: total before the request is below 95% of 30 (28.5)");
    });

    it("fails T3 when the final total reaches the budget, as in the real run (30.1812 of 30)", () => {
      steeringFixture({ credits: RUN_CREDITS });
      const { failures } = steeringCheck();
      expect(failures).toEqual(["FAIL T3 final ai_credits_total 30.1812 is at or above the budget 30"]);
    });

    it("fails T3 when an ai_credits_limit_exceeded failure is recorded", () => {
      steeringFixture({ failure: true });
      expect(steeringCheck().failures).toEqual(["FAIL T3 1 ai_credits_limit_exceeded failure record(s) in model-routing.jsonl; 1 ai_credits_limit_exceeded firewall.model_routing failure event(s) in the session"]);
    });

    it("fails T2 and T3 when the budget is not a positive integer", () => {
      steeringFixture();
      const { failures } = steeringCheck({ steering: { maxAiCredits: -1 } });
      expect(checkIds(failures)).toEqual(["T2", "T3"]);
      expect(lineFor(failures, "T3")).toBe("FAIL T3 steering.maxAiCredits must be an integer of 1 or more, got -1");
    });

    it("does not read model-routing.jsonl or add T checks when steering is not configured", () => {
      steeringFixture({ failure: true });
      write(ROUTING_LOG, "{not json\n");
      const { passes, failures } = check({ engine: "copilot", allowedModels: [SOL] });
      expect(failures).toEqual([]);
      expect(checkIds(passes)).toEqual(["E2", "R1", "R2", "R3", "R4"]);
    });
  });

  // Copilot CLI routed to Claude: real records from
  // pkg/cli/testdata/model_routing_golden/legacy-no-session-routing (run 37505153056,
  // Copilot CLI 1.0.90, mode auto, claude-opus-5:xhigh selected). That run predates
  // runner-written routing status, so R1's status is supplied in aw_info.json the way
  // the "builds the unified session" case does. Main-agent paths are set explicitly
  // per case: /chat/completions is what routed Copilot CLI uses for Claude.
  describe("copilot cli routed to claude (real record shapes)", () => {
    const goldenDir = fileURLToPath(new URL("../../../pkg/cli/testdata/model_routing_golden/legacy-no-session-routing/", import.meta.url));
    const ROUTING = "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl";
    const golden = file => fs.readFileSync(path.join(goldenDir, file), "utf8");
    const jsonl = file =>
      golden(file)
        .split("\n")
        .filter(Boolean)
        .map(line => JSON.parse(line));
    // supported_endpoints from sandbox run 38003537540; supported_reasoning_efforts
    // from the /reflect routing_models shape in awf_model_routing.test.cjs.
    const opusReflect = (efforts = ["low", "medium", "high", "xhigh", "max"]) => ({
      endpoints: [{ provider: "copilot", routing_models: [{ model_id: "claude-opus-5", source: "provider", supported_endpoints: ["/v1/messages", "/chat/completions"], supported_reasoning_efforts: efforts }] }],
    });
    const cliExpectations = { engine: "copilot", allowedModels: ["claude-opus-5"], mainEndpoint: "/chat/completions", routingMode: "auto", executionOutcome: "success" };

    function cliFixture({ mainPath = "/chat/completions", reflectData = opusReflect() } = {}) {
      write("aw_info.json", { ...JSON.parse(golden("aw_info.json")), model_routing: { status: "selected", source: "awf-routing", wire_model: "claude-opus-5" } });
      // The golden capture dropped `event`; AWF writes `"event":"model_routing"` on every record.
      write(
        ROUTING,
        jsonl(ROUTING).map(record => ({ event: "model_routing", ...record }))
      );
      write(
        TOKEN_USAGE,
        jsonl(TOKEN_USAGE).map(record => (record.purpose === "routing_classification" ? record : { ...record, path: mainPath }))
      );
      write("sandbox/firewall/awf-reflect.json", reflectData);
    }

    const cliCheck = (overrides = {}) => checkModelRoutingEvidence({ rootDir: root, ...cliExpectations, ...overrides });

    it("passes every check, including P1, for Claude main traffic on /chat/completions", () => {
      cliFixture();
      const { passes, failures, info } = cliCheck();
      expect(failures).toEqual([]);
      expect(checkIds(passes)).toEqual(["E2", "E1", "R1", "R2", "R3", "R4", "M1", "P1"]);
      expect(passes).toContain("PASS R3 main-agent request on selected model: claude-opus-5 /chat/completions 200");
      expect(passes).toContain("PASS P1 requested mode auto; recommended profile balanced and selected effort xhigh are valid for claude-opus-5");
      expect(info).toEqual(["INFO routing recommended profile=balanced requested mode=auto selected effort=xhigh"]);
    });

    it("fails only R3 and M1 when Claude main-agent traffic ran on /responses", () => {
      cliFixture({ mainPath: "/responses" });
      const { failures } = cliCheck();
      expect(checkIds(failures)).toEqual(["R3", "M1"]);
      expect(failures[0]).toMatch(/^FAIL R3 no 200 request for claude-opus-5 on a supported endpoint \(\/v1\/messages, \/chat\/completions\); observed: claude-opus-5 \/responses 200, /);
      expect(failures[1]).toMatch(/^FAIL M1 main-agent request\(s\) not on \/chat\/completions; observed: claude-opus-5 \/responses 200, .*\(101 total\)$/);
    });

    it("fails P1 when the requested mode differs from the configured mode", () => {
      cliFixture();
      expect(cliCheck({ routingMode: "robust" }).failures).toEqual(["FAIL P1 requested mode (objective.mode) is auto, expected robust"]);
    });

    it("fails P1 when the selected effort is not a /reflect supported effort", () => {
      cliFixture({ reflectData: opusReflect(["low", "medium", "high"]) });
      expect(cliCheck().failures).toEqual(["FAIL P1 selected effort xhigh is not among claude-opus-5's /reflect supported efforts [low, medium, high]"]);
    });

    it("fails P1 when /reflect lists no efforts but an effort was selected", () => {
      cliFixture({ reflectData: opusReflect([]) });
      expect(cliCheck().failures).toEqual(["FAIL P1 selected effort is xhigh, expected null because /reflect lists no supported efforts for claude-opus-5"]);
    });

    it("fails P1 when /reflect has no effort metadata for the selected model", () => {
      cliFixture({ reflectData: { endpoints: [{ provider: "copilot", routing_models: [{ model_id: "claude-opus-5", supported_endpoints: ["/v1/messages", "/chat/completions"] }] }] } });
      expect(cliCheck().failures).toEqual(["FAIL P1 no /reflect supported_reasoning_efforts for claude-opus-5 to check selected effort xhigh against"]);
    });

    it("fails P1 when the recommended profile is not economy, balanced or robust", () => {
      cliFixture();
      write(
        ROUTING,
        jsonl(ROUTING).map(record => ({ event: "model_routing", ...record, ...(record.stage === "selection" ? { mode: "auto" } : {}) }))
      );
      expect(cliCheck().failures).toEqual(["FAIL P1 recommended profile (mode) is auto, expected one of economy, balanced, robust"]);
    });

    it("explains degraded classification in P1", () => {
      cliFixture();
      write(ROUTING, [
        ...jsonl(ROUTING).map(record => ({
          event: "model_routing",
          ...record,
          ...(record.stage === "selection" ? { degraded_classification: true, degraded_reason: "invalid_classifier_output", mode: null } : {}),
        })),
        { event: "model_routing", stage: "classification", classifier_model: "github-copilot/claude-opus-5" },
      ]);
      expect(cliCheck().failures).toEqual([
        "FAIL P1 classification degraded: degraded_reason=invalid_classifier_output; classifier_model=github-copilot/claude-opus-5; recommended profile (mode) is missing, expected one of economy, balanced, robust",
      ]);
    });

    it("fails P1 when there is no selection record", () => {
      cliFixture();
      write(ROUTING, []);
      expect(cliCheck().failures).toContain("FAIL P1 no firewall model_routing selection record with a selected model");
    });
  });

  // Copilot SDK reverse direction (Claude main, GPT sub-agent): the values quoted in
  // gh-aw#68067 from run 38090505292 (pinned claude-sonnet-5 main agent; sub-agent
  // file-summarizer on copilot-responses/gpt-5.4-mini, served as
  // gpt-5.4-mini-2026-03-17), in the SDK event shape of run 38100426674. The run was
  // pinned, so only C1, M1 and S1–S4 are asserted.
  describe("copilot sdk reverse direction (real record shapes)", () => {
    const SUMMARIZER = "file-summarizer";
    const reverseSession = () => [
      sdkEvent("subagent.started", SUMMARIZER, { agentType: SUMMARIZER, model: "copilot-responses/gpt-5.4-mini", modelSelectionSource: "agent_definition_default" }),
      { type: "subagent.configured", agentId: SDK_AGENT_ID, data: { model: "copilot-responses/gpt-5.4-mini", multiTurn: true }, provenance: { component: "agent", phase: "agent", path: "events.jsonl", index: 1 } },
      sdkEvent("subagent.selected", SUMMARIZER, { tools: null }),
      sdkEvent("subagent.completed", SUMMARIZER, { model: "copilot-responses/gpt-5.4-mini" }),
    ];
    const reverseRequests = (subPath = "/responses") => [
      usage("claude-sonnet-5", "/chat/completions", 200, { x_initiator: "user" }),
      usage("gpt-5.4-mini-2026-03-17", subPath),
      usage("gpt-5.4-mini-2026-03-17", subPath),
      usage("claude-sonnet-5", "/chat/completions"),
      usage("claude-sonnet-5", "/chat/completions"),
    ];
    const reverseExpectations = {
      engine: "copilot",
      allowedModels: ["claude-sonnet-5"],
      mainEndpoint: "/chat/completions",
      subAgents: [{ name: SUMMARIZER, model: "gpt-5.4-mini", endpoint: "/responses" }],
      requireDeclaredAgentNames: true,
    };
    const ASSERTED = ["C1", "M1", "S1", "S2", "S3", "S4"];
    const asserted = lines => lines.filter(line => ASSERTED.includes(line.split(" ")[1]));

    it("passes C1, M1 and S1–S4 with the GPT sub-agent on /responses", () => {
      fixture({ session: reverseSession(), requests: reverseRequests() });
      const { passes, failures } = check(reverseExpectations);
      expect(asserted(failures)).toEqual([]);
      expect(checkIds(asserted(passes))).toEqual(["C1", "M1", "S1", "S2", "S3", "S4"]);
      expect(passes).toContain("PASS M1 all 3 main-agent request(s) used /chat/completions");
      expect(passes).toContain("PASS S3 sub-agent file-summarizer request gpt-5.4-mini /responses 200");
    });

    it("fails only S3 when the GPT sub-agent ran on /chat/completions", () => {
      fixture({ session: reverseSession(), requests: reverseRequests("/chat/completions") });
      const { passes, failures } = check(reverseExpectations);
      expect(asserted(failures)).toEqual(["FAIL S3 sub-agent file-summarizer: no 200 request for gpt-5.4-mini on /responses; observed: /chat/completions 200, /chat/completions 200"]);
      expect(checkIds(asserted(passes))).toEqual(["C1", "M1", "S1", "S2", "S4"]);
    });
  });

  // A routed Claude model without effort support: /reflect lists no efforts for
  // claude-haiku-4.5 (awf_model_routing.test.cjs) and the router selects it with a null
  // effort. Selection record in the unified-session shape of the
  // copilot-subagent-failed-and-alias golden, on claude-haiku-4.5.
  describe("P1 for a model selected without an effort", () => {
    const haikuReflect = { endpoints: [{ provider: "copilot", routing_models: [{ model_id: "claude-haiku-4.5", source: "provider", supported_endpoints: ["/chat/completions", "/v1/messages"], supported_reasoning_efforts: [] }] }] };
    const haikuSelection = selectedEffort => {
      const data = {
        schema: "model-routing/v0.28.50",
        stage: "selection",
        objective: { goal: "cost", mode: "economy" },
        provider: "copilot",
        mode: "balanced",
        selectedProvider: "copilot",
        selectedModel: "github-copilot/claude-haiku-4.5",
        wireModel: "claude-haiku-4.5",
        endpoint: "/chat/completions",
      };
      if (selectedEffort !== undefined) data.selectedEffort = selectedEffort;
      return {
        type: "firewall.model_routing",
        data,
        provenance: { component: "firewall", phase: "agent", path: "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl", index: 1 },
      };
    };
    const haikuFixture = selectedEffort => {
      write("usage/aw_session.jsonl", [workflowInfo("selected", "claude-haiku-4.5"), haikuSelection(selectedEffort)]);
      write(TOKEN_USAGE, [classifier(), usage("claude-haiku-4.5", "/chat/completions")]);
      write("sandbox/firewall/awf-reflect.json", haikuReflect);
    };
    const haikuExpectations = { engine: "copilot", allowedModels: ["claude-haiku-4.5"], mainEndpoint: "/chat/completions", routingMode: "economy" };

    it("passes P1 with a null effort when /reflect lists no efforts", () => {
      haikuFixture(null);
      const { passes, failures, info } = check(haikuExpectations);
      expect(failures).toEqual([]);
      expect(passes).toContain("PASS P1 requested mode economy; recommended profile balanced and selected effort null are valid for claude-haiku-4.5");
      expect(info).toEqual(["INFO routing recommended profile=balanced requested mode=economy selected effort=<none>"]);
    });

    it("fails P1 when selectedEffort is absent from the selection record", () => {
      haikuFixture(undefined);
      expect(check(haikuExpectations).failures).toEqual(["FAIL P1 selected effort is undefined, expected null because /reflect lists no supported efforts for claude-haiku-4.5"]);
    });

    it("fails P1 when an effort was selected for a model without effort support", () => {
      haikuFixture("low");
      expect(check(haikuExpectations).failures).toEqual(["FAIL P1 selected effort is low, expected null because /reflect lists no supported efforts for claude-haiku-4.5"]);
    });
  });

  describe("main", () => {
    it("logs every check and fails the step on failure", async () => {
      fixture({ session: [workflowInfo()], requests: copilotRequests() });
      const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      const results = await main({ core, rootDir: root, buildSession: false, ...copilotExpectations });
      expect(core.error).toHaveBeenCalledWith(expect.stringMatching(/^FAIL R1 /));
      expect(core.info).toHaveBeenCalledWith(expect.stringMatching(/^PASS R2 /));
      expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining(`Model-routing smoke assertions failed (${results.failures.length}):\nFAIL R1`));
    });

    it("does not fail the step when every check passes", async () => {
      fixture(copilotPass());
      const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      await main({ core, rootDir: root, buildSession: false, ...copilotExpectations });
      expect(core.setFailed).not.toHaveBeenCalled();
      expect(core.error).not.toHaveBeenCalled();
      expect(core.info).toHaveBeenLastCalledWith("All 5 model-routing smoke assertions passed");
    });
  });

  // Sessions are cut from published usage/aw_session.jsonl files; see
  // fixtures/session_contract_sessions.cjs for the source run of each.
  describe("unified-session contract (U1–U3)", () => {
    const unroutedCopilot = { engine: "copilot", expectModelInventory: true, copilotInventory: true, resolutionRoles: ["main"] };
    const contract = (expectations = unroutedCopilot) => checkSessionContract({ rootDir: root, buildSession: false, ...expectations });
    const withoutWarnings = events => events.filter(event => event.type !== "session.collection_warning");
    const lineFor = (lines, id) => lines.find(line => line.split(" ")[1] === id);

    it("fails U1 and U2 for the pre-fix Smoke Copilot session (run 39181772988)", () => {
      write("usage/aw_session.jsonl", preFixCopilotSession());
      const results = contract();
      expect(results.failures).toEqual([
        "FAIL U1 1 session.collection_warning event(s): invalid_model_inventory in sandbox/firewall/awf-reflect.json (1 occurrence(s))",
        "FAIL U2 expected exactly 1 agent-phase model.inventory, observed 0; model.inventory events: none",
      ]);
      expect(checkIds(results.passes)).toEqual(["E2", "U3"]);
    });

    it("groups repeated U1 warnings by code and path with counts and an example line", () => {
      const warning = (code, file, line) => ({ type: "session.collection_warning", data: { code, path: file, ...(line === undefined ? {} : { line }) } });
      write("usage/aw_session.jsonl", [
        ...postFixCopilotSession(),
        warning("invalid_json", "agent/log.jsonl"),
        ...Array.from({ length: 1000 }, (_, index) => warning("invalid_json", "agent/log.jsonl", index + 2)),
        warning("invalid_record", "agent/log.jsonl", 0),
        warning("invalid_json", "agent/other.jsonl", 9),
      ]);
      expect(lineFor(contract().failures, "U1")).toBe(
        "FAIL U1 1003 session.collection_warning event(s): invalid_json in agent/log.jsonl (1001 occurrence(s), example line 2), invalid_record in agent/log.jsonl (1 occurrence(s), example line 0), invalid_json in agent/other.jsonl (1 occurrence(s), example line 9)"
      );
    });

    it("passes U1–U3 for the post-fix Smoke Copilot session (run 39176941475)", () => {
      write("usage/aw_session.jsonl", postFixCopilotSession());
      const results = contract();
      expect(results.failures).toEqual([]);
      expect(results.passes).toEqual([
        "PASS E2 evidence: unified session present",
        "PASS U1 no session.collection_warning events",
        "PASS U2 one agent-phase model.inventory from sandbox/firewall/awf-reflect.json; copilot endpoint configured with 40 model(s)",
        "PASS U3 model.resolution recorded for main: auto -> claude-sonnet-5.5 (resolved)",
      ]);
    });

    it("passes U1–U3 alongside R1–R4 for the post-fix routed session without a main model.resolution (run 39176939097)", () => {
      fixture({ session: [...postFixCopilotRoutedSession(), workflowInfo(), selection(), harnessOutcome()], requests: copilotRequests() });
      const results = check({ ...copilotExpectations, expectModelInventory: true, copilotInventory: true, resolutionRoles: [] });
      expect(results.failures).toEqual([]);
      expect(checkIds(results.passes)).toEqual(["E2", "R1", "R2", "R3", "R4", "U1", "U2", "U3"]);
      expect(lineFor(results.passes, "U3")).toBe("PASS U3 no model.resolution roles required; observed model.resolution: none");
    });

    it("passes U3 for both Pi sub-agent roles and names a missing role (run 39178909557)", () => {
      const piRoles = { engine: "pi", resolutionRoles: ["haiku-whoami", "mini-whoami"] };
      write("usage/aw_session.jsonl", withoutWarnings(piRoutedSession()));
      const results = contract(piRoles);
      expect(results.failures).toEqual([]);
      expect(lineFor(results.passes, "U3")).toBe("PASS U3 model.resolution recorded for haiku-whoami: copilot/claude-haiku-4.5 -> claude-haiku-4.5 (exact), mini-whoami: copilot/gpt-5.4-mini -> gpt-5.4-mini (exact)");

      write(
        "usage/aw_session.jsonl",
        withoutWarnings(piRoutedSession()).filter(event => event.data?.role !== "mini-whoami")
      );
      expect(contract(piRoles).failures).toEqual(["FAIL U3 no model.resolution for role mini-whoami; observed model.resolution: haiku-whoami: copilot/claude-haiku-4.5 -> claude-haiku-4.5 (exact)"]);
    });

    it("fails U2 for two agent-phase inventories and for an unconfigured copilot endpoint", () => {
      const session = postFixCopilotSession();
      const inventory = session.find(event => event.type === "model.inventory");
      write("usage/aw_session.jsonl", [...session, { ...inventory, provenance: { ...inventory.provenance, path: "sandbox/firewall/models.json" } }]);
      expect(lineFor(contract().failures, "U2")).toBe("FAIL U2 expected exactly 1 agent-phase model.inventory, observed 2; model.inventory events: agent sandbox/firewall/awf-reflect.json, agent sandbox/firewall/models.json");

      // Detection-phase inventories are not counted.
      write("usage/aw_session.jsonl", [...session, { ...inventory, provenance: { ...inventory.provenance, phase: "detection", path: "threat-detection/sandbox/firewall/awf-reflect.json" } }]);
      expect(contract().failures).toEqual([]);

      const unconfigured = postFixCopilotSession().map(event =>
        event.type === "model.inventory" ? { ...event, data: { ...event.data, endpoints: event.data.endpoints.map(endpoint => (endpoint.provider === "copilot" ? { provider: "copilot", configured: false } : endpoint)) } } : event
      );
      write("usage/aw_session.jsonl", unconfigured);
      expect(contract().failures).toEqual([
        "FAIL U2 agent-phase model.inventory from sandbox/firewall/awf-reflect.json has no configured copilot endpoint with models; observed endpoints: openai configured=false models=<none>, anthropic configured=false models=<none>, copilot configured=false models=<none>, gemini configured=false models=<none>, vertex configured=false models=<none>",
      ]);
      expect(contract({ ...unroutedCopilot, copilotInventory: false }).failures).toEqual([]);
    });

    it("fails U3 and prints the cause for a failed model.resolution", () => {
      const failed = postFixCopilotSession().map(event => (event.type === "model.resolution" ? { ...event, data: { ...event.data, resolved_model: null, status: "failed", cause: "model_unavailable" } } : event));
      write("usage/aw_session.jsonl", failed);
      expect(contract().failures).toEqual(["FAIL U3 role main status is failed (cause model_unavailable), expected exact or resolved; observed model.resolution: main: auto -> <none> (failed)"]);
    });

    it("fails U3 for a duplicate role resolution", () => {
      const session = postFixCopilotSession();
      const resolution = session.find(event => event.type === "model.resolution");
      write("usage/aw_session.jsonl", [...session, { ...resolution }]);
      expect(lineFor(contract().failures, "U3")).toBe(
        "FAIL U3 expected exactly 1 model.resolution for role main, observed 2; observed model.resolution: main: auto -> claude-sonnet-5.5 (resolved), main: auto -> claude-sonnet-5.5 (resolved)"
      );
    });

    it.each(["", null, undefined])("fails U3 for resolved_model %s despite a successful status", resolvedModel => {
      const session = postFixCopilotSession().map(event => (event.type === "model.resolution" ? { ...event, data: { ...event.data, resolved_model: resolvedModel } } : event));
      write("usage/aw_session.jsonl", session);
      expect(lineFor(contract().failures, "U3")).toBe("FAIL U3 role main has no resolved_model; observed model.resolution: main: auto -> <none> (resolved)");
    });

    it("fails U2 gracefully when the session has no inventory", async () => {
      write(
        "usage/aw_session.jsonl",
        postFixCopilotSession().filter(event => event.type !== "model.inventory")
      );
      const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      const results = await sessionContractMain({ core, rootDir: root, buildSession: false, ...unroutedCopilot });
      expect(results.failures).toEqual(["FAIL U2 expected exactly 1 agent-phase model.inventory, observed 0; model.inventory events: none"]);
      expect(core.setFailed).toHaveBeenCalledWith(`Unified-session smoke assertions failed (1):\n${results.failures[0]}`);
    });

    it.each([{ resolutionRoles: [] }, { copilotInventory: false, resolutionRoles: [] }, { expectModelInventory: false, copilotInventory: false, resolutionRoles: [] }])("logs when U2 is not requested: %j", async expectations => {
      fixture({ session: [...postFixCopilotRoutedSession(), workflowInfo(), selection(), harnessOutcome()], requests: copilotRequests() });
      for (const [entryPoint, extra] of [
        [main, copilotExpectations],
        [sessionContractMain, {}],
      ]) {
        const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
        const results = await entryPoint({ core, rootDir: root, buildSession: false, ...extra, ...expectations });
        expect(results.failures).toEqual([]);
        expect(core.info).toHaveBeenCalledWith("U2 not evaluated: neither expectModelInventory nor copilotInventory is true");
        expect([...results.passes, ...results.failures].filter(line => /^(PASS|FAIL) U2 /.test(line))).toEqual([]);
      }
    });

    it.each([
      [{}, []],
      [{ copilotInventory: false }, []],
      [{ expectModelInventory: false, copilotInventory: false }, []],
      [{ expectModelInventory: true }, ["U1", "U2", "U3"]],
      [{ copilotInventory: true }, ["U1", "U2", "U3"]],
      [{ resolutionRoles: [] }, ["U1", "U3"]],
    ])("runs U1–U3 from main only when the smoke opts in: %j", async (expectations, expectedIds) => {
      fixture({ session: [...postFixCopilotRoutedSession(), workflowInfo(), selection(), harnessOutcome()], requests: copilotRequests() });
      const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      const results = await main({ core, rootDir: root, buildSession: false, ...copilotExpectations, ...expectations });
      expect(results.failures).toEqual([]);
      expect(checkIds(results.passes).filter(id => /^U\d$/.test(id))).toEqual(expectedIds);
    });

    it("reports E2 without throwing when the evidence root is unavailable", async () => {
      const unavailableRoot = path.join(root, "not-a-directory");
      fs.writeFileSync(unavailableRoot, "");
      const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      const results = await sessionContractMain({ core, rootDir: unavailableRoot, ...unroutedCopilot });
      expect(results.failures.some(line => line.startsWith("FAIL E2 evidence: failed to build the unified session:"))).toBe(true);
      expect(results.failures).toContain("FAIL E2 evidence: missing usage/aw_session.jsonl");
      expect(core.setFailed).toHaveBeenCalled();
    });

    it("reports E2 without throwing when the session path cannot be read", async () => {
      fs.mkdirSync(path.join(root, "usage/aw_session.jsonl"), { recursive: true });
      const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      const results = await sessionContractMain({ core, rootDir: root, buildSession: false, ...unroutedCopilot });
      expect(results.failures.some(line => line.startsWith("FAIL E2 evidence: failed to read ") && line.includes("aw_session.jsonl"))).toBe(true);
      expect(core.setFailed).toHaveBeenCalled();
    });

    it("reports FAIL E2 without U checks when the session file is missing, for both entry points", async () => {
      write(TOKEN_USAGE, copilotRequests());
      const routedCore = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      const routed = await main({ core: routedCore, rootDir: root, buildSession: false, ...copilotExpectations, expectModelInventory: true, copilotInventory: true, resolutionRoles: [] });
      expect(routed.failures[0]).toBe("FAIL E2 evidence: missing usage/aw_session.jsonl");
      expect([...routed.passes, ...routed.failures].filter(line => /^(PASS|FAIL) U/.test(line))).toEqual([]);
      expect(routedCore.info).toHaveBeenCalledWith("U1–U3 not evaluated: missing usage/aw_session.jsonl");
      expect(routedCore.setFailed).toHaveBeenCalled();

      const unroutedCore = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      const unrouted = await sessionContractMain({ core: unroutedCore, rootDir: root, buildSession: false, ...unroutedCopilot });
      expect(unrouted.failures).toEqual(["FAIL E2 evidence: missing usage/aw_session.jsonl"]);
      expect(unrouted.passes).toEqual([]);
      expect(unroutedCore.info).toHaveBeenCalledWith("U1–U3 not evaluated: missing usage/aw_session.jsonl");
      expect(unroutedCore.setFailed).toHaveBeenCalledWith("Unified-session smoke assertions failed (1):\nFAIL E2 evidence: missing usage/aw_session.jsonl");
    });

    it("logs every check and passes the step from sessionContractMain", async () => {
      write("usage/aw_session.jsonl", postFixCopilotSession());
      const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      await sessionContractMain({ core, rootDir: root, buildSession: false, ...unroutedCopilot });
      expect(core.setFailed).not.toHaveBeenCalled();
      expect(core.info).toHaveBeenLastCalledWith("All 4 unified-session smoke assertions passed");
    });

    describe("required sub-agent lifecycle (U4)", () => {
      // Copilot CLI lifecycle record shapes (copilot-cli-builtin-subagents golden
      // and the failed start in run 39181772988).
      const FS_ID = "6f1c9a52-3d0e-4c55-9a7e-0b2f6d1e8c41";
      const cliEvent = (type, data, agentId = FS_ID) => ({
        type,
        agentId,
        data: { toolCallId: "call_fs", ...data },
        provenance: { component: "agent", phase: "agent", path: "sandbox/agent/logs/copilot-session-state/s/events.jsonl", index: 0 },
      });
      const started = (model = "claude-haiku-4.5", agentId = FS_ID) =>
        cliEvent("subagent.started", { agentName: "file-summarizer", agentType: "file-summarizer", model, modelSelectionSource: "agent_definition_default", executionMode: "sync" }, agentId);
      const completed = (model = "claude-haiku-4.5", agentId = FS_ID) => cliEvent("subagent.completed", { agentName: "file-summarizer", model, firstDispatchedModel: model, durationMs: 4100, totalToolCalls: 1 }, agentId);
      const failed = (model, error) => cliEvent("subagent.failed", { agentName: "file-summarizer", durationMs: 280, totalToolCalls: 0, error: `400 Unsupported model selector "${model}" on copilot: no advertised display name matches.` });
      const smokeCopilot = { ...unroutedCopilot, requiredSubAgents: ["file-summarizer"] };
      const run = async session => {
        write("usage/aw_session.jsonl", [...postFixCopilotSession(), ...session]);
        const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
        const results = await sessionContractMain({ core, rootDir: root, buildSession: false, ...smokeCopilot });
        return { results, core };
      };

      it("passes when file-summarizer starts and completes on a resolved model", async () => {
        const { results, core } = await run([started(), cliEvent("subagent.configured", { model: "claude-haiku-4.5" }), completed()]);
        expect(results.failures).toEqual([]);
        expect(lineFor(results.passes, "U4")).toBe("PASS U4 sub-agent file-summarizer started 1 time(s) and completed on model(s) [claude-haiku-4.5]");
        expect(checkIds(results.passes)).toEqual(["E2", "U1", "U2", "U3", "U4"]);
        expect(core.setFailed).not.toHaveBeenCalled();
      });

      it("smoke_fails_when_file_summarizer_fails_to_start", async () => {
        const expression = "${{ experiments.subagent_model }}";
        const { results, core } = await run([started(expression), failed(expression)]);
        expect(results.failures).toEqual([
          `FAIL U4 sub-agent file-summarizer: 1 subagent.failed event(s): 400 Unsupported model selector "${expression}" on copilot: no advertised display name matches.; 1 of 1 invocation(s) have no subagent.completed; unresolved model expression(s) [${expression}]; observed events: subagent.started(agentName=file-summarizer model=${expression}), subagent.failed(agentName=file-summarizer)`,
        ]);
        expect(core.setFailed).toHaveBeenCalled();

        // A failed start on an unresolved alias fails as well, without the expression problem.
        const alias = await run([started("small"), failed("small")]);
        expect(lineFor(alias.results.failures, "U4")).toMatch(
          /^FAIL U4 sub-agent file-summarizer: 1 subagent\.failed event\(s\): 400 Unsupported model selector "small".*; 1 of 1 invocation\(s\) have no subagent\.completed; observed events: /
        );
      });

      it("fails when a later invocation fails or never completes", async () => {
        const second = "9a0d7c11-5b2e-4f8a-8c3d-2e6f4b1a7d90";
        const { results } = await run([started(), completed(), started("claude-haiku-4.5", second)]);
        expect(lineFor(results.failures, "U4")).toMatch(/^FAIL U4 sub-agent file-summarizer: 1 of 2 invocation\(s\) have no subagent\.completed; /);
      });

      it("fails when completion is cancellation only", async () => {
        const cancelled = { ...completed(), data: { ...completed().data, cancelled: true } };
        const { results, core } = await run([started(), cancelled]);
        expect(lineFor(results.failures, "U4")).toMatch(/1 of 1 invocation\(s\) have no subagent\.completed/);
        expect(core.setFailed).toHaveBeenCalled();
      });

      it("preserves a genuine completion followed by cleanup cancellation", async () => {
        const cancelled = { ...completed(), data: { ...completed().data, cancelled: true } };
        const { results, core } = await run([started(), completed(), cancelled]);
        expect(results.failures).toEqual([]);
        expect(lineFor(results.passes, "U4")).toContain("started 1 time(s) and completed");
        expect(core.setFailed).not.toHaveBeenCalled();
      });

      it("requires non-cancelled completion for each invocation", async () => {
        const second = "second-invocation";
        const cancelled = { ...completed("claude-haiku-4.5", second), data: { ...completed().data, cancelled: true } };
        const { results } = await run([started(), completed(), started("claude-haiku-4.5", second), cancelled]);
        expect(lineFor(results.failures, "U4")).toMatch(/1 of 2 invocation\(s\) have no subagent\.completed/);
      });

      it("smoke_fails_when_file_summarizer_is_not_invoked", async () => {
        const { results, core } = await run([]);
        expect(results.failures).toEqual(["FAIL U4 sub-agent file-summarizer: never started (no subagent.started); observed events: none"]);
        expect(checkIds(results.passes)).toEqual(["E2", "U1", "U2", "U3"]);
        expect(core.setFailed).toHaveBeenCalled();

        // Another sub-agent's successful lifecycle does not satisfy the requirement.
        const other = await run([{ ...started(), data: { ...started().data, agentName: "explore" } }, completed()]);
        expect(lineFor(other.results.failures, "U4")).toMatch(/^FAIL U4 sub-agent file-summarizer: never started \(no subagent\.started\); observed events: subagent\.started\(agentName=explore /);
      });
    });

    it("counts U failures in T4 when steering is configured", () => {
      write("usage/aw_session.jsonl", preFixCopilotSession());
      write(TOKEN_USAGE, copilotRequests());
      const { failures } = check({ ...copilotExpectations, steering: { maxAiCredits: 7 }, expectModelInventory: true, copilotInventory: true, resolutionRoles: [] });
      expect(lineFor(failures, "T4")).toMatch(/^FAIL T4 existing check\(s\) failed: .*U1, U2$/);
    });
  });
});

// Detection fixtures are copied from real records with request IDs and timestamps
// redacted: the detection job's api-proxy token-usage.jsonl from run 38173405739
// (Smoke Copilot Routed, schema token-usage/v0.28.51), the external engine's
// detection_result.json from run 38174054652, and the detection and agent
// awf-resolved-config.json / model-routing.jsonl from run 38173405739. The
// classifier record is the agent-side classifier record from run 38173405739.
const DETECTION_MODEL = "claude-haiku-5.5";
const detectionUsage = (model = DETECTION_MODEL, extra = {}) => ({
  _schema: "token-usage/v0.28.51",
  timestamp: "<redacted>",
  event: "token_usage",
  request_id: "<redacted>",
  provider: "copilot",
  model,
  path: "/chat/completions",
  status: 200,
  streaming: true,
  input_tokens: 2271,
  output_tokens: 191,
  cache_read_tokens: 9290,
  cache_write_tokens: 7932,
  duration_ms: 2938,
  response_bytes: 10983,
  x_initiator: "user",
  ...extra,
});
const detectionRequests = () => [detectionUsage(), detectionUsage(DETECTION_MODEL, { input_tokens: 2, output_tokens: 130, x_initiator: "agent" })];
const agentClassifierRecord = () => ({
  _schema: "token-usage/v0.28.51",
  timestamp: "<redacted>",
  event: "token_usage",
  request_id: "<redacted>",
  provider: "copilot",
  model: "gpt-5.6-luna",
  path: "/responses",
  status: 200,
  streaming: false,
  input_tokens: 941,
  output_tokens: 283,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  duration_ms: 3847,
  response_bytes: 14005,
  purpose: "routing_classification",
  x_initiator: "agent",
});
const detectionResult = () => ({ prompt_injection: false, secret_leak: false, malicious_patch: false, reasons: [], warnings: [] });
const detectionResolvedConfig = () => ({ enableApiProxy: true, maxAiCredits: 400, maxRuns: 500, maxCacheMisses: 5, enableTokenSteering: true });
const agentRoutingConfig = () => ({
  experimentalModelRouting: true,
  modelRouting: {
    objective: { goal: "cost", mode: "economy" },
    task: { conversationFile: "/tmp/gh-aw/routing-conversation.json" },
    candidateModels: ["github-copilot/gpt-5.4-mini", "github-copilot/gpt-5.6-luna", "github-copilot/claude-haiku-4.5"],
  },
});
const agentRoutingSelection = () => ({
  _schema: "model-routing/v0.28.51",
  timestamp: "<redacted>",
  event: "model_routing",
  stage: "selection",
  objective: { goal: "cost", mode: "economy" },
  provider: "copilot",
  selected_provider: "copilot",
  wire_model: "gpt-5.6-luna",
  endpoint: "/responses",
  selected_model: "github-copilot/gpt-5.6-luna",
});
const detectionExpectations = { detectionModel: DETECTION_MODEL, allowedModels: ROUTED_ALLOWED, executionOutcome: "success" };
const API_PROXY_LOGS = "sandbox/firewall/logs/api-proxy-logs";
const RESOLVED_CONFIG = "sandbox/firewall/audit/awf-resolved-config.json";

describe("smoke_model_routing_assertions detection", () => {
  let root;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-smoke-detection-"));
  });
  afterEach(() => {
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  function write(file, entries) {
    const target = path.join(root, file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, typeof entries === "string" ? entries : file.endsWith(".json") ? JSON.stringify(entries) : entries.map(entry => JSON.stringify(entry)).join("\n") + "\n");
  }

  function fixture({ result = detectionResult(), requests = detectionRequests(), config = detectionResolvedConfig() } = {}) {
    if (result) write("detection_result.json", result);
    if (requests) write(TOKEN_USAGE, requests);
    if (config) write(RESOLVED_CONFIG, config);
  }

  const check = (expectations = detectionExpectations) => checkDetectionEvidence({ rootDir: root, ...expectations });
  const checkIds = lines => lines.map(line => line.split(" ")[1]);

  it("passes D1–D3 for an unrouted detection run on the pinned model", () => {
    fixture();
    const { passes, failures } = check();
    expect(failures).toEqual([]);
    expect(checkIds(passes)).toEqual(["D1", "D2", "D3"]);
    expect(passes[1]).toBe("PASS D2 all 2 detection request(s) used claude-haiku-5.5, outside allowed-models [gpt-5.4-mini, gpt-5.6-luna, claude-haiku-4.5]");
  });

  it("passes D3 when model-routing.jsonl exists without selection events", () => {
    fixture();
    write(`${API_PROXY_LOGS}/model-routing.jsonl`, []);
    expect(check().failures).toEqual([]);
  });

  it("fails D1 when the detection step did not succeed, without consulting execution.json", () => {
    fixture();
    write("execution.json", { version: 1, component: "detection", state: "started" });
    expect(check({ ...detectionExpectations, executionOutcome: "failure" }).failures).toEqual(["FAIL D1 detection execution outcome is failure, expected success"]);
  });

  it("fails D1 when the result file is missing, unparseable, or lacks verdict keys", () => {
    fixture({ result: null });
    expect(check().failures).toEqual([expect.stringMatching(/^FAIL D1 missing .*detection_result\.json$/)]);
    write("detection_result.json", "{not json");
    expect(check().failures).toEqual([expect.stringMatching(/^FAIL D1 .*detection_result\.json is not valid JSON/)]);
    write("detection_result.json", { prompt_injection: false, reasons: [] });
    expect(check().failures).toEqual(["FAIL D1 detection_result.json is missing or has malformed verdict key(s) [secret_leak, malicious_patch, warnings]; observed keys: [prompt_injection, reasons]"]);
  });

  it("fails D2 when a detection request used the routed model", () => {
    fixture({ requests: [detectionUsage(), detectionUsage("gpt-5.6-luna", { path: "/responses" })] });
    const { failures } = check();
    expect(failures).toEqual([
      "FAIL D2 detection request(s) not on configured model claude-haiku-5.5: gpt-5.6-luna /responses 200; detection request(s) on allowed-models [gpt-5.4-mini, gpt-5.6-luna, claude-haiku-4.5]: gpt-5.6-luna /responses 200",
    ]);
  });

  it("fails D2 when there are no detection requests or the token usage is missing", () => {
    fixture({ requests: [] });
    expect(check().failures).toEqual(["FAIL D2 no detection requests in the api-proxy token usage"]);
    fs.rmSync(path.join(root, TOKEN_USAGE));
    const { failures } = check();
    expect(checkIds(failures)).toEqual(["D2", "D3"]);
    expect(failures[0]).toMatch(/^FAIL D2 missing api-proxy token-usage\.jsonl/);
  });

  it("fails D2 when the configured detection model is a routing candidate", () => {
    fixture({ requests: [detectionUsage("claude-haiku-4.5")] });
    expect(check({ ...detectionExpectations, detectionModel: "claude-haiku-4.5" }).failures).toEqual([
      "FAIL D2 configured detection model claude-haiku-4.5 is in allowed-models [gpt-5.4-mini, gpt-5.6-luna, claude-haiku-4.5]; it must be disjoint from the routing candidates; detection request(s) on allowed-models [gpt-5.4-mini, gpt-5.6-luna, claude-haiku-4.5]: claude-haiku-4.5 /chat/completions 200",
    ]);
  });

  it("fails D3 when detection token usage contains a routing_classification record", () => {
    fixture({ requests: [agentClassifierRecord(), ...detectionRequests()] });
    const { failures } = check();
    expect(checkIds(failures)).toEqual(["D2", "D3"]);
    expect(failures[1]).toBe("FAIL D3 detection made 1 routing_classification request(s): gpt-5.6-luna /responses 200");
  });

  it("fails D3 when detection has a routing selection or a model-routing config block", () => {
    fixture({ config: agentRoutingConfig() });
    write(`${API_PROXY_LOGS}/model-routing.jsonl`, [agentRoutingSelection()]);
    expect(check().failures).toEqual(["FAIL D3 detection model-routing.jsonl has 1 selection event(s): gpt-5.6-luna /responses; detection AWF resolved config has model-routing setting(s) [modelRouting, experimentalModelRouting]"]);
  });

  it("fails D3 when the resolved AWF config is missing", () => {
    fixture({ config: null });
    expect(check().failures).toEqual([expect.stringMatching(/^FAIL D3 missing .*awf-resolved-config\.json$/)]);
  });

  it("reports missing evidence without duplicate messages when evaluated directly", () => {
    const { failures } = evaluateDetectionEvidence({ requests: [], expectations: detectionExpectations });
    expect(failures).toEqual(["FAIL D1 no parsed detection_result.json", "FAIL D2 no detection requests in the api-proxy token usage", "FAIL D3 no parsed detection AWF resolved config"]);
  });

  describe("detectionMain", () => {
    it("logs every check and fails the step on failure", async () => {
      fixture({ requests: [agentClassifierRecord()] });
      const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      const results = await detectionMain({ core, rootDir: root, ...detectionExpectations });
      expect(core.info).toHaveBeenCalledWith(expect.stringMatching(/^PASS D1 /));
      expect(core.error).toHaveBeenCalledWith(expect.stringMatching(/^FAIL D3 /));
      expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining(`Threat-detection smoke assertions failed (${results.failures.length}):\nFAIL D2`));
    });

    it("does not fail the step when every check passes", async () => {
      fixture();
      const core = { info: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
      await detectionMain({ core, rootDir: root, ...detectionExpectations });
      expect(core.setFailed).not.toHaveBeenCalled();
      expect(core.error).not.toHaveBeenCalled();
      expect(core.info).toHaveBeenLastCalledWith("All 3 threat-detection smoke assertions passed");
    });
  });
});
