import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

// awf_reflect.cjs computes its default reflect-output path once at module load time from
// RUNNER_TEMP, so point it at a scratch directory before importing to avoid writing to (and
// polluting) the shared os.tmpdir() awf-reflect.json used by other test files. Restore the
// original value in afterAll since process.env is a real Node global shared by every test
// file that ends up running in the same worker process.
const originalRunnerTemp = process.env.RUNNER_TEMP;
const scratchRunnerTemp = fs.mkdtempSync(path.join(os.tmpdir(), "pi-models-json-runner-temp-"));
process.env.RUNNER_TEMP = scratchRunnerTemp;

const piModelsJson = await import("./pi_models_json.cjs");
const loadSDK = async () => ({ ModelRuntime: { create: async () => ({ getModel: () => undefined }) } });
const { resolveProviderEndpointFromReflect } = await import("./awf_reflect.cjs");

afterAll(() => {
  if (originalRunnerTemp === undefined) {
    delete process.env.RUNNER_TEMP;
  } else {
    process.env.RUNNER_TEMP = originalRunnerTemp;
  }
  fs.rmSync(scratchRunnerTemp, { recursive: true, force: true });
});

describe("pi_models_json.cjs", () => {
  let originalEnv;
  let stderrOutput;
  let tmpDir;

  beforeEach(() => {
    originalEnv = { ...process.env };
    stderrOutput = [];
    vi.spyOn(process.stderr, "write").mockImplementation(msg => {
      stderrOutput.push(String(msg));
      return true;
    });
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-models-json-test-"));
  });

  afterEach(() => {
    process.env = originalEnv;
    fs.rmSync(tmpDir, { recursive: true, force: true });
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  describe("resolveGatewayBaseUrl", () => {
    it("falls back to the compile-time port when no reflect data is available", () => {
      const result = piModelsJson.resolveGatewayBaseUrl({
        provider: "openai",
        fallbackPort: 10000,
        reflectData: null,
        logger: () => {},
      });
      expect(result).toEqual({ baseUrl: "http://api-proxy:10000", source: "fallback" });
    });

    it("prefers the live baseUrl reported by /reflect when a matching endpoint is configured", () => {
      const reflectData = { endpoints: [{ provider: "openai", configured: true, port: 10000, base_url: "http://api-proxy:10000" }] };
      // Sanity-check the real resolver agrees the fixture resolves to the live port before
      // exercising resolveGatewayBaseUrl(), which delegates to it.
      expect(resolveProviderEndpointFromReflect({ provider: "openai", reflectData, logger: () => {} }).baseUrl).toBe("http://api-proxy:10000");

      const result = piModelsJson.resolveGatewayBaseUrl({
        provider: "openai",
        fallbackPort: 10001,
        reflectData,
        logger: () => {},
      });
      expect(result).toEqual({ baseUrl: "http://api-proxy:10000", source: "reflect" });
    });

    it("falls back to the compile-time port when /reflect has no matching configured endpoint", () => {
      const result = piModelsJson.resolveGatewayBaseUrl({
        provider: "anthropic",
        fallbackPort: 10001,
        reflectData: { endpoints: [] },
        logger: () => {},
      });
      expect(result).toEqual({ baseUrl: "http://api-proxy:10001", source: "fallback" });
    });

    it("falls back to the compile-time port when /reflect only has another provider configured", () => {
      const result = piModelsJson.resolveGatewayBaseUrl({
        provider: "openai",
        fallbackPort: 10000,
        reflectData: { endpoints: [{ provider: "anthropic", configured: true, port: 10001, base_url: "http://api-proxy:10001" }] },
        logger: () => {},
      });
      expect(result).toEqual({ baseUrl: "http://api-proxy:10000", source: "fallback" });
    });
  });

  describe("AWF model routing", () => {
    it.each([
      ["/v1/messages", "anthropic-messages"],
      ["/responses", "openai-responses"],
      ["/chat/completions", "openai-completions"],
    ])("maps routed endpoint %s to Pi API %s", (endpoint, api) => {
      const result = piModelsJson.resolvePiModelRouting({
        endpoints: [{ provider: "github", configured: true, models: ["routed-model"] }],
        routing: {
          status: "selected",
          selection: { provider: "github", model: "github-copilot/routed-model", wire_model: "routed-model", effort: "none", endpoint },
        },
      });
      expect(result.error).toBeNull();
      expect(result.api).toBe(api);
      expect(result.selection).toMatchObject({ wire_model: "routed-model", mapped_effort: "off" });
    });

    it.each([
      [null, "required model-routing selection"],
      [{ routing: { status: "pending" } }, "pending"],
      [
        { endpoints: [{ provider: "github", configured: true, models: ["other"] }], routing: { status: "selected", selection: { provider: "github", wire_model: "routed-model", effort: "high", endpoint: "/responses" } } },
        "unavailable Copilot wire model",
      ],
      [
        { endpoints: [{ provider: "github", configured: true, models: ["routed-model"] }], routing: { status: "selected", selection: { provider: "github", wire_model: "routed-model", effort: "high", endpoint: "/unknown" } } },
        "not supported by this engine",
      ],
    ])("fails closed for invalid routing selection: %s", (reflect, error) => {
      expect(piModelsJson.resolvePiModelRouting(reflect).error).toContain(error);
    });

    it("writes the selected model, endpoint API, and mapped effort for Pi", async () => {
      const agentDir = path.join(tmpDir, "routed-pi");
      const modelsPath = path.join(agentDir, "models.json");
      const routingModelPath = path.join(tmpDir, "routed-model");
      process.env.GH_AW_MODEL_ROUTING = "1";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "COPILOT_GITHUB_TOKEN";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10002";
      process.env.PI_CODING_AGENT_DIR = agentDir;
      process.env.GH_AW_PI_MODELS_JSON_PATH = modelsPath;
      process.env.GH_AW_PI_MODEL_ROUTING_MODEL_FILE = routingModelPath;
      const messages = [];
      const reflect = {
        endpoints: [{ provider: "github", configured: true, port: 10002, base_url: "http://api-proxy:10002", models: ["gpt-5.6-sol"] }],
        routing: {
          status: "selected",
          selection: { provider: "github", model: "github-copilot/gpt-5.6-sol", wire_model: "gpt-5.6-sol", effort: "none", endpoint: "/responses" },
        },
      };
      await piModelsJson.main({
        fetchReflect: async () => ({ ok: true, reflectData: reflect }),
        loadModelsJson: () => ({ data: [] }),
        loadSDK: async () => ({ ModelRuntime: { create: async () => ({ getModel: () => undefined }) } }),
        logger: message => messages.push(message),
      });
      const models = JSON.parse(fs.readFileSync(modelsPath, "utf8"));
      expect(models.providers["aw-gateway"].api).toBe("openai-responses");
      expect(models.providers["aw-gateway"].models[0].id).toBe("gpt-5.6-sol");
      expect(fs.readFileSync(routingModelPath, "utf8")).toBe("gpt-5.6-sol");
      expect(JSON.parse(fs.readFileSync(path.join(agentDir, "model-routing-selection.json"), "utf8"))).toMatchObject({
        wire_model: "gpt-5.6-sol",
        mapped_effort: "off",
      });
      expect(messages.join("\n")).toContain("inference routing: mode=awf-routed model=gpt-5.6-sol effort=none");
    });
  });

  describe("buildModelsJSON", () => {
    it("builds the aw-gateway provider payload with the default api", () => {
      const json = piModelsJson.buildModelsJSON({
        baseUrl: "http://api-proxy:10000",
        apiKeyEnvVar: "CODEX_API_KEY",
        modelId: "gpt-4.1",
      });
      const parsed = JSON.parse(json);
      expect(parsed).toEqual({
        providers: {
          "aw-gateway": {
            baseUrl: "http://api-proxy:10000",
            api: "openai-completions",
            apiKey: "awf-proxy",
            models: [{ id: "gpt-4.1" }],
          },
        },
      });
    });

    it("builds the aw-gateway provider payload with an explicit api", () => {
      const json = piModelsJson.buildModelsJSON({
        baseUrl: "http://api-proxy:10000",
        apiKeyEnvVar: "CODEX_API_KEY",
        modelId: "gpt-5.4",
        api: "openai-responses",
      });
      const parsed = JSON.parse(json);
      expect(parsed.providers["aw-gateway"].api).toBe("openai-responses");
    });

    it("uses function tools for codemode on Copilot Responses routes while preserving other compatibility metadata", () => {
      const json = piModelsJson.buildModelsJSON({
        baseUrl: "http://api-proxy:10002",
        apiKeyEnvVar: "COPILOT_GITHUB_TOKEN",
        modelId: "gpt-5.5",
        provider: "github",
        api: "openai-responses",
        metadata: { compat: { supportsOpenAIGrammarTools: true, supportsStrictMode: true, supportsDeveloperRole: false } },
      });
      expect(JSON.parse(json).providers["aw-gateway"].models).toEqual([{ id: "gpt-5.5", compat: { supportsOpenAIGrammarTools: false, supportsStrictMode: true, supportsDeveloperRole: false } }]);
    });

    it("disables Responses grammar tools even without catalog compatibility metadata", () => {
      const json = piModelsJson.buildModelsJSON({
        baseUrl: "http://api-proxy:10002",
        apiKeyEnvVar: "COPILOT_GITHUB_TOKEN",
        modelId: "custom-model",
        provider: "github",
        api: "openai-responses",
      });
      expect(JSON.parse(json).providers["aw-gateway"].models).toEqual([{ id: "custom-model", compat: { supportsOpenAIGrammarTools: false } }]);
    });

    it.each([
      ["openai", "openai-responses"],
      ["github", "openai-completions"],
      ["anthropic", "anthropic-messages"],
    ])("preserves grammar-tool compatibility for %s using %s", (provider, api) => {
      const json = piModelsJson.buildModelsJSON({
        baseUrl: "http://api-proxy:10000",
        apiKeyEnvVar: "CODEX_API_KEY",
        modelId: "fixture",
        provider,
        api,
        metadata: { compat: { supportsOpenAIGrammarTools: true } },
      });
      expect(JSON.parse(json).providers["aw-gateway"].models).toEqual([{ id: "fixture", compat: { supportsOpenAIGrammarTools: true } }]);
    });

    it("uses the native Copilot context window for claude-sonnet-5", () => {
      const json = piModelsJson.buildModelsJSON({
        baseUrl: "http://api-proxy:10002",
        apiKeyEnvVar: "COPILOT_GITHUB_TOKEN",
        modelId: "claude-sonnet-5",
        provider: "github",
      });
      expect(JSON.parse(json).providers["aw-gateway"].models).toEqual([{ id: "claude-sonnet-5", contextWindow: 1000000 }]);
    });

    it("preserves configured reasoning instead of hardcoding Claude Haiku capabilities", () => {
      const options = {
        baseUrl: "http://api-proxy:10002",
        apiKeyEnvVar: "COPILOT_GITHUB_TOKEN",
        modelId: "claude-haiku-4.5",
        metadata: { reasoning: true },
      };
      const github = JSON.parse(piModelsJson.buildModelsJSON({ ...options, provider: "github" }));
      expect(github.providers["aw-gateway"].models).toEqual([{ id: "claude-haiku-4.5", reasoning: true }]);

      const anthropic = JSON.parse(piModelsJson.buildModelsJSON({ ...options, provider: "anthropic" }));
      expect(anthropic.providers["aw-gateway"].models).toEqual([{ id: "claude-haiku-4.5", reasoning: true }]);
    });

    it("uses a configured context window for any routed model", () => {
      const json = piModelsJson.buildModelsJSON({
        baseUrl: "http://api-proxy:10001",
        apiKeyEnvVar: "ANTHROPIC_API_KEY",
        modelId: "custom-claude",
        provider: "anthropic",
        contextWindow: "256000",
      });
      expect(JSON.parse(json).providers["aw-gateway"].models).toEqual([{ id: "custom-claude", contextWindow: 256000 }]);
    });

    it("warns and falls back when configured context window is invalid", () => {
      const warnings = [];
      const json = piModelsJson.buildModelsJSON({
        baseUrl: "http://api-proxy:10002",
        apiKeyEnvVar: "COPILOT_GITHUB_TOKEN",
        modelId: "claude-sonnet-5",
        provider: "github",
        contextWindow: "not-a-number",
        logger: message => warnings.push(message),
      });
      expect(JSON.parse(json).providers["aw-gateway"].models).toEqual([{ id: "claude-sonnet-5", contextWindow: 1000000 }]);
      expect(warnings).toEqual(['warning: ignoring invalid contextWindow="not-a-number"; expected a positive integer']);
    });

    it.each([
      ["github", "custom-model"],
      ["anthropic", "claude-sonnet-5"],
    ])("does not invent a context window for %s/%s", (provider, modelId) => {
      const json = piModelsJson.buildModelsJSON({
        baseUrl: "http://api-proxy:10002",
        apiKeyEnvVar: "COPILOT_GITHUB_TOKEN",
        modelId,
        provider,
      });
      expect(JSON.parse(json).providers["aw-gateway"].models).toEqual([{ id: modelId }]);
    });
  });

  describe("resolvePiApiForProvider", () => {
    it("routes the openai provider through openai-responses", () => {
      expect(piModelsJson.resolvePiApiForProvider("openai")).toBe("openai-responses");
    });

    it("routes the codex provider through openai-responses", () => {
      expect(piModelsJson.resolvePiApiForProvider("codex")).toBe("openai-responses");
    });

    it("keeps the github provider on openai-completions", () => {
      expect(piModelsJson.resolvePiApiForProvider("github")).toBe("openai-completions");
    });

    it("routes the anthropic provider through its native Messages API", () => {
      expect(piModelsJson.resolvePiApiForProvider("anthropic")).toBe("anthropic-messages");
    });
  });

  describe("resolvePiApiForModel", () => {
    it("routes a Copilot model marked Responses-only through the Responses API", () => {
      expect(
        piModelsJson.resolvePiApiForModel({
          provider: "github",
          modelId: "gpt-5.5",
          modelsJson: { providers: { "github-copilot": { models: { "gpt-5.5": { wire_api: "responses" } } } } },
        })
      ).toBe("openai-responses");
    });

    it("rejects a chat-completions override for a Responses-only model", () => {
      const logs = [];
      expect(() =>
        piModelsJson.resolvePiApiForModel({
          provider: "github",
          modelId: "gpt-5.5",
          modelsJson: { providers: { "github-copilot": { models: { "gpt-5.5": { wire_api: "responses" } } } } },
          overrideApi: "openai-completions",
          logger: message => logs.push(message),
        })
      ).toThrow('Pi model "gpt-5.5" requires the OpenAI Responses API');
      expect(logs).toContain("warning: Pi model API override conflicts with Responses-only model (model=gpt-5.5, override_api=openai-completions)");
    });

    it("rejects routed endpoints that conflict with catalog API metadata", () => {
      expect(() =>
        piModelsJson.resolvePiApiForModel({
          provider: "github",
          modelId: "gpt-5.5",
          model: { api: "openai-completions" },
          overrideApi: "openai-responses",
          strictOverrideApi: true,
        })
      ).toThrow('Pi model "gpt-5.5" API metadata is "openai-completions"');
    });
  });

  describe("resolvePiReasoningForModel", () => {
    it.each([
      [[], false],
      [["none"], false],
      [["low", "high"], true],
      [["none", "high"], true],
      [null, undefined],
    ])("uses reflected routing efforts %j to resolve reasoning=%s", (efforts, reasoning) => {
      expect(
        piModelsJson.resolvePiReasoningForModel({
          provider: "github",
          modelId: "Claude-Haiku-4.5?effort=high",
          reflectData: { models_fetch_complete: true, endpoints: [{ provider: "copilot", configured: true, routing_models: [{ model_id: "claude-haiku-4.5", supported_reasoning_efforts: efforts }] }] },
        })
      ).toBe(reasoning);
    });

    it.each([
      [{ supportedReasoningEfforts: ["high"] }, true],
      [{ capabilities: { supports: { reasoning_effort: [] } } }, false],
      [{ capabilities: { supports: { reasoning_effort: ["medium"] } } }, true],
      [{ capabilities: { supports: { reasoningEffort: false } } }, false],
      [{ capabilities: { supports: { streaming: true } } }, undefined],
      [{ capabilities: { supports: { reasoning_effort: null } } }, undefined],
      [{}, undefined],
    ])("uses reflected provider capabilities %j", (metadata, reasoning) => {
      expect(
        piModelsJson.resolvePiReasoningForModel({
          provider: "github",
          modelId: "custom-model",
          reflectData: { models_fetch_complete: true, endpoints: [{ provider: "copilot", configured: true, model_metadata: [{ id: "custom-model", ...metadata }] }] },
        })
      ).toBe(reasoning);
    });

    it("returns undefined when a matched Copilot model has no capabilities.supports", () => {
      expect(
        piModelsJson.resolvePiReasoningForModel({
          provider: "github",
          modelId: "custom-model",
          reflectData: { models_fetch_complete: true, endpoints: [{ provider: "copilot", configured: true, model_metadata: [{ id: "custom-model" }] }] },
        })
      ).toBeUndefined();
    });

    it.each([undefined, null, {}, { models_fetch_complete: false }, { models_fetch_complete: true }])("returns undefined for unavailable reflection or absent metadata: %j", reflectData => {
      expect(piModelsJson.resolvePiReasoningForModel({ provider: "github", modelId: "custom-model", reflectData })).toBeUndefined();
    });

    it.each([undefined, false])("ignores populated reasoning metadata when discovery completion is %s", models_fetch_complete => {
      expect(
        piModelsJson.resolvePiReasoningForModel({
          provider: "github",
          modelId: "custom-model",
          reflectData: {
            models_fetch_complete,
            endpoints: [
              { provider: "copilot", configured: true, routing_models: [{ model_id: "custom-model", supported_reasoning_efforts: [] }], model_metadata: [{ id: "custom-model", capabilities: { supports: { reasoningEffort: false } } }] },
            ],
          },
        })
      ).toBeUndefined();
    });

    it.each([
      [[], ["high"], false],
      [["high"], [], true],
      [undefined, [], false],
      [undefined, ["high"], true],
    ])("prefers routing efforts %j over model efforts %j", (routingEfforts, modelEfforts, reasoning) => {
      expect(
        piModelsJson.resolvePiReasoningForModel({
          provider: "github",
          modelId: "custom-model",
          reflectData: {
            models_fetch_complete: true,
            endpoints: [
              {
                provider: "copilot",
                configured: true,
                routing_models: [{ model_id: "custom-model", supported_reasoning_efforts: routingEfforts }],
                model_metadata: [{ id: "custom-model", supportedReasoningEfforts: modelEfforts, capabilities: { supports: { reasoning_effort: ["medium"], reasoningEffort: false } } }],
              },
            ],
          },
        })
      ).toBe(reasoning);
    });

    it("does not use metadata for other models, providers, or unconfigured endpoints", () => {
      expect(
        piModelsJson.resolvePiReasoningForModel({
          provider: "github",
          modelId: "custom-model",
          reflectData: {
            models_fetch_complete: true,
            endpoints: [
              { provider: "openai", configured: true, routing_models: [{ model_id: "custom-model", supported_reasoning_efforts: ["high"] }] },
              { provider: "copilot", configured: false, routing_models: [{ model_id: "custom-model", supported_reasoning_efforts: ["high"] }] },
              { provider: "copilot", configured: true, routing_models: [{ model_id: "another-model", supported_reasoning_efforts: ["high"] }] },
            ],
          },
        })
      ).toBeUndefined();
    });

    it("does not infer missing effort control for non-Copilot models", () => {
      expect(
        piModelsJson.resolvePiReasoningForModel({
          provider: "anthropic",
          modelId: "custom-model",
          reflectData: { models_fetch_complete: true, endpoints: [{ provider: "anthropic", configured: true, model_metadata: [{ id: "custom-model", capabilities: { supports: { streaming: true } } }] }] },
        })
      ).toBeUndefined();
    });
  });

  describe("validatePiModelAvailability", () => {
    it.each(["github", "copilot", "github-copilot"].flatMap(provider => ["auto", "auto?effort=high"].map(modelId => [provider, modelId])))("accepts the %s/%s routing sentinel without a catalog entry", (provider, modelId) => {
      const logs = [];
      expect(() =>
        piModelsJson.validatePiModelAvailability({
          provider,
          modelId,
          logger: message => logs.push(message),
          reflectData: {
            models_fetch_complete: true,
            endpoints: [{ provider: "copilot", configured: true, models: ["gpt-5.4"] }],
          },
        })
      ).not.toThrow();
      expect(logs).toContain("awf-reflect: Copilot auto selection delegated to the proxy");
    });

    it.each(["openai", "anthropic"].flatMap(provider => ["auto", "auto?effort=high"].map(modelId => [provider, modelId])))("still rejects an unadvertised auto model for %s/%s", (provider, modelId) => {
      expect(() =>
        piModelsJson.validatePiModelAvailability({
          provider,
          modelId,
          reflectData: {
            models_fetch_complete: true,
            endpoints: [{ provider, configured: true, models: [] }],
          },
        })
      ).toThrow(`Pi model "${modelId}" is not advertised by the configured ${provider} proxy endpoint`);
    });

    it("rejects a model absent from a completed reflected endpoint inventory", () => {
      const logs = [];
      expect(() =>
        piModelsJson.validatePiModelAvailability({
          provider: "github",
          modelId: "gpt-5.5",
          logger: message => logs.push(message),
          reflectData: {
            models_fetch_complete: true,
            endpoints: [{ provider: "copilot", configured: true, models: ["gpt-4o"] }],
          },
        })
      ).toThrow('Pi model "gpt-5.5" is not advertised by the configured copilot proxy endpoint');
      expect(logs).toContain("warning: awf-reflect model availability check failed (provider=copilot, model=gpt-5.5)");
    });

    it("does not reject when reflected model discovery is incomplete", () => {
      const logs = [];
      expect(() =>
        piModelsJson.validatePiModelAvailability({
          provider: "github",
          modelId: "gpt-5.5",
          logger: message => logs.push(message),
          reflectData: {
            models_fetch_complete: false,
            endpoints: [{ provider: "copilot", configured: true, models: [] }],
          },
        })
      ).not.toThrow();
      expect(logs).toContain("awf-reflect: model availability check skipped (model discovery incomplete)");
    });
  });

  describe("main", () => {
    it.each([false, true])("stages delegated models with their own gateway protocol (routing=%s)", async routing => {
      if (routing) process.env.GH_AW_MODEL_ROUTING = "1";
      else delete process.env.GH_AW_MODEL_ROUTING;
      process.env.GH_AW_PI_MODEL_ID = "gpt-5.6-luna";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "COPILOT_GITHUB_TOKEN";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10002";
      process.env.GH_AW_LLM_PROVIDER = "github";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      process.env.GH_AW_PI_CONFIG = "{}";
      process.env.GH_AW_PI_MODEL_ALIASES = '{"small":["copilot/*haiku*"]}';
      process.env.GH_AW_PI_STAGING_DIR = path.join(tmpDir, "staged");
      process.env.GH_AW_PI_MODEL_ROUTING_MODEL_FILE = path.join(tmpDir, "routing-model");
      delete process.env.AWF_REFLECT_ENABLED;
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;
      fs.mkdirSync(path.join(process.env.GH_AW_PI_STAGING_DIR, "agents"), { recursive: true });
      fs.writeFileSync(path.join(process.env.GH_AW_PI_STAGING_DIR, "agents/reader.md"), "Fixture");
      const catalog = [
        { provider: "github-copilot", id: "gpt-5.6-luna", api: "openai-responses", reasoning: true, input: ["text"], contextWindow: 128000, maxTokens: 32000 },
        { provider: "github-copilot", id: "claude-haiku-4.5", api: "openai-completions", reasoning: false, input: ["text"], contextWindow: 200000, maxTokens: 8192 },
      ];
      const sdk = {
        parseFrontmatter: () => ({ frontmatter: { description: "Read files", model: "small" }, body: "Read only." }),
        ModelRuntime: { create: async () => ({ getModels: () => catalog, getModel: (_provider, id) => catalog.find(model => model.id === id) }) },
      };
      await piModelsJson.main({
        loadSDK: async () => sdk,
        loadModelsJson: () => ({ providers: {} }),
        fetchReflect: async () => ({
          ok: true,
          reflectData: {
            models_fetch_complete: true,
            endpoints: [{ provider: "copilot", configured: true, port: 10002, models: catalog.map(model => model.id) }],
            routing: { status: "selected", selection: { provider: "github", wire_model: "gpt-5.6-luna", effort: "none", endpoint: "/responses" } },
          },
        }),
      });
      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8")).providers["aw-gateway"];
      expect(written.api).toBe("openai-responses");
      expect(written.models).toEqual([
        expect.objectContaining({ id: "gpt-5.6-luna", compat: { supportsOpenAIGrammarTools: false } }),
        expect.objectContaining({ id: "claude-haiku-4.5", api: "openai-completions", contextWindow: 200000, maxTokens: 8192 }),
      ]);
      expect(JSON.parse(fs.readFileSync(path.join(tmpDir, "subagents.json"), "utf8"))[0]).toMatchObject({ declaredModel: "small", model: "aw-gateway/claude-haiku-4.5" });
    });

    it.each(["auto", "auto?effort=high"])("writes the Copilot %s gateway model when completed discovery only advertises concrete models", async modelId => {
      process.env.GH_AW_PI_MODEL_ID = modelId;
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "COPILOT_GITHUB_TOKEN";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10002";
      process.env.GH_AW_LLM_PROVIDER = "github";
      process.env.AWF_REFLECT_ENABLED = "1";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      process.env.GH_AW_PI_CONFIG = "{}";
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: async () => ({
            models_fetch_complete: true,
            endpoints: [{ provider: "copilot", configured: true, port: 10002, models: ["gpt-5.4"] }],
          }),
        })
      );

      await piModelsJson.main({ loadSDK, loadModelsJson: () => ({ providers: {} }) });

      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"]).toEqual({
        baseUrl: "http://api-proxy:10002",
        api: "openai-completions",
        apiKey: "awf-proxy",
        models: [{ id: modelId }],
      });
      expect(stderrOutput.join("")).toContain("Copilot auto selection delegated to the proxy");
      expect(stderrOutput.join("")).toContain("Copilot auto uses gateway-selected model metadata");
      expect(stderrOutput.join("")).not.toContain("configure engine.config.model for a custom model");
    });

    it.each([
      [[], false],
      [["high"], true],
      [undefined, false],
    ])("writes reflected reasoning support for arbitrary models with efforts %j", async (efforts, reasoning) => {
      process.env.GH_AW_PI_MODEL_ID = "custom-model";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "COPILOT_GITHUB_TOKEN";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10002";
      process.env.GH_AW_LLM_PROVIDER = "github";
      process.env.AWF_REFLECT_ENABLED = "1";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      process.env.GH_AW_PI_CONFIG = JSON.stringify({ model: { reasoning: !reasoning } });
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: async () => ({
            models_fetch_complete: true,
            endpoints: [
              {
                provider: "copilot",
                configured: true,
                port: 10002,
                models: ["custom-model"],
                routing_models: [{ model_id: "custom-model", supported_reasoning_efforts: efforts }],
                model_metadata: [{ id: "custom-model", capabilities: { supports: { reasoningEffort: false } } }],
              },
            ],
          }),
        })
      );
      await piModelsJson.main({ loadSDK, loadModelsJson: () => ({ providers: {} }) });
      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"].models[0].reasoning).toBe(reasoning);
      expect(stderrOutput.join("")).toContain(`resolved reasoning=${reasoning} from AWF /reflect`);
    });

    it.each([
      [true, { id: "custom-model", capabilities: { supports: { streaming: true } } }],
      [true, { id: "custom-model" }],
      [true, undefined],
      [false, { id: "custom-model", capabilities: { supports: { reasoningEffort: false } } }],
      [undefined, { id: "custom-model", capabilities: { supports: { reasoningEffort: false } } }],
    ])("preserves catalog and explicit reasoning with discovery=%s and metadata=%j", async (models_fetch_complete, modelMetadata) => {
      process.env.GH_AW_PI_MODEL_ID = "custom-model";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "COPILOT_GITHUB_TOKEN";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10002";
      process.env.GH_AW_LLM_PROVIDER = "github";
      process.env.AWF_REFLECT_ENABLED = "1";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: async () => ({
            models_fetch_complete,
            endpoints: [{ provider: "copilot", configured: true, port: 10002, models: ["custom-model"], model_metadata: modelMetadata ? [modelMetadata] : [] }],
          }),
        })
      );
      for (const configuredReasoning of [undefined, true, false]) {
        process.env.GH_AW_PI_CONFIG = JSON.stringify({ model: configuredReasoning === undefined ? {} : { reasoning: configuredReasoning } });
        await piModelsJson.main({
          loadSDK: async () => ({ ModelRuntime: { create: async () => ({ getModel: () => ({ reasoning: true }) }) } }),
          loadModelsJson: () => ({ providers: {} }),
        });
        const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
        expect(written.providers["aw-gateway"].models[0].reasoning).toBe(configuredReasoning ?? true);
      }
      expect(stderrOutput.join("")).toContain("reasoning metadata unavailable; retaining Pi model configuration");
    });

    it("preserves native thinking, vision, token limits, pricing, and cache metadata", async () => {
      process.env.GH_AW_PI_MODEL_ID = "gpt-5.4";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "CODEX_API_KEY";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10000";
      process.env.GH_AW_LLM_PROVIDER = "openai";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      delete process.env.AWF_REFLECT_ENABLED;
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;
      const metadata = { reasoning: true, input: ["text", "image"], contextWindow: 1000000, maxTokens: 32000, promptCache: { short: 300 }, cost: { input: 1, output: 2, cacheRead: 0.1, cacheWrite: 0 } };
      await piModelsJson.main({ loadSDK: async () => ({ ModelRuntime: { create: async () => ({ getModel: () => metadata }) } }) });
      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"].models).toEqual([{ ...metadata, id: "gpt-5.4" }]);
      expect(written.providers["aw-gateway"].apiKey).toBe("awf-proxy");
    });

    it.each(["openai", "codex"])("writes models.json using the live /reflect baseUrl and responses api for the %s provider", async provider => {
      process.env.GH_AW_PI_MODEL_ID = "gpt-4.1";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "CODEX_API_KEY";
      // Deliberately pass the wrong fallback port (10001 is anthropic's port, not openai's)
      // to prove that the live /reflect data overrides the compile-time fallback value.
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10001";
      process.env.GH_AW_LLM_PROVIDER = provider;
      process.env.AWF_REFLECT_ENABLED = "1";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;

      const reflectPayload = {
        endpoints: [{ provider, configured: true, port: 10000, base_url: "http://api-proxy:10000", models: [] }],
      };
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => reflectPayload }));

      await piModelsJson.main({ loadSDK });

      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"].baseUrl).toBe("http://api-proxy:10000");
      expect(written.providers["aw-gateway"].apiKey).toBe("awf-proxy");
      expect(written.providers["aw-gateway"].models).toEqual([{ id: "gpt-4.1" }]);
      expect(written.providers["aw-gateway"].api).toBe("openai-responses");
      expect(fetch).toHaveBeenCalled();
    });

    it("routes an advertised Responses-only Copilot model before Pi starts", async () => {
      process.env.GH_AW_PI_MODEL_ID = "gpt-5.5";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "COPILOT_GITHUB_TOKEN";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10002";
      process.env.GH_AW_LLM_PROVIDER = "github";
      process.env.AWF_REFLECT_ENABLED = "1";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: async () => ({
            models_fetch_complete: true,
            endpoints: [{ provider: "copilot", configured: true, port: 10002, models: ["gpt-5.5"] }],
          }),
        })
      );

      await piModelsJson.main({
        loadSDK,
        loadModelsJson: () => ({ providers: { "github-copilot": { models: { "gpt-5.5": { wire_api: "responses" } } } } }),
      });

      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"].api).toBe("openai-responses");
      expect(written.providers["aw-gateway"].models[0].compat).toEqual({ supportsOpenAIGrammarTools: false });
      expect(stderrOutput.join("")).toContain("awf-reflect: model availability confirmed (provider=copilot, model=gpt-5.5)");
      expect(stderrOutput.join("")).toContain("resolved model API=openai-responses");
    });

    it("fails before generating models.json when a completed reflected inventory excludes the model", async () => {
      process.env.GH_AW_PI_MODEL_ID = "gpt-5.5";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "COPILOT_GITHUB_TOKEN";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10002";
      process.env.GH_AW_LLM_PROVIDER = "github";
      process.env.AWF_REFLECT_ENABLED = "1";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: async () => ({
            models_fetch_complete: true,
            endpoints: [{ provider: "copilot", configured: true, port: 10002, models: ["gpt-4o"] }],
          }),
        })
      );

      await expect(piModelsJson.main({ loadSDK, loadModelsJson: () => ({ providers: {} }) })).rejects.toThrow('Pi model "gpt-5.5" is not advertised by the configured copilot proxy endpoint');
      expect(fs.existsSync(path.join(tmpDir, "models.json"))).toBe(false);
    });

    it("writes models.json using the fallback port when AWF_REFLECT_ENABLED is not set", async () => {
      process.env.GH_AW_PI_MODEL_ID = "claude-opus-4-20251101";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "ANTHROPIC_API_KEY";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10001";
      process.env.GH_AW_LLM_PROVIDER = "anthropic";
      delete process.env.AWF_REFLECT_ENABLED;
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;

      const fetchSpy = vi.fn();
      vi.stubGlobal("fetch", fetchSpy);

      await piModelsJson.main({ loadSDK });

      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"].baseUrl).toBe("http://api-proxy:10001");
      expect(written.providers["aw-gateway"].api).toBe("anthropic-messages");
      expect(fetchSpy).not.toHaveBeenCalled();
    });

    it("falls back to the compile-time port when /reflect fetch fails", async () => {
      process.env.GH_AW_PI_MODEL_ID = "claude-sonnet-4-20250514";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "COPILOT_GITHUB_TOKEN";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10002";
      process.env.GH_AW_LLM_PROVIDER = "github";
      process.env.AWF_REFLECT_ENABLED = "1";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;

      vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("network unreachable")));

      await piModelsJson.main({ loadSDK });

      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"].baseUrl).toBe("http://api-proxy:10002");
    });

    it("writes the Copilot context window to models.json", async () => {
      process.env.GH_AW_PI_MODEL_ID = "claude-sonnet-5";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "COPILOT_GITHUB_TOKEN";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10002";
      process.env.GH_AW_LLM_PROVIDER = "github";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      delete process.env.AWF_REFLECT_ENABLED;
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;

      await piModelsJson.main({ loadSDK });

      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"].models).toEqual([{ id: "claude-sonnet-5", contextWindow: 1000000 }]);
    });

    it("writes configured context window to models.json", async () => {
      process.env.GH_AW_PI_MODEL_ID = "custom-model";
      process.env.GH_AW_PI_CONTEXT_WINDOW = "256000";
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "COPILOT_GITHUB_TOKEN";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10002";
      process.env.GH_AW_LLM_PROVIDER = "github";
      process.env.PI_CODING_AGENT_DIR = tmpDir;
      delete process.env.AWF_REFLECT_ENABLED;
      delete process.env.GH_AW_PI_MODELS_JSON_PATH;

      await piModelsJson.main({ loadSDK });

      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"].models).toEqual([{ id: "custom-model", contextWindow: 256000 }]);
    });

    it("exits with an error when required env vars are missing", async () => {
      delete process.env.GH_AW_PI_MODEL_ID;
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "CODEX_API_KEY";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10000";

      await piModelsJson.main({ loadSDK });

      expect(process.exitCode).toBe(1);
      process.exitCode = 0;
      expect(stderrOutput.some(line => line.includes("fatal: missing required env vars"))).toBe(true);
    });
  });
});
