import fs from "node:fs";
import { createRequire } from "node:module";
import { describe, test, expect } from "vitest";

const require = createRequire(import.meta.url);
const workflow = fs.readFileSync(new URL("../../../.github/workflows/smoke-copilot-sub-agents.md", import.meta.url), "utf8");
const script = workflow
  .split("      script: |\n")[1]
  .split("\n---")[0]
  .replace(/^        /gm, "");
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;

function evidence() {
  const agents = [
    ["haiku-whoami", "copilot-completions/claude-haiku-4.5", "/chat/completions"],
    ["mini-whoami", "copilot-responses/gpt-5-mini", "/responses"],
    ["nano-whoami", "copilot-responses/gpt-5-nano", "/responses"],
  ];
  return {
    events: agents.flatMap(([agentName, model]) => [
      { type: "subagent.started", data: { agentName, model, invocationId: agentName } },
      { type: "subagent.completed", data: { model, invocationId: agentName } },
    ]),
    requests: agents.map(([, model, endpoint]) => ({ model: model.split("/")[1], path: endpoint, status: 200 })),
  };
}

async function assertEvidence({ events, requests }, variant = "inline_strict") {
  const mockRequire = name => {
    if (name.endsWith("unified_session.cjs")) return { writeUnifiedSession() {} };
    if (name === "node:fs") {
      return {
        existsSync: () => true,
        readFileSync: file => (file.endsWith("aw_session.jsonl") ? events : requests).map(record => JSON.stringify(record)).join("\n"),
      };
    }
    return require(name);
  };
  await new AsyncFunction("require", "process", script)(mockRequire, {
    env: { RUNNER_TEMP: "/tmp", SMOKE_VARIANT: variant, SMOKE_EXECUTION: "success" },
  });
}

describe("Copilot sub-agent smoke evidence", () => {
  test("accepts completed delegation with qualified models and matching proxy requests", async () => {
    await expect(assertEvidence(evidence())).resolves.toBeUndefined();
  });

  test("accepts the served dated Claude model identity", async () => {
    const data = evidence();
    data.events[1].data.model = "copilot-completions/claude-haiku-4-5-20251001";
    await expect(assertEvidence(data)).resolves.toBeUndefined();
  });

  test("rejects success without delegation", async () => {
    await expect(assertEvidence({ events: [], requests: evidence().requests })).rejects.toThrow("must actually run");
  });

  test("rejects a sub-agent that falls back to the main model", async () => {
    const data = evidence();
    data.events[1].data.model = "copilot-responses/gpt-5.3-codex";
    await expect(assertEvidence(data)).rejects.toThrow("changed or fell back");
  });

  test("rejects selection of a different model", async () => {
    const data = evidence();
    data.events.push({ type: "subagent.selected", data: { invocationId: "haiku-whoami", selectedModel: "copilot-responses/gpt-5-mini" } });
    await expect(assertEvidence(data)).rejects.toThrow("changed or fell back");
  });

  test("rejects requests using the wrong wire API", async () => {
    const data = evidence();
    data.requests[0].path = "/responses";
    await expect(assertEvidence(data)).rejects.toThrow("no successful");
  });

  test("rejects failed invocations even with successful model requests", async () => {
    const data = evidence();
    data.events.push({ type: "subagent.failed", data: { invocationId: "haiku-whoami" } });
    await expect(assertEvidence(data)).rejects.toThrow("failed");
  });

  test("requires no delegation in the negative control", async () => {
    await expect(assertEvidence({ events: [], requests: [] }, "single_agent_control")).resolves.toBeUndefined();
    await expect(assertEvidence(evidence(), "single_agent_control")).rejects.toThrow("must not invoke");
  });
});
