import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { checkModelRoutingEvidence, main, normalizeEndpoint, servedModel } from "./smoke_model_routing_assertions.cjs";

const TOKEN_USAGE = "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl";
const ALLOWED = ["gpt-5.4-mini", "gpt-5.6-luna", "claude-haiku-4.5"];

const workflowInfo = (status = "selected", model = "gpt-5.6-luna") => ({
  type: "workflow.info",
  data: { engineId: "copilot", modelRouting: { status, source: "awf-routing", wireModel: model } },
  provenance: { component: "workflow", phase: "activation", path: "aw_info.json", index: 0 },
});
const selection = (model = "gpt-5.6-luna", endpoint = "/responses") => ({
  type: "firewall.model_routing",
  data: { schema: "model-routing/v0.28.49", stage: "selection", selectedModel: model, endpoint },
  provenance: { component: "firewall", phase: "agent", path: "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl", index: 0 },
});
const harnessOutcome = (status = "selected") => ({
  type: "model_routing.outcome",
  data: { status, wireModel: "gpt-5.6-luna" },
  provenance: { component: "agent", phase: "agent", path: "agent/awf-routing-outcome.json", index: 0 },
});
const subagent = (type, agentName, invocationId, extra = {}) => ({
  type,
  agentId: invocationId,
  data: { invocationId, agentName, ...extra },
  provenance: { component: "agent", phase: "agent", path: "agent-session.jsonl", index: 0 },
});
const classifier = (model = "gpt-5.4-mini") => ({ event: "token_usage", model, path: "/responses", status: 200, purpose: "routing_classification" });
const request = (model, endpoint, status = 200) => ({ event: "token_usage", model, path: endpoint, status, x_initiator: "agent" });
const reflect = {
  endpoints: [
    {
      provider: "copilot",
      routing_models: [
        { model_id: "gpt-5.6-luna", supported_endpoints: ["/chat/completions", "/responses"] },
        { model_id: "gpt-5.4-mini", supported_endpoints: ["/chat/completions", "/responses"] },
        { model_id: "claude-haiku-4.5", supported_endpoints: ["/chat/completions", "/v1/messages"] },
      ],
    },
  ],
};

const copilotExpectations = { engine: "copilot", allowedModels: ALLOWED };
const piExpectations = {
  engine: "pi",
  allowedModels: ALLOWED,
  subAgents: [
    { name: "haiku-whoami", model: "copilot/claude-haiku-4.5", endpoint: "/v1/messages" },
    { name: "mini-whoami", model: "copilot/gpt-5.4-mini", endpoint: "/responses" },
  ],
};
const sdkExpectations = {
  engine: "copilot",
  allowedModels: ["gpt-5.6-luna"],
  mainEndpoint: "/responses",
  subAgents: [{ name: "haiku-whoami", model: "claude-haiku-4.5", endpoint: "/chat/completions" }],
  requireDeclaredAgentNames: true,
};

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

  const copilotPass = () => ({
    session: [workflowInfo(), selection()],
    requests: [classifier(), request("gpt-5.6-luna", "/responses"), request("gpt-5.6-luna", "/responses")],
  });
  const piSession = (overrides = {}) => [
    workflowInfo("selected", "claude-haiku-4.5"),
    selection("claude-haiku-4.5", "/v1/messages"),
    subagent("subagent.started", "haiku-whoami", "legacy:pi:haiku-whoami:1", { model: "copilot/claude-haiku-4.5", resolvedModel: "claude-haiku-4.5" }),
    subagent("subagent.request", "haiku-whoami", "legacy:pi:haiku-whoami:1", { model: overrides.haikuModel ?? "claude-haiku-4-5-20251001" }),
    subagent("subagent.completed", "haiku-whoami", "legacy:pi:haiku-whoami:1", { outcome: "completed" }),
    subagent("subagent.started", "mini-whoami", "legacy:pi:mini-whoami:1", { model: "copilot/gpt-5.4-mini", resolvedModel: "gpt-5.4-mini" }),
    subagent("subagent.request", "mini-whoami", "legacy:pi:mini-whoami:1", { model: "gpt-5.4-mini" }),
    subagent(overrides.miniFailed ? "subagent.failed" : "subagent.completed", "mini-whoami", "legacy:pi:mini-whoami:1", { outcome: overrides.miniFailed ? "failed" : "completed" }),
  ];
  const piRequests = () => [classifier(), request("claude-haiku-4.5", "/v1/messages?beta=true"), request("claude-haiku-4-5-20251001", "/v1/messages?beta=true"), request("gpt-5.4-mini", "/responses")];
  const sdkSession = (agentName = "haiku-whoami") => [
    workflowInfo(),
    selection(),
    subagent("subagent.started", agentName, "call-1", { model: "copilot-completions/claude-haiku-4.5", resolvedModel: "claude-haiku-4-5-20251001" }),
    subagent("subagent.completed", agentName, "call-1", { outcome: "completed" }),
  ];
  const sdkRequests = (haiku = request("claude-haiku-4-5-20251001", "/chat/completions")) => [classifier(), request("gpt-5.6-luna", "/responses"), haiku, request("gpt-5.6-luna", "/responses")];

  describe("normalization", () => {
    it.each([
      ["copilot/claude-haiku-4.5", "claude-haiku-4.5"],
      ["copilot-completions/claude-haiku-4.5", "claude-haiku-4.5"],
      ["claude-haiku-4-5-20251001", "claude-haiku-4.5"],
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

  describe("passing variants", () => {
    it("passes routed Copilot CLI evidence", () => {
      fixture(copilotPass());
      const results = check(copilotExpectations);
      expect(results.failures).toEqual([]);
      expect(results.passes.map(line => line.split(" ")[1])).toEqual(["E2", "R1", "R2", "R3", "R4"]);
    });

    it("passes routed pi evidence with cross-family sub-agents", () => {
      fixture({ session: piSession(), requests: piRequests() });
      const results = check(piExpectations);
      expect(results.failures).toEqual([]);
      expect(results.passes.filter(line => line.startsWith("PASS S"))).toHaveLength(6);
    });

    it("passes routed Copilot SDK evidence", () => {
      fixture({ session: sdkSession(), requests: sdkRequests() });
      const results = check({ ...sdkExpectations, executionOutcome: "success" });
      expect(results.failures).toEqual([]);
      expect(results.passes.map(line => line.split(" ")[1])).toEqual(expect.arrayContaining(["E1", "M1", "S1", "S2", "S3", "S4"]));
    });

    it("falls back to the selection endpoint when /reflect has no metadata", () => {
      fixture({ ...copilotPass(), withReflect: false });
      expect(check(copilotExpectations).failures).toEqual([]);
    });
  });

  describe("wrong model", () => {
    it("fails R1 when the selected model is outside allowed-models", () => {
      fixture({
        session: [workflowInfo("selected", "gpt-5.5"), selection("gpt-5.5")],
        requests: [classifier(), request("gpt-5.5", "/responses")],
      });
      const { failures } = check(copilotExpectations);
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL R1 .*selected model gpt-5\.5 is outside allowed-models \[gpt-5\.4-mini, gpt-5\.6-luna, claude-haiku-4\.5\]/));
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL R4 .*gpt-5\.5 \/responses 200/));
    });

    it("fails S3 when a sub-agent runs on a model other than its declared model", () => {
      fixture({
        session: piSession({ haikuModel: "gpt-5.4-mini" }),
        requests: [classifier(), request("gpt-5.4-mini", "/responses"), request("gpt-5.4-mini", "/responses")],
      });
      const { failures } = check(piExpectations);
      expect(failures).toContainEqual(
        "FAIL S3 sub-agent haiku-whoami: no 200 request for claude-haiku-4.5 on /v1/messages; recorded model(s) [gpt-5.4-mini] differ from declared claude-haiku-4.5; observed: no claude-haiku-4.5 requests; other requests: gpt-5.4-mini /responses 200, gpt-5.4-mini /responses 200"
      );
    });

    it("fails R4 for a main-agent request on an out-of-policy model", () => {
      const evidence = copilotPass();
      evidence.requests.push(request("gpt-5.5", "/responses"));
      fixture(evidence);
      expect(check(copilotExpectations).failures).toEqual(["FAIL R4 request(s) on models outside allowed-models [gpt-5.4-mini, gpt-5.6-luna, claude-haiku-4.5]: gpt-5.5 /responses 200"]);
    });
  });

  describe("wrong endpoint", () => {
    it("fails S3 and names the sub-agent, model and endpoint when the SDK sub-agent only hit /responses with 400", () => {
      fixture({ session: sdkSession(), requests: sdkRequests(request("claude-haiku-4.5", "/responses", 400)) });
      const { failures } = check(sdkExpectations);
      expect(failures).toContain("FAIL S3 sub-agent haiku-whoami: no 200 request for claude-haiku-4.5 on /chat/completions; observed: /responses 400");
    });

    it("fails M1 when a main-agent request is not on /responses", () => {
      fixture({ session: sdkSession(), requests: [...sdkRequests(), request("gpt-5.6-luna", "/chat/completions")] });
      expect(check(sdkExpectations).failures).toEqual(["FAIL M1 main-agent request(s) not on /responses; observed: gpt-5.6-luna /chat/completions 200"]);
    });

    it("fails R3 when the selected model only ran on an unsupported endpoint", () => {
      fixture({ session: [workflowInfo(), selection()], requests: [classifier(), request("gpt-5.6-luna", "/v1/messages")] });
      expect(check(copilotExpectations).failures).toEqual(["FAIL R3 no 200 request for gpt-5.6-luna on a supported endpoint (/chat/completions, /responses); observed: gpt-5.6-luna /v1/messages 200"]);
    });
  });

  describe("non-200 status", () => {
    it("fails R3 when every selected-model request failed", () => {
      fixture({ session: [workflowInfo(), selection()], requests: [classifier(), request("gpt-5.6-luna", "/responses", 500), request("gpt-5.6-luna", "/responses", 429)] });
      expect(check(copilotExpectations).failures).toEqual(["FAIL R3 no 200 request for gpt-5.6-luna on a supported endpoint (/chat/completions, /responses); observed: gpt-5.6-luna /responses 500, gpt-5.6-luna /responses 429"]);
    });

    it("fails S3 when the pi sub-agent request returned 400", () => {
      const requests = [classifier(), request("claude-haiku-4.5", "/v1/messages", 400), request("gpt-5.4-mini", "/responses")];
      fixture({ session: piSession(), requests });
      expect(check(piExpectations).failures).toEqual([
        // The fixture's main agent was also routed to claude-haiku-4.5, so R3 fails too.
        "FAIL R3 no 200 request for claude-haiku-4.5 on a supported endpoint (/chat/completions, /v1/messages); observed: claude-haiku-4.5 /v1/messages 400",
        "FAIL S3 sub-agent haiku-whoami: no 200 request for claude-haiku-4.5 on /v1/messages; observed: /v1/messages 400",
      ]);
    });
  });

  describe("sub-agent lifecycle", () => {
    it("fails S2 when a sub-agent failed", () => {
      fixture({ session: piSession({ miniFailed: true }), requests: piRequests() });
      const { failures } = check(piExpectations);
      expect(failures).toEqual([expect.stringMatching(/^FAIL S2 sub-agent mini-whoami: completed=0 failed=1; observed events: .*subagent\.failed\(agentName=mini-whoami outcome=failed\)/)]);
    });

    it("fails S1 and S2 when a sub-agent never ran", () => {
      fixture({ session: piSession().filter(event => event.data?.agentName !== "mini-whoami" && event.agentId !== "legacy:pi:mini-whoami:1"), requests: piRequests() });
      const { failures } = check(piExpectations);
      expect(failures).toContainEqual("FAIL S1 sub-agent mini-whoami: expected exactly 1 subagent.started, observed 0; observed agent names: [haiku-whoami]");
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL S2 sub-agent mini-whoami: never started/));
    });

    it("fails S1 when a sub-agent ran twice", () => {
      const session = [...sdkSession(), subagent("subagent.started", "haiku-whoami", "call-2", { model: "claude-haiku-4.5" }), subagent("subagent.completed", "haiku-whoami", "call-2", {})];
      fixture({ session, requests: sdkRequests() });
      expect(check(sdkExpectations).failures).toEqual(["FAIL S1 sub-agent haiku-whoami: expected exactly 1 subagent.started, observed 2; observed agent names: [haiku-whoami]"]);
    });

    it("fails S4 when SDK events carry a per-call display name instead of the declared agent name", () => {
      fixture({ session: sdkSession("Haiku-whoami call 1"), requests: sdkRequests() });
      const { failures } = check(sdkExpectations);
      expect(failures).toContainEqual("FAIL S4 sub-agent haiku-whoami: no events carry the declared agent name; observed agent names: [Haiku-whoami call 1]");
      expect(failures).toContainEqual(
        "FAIL S4 sub-agent events carry undeclared agent name(s); declared: [haiku-whoami]; observed: subagent.started(agentName=Haiku-whoami call 1 model=copilot-completions/claude-haiku-4.5 resolvedModel=claude-haiku-4-5-20251001)"
      );
    });
  });

  describe("routing evidence", () => {
    it("fails R1 when the firewall selection event is missing", () => {
      fixture({ session: [workflowInfo()], requests: copilotPass().requests });
      const { failures } = check(copilotExpectations);
      expect(failures).toContain("FAIL R1 routing not selected: no firewall model_routing selection record with a selected model (observed 0 selection record(s))");
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL R3 no selected model to verify; observed agent requests: gpt-5\.6-luna \/responses 200/));
    });

    it("fails R1 when only the agent-written routing outcome reports selection", () => {
      const forgedSelection = { ...selection(), provenance: { component: "agent", phase: "agent", path: "agent/model-routing.jsonl", index: 0 } };
      const agentInfo = { ...workflowInfo(), provenance: { component: "workflow", phase: "agent", path: "agent/aw_info.json", index: 0 } };
      fixture({ session: [agentInfo, forgedSelection, harnessOutcome("selected")], requests: copilotPass().requests });
      const { failures } = check(copilotExpectations);
      expect(failures).toContain(
        "FAIL R1 routing not selected: runner-written routing status is missing, expected selected; no firewall model_routing selection record with a selected model (observed 0 selection record(s)); ignored agent-written model_routing.outcome (status=selected)"
      );
    });

    it("fails R1 when the runner reports a routing failure", () => {
      fixture({ session: [workflowInfo("failed"), selection()], requests: copilotPass().requests });
      expect(check(copilotExpectations).failures).toEqual(["FAIL R1 routing not selected: runner-written routing status is failed, expected selected"]);
    });

    it.each([
      [0, []],
      [2, [classifier(), classifier("gpt-5.6-luna")]],
    ])("fails R2 when the classifier ran %i times", (count, classifiers) => {
      fixture({ session: [workflowInfo(), selection()], requests: [...classifiers, request("gpt-5.6-luna", "/responses")] });
      const { failures } = check(copilotExpectations);
      expect(failures).toHaveLength(1);
      expect(failures[0]).toMatch(new RegExp(`^FAIL R2 expected exactly 1 routing_classification request, observed ${count}`));
    });

    it("ignores the classifier request when checking out-of-policy models", () => {
      fixture({ session: [workflowInfo(), selection()], requests: [classifier("gpt-5-mini"), request("gpt-5.6-luna", "/responses")] });
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
      write(TOKEN_USAGE, copilotPass().requests);
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
      write("sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl", [{ _schema: "model-routing/v0.28.49", event: "model_routing", stage: "selection", selected_model: "gpt-5.6-luna", endpoint: "/responses" }]);
      write(TOKEN_USAGE, copilotPass().requests);
      expect(checkModelRoutingEvidence({ rootDir: root, engine: "copilot", allowedModels: ALLOWED }).failures).toEqual([]);
      expect(fs.existsSync(path.join(root, "usage/aw_session.jsonl"))).toBe(true);

      fs.rmSync(path.join(root, "aw_info.json"));
      fs.rmSync(path.join(root, "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl"));
      write("agent/awf-routing-outcome.json", { status: "selected", wire_model: "gpt-5.6-luna" });
      const { failures } = checkModelRoutingEvidence({ rootDir: root, engine: "copilot", allowedModels: ALLOWED });
      expect(failures).toContainEqual(expect.stringMatching(/^FAIL R1 routing not selected: runner-written routing status is missing.*ignored agent-written model_routing\.outcome \(status=selected\)$/));
    });
  });

  describe("main", () => {
    it("logs every check and fails the step on failure", async () => {
      fixture({ session: [workflowInfo()], requests: copilotPass().requests });
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
