import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { resolveModelRoutingSummary } = require("./model_attribution.cjs");

function createRoot() {
  return fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-model-routing-"));
}

function writeJSONL(filePath, records) {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, records.map(record => JSON.stringify(record)).join("\n"));
}

describe("resolveModelRoutingSummary", () => {
  it("reads allow-listed routing data and counts agent request deviations", () => {
    const root = createRoot();
    const sessionPath = path.join(root, "usage/aw_session.jsonl");
    try {
      writeJSONL(sessionPath, [
        {
          type: "workflow.info",
          data: { modelRouting: { status: "selected", mode: "session-mode", routerVersion: "1.2.3" } },
          provenance: { component: "workflow", phase: "agent" },
        },
        {
          type: "model_routing.outcome",
          data: { status: "selected" },
          provenance: { component: "agent", phase: "agent" },
        },
        {
          type: "firewall.model_routing",
          data: {
            schema: "model-routing/v0.28.49",
            stage: "selection",
            objective: { goal: "cost" },
            labels: { task_type: "code", scope: "repository", task_complexity: "moderate" },
            mode: "selection-mode",
            router: { version: "selection-version" },
            degraded_classification: false,
            ranked_choices: ["MUST_NOT_BE_EXPORTED"],
            rationale: "MUST_NOT_BE_EXPORTED",
          },
          provenance: { component: "firewall", phase: "agent" },
        },
        {
          type: "firewall.model_routing",
          data: { schema: "model-routing/v0.28.49", stage: "request", routed: "as_selected" },
          provenance: { component: "firewall", phase: "agent" },
        },
        {
          type: "firewall.model_routing",
          data: { schema: "model-routing/v0.28.49", stage: "request", routed: "deviated", deviations: ["effort"] },
          provenance: { component: "firewall", phase: "agent" },
        },
        {
          type: "firewall.model_routing",
          data: { schema: "model-routing/v0.28.49", stage: "request", routed: "deviated" },
          provenance: { component: "firewall", phase: "subagent" },
        },
      ]);

      expect(resolveModelRoutingSummary({ sessionPath, infoPath: path.join(root, "aw_info.json"), ghAwDir: root })).toEqual({
        status: "selected",
        mode: "session-mode",
        router_version: "1.2.3",
        objective: "cost",
        task_type: "code",
        scope: "repository",
        complexity: "moderate",
        degraded: false,
        deviated_requests: 1,
      });
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("uses aw_info and proxy logs when the session file is absent", () => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    const proxyPath = path.join(root, "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl");
    try {
      fs.writeFileSync(infoPath, JSON.stringify({ model_routing: { status: "selected", mode: "awf-routed", router_version: "0.28.49" } }));
      writeJSONL(proxyPath, [
        {
          _schema: "model-routing/v0.28.49",
          stage: "selection",
          objective: { goal: "cost" },
          labels: { task_type: "code", scope: "repository", task_complexity: "moderate" },
          degraded_classification: true,
        },
        { _schema: "model-routing/v0.28.49", stage: "request", routed: "deviated", deviations: ["effort"] },
      ]);

      expect(resolveModelRoutingSummary({ infoPath, sessionPath: path.join(root, "missing.jsonl"), ghAwDir: root })).toEqual({
        status: "selected",
        mode: "awf-routed",
        router_version: "0.28.49",
        objective: "cost",
        task_type: "code",
        scope: "repository",
        complexity: "moderate",
        degraded: true,
        deviated_requests: 1,
      });
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("uses an agent routing failure code and normalizes legacy endpoint-only deviations", () => {
    const root = createRoot();
    const sessionPath = path.join(root, "usage/aw_session.jsonl");
    try {
      writeJSONL(sessionPath, [
        {
          type: "model_routing.outcome",
          data: { status: "rejected", failureCode: "unsupported_endpoint" },
          provenance: { component: "agent", phase: "agent" },
        },
        {
          type: "firewall.model_routing",
          data: { schema: "model-routing/v0.28.38", stage: "selection", labels: { scope: "unsafe value" } },
          provenance: { component: "firewall", phase: "agent" },
        },
        {
          type: "firewall.model_routing",
          data: { schema: "model-routing/v0.28.38", stage: "request", routed: "deviated", deviations: ["endpoint"] },
          provenance: { component: "firewall", phase: "agent" },
        },
      ]);

      expect(resolveModelRoutingSummary({ sessionPath, infoPath: path.join(root, "missing.json"), ghAwDir: root })).toEqual({
        status: "rejected",
        mode: "",
        router_version: "",
        failure_code: "unsupported_endpoint",
        objective: "",
        task_type: "",
        scope: "",
        complexity: "",
        deviated_requests: 0,
      });
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
