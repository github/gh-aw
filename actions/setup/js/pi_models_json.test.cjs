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
            apiKey: "CODEX_API_KEY",
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

  describe("main", () => {
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

      await piModelsJson.main();

      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"].baseUrl).toBe("http://api-proxy:10000");
      expect(written.providers["aw-gateway"].apiKey).toBe("CODEX_API_KEY");
      expect(written.providers["aw-gateway"].models).toEqual([{ id: "gpt-4.1" }]);
      expect(written.providers["aw-gateway"].api).toBe("openai-responses");
      expect(fetch).toHaveBeenCalled();
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

      await piModelsJson.main();

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

      await piModelsJson.main();

      const written = JSON.parse(fs.readFileSync(path.join(tmpDir, "models.json"), "utf8"));
      expect(written.providers["aw-gateway"].baseUrl).toBe("http://api-proxy:10002");
    });

    it("exits with an error when required env vars are missing", async () => {
      delete process.env.GH_AW_PI_MODEL_ID;
      process.env.GH_AW_PI_GATEWAY_SECRET_ENV = "CODEX_API_KEY";
      process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT = "10000";

      await piModelsJson.main();

      expect(process.exitCode).toBe(1);
      process.exitCode = 0;
      expect(stderrOutput.some(line => line.includes("fatal: missing required env vars"))).toBe(true);
    });
  });
});
