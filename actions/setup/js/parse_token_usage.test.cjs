// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const os = require("os");

const {
  main,
  getReadableTokenUsagePaths,
  extractRequestId,
  extractTokenUsageDedupeKey,
  readDedupedTokenUsage,
  getSummaryTitle,
  buildStepSummarySection,
  buildWorkingSetDetailsSection,
  renderTokenTableAsPlainText,
  findCopilotUsageCheckpoint,
  TOKEN_USAGE_AUDIT_PATH,
  TOKEN_USAGE_PATH,
  TOKEN_USAGE_AWF_AUDIT_PATH,
  TOKEN_USAGE_PATHS,
  persistAgentAwInfoCopy,
  AGENT_USAGE_PATH,
  AGENT_USAGE_JSONL_PATH,
  COPILOT_SESSION_STATE_DIR,
  DEFAULT_SUMMARY_TITLE,
} = require("./parse_token_usage.cjs");
const { getEffectiveModelLabel } = require("./model_attribution.cjs");

describe("parse_token_usage", () => {
  const singleEntry = JSON.stringify({
    model: "claude-sonnet-4-6",
    provider: "anthropic",
    input_tokens: 100,
    output_tokens: 200,
    cache_read_tokens: 5000,
    cache_write_tokens: 3000,
    duration_ms: 2500,
  });

  const multiEntry = [
    JSON.stringify({ model: "claude-sonnet-4-6", provider: "anthropic", input_tokens: 100, output_tokens: 200, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 1000 }),
    JSON.stringify({ model: "gpt-4o", provider: "openai", input_tokens: 50, output_tokens: 80, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 500 }),
  ].join("\n");

  describe("constant paths", () => {
    test("TOKEN_USAGE_AUDIT_PATH points to firewall audit log file", () => {
      expect(TOKEN_USAGE_AUDIT_PATH).toBe("/tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl");
    });

    test("TOKEN_USAGE_PATH points to firewall proxy log file", () => {
      expect(TOKEN_USAGE_PATH).toBe("/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl");
    });

    test("TOKEN_USAGE_AWF_AUDIT_PATH points to firewall AWF audit log file", () => {
      expect(TOKEN_USAGE_AWF_AUDIT_PATH).toBe("/tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl");
    });

    test("TOKEN_USAGE_PATHS includes legacy, AWF audit, and proxy log paths", () => {
      expect(TOKEN_USAGE_PATHS).toEqual([TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_AWF_AUDIT_PATH, TOKEN_USAGE_PATH]);
    });

    test("AGENT_USAGE_PATH points to agent_usage.json", () => {
      expect(AGENT_USAGE_PATH).toBe("/tmp/gh-aw/agent_usage.json");
    });

    test("Copilot fallback paths point to the session state and JSONL usage files", () => {
      expect(AGENT_USAGE_JSONL_PATH).toBe("/tmp/gh-aw/agent_usage.jsonl");
      expect(COPILOT_SESSION_STATE_DIR).toBe("/tmp/gh-aw/sandbox/agent/logs/copilot-session-state");
    });

    test("DEFAULT_SUMMARY_TITLE points to Token Usage", () => {
      expect(DEFAULT_SUMMARY_TITLE).toBe("Token Usage");
    });
  });

  describe("main function", () => {
    let tmpDir;
    let mockCore;
    let originalAppendFileSync;
    let originalExistsSync;
    let originalStatSync;
    let originalReadFileSync;
    let originalWriteFileSync;

    beforeEach(() => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "parse-token-usage-test-"));
      delete process.env.GH_AW_TOKEN_USAGE_SUMMARY_TITLE;
      delete process.env.GH_AW_AGENT_USAGE_PATH;
      delete process.env.GH_AW_AGENT_USAGE_JSONL_PATH;
      delete process.env.GH_AW_TMP_DIR;
      delete process.env.GH_AW_WRITE_EMPTY_USAGE;
      delete process.env.GH_AW_PHASE;
      delete process.env.GH_AW_INFO_MODEL;
      delete process.env.GH_AW_MODEL_ROUTING_ENABLED;
      delete process.env.GH_AW_ENGINE_ID;
      delete process.env.GH_AW_ENGINE_MODEL;
      process.env.GITHUB_STEP_SUMMARY = "";

      mockCore = {
        info: vi.fn(),
        debug: vi.fn(),
        warning: vi.fn(),
        error: vi.fn(),
        setFailed: vi.fn(),
        exportVariable: vi.fn(),
        setOutput: vi.fn(),
        summary: {
          addDetails: vi.fn().mockReturnThis(),
          addRaw: vi.fn().mockReturnThis(),
          write: vi.fn().mockResolvedValue(undefined),
        },
      };

      global.core = mockCore;

      originalAppendFileSync = fs.appendFileSync;
      originalExistsSync = fs.existsSync;
      originalStatSync = fs.statSync;
      originalReadFileSync = fs.readFileSync;
      originalWriteFileSync = fs.writeFileSync;

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return "";
        return originalReadFileSync(p, enc);
      });
    });

    afterEach(() => {
      fs.appendFileSync = originalAppendFileSync;
      fs.existsSync = originalExistsSync;
      fs.statSync = originalStatSync;
      fs.readFileSync = originalReadFileSync;
      fs.writeFileSync = originalWriteFileSync;
      delete process.env.GH_AW_AGENT_USAGE_PATH;
      delete process.env.GH_AW_AGENT_USAGE_JSONL_PATH;
      delete process.env.GH_AW_TMP_DIR;
      delete process.env.GH_AW_WRITE_EMPTY_USAGE;
      delete process.env.GH_AW_PHASE;
      delete process.env.GH_AW_INFO_MODEL;
      delete process.env.GH_AW_MODEL_ROUTING_ENABLED;
      delete process.env.GH_AW_ENGINE_ID;
      delete process.env.GH_AW_ENGINE_MODEL;
      delete global.core;
      fs.rmSync(tmpDir, { recursive: true, force: true });
    });

    /**
     * @param {string} summaryText
     * @param {Array<[string, string, string]>} rows [alias, input, output]
     */
    function expectTokenUsageTableRows(summaryText, rows) {
      const escapeRegex = value => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      expect(summaryText).toContain("| # | Alias | Input | Output |");
      for (const [alias, input, output] of rows) {
        const aliasPattern = escapeRegex(alias);
        const inputPattern = escapeRegex(input);
        const outputPattern = escapeRegex(output);
        expect(summaryText).toMatch(new RegExp(`\\|\\s*\\d+\\s*\\|\\s*${aliasPattern}\\s*\\|\\s*${inputPattern}\\s*\\|\\s*${outputPattern}\\s*\\|`));
      }
    }

    function setupClaudeRoutingFixture({ supportedEndpoints = ["/chat/completions", "/v1/messages"], withOutcome = true, requests = undefined, engine = "claude", selectedEndpoint = "/chat/completions" } = {}) {
      process.env.GH_AW_TMP_DIR = tmpDir;
      process.env.GH_AW_MODEL_ROUTING_ENABLED = "true";
      process.env.GH_AW_ENGINE_ID = engine;
      process.env.GH_AW_ENGINE_MODEL = "agent";
      fs.writeFileSync(path.join(tmpDir, "aw_info.json"), JSON.stringify({ model: "agent" }));
      const reflectDir = path.join(tmpDir, "agent");
      fs.mkdirSync(reflectDir, { recursive: true });
      fs.writeFileSync(
        path.join(reflectDir, "awf-reflect.json"),
        JSON.stringify({
          endpoints: [
            {
              provider: "github",
              configured: true,
              models: ["claude-opus-5"],
              routing_models: [{ model_id: "claude-opus-5", candidate_metadata_complete: true, supported_endpoints: supportedEndpoints }],
            },
          ],
        })
      );
      if (withOutcome) {
        fs.writeFileSync(
          path.join(reflectDir, "awf-routing-outcome.json"),
          JSON.stringify({
            status: "selected",
            wire_model: "claude-opus-5",
            endpoint: "/v1/messages",
            selected_endpoint: selectedEndpoint,
          })
        );
      }
      const routingPath = path.join(tmpDir, "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl");
      fs.mkdirSync(path.dirname(routingPath), { recursive: true });
      const records = [
        {
          _schema: "model-routing/v0.28.49",
          stage: "selection",
          selected_provider: "github",
          selected_model: "claude-opus-5",
          wire_model: "claude-opus-5",
          selected_effort: "max",
          endpoint: selectedEndpoint,
        },
        ...(requests === undefined
          ? [
              {
                _schema: "model-routing/v0.28.49",
                stage: "request",
                requested_model: "claude-opus-5",
                routed: "as_selected",
                upstream_endpoint: "/v1/messages",
                deviations: ["endpoint"],
              },
            ]
          : requests),
      ];
      fs.writeFileSync(routingPath, records.map(record => JSON.stringify(record)).join("\n") + "\n");
    }

    test("attributes a corroborated Claude harness endpoint override", async () => {
      setupClaudeRoutingFixture();

      await main();

      const info = JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8"));
      expect(info).toMatchObject({
        model: "claude-opus-5",
        model_routing: { status: "selected", endpoint: "/v1/messages", selected_endpoint: "/chat/completions" },
      });
      expect(mockCore.setOutput).toHaveBeenCalledWith("model", "claude-opus-5");
      expect(mockCore.setOutput).toHaveBeenCalledWith("model_effort", "max");
      expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "selected");
      expect(getEffectiveModelLabel(path.join(tmpDir, "aw_info.json"), "agent", process.env)).toBe("routed: opus50 max");
    });

    test("records a corroborated pi effective endpoint even when AWF's endpoint is supported", async () => {
      setupClaudeRoutingFixture({ engine: "pi" });

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8")).model_routing).toMatchObject({
        status: "selected",
        endpoint: "/v1/messages",
        selected_endpoint: "/chat/completions",
      });
    });

    test("attributes a corroborated Claude endpoint override from reflect when no harness outcome exists", async () => {
      setupClaudeRoutingFixture({ withOutcome: false });

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8")).model_routing).toMatchObject({
        status: "selected",
        endpoint: "/v1/messages",
        selected_endpoint: "/chat/completions",
      });
    });

    test("rejects endpoint overrides the selected model does not advertise", async () => {
      setupClaudeRoutingFixture({ supportedEndpoints: ["/chat/completions"] });

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8"))).toMatchObject({
        model: "agent",
        model_routing: { status: "rejected", failure_code: "unsupported_endpoint" },
      });
      expect(mockCore.setOutput).toHaveBeenCalledWith("model", "");
    });

    test("rejects an uncorroborated harness endpoint claim", async () => {
      setupClaudeRoutingFixture({ requests: [] });

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8")).model_routing).toMatchObject({
        status: "rejected",
        failure_code: "uncorroborated_endpoint",
      });
    });

    test("keeps Copilot endpoint routing strict", async () => {
      setupClaudeRoutingFixture({ engine: "copilot", requests: [], selectedEndpoint: "/v1/messages" });

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8"))).toMatchObject({
        model: "agent",
        model_routing: { status: "rejected", failure_code: "unsupported_endpoint" },
      });
      expect(mockCore.setOutput).toHaveBeenCalledWith("model", "");
    });

    // Native routing: the selection and request records carry the native provider,
    // and /reflect metadata comes from that provider's endpoint (protocol names).
    function setupNativeRoutingFixture({ engine, provider, wireModel, selectedEndpoint, upstreamEndpoint, supportedEndpoints, efforts, effort, reflectProvider = provider, outcomeEndpoint = selectedEndpoint }) {
      process.env.GH_AW_TMP_DIR = tmpDir;
      process.env.GH_AW_MODEL_ROUTING_ENABLED = "true";
      process.env.GH_AW_ENGINE_ID = engine;
      process.env.GH_AW_ENGINE_MODEL = "agent";
      fs.writeFileSync(path.join(tmpDir, "aw_info.json"), JSON.stringify({ model: "agent" }));
      const agentDir = path.join(tmpDir, "agent");
      fs.mkdirSync(agentDir, { recursive: true });
      fs.writeFileSync(
        path.join(agentDir, "awf-reflect.json"),
        JSON.stringify({
          endpoints: [
            {
              provider: reflectProvider,
              configured: true,
              models: [wireModel],
              routing_models: [{ model_id: wireModel, source: "maintained", supported_endpoints: supportedEndpoints, supported_reasoning_efforts: efforts, context_window_tokens: 1000000, candidate_metadata_complete: true }],
            },
          ],
        })
      );
      fs.writeFileSync(path.join(agentDir, "awf-routing-outcome.json"), JSON.stringify({ status: "selected", wire_model: wireModel, endpoint: outcomeEndpoint, selected_endpoint: selectedEndpoint, effort, applied_effort: effort }));
      const routingPath = path.join(tmpDir, "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl");
      fs.mkdirSync(path.dirname(routingPath), { recursive: true });
      const records = [
        {
          _schema: "model-routing/v0.28.62",
          event: "model_routing",
          stage: "selection",
          provider,
          selected_provider: provider,
          selected_model: `${provider}/${wireModel}`,
          wire_model: wireModel,
          selected_effort: effort,
          endpoint: selectedEndpoint,
          mode: "economy",
        },
        {
          _schema: "model-routing/v0.28.62",
          event: "model_routing",
          stage: "request",
          provider,
          selected_provider: provider,
          requested_model: wireModel,
          requested_effort: effort,
          routed: "as_selected",
          deviations: outcomeEndpoint === selectedEndpoint ? [] : ["endpoint"],
          upstream_endpoint: upstreamEndpoint,
        },
      ];
      fs.writeFileSync(routingPath, records.map(record => JSON.stringify(record)).join("\n") + "\n");
    }

    test("records a native Anthropic selection checked against the Anthropic endpoint", async () => {
      setupNativeRoutingFixture({
        engine: "claude",
        provider: "anthropic",
        wireModel: "claude-sonnet-5-5",
        selectedEndpoint: "/v1/messages",
        upstreamEndpoint: "/v1/messages",
        supportedEndpoints: ["messages"],
        efforts: ["low", "medium", "high", "max"],
        effort: "high",
      });

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8"))).toMatchObject({
        model: "claude-sonnet-5-5",
        model_routing: { status: "selected", provider: "anthropic", wire_model: "claude-sonnet-5-5", effort: "high", endpoint: "/v1/messages" },
      });
      expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "selected");
    });

    test("rejects a native selection when only another provider's endpoint lists the model", async () => {
      setupNativeRoutingFixture({
        engine: "claude",
        provider: "anthropic",
        wireModel: "claude-sonnet-5",
        selectedEndpoint: "/v1/messages",
        upstreamEndpoint: "/v1/messages",
        supportedEndpoints: ["/v1/messages"],
        efforts: ["low", "medium", "high", "max"],
        effort: "high",
        reflectProvider: "copilot",
      });

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8")).model_routing).toMatchObject({ status: "rejected", failure_code: "invalid_selection" });
    });

    test("corroborates a native OpenAI endpoint override forwarded upstream as /v1/responses", async () => {
      setupNativeRoutingFixture({
        engine: "codex",
        provider: "openai",
        wireModel: "gpt-5.4",
        selectedEndpoint: "/chat/completions",
        outcomeEndpoint: "/responses",
        upstreamEndpoint: "/v1/responses",
        supportedEndpoints: ["chat_completions", "responses"],
        efforts: ["none", "low", "medium", "high", "xhigh"],
        effort: "high",
      });

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8")).model_routing).toMatchObject({
        status: "selected",
        provider: "openai",
        endpoint: "/responses",
        selected_endpoint: "/chat/completions",
      });
    });

    test.each([
      ["a malformed provider", "claude", "not a provider"],
      ["another native provider", "claude", "openai"],
      ["the other engine's native provider", "codex", "anthropic"],
    ])("rejects a native selection with %s instead of checking it against GitHub", async (_name, engine, selectedProvider) => {
      setupNativeRoutingFixture({
        engine,
        provider: selectedProvider,
        reflectProvider: "copilot",
        wireModel: "claude-sonnet-5",
        selectedEndpoint: "/v1/messages",
        upstreamEndpoint: "/v1/messages",
        supportedEndpoints: ["/v1/messages"],
        efforts: ["low", "medium", "high", "max"],
        effort: "high",
      });

      await main();

      const info = JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8"));
      expect(info.model_routing).toMatchObject({ status: "rejected", failure_code: "provider_mismatch" });
      expect(info.model).toBe("agent");
      expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "rejected");
    });

    test("rejects a GitHub selection when the compiled routing provider is native", async () => {
      setupNativeRoutingFixture({
        engine: "claude",
        provider: "copilot",
        wireModel: "claude-sonnet-5",
        selectedEndpoint: "/v1/messages",
        upstreamEndpoint: "/v1/messages",
        supportedEndpoints: ["/v1/messages"],
        efforts: ["low", "medium", "high", "max"],
        effort: "high",
      });
      fs.writeFileSync(path.join(tmpDir, "aw_info.json"), JSON.stringify({ model: "agent", model_config: { routing: { enabled: true, provider: "anthropic" } } }));

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8")).model_routing).toMatchObject({ status: "rejected", failure_code: "provider_mismatch" });
    });

    test("records selected AWF model and mapped effort in aw_info and step outputs", async () => {
      process.env.GH_AW_TMP_DIR = tmpDir;
      process.env.GH_AW_MODEL_ROUTING_ENABLED = "true";
      process.env.GH_AW_ENGINE_ID = "pi";
      process.env.GH_AW_ENGINE_MODEL = "agent";
      fs.writeFileSync(path.join(tmpDir, "aw_info.json"), JSON.stringify({ model: "agent" }));
      const routingPath = path.join(tmpDir, "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl");
      fs.mkdirSync(path.dirname(routingPath), { recursive: true });
      fs.writeFileSync(
        routingPath,
        JSON.stringify({
          _schema: "model-routing/v0.28.44",
          stage: "selection",
          selected_model: "gpt-5.6-luna",
          wire_model: "gpt-5.6-luna",
          selected_effort: "none",
          endpoint: "/responses",
          mode: "balanced",
          router: { version: "0.1.3" },
        })
      );

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8"))).toMatchObject({
        model: "gpt-5.6-luna",
        requested_model: "agent",
        model_routing: { status: "selected", applied_effort: "off", wire_model: "gpt-5.6-luna" },
      });
      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "agent/aw_info.json"), "utf8")).model_routing.status).toBe("selected");
      expect(mockCore.setOutput).toHaveBeenCalledWith("model", "gpt-5.6-luna");
      expect(mockCore.setOutput).toHaveBeenCalledWith("model_effort", "off");
      expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "selected");
    });

    test("overwrites the agent aw_info artifact with runner metadata without routing", () => {
      const runnerInfo = { engine_id: "claude", model: "runner-model" };
      const agentInfoPath = path.join(tmpDir, "agent/aw_info.json");
      fs.writeFileSync(path.join(tmpDir, "aw_info.json"), JSON.stringify(runnerInfo));
      fs.mkdirSync(path.dirname(agentInfoPath), { recursive: true });
      fs.writeFileSync(agentInfoPath, JSON.stringify({ model_routing: { status: "selected", wire_model: "forged-model" } }));
      const runtimeInfoPath = path.join(tmpDir, "runtime/aw_info.json");
      fs.mkdirSync(path.dirname(runtimeInfoPath), { recursive: true });
      fs.writeFileSync(runtimeInfoPath, JSON.stringify({ model: "forged-model", model_selection: { status: "observed", effective_model: "forged-model" } }));

      persistAgentAwInfoCopy(tmpDir);

      expect(JSON.parse(originalReadFileSync(agentInfoPath, "utf8"))).toEqual(runnerInfo);
      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "runtime/aw_info.json"), "utf8"))).toEqual(runnerInfo);
    });

    test("rejects a symlinked runtime directory without writing its target", () => {
      fs.writeFileSync(path.join(tmpDir, "aw_info.json"), JSON.stringify({ model: "runner-model" }));
      const target = path.join(tmpDir, "other");
      fs.mkdirSync(target);
      fs.writeFileSync(path.join(target, "aw_info.json"), "untouched");
      fs.symlinkSync(target, path.join(tmpDir, "runtime"), "dir");
      persistAgentAwInfoCopy(tmpDir);
      expect(originalReadFileSync(path.join(target, "aw_info.json"), "utf8")).toBe("untouched");
      expect(originalExistsSync(path.join(tmpDir, "runtime"))).toBe(false);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("symlinked runtime"));
    });

    test("replaces a symlinked runtime file without writing its target", () => {
      const runnerInfo = { model: "runner-model" };
      fs.writeFileSync(path.join(tmpDir, "aw_info.json"), JSON.stringify(runnerInfo));
      const target = path.join(tmpDir, "other.json");
      fs.writeFileSync(target, "untouched");
      fs.mkdirSync(path.join(tmpDir, "runtime"));
      const runtimeInfoPath = path.join(tmpDir, "runtime/aw_info.json");
      fs.symlinkSync(target, runtimeInfoPath);
      persistAgentAwInfoCopy(tmpDir);
      expect(originalReadFileSync(target, "utf8")).toBe("untouched");
      expect(fs.lstatSync(runtimeInfoPath).isSymbolicLink()).toBe(false);
      expect(JSON.parse(originalReadFileSync(runtimeInfoPath, "utf8"))).toEqual(runnerInfo);
    });

    test("removes stale runtime metadata when the source is missing or symlinked", () => {
      const runtimeInfoPath = path.join(tmpDir, "runtime/aw_info.json");
      fs.mkdirSync(path.dirname(runtimeInfoPath), { recursive: true });
      fs.writeFileSync(runtimeInfoPath, '{"model":"forged-model"}');
      persistAgentAwInfoCopy(tmpDir);
      expect(originalExistsSync(runtimeInfoPath)).toBe(false);
      const target = path.join(tmpDir, "other.json");
      fs.writeFileSync(target, '{"model":"forged-model"}');
      fs.symlinkSync(target, path.join(tmpDir, "aw_info.json"));
      fs.writeFileSync(runtimeInfoPath, '{"model":"forged-model"}');
      persistAgentAwInfoCopy(tmpDir);
      expect(originalExistsSync(runtimeInfoPath)).toBe(false);
      expect(mockCore.warning).toHaveBeenCalled();
    });

    test.each([
      ["substituted", "claude-sonnet-5", "", "claude-sonnet-5"],
      ["unavailable", "", "", ""],
      ["observed", "gemini-3.8-flash", "claude-sonnet-5", "claude-sonnet-5"],
    ])("preserves %s selection through postrun routing persistence and artifact copy", async (status, effectiveModel, fallbackModel, expectedModel) => {
      process.env.GH_AW_TMP_DIR = tmpDir;
      process.env.GH_AW_MODEL_ROUTING_ENABLED = "true";
      process.env.GH_AW_ENGINE_ID = "copilot";
      process.env.GH_AW_ENGINE_MODEL = "gemini-3.8-flash";
      const selection = { status, configured_model: "gemini-3.8-flash", resolved_model: status === "unavailable" ? "" : "gemini-3.8-flash", effective_model: effectiveModel };
      const selectionPath = path.join(tmpDir, "sandbox/agent/logs/copilot-model-selection.json");
      fs.mkdirSync(path.dirname(selectionPath), { recursive: true });
      fs.writeFileSync(selectionPath, JSON.stringify(selection));
      fs.writeFileSync(
        path.join(tmpDir, "aw_info.json"),
        JSON.stringify({
          model: "gemini-3.8-flash",
          ...(fallbackModel ? { fallback_model: fallbackModel } : {}),
        })
      );
      const routingPath = path.join(tmpDir, "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl");
      fs.mkdirSync(path.dirname(routingPath), { recursive: true });
      fs.writeFileSync(
        routingPath,
        JSON.stringify({
          _schema: "model-routing/v0.28.44",
          stage: "selection",
          selected_model: "gemini-3.8-flash",
          wire_model: "gemini-3.8-flash",
          endpoint: "/chat/completions",
        })
      );
      fs.writeFileSync(
        path.join(path.dirname(routingPath), "token-usage.jsonl"),
        JSON.stringify({
          _schema: "token-usage/v1",
          model: fallbackModel || "gpt-5.4",
          provider: "openai",
          model_fallback: { model: fallbackModel || "gpt-5.4" },
          status: 200,
          input_tokens: 10,
          output_tokens: 5,
        })
      );

      await main();

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      for (const relative of ["aw_info.json", "agent/aw_info.json", "runtime/aw_info.json"]) {
        expect(JSON.parse(originalReadFileSync(path.join(tmpDir, relative), "utf8"))).toMatchObject({
          model: expectedModel,
          requested_model: "gemini-3.8-flash",
          model_selection: selection,
          model_routing: { status: "selected", wire_model: "gemini-3.8-flash" },
        });
      }
    });

    test("persists Copilot observations without routing or usage telemetry before snapshotting runner metadata", async () => {
      process.env.GH_AW_TMP_DIR = tmpDir;
      const infoPath = path.join(tmpDir, "aw_info.json");
      fs.writeFileSync(infoPath, JSON.stringify({ engine_id: "copilot", model: "auto", workflow_name: "runner-owned" }));
      const selectionPath = path.join(tmpDir, "sandbox/agent/logs/copilot-model-selection.json");
      fs.mkdirSync(path.dirname(selectionPath), { recursive: true });
      fs.writeFileSync(selectionPath, JSON.stringify({ status: "observed", configured_model: "auto", resolved_model: "auto", effective_model: "gpt-6.1-sol" }));

      await main();

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      for (const relative of ["aw_info.json", "agent/aw_info.json", "runtime/aw_info.json"]) {
        expect(JSON.parse(originalReadFileSync(path.join(tmpDir, relative), "utf8"))).toMatchObject({
          model: "gpt-6.1-sol",
          requested_model: "auto",
          workflow_name: "runner-owned",
          model_selection: { status: "observed", effective_model: "gpt-6.1-sol" },
        });
      }
    });

    describe("routed Copilot CLI run 39147795806", () => {
      // Unmodified records from the run's agent artifact (JSON reformatted only).
      const fixtureDir = path.join(__dirname, "fixtures", "copilot-routed-run-39147795806");
      const realSelection = fs.readFileSync(path.join(fixtureDir, "copilot-model-selection.json"), "utf8");

      function setupRoutedRun(selectionContent) {
        process.env.GH_AW_TMP_DIR = tmpDir;
        process.env.GH_AW_MODEL_ROUTING_ENABLED = "true";
        process.env.GH_AW_ENGINE_ID = "copilot";
        process.env.GITHUB_STEP_SUMMARY = path.join(tmpDir, "step-summary.md");
        const proxyLogs = path.join(tmpDir, "sandbox/firewall/logs/api-proxy-logs");
        fs.mkdirSync(proxyLogs, { recursive: true });
        fs.mkdirSync(path.join(tmpDir, "agent"), { recursive: true });
        fs.copyFileSync(path.join(fixtureDir, "aw_info.json"), path.join(tmpDir, "aw_info.json"));
        fs.copyFileSync(path.join(fixtureDir, "awf-routing-outcome.json"), path.join(tmpDir, "agent/awf-routing-outcome.json"));
        fs.copyFileSync(path.join(fixtureDir, "model-routing.jsonl"), path.join(proxyLogs, "model-routing.jsonl"));
        fs.copyFileSync(path.join(fixtureDir, "token-usage.jsonl"), path.join(proxyLogs, "token-usage.jsonl"));
        const selectionPath = path.join(tmpDir, "sandbox/agent/logs/copilot-model-selection.json");
        fs.mkdirSync(path.dirname(selectionPath), { recursive: true });
        fs.writeFileSync(selectionPath, selectionContent);
      }

      function expectRoutingAndUsage() {
        for (const relative of ["aw_info.json", "agent/aw_info.json", "runtime/aw_info.json"]) {
          expect(JSON.parse(originalReadFileSync(path.join(tmpDir, relative), "utf8"))).toMatchObject({
            engine_id: "copilot",
            model: "gpt-5.6-luna",
            requested_model: "auto",
            model_routing: { status: "selected", wire_model: "gpt-5.6-luna", effort: "max", endpoint: "/responses" },
          });
        }
        const summaryText = originalReadFileSync(path.join(tmpDir, "step-summary.md"), "utf8");
        expect(summaryText).toContain(DEFAULT_SUMMARY_TITLE);
        expect(summaryText).toContain("| **Total** | | **95,891** | **1,299** |");
        expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "agent_usage.json"), "utf8"))).toMatchObject({
          primary_model: "gpt-5.6-luna",
          model_routing: { status: "selected", wire_model: "gpt-5.6-luna" },
        });
        expect(mockCore.setOutput).toHaveBeenCalledWith("model", "gpt-5.6-luna");
        expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "selected");
        expect(mockCore.setOutput).toHaveBeenCalledWith("primary_model", "gpt-5.6-luna");
        expect(mockCore.setOutput).toHaveBeenCalledWith("aic", expect.any(String));
      }

      test.each([
        ["the real empty configured_model record", realSelection, "Invalid Copilot model selection"],
        ["an empty file", "", "Unexpected end of JSON input"],
        ["malformed JSON", "{", "JSON"],
      ])("records routing and usage when Copilot model selection persistence fails (%s)", async (_name, selectionContent, diagnostic) => {
        setupRoutedRun(selectionContent);

        await main();

        expect(mockCore.setFailed).toHaveBeenCalledTimes(1);
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringMatching(/^ERR_PARSE: Cannot record Copilot model selection: /));
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(diagnostic));
        expectRoutingAndUsage();
        for (const relative of ["aw_info.json", "agent/aw_info.json", "runtime/aw_info.json"]) {
          expect(JSON.parse(originalReadFileSync(path.join(tmpDir, relative), "utf8"))).not.toHaveProperty("model_selection");
        }
      });

      test("persists the corrected routed selection with routing and usage", async () => {
        setupRoutedRun(JSON.stringify({ ...JSON.parse(realSelection), configured_model: "auto" }));

        await main();

        expect(mockCore.setFailed).not.toHaveBeenCalled();
        expectRoutingAndUsage();
        for (const relative of ["aw_info.json", "agent/aw_info.json", "runtime/aw_info.json"]) {
          expect(JSON.parse(originalReadFileSync(path.join(tmpDir, relative), "utf8")).model_selection).toEqual({
            status: "observed",
            configured_model: "auto",
            resolved_model: "gpt-5.6-luna",
            effective_model: "gpt-5.6-luna",
          });
        }
      });
    });

    test("does not trust selected routing from sandbox-writable reflect data", async () => {
      process.env.GH_AW_TMP_DIR = tmpDir;
      process.env.GH_AW_MODEL_ROUTING_ENABLED = "true";
      process.env.GH_AW_ENGINE_ID = "pi";
      process.env.GH_AW_ENGINE_MODEL = "agent-placeholder";
      fs.writeFileSync(path.join(tmpDir, "aw_info.json"), JSON.stringify({ model: "agent-placeholder" }));
      const reflectPath = path.join(tmpDir, "agent/awf-reflect.json");
      fs.mkdirSync(path.dirname(reflectPath), { recursive: true });
      fs.writeFileSync(
        reflectPath,
        JSON.stringify({
          endpoints: [{ provider: "github", configured: true, models: ["forged-model"], routing_models: [{ model_id: "forged-model" }] }],
          routing: {
            status: "selected",
            mode: "balanced",
            selection: { provider: "github", model: "forged-model", wire_model: "forged-model", effort: "high", endpoint: "/responses" },
          },
        })
      );

      await main();

      expect(JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8"))).toMatchObject({
        model: "agent-placeholder",
        model_routing: { status: "rejected", failure_code: "uncorroborated_selection" },
      });
      expect(mockCore.setOutput).toHaveBeenCalledWith("model", "");
      expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "rejected");
    });

    describe("proxy failure after a routed selection", () => {
      const goldenDir = path.join(__dirname, "../../../pkg/cli/testdata/model_routing_golden");
      const routingLogPath = "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl";

      /** @param {string} fixture */
      function readFixtureRoutingRecords(fixture) {
        return originalReadFileSync(path.join(goldenDir, fixture, routingLogPath), "utf8")
          .split("\n")
          .filter(line => line.trim())
          .map(line => JSON.parse(line));
      }

      /** @param {string} fixture */
      function readFixtureAdvisory(fixture) {
        return JSON.parse(originalReadFileSync(path.join(goldenDir, fixture, "agent/awf-routing-outcome.json"), "utf8"));
      }

      /**
       * @param {{engine?: string, records: any[], advisory?: any, mirrors?: string[]}} options
       */
      function setupRoutingRun({ engine = "copilot", records, advisory, mirrors = [] }) {
        process.env.GH_AW_TMP_DIR = tmpDir;
        process.env.GH_AW_MODEL_ROUTING_ENABLED = "true";
        process.env.GH_AW_ENGINE_ID = engine;
        process.env.GH_AW_ENGINE_MODEL = "auto";
        fs.writeFileSync(path.join(tmpDir, "aw_info.json"), JSON.stringify({ model: "auto" }));
        const content = records.map(record => JSON.stringify(record)).join("\n") + "\n";
        for (const relativePath of [routingLogPath, ...mirrors]) {
          const routingPath = path.join(tmpDir, relativePath);
          fs.mkdirSync(path.dirname(routingPath), { recursive: true });
          fs.writeFileSync(routingPath, content);
        }
        if (advisory) {
          fs.mkdirSync(path.join(tmpDir, "agent"), { recursive: true });
          fs.writeFileSync(path.join(tmpDir, "agent/awf-routing-outcome.json"), JSON.stringify(advisory));
        }
      }

      function readRecordedRouting() {
        const info = JSON.parse(originalReadFileSync(path.join(tmpDir, "aw_info.json"), "utf8")).model_routing;
        const agentInfo = JSON.parse(originalReadFileSync(path.join(tmpDir, "agent/aw_info.json"), "utf8")).model_routing;
        expect(agentInfo).toEqual(info);
        return info;
      }

      const budgetFailedRouting = {
        status: "failed",
        source: "awf-routing",
        failure_code: "ai_credits_limit_exceeded",
        wire_model: "gpt-5.6-sol",
        model: "github-copilot/gpt-5.6-sol",
        effort: "medium",
        endpoint: "/responses",
        mode: "economy",
        provider: "copilot",
        selected_id: "choice-0004",
        router_version: "0.1.3",
      };

      test("keeps the routed selection when the AI-credit budget fails after selection", async () => {
        setupRoutingRun({ records: readFixtureRoutingRecords("copilot-budget-exhausted-steering"), advisory: readFixtureAdvisory("copilot-budget-exhausted-steering") });

        await main();

        const routing = readRecordedRouting();
        expect(routing).toMatchObject(budgetFailedRouting);
        expect(JSON.stringify(routing)).not.toContain("harness_selection_mismatch");
        expect(JSON.stringify(routing)).not.toContain("Harness routing outcome did not match the proxy selection");
        expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "failed");
      });

      test("keeps the routed selection when mirrored proxy logs repeat the failure", async () => {
        setupRoutingRun({
          records: readFixtureRoutingRecords("copilot-budget-exhausted-steering"),
          advisory: readFixtureAdvisory("copilot-budget-exhausted-steering"),
          mirrors: ["sandbox/firewall/audit/api-proxy-logs/model-routing.jsonl", "sandbox/firewall-audit-logs/api-proxy-logs/model-routing.jsonl"],
        });

        await main();

        expect(readRecordedRouting()).toMatchObject(budgetFailedRouting);
        expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "failed");
      });

      test.each([{ mirrors: [] }, { mirrors: ["sandbox/firewall/audit/api-proxy-logs/model-routing.jsonl", "sandbox/firewall-audit-logs/api-proxy-logs/model-routing.jsonl"] }])(
        "clears an earlier failure when an identical selection recurs within a log (mirrors: $mirrors)",
        async ({ mirrors }) => {
          const records = readFixtureRoutingRecords("copilot-budget-exhausted-steering");
          const selection = { ...records.find(record => record.stage === "selection") };
          delete selection.timestamp;
          delete selection.interaction_id;
          const failure = records.find(record => record.stage === "failure");
          setupRoutingRun({ records: [selection, failure, selection], advisory: readFixtureAdvisory("copilot-budget-exhausted-steering"), mirrors });

          await main();

          const expectedRouting = { ...budgetFailedRouting, status: "selected" };
          delete expectedRouting.failure_code;
          const routing = readRecordedRouting();
          expect(routing).toMatchObject(expectedRouting);
          expect(routing).not.toHaveProperty("failure_code");
          expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "selected");
        }
      );

      test("still rejects a mismatched harness outcome when a proxy failure follows selection", async () => {
        const records = readFixtureRoutingRecords("copilot-budget-exhausted-steering");
        setupRoutingRun({ records, advisory: { ...readFixtureAdvisory("copilot-budget-exhausted-steering"), wire_model: "gpt-5.6-luna" } });

        await main();

        expect(readRecordedRouting()).toMatchObject({
          status: "rejected",
          failure_code: "harness_selection_mismatch",
          detail: "Harness routing outcome did not match the proxy selection",
        });
        expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "rejected");
      });

      test("keeps pre-agent routing failures without a selection unchanged", async () => {
        const fixtureFailure = readFixtureRoutingRecords("copilot-budget-exhausted-steering").find(record => record.stage === "failure");
        const records = [{ ...fixtureFailure, code: "no_route" }];
        setupRoutingRun({ records });

        await main();

        const routing = readRecordedRouting();
        expect(routing).toMatchObject({ status: "failed", failure_code: "no_route" });
        for (const key of ["model", "wire_model", "effort", "endpoint", "mode"]) {
          expect(routing).not.toHaveProperty(key);
        }
        expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "failed");

        mockCore.setOutput.mockClear();
        setupRoutingRun({ records, advisory: readFixtureAdvisory("copilot-budget-exhausted-steering") });

        await main();

        expect(readRecordedRouting()).toMatchObject({ status: "rejected", failure_code: "harness_selection_mismatch" });
        expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "rejected");
      });

      test("keeps the selection and takes the failure code when no harness outcome exists", async () => {
        setupRoutingRun({ records: readFixtureRoutingRecords("copilot-budget-exhausted-steering") });

        await main();

        expect(readRecordedRouting()).toMatchObject(budgetFailedRouting);
        expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "failed");
      });

      test("keeps the codex routed selection when the provider becomes unavailable", async () => {
        const records = readFixtureRoutingRecords("codex-routed-provider-unavailable");
        const selection = records.find(record => record.stage === "selection");
        setupRoutingRun({ engine: "codex", records });

        await main();

        expect(readRecordedRouting()).toMatchObject({
          status: "failed",
          source: "awf-routing",
          failure_code: "provider_unavailable",
          wire_model: "gpt-5.6-luna",
          model: selection.selected_model,
          effort: "high",
          endpoint: "/responses",
          mode: selection.mode,
          provider: "copilot",
          selected_id: selection.selected_id,
          router_version: selection.router.version,
        });
        expect(mockCore.setOutput).toHaveBeenCalledWith("model_routing_status", "failed");
      });
    });

    test("skips summary when token usage file does not exist", async () => {
      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No token usage data found"));
      expect(mockCore.summary.addDetails).not.toHaveBeenCalled();
      expect(mockCore.summary.write).not.toHaveBeenCalled();
    });

    test("reads and reports token usage from the configured ARC/DinD directory", async () => {
      const ghAwDir = path.join(tmpDir, "runner-temp", "gh-aw");
      const tokenUsagePath = path.join(ghAwDir, "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl");
      fs.mkdirSync(path.dirname(tokenUsagePath), { recursive: true });
      fs.writeFileSync(
        tokenUsagePath,
        JSON.stringify({
          model: "gpt-4o-mini-2024-07-18",
          provider: "copilot",
          input_tokens: 19288,
          output_tokens: 35,
          ai_credits_this_response: 0.29142,
          ai_credits_total: 0.29142,
        })
      );
      process.env.GH_AW_TMP_DIR = ghAwDir;

      await main();

      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", "0.29142");
      expect(mockCore.setOutput).toHaveBeenCalledWith("aic", "0.29142");
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("0.29142"), true);
      expect(JSON.parse(originalReadFileSync(path.join(ghAwDir, "agent_usage.json"), "utf8")).ai_credits).toBe(0.29142);
    });

    test("writes explicit zero usage evidence when requested and no usage exists", async () => {
      const usagePath = path.join(tmpDir, "detection_usage.json");
      const usageJSONLPath = path.join(tmpDir, "detection_usage.jsonl");
      process.env.GH_AW_AGENT_USAGE_PATH = usagePath;
      process.env.GH_AW_AGENT_USAGE_JSONL_PATH = usageJSONLPath;
      process.env.GH_AW_WRITE_EMPTY_USAGE = "true";

      await main(path.join(tmpDir, "missing-session-state"));

      expect(JSON.parse(originalReadFileSync(usagePath, "utf8"))).toEqual({
        input_tokens: 0,
        output_tokens: 0,
        ai_credits: 0,
      });
      expect(JSON.parse(originalReadFileSync(usageJSONLPath, "utf8"))).toEqual({
        provider: "unknown",
        ai_credits: 0,
      });
    });

    test.each(["agent", "detection"])("persists AWF actual-model attribution for the %s phase", async phase => {
      const tokenUsagePath = path.join(tmpDir, "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl");
      const infoPath = path.join(tmpDir, "aw_info.json");
      fs.mkdirSync(path.dirname(tokenUsagePath), { recursive: true });
      fs.writeFileSync(infoPath, JSON.stringify({ model: "primary", fallback_model: "agent-recovery" }));
      fs.writeFileSync(
        tokenUsagePath,
        JSON.stringify({
          _schema: "token-usage/v0.28.44",
          status: 200,
          model: "secondary",
          provider: "copilot",
          input_tokens: 100,
          output_tokens: 20,
          ai_credits_this_response: 1,
          model_fallback: { requested_model: "primary", model: "secondary", status: 503, attempt: 1 },
        })
      );
      process.env.GH_AW_TMP_DIR = tmpDir;
      process.env.GH_AW_PHASE = phase;

      await main();

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      const usage = JSON.parse(originalReadFileSync(path.join(tmpDir, "agent_usage.json"), "utf8"));
      expect(usage.primary_model).toBe("secondary");
      const info = JSON.parse(originalReadFileSync(infoPath, "utf8"));
      expect(info[phase === "agent" ? "fallback_model" : "detection_fallback_model"]).toBe("secondary");
      if (phase === "detection") {
        expect(info.model).toBe("primary");
        expect(info.fallback_model).toBe("agent-recovery");
      }
    });

    test("writes authoritative Copilot checkpoint usage when proxy usage is unavailable", async () => {
      const sessionStateDir = path.join(tmpDir, "copilot-session-state");
      const sessionDir = path.join(sessionStateDir, "session-1");
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");
      const agentUsageJSONLFile = path.join(tmpDir, "agent_usage.jsonl");
      fs.mkdirSync(sessionDir, { recursive: true });
      fs.writeFileSync(
        path.join(sessionDir, "events.jsonl"),
        JSON.stringify({
          type: "session.usage_checkpoint",
          data: {
            totalNanoAiu: 451090000000,
            totalPremiumRequests: 1,
            modelCacheState: [{ modelId: "claude-sonnet-5" }],
          },
          timestamp: "2026-09-11T01:00:00Z",
        })
      );
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) return originalWriteFileSync(agentUsageFile, data);
        if (p === AGENT_USAGE_JSONL_PATH) return originalWriteFileSync(agentUsageJSONLFile, data);
        return originalWriteFileSync(p, data);
      });

      await main(sessionStateDir);

      expect(JSON.parse(originalReadFileSync(agentUsageFile, "utf8"))).toEqual(
        expect.objectContaining({
          ai_credits: 451.09,
          premium_requests: 1,
        })
      );
      expect(JSON.parse(originalReadFileSync(agentUsageFile, "utf8"))).not.toHaveProperty("input_tokens");
      expect(JSON.parse(originalReadFileSync(agentUsageJSONLFile, "utf8"))).toEqual({
        provider: "copilot",
        ai_credits: 451.09,
        premium_requests: 1,
      });
      expect(mockCore.setOutput).toHaveBeenCalledWith("aic", "451.09");
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Premium Requests"), true);
    });

    test("skips summary when token usage file is empty", async () => {
      const emptyFile = path.join(tmpDir, "token-usage.jsonl");
      fs.writeFileSync(emptyFile, "");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });

      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: 0 };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No token usage data found"));
      expect(mockCore.summary.addDetails).not.toHaveBeenCalled();
    });

    test("uses the Copilot checkpoint when proxy usage has no valid entries", async () => {
      const sessionStateDir = path.join(tmpDir, "copilot-session-state");
      const sessionDir = path.join(sessionStateDir, "session-1");
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");
      const agentUsageJSONLFile = path.join(tmpDir, "agent_usage.jsonl");
      fs.mkdirSync(sessionDir, { recursive: true });
      fs.writeFileSync(
        path.join(sessionDir, "events.jsonl"),
        JSON.stringify({
          type: "session.usage_checkpoint",
          data: { totalNanoAiu: 1000000000, totalPremiumRequests: 2 },
          timestamp: "2026-09-11T01:00:00Z",
        })
      );
      fs.existsSync = vi.fn(p => p === TOKEN_USAGE_PATH || originalExistsSync(p));
      fs.statSync = vi.fn(p => (p === TOKEN_USAGE_PATH ? { size: 1 } : originalStatSync(p)));
      fs.readFileSync = vi.fn((p, enc) => (p === TOKEN_USAGE_PATH ? "malformed" : originalReadFileSync(p, enc)));
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) return originalWriteFileSync(agentUsageFile, data);
        if (p === AGENT_USAGE_JSONL_PATH) return originalWriteFileSync(agentUsageJSONLFile, data);
        return originalWriteFileSync(p, data);
      });

      await main(sessionStateDir);

      expect(JSON.parse(originalReadFileSync(agentUsageFile, "utf8"))).toMatchObject({ ai_credits: 1, premium_requests: 2 });
      expect(mockCore.setOutput).toHaveBeenCalledWith("aic", "1");
    });

    test("returns the latest valid checkpoint across Copilot sessions", () => {
      const sessionStateDir = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-usage-checkpoint-test-"));
      try {
        const firstSession = path.join(sessionStateDir, "session-1");
        const secondSession = path.join(sessionStateDir, "session-2");
        fs.mkdirSync(firstSession);
        fs.mkdirSync(secondSession);
        fs.writeFileSync(
          path.join(firstSession, "events.jsonl"),
          [
            "not-json",
            JSON.stringify({
              type: "session.usage_checkpoint",
              data: { totalNanoAiu: 1000000000, totalPremiumRequests: 1 },
              timestamp: "2026-09-11T00:00:00Z",
            }),
          ].join("\n")
        );
        fs.writeFileSync(
          path.join(secondSession, "events.jsonl"),
          JSON.stringify({
            type: "session.usage_checkpoint",
            data: {
              totalNanoAiu: 451090000000,
              totalPremiumRequests: 2,
              modelCacheState: [{ modelId: "claude-sonnet-5" }],
            },
            timestamp: "2026-09-11T01:00:00Z",
          })
        );

        expect(findCopilotUsageCheckpoint(sessionStateDir)).toEqual({
          aiCredits: 451.09,
          premiumRequests: 2,
        });
      } finally {
        fs.rmSync(sessionStateDir, { recursive: true, force: true });
      }
    });

    test("writes token usage details section to summary", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: singleEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return singleEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("<summary>Token Usage</summary>"), true);
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("| Alias |"), true);
      expect(mockCore.summary.write).toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Token usage summary appended"));
      // Token table should also be rendered to core.info
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Token Usage"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Alias"));
    });

    test("keeps threat-detection title and rows with AWF-reported credits", async () => {
      process.env.GH_AW_TOKEN_USAGE_SUMMARY_TITLE = "Threat Detection Token Usage";
      const reportedEntry = JSON.stringify({
        model: "gpt-4o-mini-2024-07-18",
        provider: "copilot",
        input_tokens: 19288,
        output_tokens: 35,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        duration_ms: 2242,
        ai_credits_this_response: 0.29142,
        ai_credits_total: 0.29142,
      });

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: reportedEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return reportedEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });

      await main();

      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("<summary>Threat Detection Token Usage</summary>"), true);
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("gpt40mini"), true);
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("0.29142"), true);
    });

    test("appends token usage section to GITHUB_STEP_SUMMARY when configured", async () => {
      const stepSummaryPath = path.join(tmpDir, "step-summary.md");
      process.env.GITHUB_STEP_SUMMARY = stepSummaryPath;
      fs.appendFileSync = vi.fn((...args) => originalAppendFileSync(...args));

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: singleEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return singleEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });

      await main();

      const stepSummary = originalReadFileSync(stepSummaryPath, "utf8");
      expect(stepSummary).toContain("<summary>Token Usage</summary>");
      expect(stepSummary).toContain("Per-request AI credits and token totals");
      expect(stepSummary).toContain("Working-Set Rebuild Factor (WSRF): 1.00× (measured)");
      expect(stepSummary).toContain("- Cumulative input tokens: 100");
      expect(stepSummary).toContain("| ΔAI Credits | AI Credits |");
      expect(fs.appendFileSync).toHaveBeenCalledWith(stepSummaryPath, expect.any(String), "utf8");
      expect(mockCore.summary.addRaw).not.toHaveBeenCalled();
      expect(mockCore.summary.write).not.toHaveBeenCalled();
    });

    test("writes agent_usage.json with aggregated token totals and primary_model", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: singleEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return singleEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      expect(fs.existsSync(agentUsageFile)).toBe(true);
      const agentUsage = JSON.parse(fs.readFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.input_tokens).toBe(100);
      expect(agentUsage.output_tokens).toBe(200);
      expect(agentUsage.cache_read_tokens).toBe(5000);
      expect(agentUsage.cache_write_tokens).toBe(3000);
      expect(agentUsage.ambient_context).toBe(900);
      expect(typeof agentUsage.ai_credits).toBe("number");
      // primary_model is the actual model from token-usage data (not a user alias)
      expect(agentUsage.primary_model).toBe("claude-sonnet-4-6");
      // GH_AW_PRIMARY_MODEL is exported so footer attribution can use the real model name
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PRIMARY_MODEL", "claude-sonnet-4-6");
      expect(mockCore.setOutput).toHaveBeenCalledWith("primary_model", "claude-sonnet-4-6");
    });

    test("writes the exact AWF-reported AIC total without repricing", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");
      const fixtureContent = originalReadFileSync(path.join(__dirname, "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"), "utf8");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: fixtureContent.length };
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return fixtureContent;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const agentUsage = JSON.parse(originalReadFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.ai_credits).toBe(1.03602);
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", "1.03602");
      expect(mockCore.setOutput).toHaveBeenCalledWith("aic", "1.03602");
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("1.03602"));
    });

    test("exports AWF-reported zero AIC instead of treating it as missing", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");
      const zeroEntry = JSON.stringify({
        model: "gpt-4o-mini-2024-07-18",
        provider: "copilot",
        input_tokens: 1000,
        output_tokens: 100,
        ai_credits_this_response: 0,
        ai_credits_total: 0,
      });

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: zeroEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return zeroEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const agentUsage = JSON.parse(originalReadFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.ai_credits).toBe(0);
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", "0");
      expect(mockCore.setOutput).toHaveBeenCalledWith("aic", "0");
    });

    test.each([undefined, 0])("preserves unknown versus reported zero cost through parsing and OTLP export: %s", async reportedAIC => {
      process.env.GH_AW_TMP_DIR = tmpDir;
      const tokenUsagePath = path.join(tmpDir, "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl");
      fs.mkdirSync(path.dirname(tokenUsagePath), { recursive: true });
      fs.writeFileSync(
        tokenUsagePath,
        JSON.stringify({
          model: "unpriced-regression-model",
          provider: "unknown-provider",
          input_tokens: 1000,
          output_tokens: 100,
          ai_credits_this_response: reportedAIC,
          ai_credits_total: reportedAIC,
        })
      );

      await main(path.join(tmpDir, "missing-session-state"));

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      const usageJSON = originalReadFileSync(path.join(tmpDir, "agent_usage.json"), "utf8");
      const usage = JSON.parse(usageJSON);
      expect(usage.input_tokens).toBe(1000);
      if (reportedAIC === undefined) {
        expect(usage).not.toHaveProperty("ai_credits");
        expect(mockCore.exportVariable).not.toHaveBeenCalledWith("GH_AW_AIC", expect.anything());
      } else {
        expect(usage.ai_credits).toBe(0);
        expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", "0");
      }

      const { sendJobConclusionSpan } = require("./send_otlp_span.cjs");
      vi.stubEnv("INPUT_JOB_NAME", "agent");
      vi.stubEnv("GH_AW_AIC", "");
      vi.stubEnv("GH_AW_OTLP_ENDPOINTS", "");
      const mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
      const appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
      fs.readFileSync = vi.fn(p => {
        if (p === "/tmp/gh-aw/agent_usage.json") return usageJSON;
        throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
      });
      try {
        await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });
        const spans = appendSpy.mock.calls.flatMap(([, content]) => JSON.parse(content).resourceSpans[0].scopeSpans[0].spans);
        const aicAttributes = spans.flatMap(span => span.attributes).filter(attribute => attribute.key === "gh-aw.aic");
        expect(aicAttributes).toEqual(reportedAIC === undefined ? [] : [{ key: "gh-aw.aic", value: { doubleValue: 0 } }]);
      } finally {
        mkdirSpy.mockRestore();
        appendSpy.mockRestore();
        vi.unstubAllEnvs();
      }
    });

    test("surfaces fallback accounting warnings", async () => {
      const malformedEntry = JSON.stringify({
        model: "gpt-4o-mini-2024-07-18",
        provider: "copilot",
        input_tokens: 19288,
        output_tokens: 35,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        ai_credits_this_response: null,
        ai_credits_total: null,
      });

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: malformedEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return malformedEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });

      await main();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("[ai-credits]"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("fallback accounting"));
    });

    test("handles multiple model entries", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: multiEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return multiEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const summaryCall = mockCore.summary.addRaw.mock.calls[0];
      expect(summaryCall[0]).toContain("<summary>Token Usage</summary>");
      expectTokenUsageTableRows(summaryCall[0], [
        ["sonnet46", "100", "200"],
        ["gpt40", "50", "80"],
      ]);
      expect(summaryCall[0]).toContain("**Total**");

      const agentUsage = JSON.parse(fs.readFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.input_tokens).toBe(150);
      expect(agentUsage.output_tokens).toBe(280);
    });

    test("reads token usage from firewall-audit-logs path", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return true;
        if (p === TOKEN_USAGE_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: multiEntry.length };
        if (p === TOKEN_USAGE_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return multiEntry;
        if (p === TOKEN_USAGE_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const summaryCall = mockCore.summary.addRaw.mock.calls[0];
      expectTokenUsageTableRows(summaryCall[0], [
        ["sonnet46", "100", "200"],
        ["gpt40", "50", "80"],
      ]);

      const agentUsage = JSON.parse(fs.readFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.input_tokens).toBe(150);
      expect(agentUsage.output_tokens).toBe(280);
    });

    test("deduplicates overlapping entries across audit and legacy token usage files", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");
      const sharedEntry = JSON.stringify({
        request_id: "req-shared",
        model: "claude-sonnet-4-6",
        provider: "anthropic",
        input_tokens: 100,
        output_tokens: 200,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        duration_ms: 1000,
      });
      const auditOnlyEntry = JSON.stringify({
        request_id: "req-audit",
        model: "claude-haiku-4-5",
        provider: "anthropic",
        input_tokens: 50,
        output_tokens: 75,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        duration_ms: 500,
      });
      const legacyOnlyEntry = JSON.stringify({
        request_id: "req-legacy",
        model: "gpt-4o",
        provider: "openai",
        input_tokens: 20,
        output_tokens: 30,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        duration_ms: 400,
      });

      const auditContent = [sharedEntry, auditOnlyEntry].join("\n");
      const legacyContent = [sharedEntry, legacyOnlyEntry].join("\n");

      fs.existsSync = vi.fn(p => (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH ? true : originalExistsSync(p)));
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: auditContent.length };
        if (p === TOKEN_USAGE_PATH) return { size: legacyContent.length };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return auditContent;
        if (p === TOKEN_USAGE_PATH) return legacyContent;
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const summaryCall = mockCore.summary.addRaw.mock.calls[0];
      expectTokenUsageTableRows(summaryCall[0], [
        ["sonnet46", "100", "200"],
        ["haiku45", "50", "75"],
        ["gpt40", "20", "30"],
      ]);

      const agentUsage = JSON.parse(fs.readFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.input_tokens).toBe(170);
      expect(agentUsage.output_tokens).toBe(305);
    });

    test("deduplicates mirrored AWF token usage files before writing agent_usage", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");
      const fixtureContent = originalReadFileSync(path.join(__dirname, "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"), "utf8");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AWF_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return { size: fixtureContent.length };
        if (p === TOKEN_USAGE_AWF_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return fixtureContent;
        if (p === TOKEN_USAGE_AWF_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const agentUsage = JSON.parse(originalReadFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.input_tokens).toBe(39376);
      expect(agentUsage.output_tokens).toBe(175);
      expect(agentUsage.ai_credits).toBe(1.03602);
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", "1.03602");
    });

    test("calls setFailed when an error is thrown", async () => {
      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: singleEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return singleEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn(p => {
        if (p === AGENT_USAGE_PATH) throw new Error("write error");
      });

      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("write error"));
    });
  });

  describe("helpers", () => {
    let originalExistsSync;
    let originalStatSync;
    let originalReadFileSync;

    beforeEach(() => {
      originalExistsSync = fs.existsSync;
      originalStatSync = fs.statSync;
      originalReadFileSync = fs.readFileSync;
      global.core = { warning: vi.fn() };
    });

    afterEach(() => {
      fs.existsSync = originalExistsSync;
      fs.statSync = originalStatSync;
      fs.readFileSync = originalReadFileSync;
      delete global.core;
    });

    test("extractRequestId reads request_id without parsing JSON", () => {
      expect(extractRequestId('{"request_id":"req-123","model":"m"}')).toBe("req-123");
      expect(extractRequestId('{"model":"m"}')).toBe("");
    });

    test("extractTokenUsageDedupeKey includes event and request_id", () => {
      expect(extractTokenUsageDedupeKey('{"event":"token_usage","request_id":"req-123","model":"m"}')).toBe("token_usage:req-123");
      expect(extractTokenUsageDedupeKey('{"event":"other","request_id":"req-123","model":"m"}')).toBe("other:req-123");
      expect(extractTokenUsageDedupeKey('{"request_id":"req-123","model":"m"}')).toBe("token_usage:req-123");
      expect(extractTokenUsageDedupeKey('{"model":"m"}')).toBe("");
    });

    test("getReadableTokenUsagePaths skips failing stat path and keeps valid path", () => {
      fs.existsSync = vi.fn(p => p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH);
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) throw new Error("stat fail");
        if (p === TOKEN_USAGE_PATH) return { size: 42 };
        return originalStatSync(p);
      });

      const paths = getReadableTokenUsagePaths(TOKEN_USAGE_PATHS);
      expect(paths).toEqual([TOKEN_USAGE_PATH]);
    });

    test("readDedupedTokenUsage deduplicates by request_id across files", () => {
      const fileA = '{"request_id":"req-1","model":"m1","input_tokens":1}\n{"request_id":"req-2","model":"m2","input_tokens":2}';
      const fileB = '{"request_id":"req-1","model":"m1","input_tokens":1}\n{"request_id":"req-3","model":"m3","input_tokens":3}';

      fs.readFileSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return fileA;
        if (p === TOKEN_USAGE_PATH) return fileB;
        return originalReadFileSync(p, "utf8");
      });

      const deduped = readDedupedTokenUsage([TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_PATH]);
      expect(deduped).toContain('"request_id":"req-1"');
      expect(deduped).toContain('"request_id":"req-2"');
      expect(deduped).toContain('"request_id":"req-3"');
      expect(deduped.match(/"request_id":"req-1"/g)).toHaveLength(1);
    });

    test("readDedupedTokenUsage keeps different events with the same request_id", () => {
      const fileA = '{"event":"token_usage","request_id":"req-1","model":"m1","input_tokens":1}';
      const fileB = '{"event":"token_steering","request_id":"req-1","model":"m1","input_tokens":2}';

      fs.readFileSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return fileA;
        if (p === TOKEN_USAGE_PATH) return fileB;
        return originalReadFileSync(p, "utf8");
      });

      const deduped = readDedupedTokenUsage([TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_PATH]);
      expect(deduped).toContain('"event":"token_usage"');
      expect(deduped).toContain('"event":"token_steering"');
      expect(deduped.match(/"request_id":"req-1"/g)).toHaveLength(2);
    });

    test("deduplicates mirrored AWF records before aggregating reported credits", () => {
      const fixture = originalReadFileSync(path.join(__dirname, "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"), "utf8");
      fs.readFileSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return fixture;
        return originalReadFileSync(p, "utf8");
      });

      const deduped = readDedupedTokenUsage([TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_PATH]);
      const { parseTokenUsageJsonl } = require("./parse_mcp_gateway_log.cjs");
      const summary = parseTokenUsageJsonl(deduped);

      expect(summary.totalRequests).toBe(5);
      expect(summary.totalAIC).toBe(1.03602);
    });

    test("getSummaryTitle returns trimmed env title", () => {
      process.env.GH_AW_TOKEN_USAGE_SUMMARY_TITLE = "  Threat Detection Token Usage  ";
      expect(getSummaryTitle()).toBe("Threat Detection Token Usage");
    });

    test("getSummaryTitle falls back to default title", () => {
      delete process.env.GH_AW_TOKEN_USAGE_SUMMARY_TITLE;
      expect(getSummaryTitle()).toBe("Token Usage");
    });

    test("buildStepSummarySection wraps markdown in a heading and details block", () => {
      const section = buildStepSummarySection("Token Usage", "| Alias |\n| --- |");
      expect(section).toContain("<details>");
      expect(section).toContain("<summary>Token Usage</summary>");
      expect(section).toContain("Per-request AI credits and token totals");
    });

    test("buildStepSummarySection emits the WSRF block after the token usage details block", () => {
      const section = buildStepSummarySection("Token Usage", "| Alias |\n| --- |\n", {
        measurement_state: "measured",
        rebuild_factor: 1,
        cumulative_input_tokens: 100,
        peak_input_tokens: 100,
        rebuild_excess_tokens: 0,
        invocations: 1,
      });

      const tableIndex = section.indexOf("| Alias |");
      const closingIndex = section.indexOf("</details>");
      const wsrfIndex = section.indexOf("Working-Set Rebuild Factor");
      expect(tableIndex).toBeGreaterThan(-1);
      expect(tableIndex).toBeLessThan(closingIndex);
      expect(wsrfIndex).toBeGreaterThan(closingIndex);
    });

    test("buildWorkingSetDetailsSection renders measured WSRF details", () => {
      const section = buildWorkingSetDetailsSection({
        measurement_state: "measured",
        rebuild_factor: 3.9017857142857144,
        cumulative_input_tokens: 874000,
        peak_input_tokens: 224000,
        rebuild_excess_tokens: 650000,
        invocations: 5,
      });

      expect(section).toContain("Working-Set Rebuild Factor (WSRF): 3.90× (measured)");
      expect(section).toContain("- State: `measured`");
      expect(section).toContain("- Cumulative input tokens: 874,000");
      expect(section).toContain("- Peak invocation input tokens: 224,000");
      expect(section).toContain("- Rebuild excess tokens: 650,000");
      expect(section).toContain("- Invocations: 5");
    });

    test("buildWorkingSetDetailsSection renders unavailable state when no factor exists", () => {
      const section = buildWorkingSetDetailsSection({
        measurement_state: "unavailable",
        cumulative_input_tokens: 0,
        peak_input_tokens: 0,
        rebuild_excess_tokens: 0,
        invocations: 0,
      });

      expect(section).toContain("Working-Set Rebuild Factor (WSRF): unavailable (unavailable)");
      expect(section).toContain("- State: `unavailable`");
    });

    test("renderTokenTableAsPlainText strips table separator lines and pipes", () => {
      const markdown = ["| # | Alias | Input | Output |", "|--:|-------|------:|-------:|", "| 1 | sonnet46 | 100 | 200 |", "| **Total** | | **100** | **200** |", "", "Legend: `Alias` is the model shorthand.", ""].join("\n");

      const result = renderTokenTableAsPlainText("Token Usage", markdown);

      expect(result).toContain("Token Usage");
      // separator line is removed (no dash sequences that leak from separator rows)
      expect(result).not.toMatch(/---/);
      // leading/trailing pipes are stripped
      expect(result).not.toMatch(/^\|/m);
      expect(result).not.toMatch(/\|$/m);
      // bold markers are removed
      expect(result).not.toContain("**");
      // data is preserved
      expect(result).toContain("sonnet46");
      expect(result).toContain("100");
      expect(result).toContain("200");
      expect(result).toContain("Legend:");
    });

    test("renderTokenTableAsPlainText prefixes output with title", () => {
      const result = renderTokenTableAsPlainText("My Token Usage", "| A |\n|---|\n| 1 |");
      expect(result.startsWith("My Token Usage")).toBe(true);
    });
  });
});
