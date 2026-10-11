import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { checkModelRoutingEvidence, main, normalizeEndpoint, servedModel } from "./smoke_model_routing_assertions.cjs";

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
});
