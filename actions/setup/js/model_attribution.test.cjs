import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { resolveModelRoutingSummary, resolveEffectiveModel, getEffectiveModelLabel } = require("./model_attribution.cjs");

function createRoot() {
  return fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-model-routing-"));
}

function writeJSONL(filePath, records) {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, records.map(record => JSON.stringify(record)).join("\n"));
}

describe("observed model attribution", () => {
  it.each([
    ["gemini-3.8-flash", "auto (Gemini 3.8 Flash)"],
    ["gpt-6.1-sol", "auto (GPT 6.1 Sol)"],
    ["provider/", "auto"],
    ["", "auto"],
    ["auto", "auto"],
    ["bad model<script>", "auto"],
  ])("labels auto using validated observed model %s", (model, label) => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      fs.writeFileSync(infoPath, JSON.stringify({ requested_model: "auto", model_selection: { status: model ? "observed" : "unavailable", effective_model: model } }));
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "auto" })).toBe(label);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("lists distinct agent models without including classifier, detection, or untrusted events", () => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      writeJSONL(path.join(root, "usage/aw_session.jsonl"), [
        ...["gemini-3.8-flash", "gemini-3.8-flash", "google/gemini-3.8-flash", "Gemini-3.8-Flash", "claude-sonnet-5", "auto", "<script>"].map(model => ({
          type: "firewall.token_usage",
          data: { model, purpose: "agent" },
          provenance: { component: "firewall", phase: "agent" },
        })),
        { type: "firewall.token_usage", data: { model: "failed-candidate", purpose: "agent", status: 503 }, provenance: { component: "firewall", phase: "agent" } },
        { type: "firewall.token_usage", data: { model: "classifier", purpose: "routing_classification" }, provenance: { component: "firewall", phase: "agent" } },
        { type: "firewall.token_usage", data: { model: "detector" }, provenance: { component: "firewall", phase: "detection" } },
        { type: "firewall.token_usage", data: { model: "forged" }, provenance: { component: "agent", phase: "agent" } },
      ]);
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "auto" })).toBe("auto (Gemini 3.8 Flash, Claude Sonnet 5)");
      expect(getEffectiveModelLabel(infoPath, "detection", { GH_AW_ENGINE_MODEL: "auto" })).toBe("auto");
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "gpt-5.4" })).toBe("gpt54");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  const tokenUsageRecord = overrides => ({
    _schema: "token-usage/v0.29.6",
    event: "token_usage",
    provider: "copilot",
    model: "claude-sonnet-5.5",
    path: "/chat/completions",
    status: 200,
    ...overrides,
  });

  it.each(["sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl", "sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl"])(
    "labels auto from the agent artifact token-usage log at %s without a usage directory",
    relativePath => {
      const root = createRoot();
      const infoPath = path.join(root, "aw_info.json");
      try {
        fs.writeFileSync(infoPath, JSON.stringify({ requested_model: "auto" }));
        writeJSONL(path.join(root, relativePath), [tokenUsageRecord({})]);
        expect(fs.existsSync(path.join(root, "usage"))).toBe(false);
        expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "auto" })).toBe("auto (Claude Sonnet 5.5)");
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    }
  );

  it("ignores routing classification and non-2xx token-usage records", () => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      writeJSONL(path.join(root, "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl"), [
        tokenUsageRecord({ model: "gpt-5-mini", purpose: "routing_classification" }),
        tokenUsageRecord({ model: "gemini-3.8-flash", status: 503 }),
        tokenUsageRecord({ purpose: "agent" }),
      ]);
      writeJSONL(path.join(root, "usage/aw_session.jsonl"), [{ type: "firewall.token_usage", data: { model: "gpt-6.1-sol", purpose: "agent" }, provenance: { component: "firewall", phase: "agent" } }]);
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "auto" })).toBe("auto (Claude Sonnet 5.5)");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("falls back to the unified session when the token-usage log is missing", () => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      writeJSONL(path.join(root, "usage/aw_session.jsonl"), [{ type: "firewall.token_usage", data: tokenUsageRecord({ purpose: "agent" }), provenance: { component: "firewall", phase: "agent" } }]);
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "auto" })).toBe("auto (Claude Sonnet 5.5)");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("shows plain auto when no model usage files exist", () => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "auto" })).toBe("auto");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("preserves fallback-only labels when auto has no observed model evidence", () => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      fs.writeFileSync(infoPath, JSON.stringify({ requested_model: "auto", fallback_model: "gpt-5.4" }));
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "auto" })).toBe("gpt54");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("keeps the fallback label over native observations without proxy usage evidence", () => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      fs.writeFileSync(
        infoPath,
        JSON.stringify({
          requested_model: "auto",
          fallback_model: "gpt-5.4",
          model_selection: { status: "observed", effective_model: "gpt-5.6-luna", observed_models: ["copilot/gpt-5.6-luna", "gpt-5.6-luna"] },
        })
      );
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "auto" })).toBe("gpt54");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("uses validated native model observations when the session usage file is absent", () => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      fs.writeFileSync(
        infoPath,
        JSON.stringify({
          requested_model: "auto",
          model_selection: {
            status: "observed",
            effective_model: "claude-sonnet-5",
            observed_models: ["google/gemini-3.8-flash", "claude-sonnet-5", "bad model<script>", "provider/"],
          },
        })
      );
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "auto" })).toBe("auto (Gemini 3.8 Flash, Claude Sonnet 5)");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it.each(["observed", "substituted"])("normalizes requested query parameters for %s selections", status => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      const configured = "gemini-3.8-flash?effort=high";
      fs.writeFileSync(
        infoPath,
        JSON.stringify({
          requested_model: configured,
          model_selection: { status, configured_model: configured, effective_model: "claude-sonnet-5" },
        })
      );
      expect(resolveEffectiveModel(infoPath, "agent", {})).toMatchObject({ model: "claude-sonnet-5", requestedModel: "gemini-3.8-flash" });
      expect(JSON.parse(fs.readFileSync(infoPath, "utf8")).model_selection.configured_model).toBe(configured);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("labels an observed model that differs from routing without an authorized fallback", () => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      fs.writeFileSync(
        infoPath,
        JSON.stringify({
          model_routing: { status: "selected", wire_model: "gemini-3.8-flash" },
          model_selection: { status: "substituted", effective_model: "claude-sonnet-5" },
        })
      );
      expect(getEffectiveModelLabel(infoPath, "agent", {})).toBe("observed: sonnet50 (routed: gem38)");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("does not apply agent selections or unsupported phase-prefixed metadata to detection", () => {
    const root = createRoot();
    const infoPath = path.join(root, "aw_info.json");
    try {
      fs.writeFileSync(
        infoPath,
        JSON.stringify({
          model_selection: { status: "substituted", effective_model: "claude-sonnet-5" },
          detection_model_selection: { status: "unavailable", effective_model: "" },
        })
      );
      expect(resolveEffectiveModel(infoPath, "detection", { GH_AW_ENGINE_MODEL: "gpt-5.4" }).model).toBe("gpt-5.4");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});

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
            labels: { task_type: "fix", scope: "local", task_complexity: "medium" },
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
          type: "firewall.token_usage",
          data: { purpose: "routing_classification", path: "/responses", xInitiator: "router", aic: 0.04, totalAic: 99 },
          provenance: { component: "firewall", phase: "agent" },
        },
        {
          type: "firewall.token_usage",
          data: { purpose: "agent", aic: 1 },
          provenance: { component: "firewall", phase: "agent" },
        },
        {
          type: "firewall.token_usage",
          data: { purpose: "routing_classification", aic: 9 },
          provenance: { component: "firewall", phase: "subagent" },
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
        task_type: "fix",
        scope: "local",
        complexity: "medium",
        degraded: false,
        classifier_aic: 0.04,
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
      const proxyRecords = [
        {
          _schema: "model-routing/v0.28.49",
          stage: "selection",
          objective: { goal: "cost" },
          labels: { task_type: "fix", scope: "local", task_complexity: "medium" },
          degraded_classification: true,
        },
        { _schema: "model-routing/v0.28.49", stage: "request", routed: "deviated", deviations: ["effort"] },
      ];
      writeJSONL(proxyPath, proxyRecords);
      writeJSONL(path.join(root, "sandbox/firewall/audit/api-proxy-logs/model-routing.jsonl"), proxyRecords);

      expect(resolveModelRoutingSummary({ infoPath, sessionPath: path.join(root, "missing.jsonl"), ghAwDir: root })).toEqual({
        status: "selected",
        mode: "awf-routed",
        router_version: "0.28.49",
        objective: "cost",
        task_type: "fix",
        scope: "local",
        complexity: "medium",
        degraded: true,
        deviated_requests: 1,
      });
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("uses runner-written routing and ignores agent outcomes", () => {
    const root = createRoot();
    const sessionPath = path.join(root, "usage/aw_session.jsonl");
    const infoPath = path.join(root, "aw_info.json");
    try {
      fs.writeFileSync(
        infoPath,
        JSON.stringify({
          model_routing: {
            status: "rejected",
            failure_code: "runner_failure",
            mode: "runner-mode",
            router_version: "runner-version",
          },
        })
      );
      writeJSONL(sessionPath, [
        {
          type: "model_routing.outcome",
          data: { status: "selected", failureCode: "prompt-derived-failure" },
          provenance: { component: "agent", phase: "agent" },
        },
        {
          type: "firewall.model_routing",
          data: {
            schema: "model-routing/v0.28.38",
            stage: "selection",
            labels: { task_type: "private_user_prompt", scope: "unsafe value", task_complexity: "prompt-derived-value" },
          },
          provenance: { component: "firewall", phase: "agent" },
        },
        {
          type: "firewall.model_routing",
          data: { schema: "model-routing/v0.28.38", stage: "request", routed: "deviated", deviations: ["endpoint"] },
          provenance: { component: "firewall", phase: "agent" },
        },
      ]);

      expect(resolveModelRoutingSummary({ sessionPath, infoPath, ghAwDir: root })).toEqual({
        status: "rejected",
        mode: "runner-mode",
        router_version: "runner-version",
        failure_code: "runner_failure",
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

  it("estimates classifier AIC from attributed token usage when response AIC is absent", () => {
    const root = createRoot();
    const sessionPath = path.join(root, "usage/aw_session.jsonl");
    const infoPath = path.join(root, "aw_info.json");
    try {
      fs.writeFileSync(infoPath, JSON.stringify({ model_routing: { status: "selected" } }));
      writeJSONL(sessionPath, [
        {
          type: "model_routing.outcome",
          data: { status: "selected" },
          provenance: { component: "agent", phase: "agent" },
        },
        {
          type: "firewall.model_routing",
          data: { stage: "selection" },
          provenance: { component: "firewall", phase: "agent" },
        },
        {
          type: "firewall.token_usage",
          data: {
            provider: "anthropic",
            model: "claude-sonnet-4.6",
            purpose: "routing_classification",
            usage: { inputTokens: 100, outputTokens: 10 },
          },
          provenance: { component: "firewall", phase: "agent" },
        },
      ]);

      expect(resolveModelRoutingSummary({ sessionPath, infoPath, ghAwDir: root }).classifier_aic).toBeGreaterThan(0);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
