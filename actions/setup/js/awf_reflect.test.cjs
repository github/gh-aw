import { afterEach, describe, it, expect, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const {
  AWF_API_PROXY_REFLECT_URL,
  AWF_REFLECT_OUTPUT_PATH,
  AWF_REFLECT_TIMEOUT_MS,
  AWF_MODELS_URL_TIMEOUT_MS,
  AWF_MODELS_URL_MAX_ATTEMPTS,
  AWF_MODELS_URL_RETRY_BASE_MS,
  AWF_MODELS_URL_RETRY_MAX_MS,
  AWF_PROVIDER_LISTENER_READY_TIMEOUT_MS,
  AWF_PROVIDER_LISTENER_READY_RETRY_MS,
  AWF_PROVIDER_LISTENER_READY_PROBE_TIMEOUT_MS,
  DEFAULT_API_PROXY_HOST_BRIDGE,
  GEMINI_MODEL_NAME_PREFIX,
  deriveBaseUrlFromModelsURL,
  enrichReflectModels,
  extractModelIds,
  fetchAWFReflect,
  fetchModelsFromUrl,
  waitForProviderListenerReady,
  getCatalogModelEntry,
  hasAPIProxyLocalhostAlias,
  inferProviderTypeForModel,
  inferWireApiForModel,
  parseReflectTimeoutMs,
  resolveOpenAICompatibleEndpointFromReflect,
  resolveProviderEndpointFromReflect,
  resolveMultiProviderFromReflect,
  rewriteAPIProxyURLForHostBridge,
} = require("./awf_reflect.cjs");

describe("awf_reflect.cjs", () => {
  describe("constants", () => {
    it("exports expected default values", () => {
      expect(AWF_API_PROXY_REFLECT_URL).toBe("http://api-proxy:10000/reflect");
      expect(AWF_REFLECT_OUTPUT_PATH).toBe("/tmp/gh-aw/agent/awf-reflect.json");
      expect(AWF_REFLECT_TIMEOUT_MS).toBe(60000);
      expect(AWF_MODELS_URL_TIMEOUT_MS).toBe(3000);
      expect(AWF_MODELS_URL_MAX_ATTEMPTS).toBe(5);
      expect(AWF_MODELS_URL_RETRY_BASE_MS).toBe(250);
      expect(AWF_MODELS_URL_RETRY_MAX_MS).toBe(2000);
      expect(AWF_PROVIDER_LISTENER_READY_TIMEOUT_MS).toBe(15000);
      expect(AWF_PROVIDER_LISTENER_READY_RETRY_MS).toBe(250);
      expect(AWF_PROVIDER_LISTENER_READY_PROBE_TIMEOUT_MS).toBe(2000);
      expect(DEFAULT_API_PROXY_HOST_BRIDGE).toBe("host.docker.internal");
      expect(GEMINI_MODEL_NAME_PREFIX).toBe("models/");
    });

    it("falls back to the default reflect timeout when the environment value is invalid", () => {
      expect(parseReflectTimeoutMs("")).toBe(60000);
      expect(parseReflectTimeoutMs("not-a-number")).toBe(60000);
      expect(parseReflectTimeoutMs("12abc")).toBe(60000);
      expect(parseReflectTimeoutMs("999999999999999999999999")).toBe(60000);
      expect(parseReflectTimeoutMs("1234")).toBe(1234);
    });
  });

  describe("waitForProviderListenerReady", () => {
    it("returns ok when listener accepts a connection", async () => {
      const probingConnect = vi.fn().mockImplementation(() => {
        const listeners = {};
        queueMicrotask(() => listeners.connect && listeners.connect());
        return {
          once(event, cb) {
            listeners[event] = cb;
            return this;
          },
          removeAllListeners() {
            return this;
          },
          end() {},
          destroy() {},
        };
      });

      const result = await waitForProviderListenerReady({
        baseUrl: "http://api-proxy:10002",
        timeoutMs: 500,
        retryDelayMs: 1,
        connectImpl: probingConnect,
        logger: () => {},
      });
      expect(result).toEqual({ ok: true });
      expect(probingConnect).toHaveBeenCalled();
    });

    it("returns timeout when listener keeps refusing connections", async () => {
      const probingConnect = vi.fn().mockImplementation(() => {
        const listeners = {};
        queueMicrotask(() => listeners.error && listeners.error(new Error("connect ECONNREFUSED")));
        return {
          once(event, cb) {
            listeners[event] = cb;
            return this;
          },
          end() {},
          destroy() {},
        };
      });

      const result = await waitForProviderListenerReady({
        baseUrl: "http://api-proxy:10002",
        timeoutMs: 20,
        retryDelayMs: 1,
        connectImpl: probingConnect,
        logger: () => {},
      });
      expect(result.ok).toBe(false);
      expect(result.reason).toBe("timeout");
      expect(result.error).toContain("ECONNREFUSED");
    });

    it("returns invalid_base_url for malformed baseUrl", async () => {
      const result = await waitForProviderListenerReady({
        baseUrl: "not a url",
        logger: () => {},
      });
      expect(result.ok).toBe(false);
      expect(result.reason).toBe("invalid_base_url");
    });

    it("returns timeout when the per-attempt timer fires (hung connect)", async () => {
      const probingConnect = vi.fn().mockImplementation(() => {
        // Never fire "connect" or "error" — simulates a hung connection attempt.
        return {
          once() {
            return this;
          },
          end() {},
          destroy() {},
          removeAllListeners() {
            return this;
          },
        };
      });

      const result = await waitForProviderListenerReady({
        baseUrl: "http://api-proxy:10002",
        timeoutMs: 50,
        retryDelayMs: 1,
        perAttemptTimeoutMs: 5,
        connectImpl: probingConnect,
        logger: () => {},
      });
      expect(result.ok).toBe(false);
      expect(result.reason).toBe("timeout");
      expect(result.error).toContain("timed out after");
    });

    it("uses a TLS secureConnect handshake for https:// baseUrls", async () => {
      const probingConnect = vi.fn().mockImplementation(() => {
        const listeners = {};
        queueMicrotask(() => listeners.secureConnect && listeners.secureConnect());
        return {
          once(event, cb) {
            listeners[event] = cb;
            return this;
          },
          removeAllListeners() {
            return this;
          },
          end() {},
          destroy() {},
        };
      });

      const result = await waitForProviderListenerReady({
        baseUrl: "https://api-proxy:10443",
        timeoutMs: 500,
        retryDelayMs: 1,
        connectImpl: probingConnect,
        logger: () => {},
      });
      expect(result).toEqual({ ok: true });
    });

    it("does not report ready on a bare TCP connect for https:// baseUrls", async () => {
      const probingConnect = vi.fn().mockImplementation(() => {
        const listeners = {};
        // Only fires "connect" (bare TCP), never "secureConnect" (TLS handshake complete).
        queueMicrotask(() => listeners.connect && listeners.connect());
        return {
          once(event, cb) {
            listeners[event] = cb;
            return this;
          },
          removeAllListeners() {
            return this;
          },
          end() {},
          destroy() {},
        };
      });

      const result = await waitForProviderListenerReady({
        baseUrl: "https://api-proxy:10443",
        timeoutMs: 20,
        retryDelayMs: 1,
        perAttemptTimeoutMs: 5,
        connectImpl: probingConnect,
        logger: () => {},
      });
      expect(result.ok).toBe(false);
      expect(result.reason).toBe("timeout");
    });

    it("ignores a late error emitted after a successful connect", async () => {
      const probingConnect = vi.fn().mockImplementation(() => {
        const listeners = {};
        queueMicrotask(() => listeners.connect && listeners.connect());
        return {
          once(event, cb) {
            listeners[event] = cb;
            return this;
          },
          removeAllListeners(event) {
            delete listeners[event];
            return this;
          },
          end() {},
          destroy() {
            // Simulate a trailing error emitted after destroy(). The "error" listener must
            // still be installed (an EventEmitter without one would throw), and the handler
            // must ignore it because the probe already settled as ready.
            if (!listeners.error) throw new Error("error listener was removed before destroy()");
            listeners.error(new Error("late ECONNRESET"));
          },
        };
      });

      const result = await waitForProviderListenerReady({
        baseUrl: "http://api-proxy:10002",
        timeoutMs: 500,
        retryDelayMs: 1,
        connectImpl: probingConnect,
        logger: () => {},
      });
      expect(result).toEqual({ ok: true });
    });
  });

  describe("rewriteAPIProxyURLForHostBridge", () => {
    it("does not rewrite api-proxy URLs without a localhost HOSTALIASES mapping", () => {
      const env = { HOSTALIASES: "/tmp/aliases" };
      const readFileSync = () => "other-host localhost\n";

      expect(hasAPIProxyLocalhostAlias(env, readFileSync)).toBe(false);
      expect(rewriteAPIProxyURLForHostBridge("http://api-proxy:10002/models", env, readFileSync)).toBe("http://api-proxy:10002/models");
    });

    it("rewrites api-proxy URLs when HOSTALIASES maps api-proxy to localhost", () => {
      const env = { HOSTALIASES: "/tmp/aliases" };
      const readFileSync = () => "# generated by awf\napi-proxy localhost\n";

      expect(hasAPIProxyLocalhostAlias(env, readFileSync)).toBe(true);
      expect(rewriteAPIProxyURLForHostBridge("http://api-proxy:10002/models", env, readFileSync)).toBe("http://host.docker.internal:10002/models");
    });

    it("uses an override bridge host when provided", () => {
      const env = { HOSTALIASES: "/tmp/aliases", GH_AW_API_PROXY_HOST_BRIDGE: "172.30.0.1" };
      const readFileSync = () => "api-proxy 127.0.0.1\n";

      expect(rewriteAPIProxyURLForHostBridge("http://api-proxy:10002", env, readFileSync)).toBe("http://172.30.0.1:10002");
    });
  });

  describe("deriveBaseUrlFromModelsURL", () => {
    it("strips a trailing /models segment and leaves non-bridged hosts untouched", () => {
      const env = {};
      const readFileSync = () => "";

      expect(deriveBaseUrlFromModelsURL("http://api-proxy:10002/v1/models", env, readFileSync)).toBe("http://api-proxy:10002/v1");
    });

    it("rewrites the api-proxy host to the HOSTALIASES bridge host, matching resolveProviderEndpointFromReflect", () => {
      // Regression test: the crush harness previously derived its chat-completions
      // base URL from models_url without reapplying the api-proxy -> host bridge
      // rewrite, so it sent requests to the unresolvable "api-proxy" hostname even
      // though resolveProviderEndpointFromReflect's baseUrl was already rewritten.
      const env = { HOSTALIASES: "/tmp/aliases" };
      const readFileSync = () => "api-proxy localhost\n";

      expect(deriveBaseUrlFromModelsURL("http://api-proxy:10002/v1/models", env, readFileSync)).toBe("http://host.docker.internal:10002/v1");
    });

    it("defaults to process.env and fs.readFileSync when not provided, with no path prefix before /models", () => {
      expect(deriveBaseUrlFromModelsURL("http://example.test:10002/models")).toBe("http://example.test:10002");
    });
  });

  describe("extractModelIds", () => {
    it("returns null for null input", () => {
      expect(extractModelIds(null)).toBeNull();
    });

    it("returns null for empty object", () => {
      expect(extractModelIds({})).toBeNull();
    });

    it("returns null for empty data array", () => {
      expect(extractModelIds({ data: [] })).toBeNull();
    });

    it("extracts ids from OpenAI format", () => {
      const json = { data: [{ id: "gpt-4o" }, { id: "gpt-4o-mini" }] };
      expect(extractModelIds(json)).toEqual(["gpt-4o", "gpt-4o-mini"]);
    });

    it("falls back to name when id is absent in OpenAI format", () => {
      const json = { data: [{ name: "model-a" }, { id: "model-b" }] };
      expect(extractModelIds(json)).toEqual(["model-a", "model-b"]);
    });

    it("extracts ids from Gemini format, stripping prefix", () => {
      const json = {
        models: [{ name: "models/gemini-1.5-pro" }, { name: "models/gemini-1.0-pro" }],
      };
      expect(extractModelIds(json)).toEqual(["gemini-1.0-pro", "gemini-1.5-pro"]);
    });

    it("handles Gemini entries without the prefix", () => {
      const json = { models: [{ name: "custom-model" }] };
      expect(extractModelIds(json)).toEqual(["custom-model"]);
    });

    it("returns sorted results", () => {
      const json = { data: [{ id: "z-model" }, { id: "a-model" }, { id: "m-model" }] };
      expect(extractModelIds(json)).toEqual(["a-model", "m-model", "z-model"]);
    });

    it("returns null for empty models array", () => {
      expect(extractModelIds({ models: [] })).toBeNull();
    });
  });

  describe("enrichReflectModels", () => {
    afterEach(() => {
      vi.unstubAllGlobals();
    });

    describe("resolveProviderEndpointFromReflect", () => {
      it("returns null when reflect data is missing", () => {
        const logs = [];
        const resolved = resolveProviderEndpointFromReflect({
          provider: "github",
          reflectData: null,
          logger: msg => logs.push(msg),
        });
        expect(resolved).toBeNull();
        expect(logs.some(l => l.includes("no configured endpoints available"))).toBe(true);
      });

      it("maps github provider to copilot endpoint", () => {
        const resolved = resolveProviderEndpointFromReflect({
          provider: "github",
          reflectData: {
            endpoints: [
              { provider: "openai", configured: true, port: 10000 },
              { provider: "copilot", configured: true, port: 10002, models_url: "http://api-proxy:10002/models" },
            ],
          },
          logger: () => {},
        });
        expect(resolved).toEqual({
          provider: "github",
          endpointProvider: "copilot",
          port: 10002,
          baseUrl: "http://api-proxy:10002",
        });
      });

      it("falls back to first configured endpoint when provider is not found", () => {
        const resolved = resolveProviderEndpointFromReflect({
          provider: "unknown-provider",
          reflectData: {
            endpoints: [{ provider: "openai", configured: true, port: 10000 }],
          },
          logger: () => {},
        });
        expect(resolved).toEqual({
          provider: "unknown-provider",
          endpointProvider: "openai",
          port: 10000,
          baseUrl: "http://api-proxy:10000",
        });
      });
    });

    describe("resolveOpenAICompatibleEndpointFromReflect", () => {
      const reflectData = {
        endpoints: [
          { provider: "openai", configured: true, models_url: "http://api-proxy:10000/v1/models" },
          { provider: "copilot", configured: true, models_url: "http://api-proxy:10002/models" },
        ],
      };

      it("derives the versionless Copilot chat endpoint for the github provider", () => {
        expect(resolveOpenAICompatibleEndpointFromReflect({ provider: "github", reflectData, logger: () => {} })).toEqual({
          provider: "github",
          endpointProvider: "copilot",
          host: "http://api-proxy:10002",
          basePath: "chat/completions",
        });
      });

      it("preserves the OpenAI v1 path", () => {
        expect(resolveOpenAICompatibleEndpointFromReflect({ provider: "openai", reflectData, logger: () => {} })).toEqual({
          provider: "openai",
          endpointProvider: "openai",
          host: "http://api-proxy:10000",
          basePath: "v1/chat/completions",
        });
      });

      it("does not fall back to a different configured provider", () => {
        expect(resolveOpenAICompatibleEndpointFromReflect({ provider: "anthropic", reflectData, logger: () => {} })).toBeNull();
      });

      it("rewrites the host bridge for definition-based engine OpenAI-compatible endpoints", () => {
        const env = { HOSTALIASES: "/tmp/aliases" };
        const readFileSync = () => "api-proxy localhost\n";

        expect(resolveOpenAICompatibleEndpointFromReflect({ provider: "github", reflectData, logger: () => {}, env, readFileSync })).toEqual({
          provider: "github",
          endpointProvider: "copilot",
          host: "http://host.docker.internal:10002",
          basePath: "chat/completions",
        });
      });
    });

    it("does nothing when all configured endpoints already have models", async () => {
      const reflectData = {
        endpoints: [{ provider: "openai", configured: true, models: ["gpt-4o"], models_url: "http://api-proxy:10000/v1/models" }],
      };
      const logger = () => {};
      await enrichReflectModels(reflectData, 1000, logger);
      expect(reflectData.endpoints[0].models).toEqual(["gpt-4o"]);
    });

    it("does nothing for unconfigured endpoints with null models", async () => {
      const reflectData = {
        endpoints: [{ provider: "anthropic", configured: false, models: null, models_url: "http://api-proxy:10001/v1/models" }],
      };
      const logger = () => {};
      await enrichReflectModels(reflectData, 1000, logger);
      expect(reflectData.endpoints[0].models).toBeNull();
    });

    it("does nothing when models_url is null", async () => {
      const reflectData = {
        endpoints: [{ provider: "opencode", configured: true, models: null, models_url: null }],
      };
      const logger = () => {};
      await enrichReflectModels(reflectData, 1000, logger);
      expect(reflectData.endpoints[0].models).toBeNull();
    });

    it("fetches models from models_url for configured endpoints with null models", async () => {
      const modelResponse = { data: [{ id: "claude-sonnet-4.6" }, { id: "gpt-4o" }] };
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => modelResponse }));

      const reflectData = {
        endpoints: [{ provider: "copilot", configured: true, models: null, models_url: "http://api-proxy:10002/models" }],
      };
      const logs = [];
      await enrichReflectModels(reflectData, 3000, msg => logs.push(msg));

      expect(reflectData.endpoints[0].models).toEqual(["claude-sonnet-4.6", "gpt-4o"]);
      expect(logs.some(l => l.includes("fetched 2 model(s)"))).toBe(true);
    });

    it("leaves models null when models_url fetch fails", async () => {
      vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("ECONNREFUSED")));

      const reflectData = {
        endpoints: [{ provider: "openai", configured: true, models: null, models_url: "http://api-proxy:10000/v1/models" }],
      };
      const logs = [];
      await enrichReflectModels(reflectData, 500, msg => logs.push(msg));
      expect(reflectData.endpoints[0].models).toBeNull();
      expect(logs.some(l => l.includes("models fetch error"))).toBe(true);
    });

    it("handles empty endpoints array", async () => {
      const reflectData = { endpoints: [] };
      const logger = () => {};
      await enrichReflectModels(reflectData, 1000, logger);
      expect(reflectData.endpoints).toEqual([]);
    });
  });

  describe("fetchModelsFromUrl", () => {
    afterEach(() => {
      vi.unstubAllGlobals();
      delete process.env.AWF_AUTH_TYPE;
      delete process.env.AWF_MODELS_URL_OIDC_INITIAL_DELAY_MS;
      vi.useRealTimers();
    });

    it("returns model IDs on successful fetch", async () => {
      const modelData = { data: [{ id: "gpt-4o" }] };
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => modelData }));

      const logs = [];
      const result = await fetchModelsFromUrl("http://api-proxy:10000/v1/models", 1000, msg => logs.push(msg));
      expect(result).toEqual(["gpt-4o"]);
      expect(logs.some(l => l.includes("fetched 1 model(s)"))).toBe(true);
    });

    it("returns null on non-ok HTTP status", async () => {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 403 }));

      const logs = [];
      const result = await fetchModelsFromUrl("http://api-proxy:10000/v1/models", 1000, msg => logs.push(msg));
      expect(result).toBeNull();
      expect(logs.some(l => l.includes("models fetch returned 403"))).toBe(true);
    });

    it("returns null on network error", async () => {
      vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("ECONNREFUSED")));

      const logs = [];
      const result = await fetchModelsFromUrl("http://api-proxy:10000/v1/models", 1000, msg => logs.push(msg));
      expect(result).toBeNull();
      expect(logs.some(l => l.includes("models fetch error"))).toBe(true);
    });

    it("retries on 503 and eventually succeeds", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn()
          .mockResolvedValueOnce({ ok: false, status: 503 })
          .mockResolvedValueOnce({ ok: false, status: 503 })
          .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ data: [{ id: "gpt-4o" }] }) })
      );

      const logs = [];
      const result = await fetchModelsFromUrl("http://api-proxy:10000/v1/models", 1000, msg => logs.push(msg));
      expect(result).toEqual(["gpt-4o"]);
      expect(logs.filter(l => l.includes("retrying (attempt")).length).toBe(2);
      expect(logs.some(l => l.includes("fetched 1 model(s)"))).toBe(true);
    });

    it("stops retrying after max attempts on repeated 503 responses", async () => {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 503 }));

      const logs = [];
      const result = await fetchModelsFromUrl("http://api-proxy:10000/v1/models", 1000, msg => logs.push(msg));
      expect(result).toBeNull();
      expect(logs.filter(l => l.includes("retrying (attempt")).length).toBe(AWF_MODELS_URL_MAX_ATTEMPTS - 1);
      expect(logs.some(l => l.includes("models fetch returned 503"))).toBe(true);
    });

    it("retries on 429 and eventually succeeds", async () => {
      vi.stubGlobal(
        "fetch",
        vi
          .fn()
          .mockResolvedValueOnce({ ok: false, status: 429, headers: { get: () => null } })
          .mockResolvedValueOnce({ ok: false, status: 429, headers: { get: () => null } })
          .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ data: [{ id: "gpt-4o" }] }) })
      );

      const logs = [];
      const result = await fetchModelsFromUrl("http://api-proxy:10000/v1/models", 1000, msg => logs.push(msg));
      expect(result).toEqual(["gpt-4o"]);
      expect(logs.filter(l => l.includes("retrying (attempt")).length).toBe(2);
      expect(logs.some(l => l.includes("models fetch returned 429"))).toBe(true);
      expect(logs.some(l => l.includes("fetched 1 model(s)"))).toBe(true);
    });

    it("stops retrying after max attempts on repeated 429 responses", async () => {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 429, headers: { get: () => null } }));

      const logs = [];
      const result = await fetchModelsFromUrl("http://api-proxy:10000/v1/models", 1000, msg => logs.push(msg));
      expect(result).toBeNull();
      expect(logs.filter(l => l.includes("retrying (attempt")).length).toBe(AWF_MODELS_URL_MAX_ATTEMPTS - 1);
      expect(logs.some(l => l.includes("models fetch returned 429"))).toBe(true);
    });

    it("honors a valid Retry-After header when retrying a 429", async () => {
      vi.useFakeTimers();
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: false, status: 429, headers: { get: name => (name === "retry-after" ? "3" : null) } })
        .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ data: [{ id: "gpt-4o" }] }) });
      vi.stubGlobal("fetch", fetchMock);

      const logs = [];
      const run = fetchModelsFromUrl("http://api-proxy:10000/v1/models", 1000, msg => logs.push(msg));

      // Let the first attempt settle before advancing timers.
      await vi.advanceTimersByTimeAsync(0);
      expect(fetchMock).toHaveBeenCalledTimes(1);

      // Retry-After: 3 requests a 3000ms wait, but it is capped to AWF_MODELS_URL_RETRY_MAX_MS (2000ms).
      await vi.advanceTimersByTimeAsync(AWF_MODELS_URL_RETRY_MAX_MS - 1);
      expect(fetchMock).toHaveBeenCalledTimes(1);

      await vi.advanceTimersByTimeAsync(1);
      const result = await run;

      expect(result).toEqual(["gpt-4o"]);
      expect(fetchMock).toHaveBeenCalledTimes(2);
    });

    it.each([400, 401, 403])("does not retry a permanent %d response", async status => {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status }));

      const logs = [];
      const result = await fetchModelsFromUrl("http://api-proxy:10000/v1/models", 1000, msg => logs.push(msg));
      expect(result).toBeNull();
      expect(logs.some(l => l.includes("retrying (attempt"))).toBe(false);
      expect(logs.some(l => l.includes(`models fetch returned ${status}`))).toBe(true);
    });

    it("delays initial probe for github-oidc auth when probing api-proxy", async () => {
      vi.useFakeTimers();
      process.env.AWF_AUTH_TYPE = "github-oidc";
      process.env.AWF_MODELS_URL_OIDC_INITIAL_DELAY_MS = "5000";

      const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ data: [{ id: "gpt-4o" }] }) });
      vi.stubGlobal("fetch", fetchMock);

      const logs = [];
      const run = fetchModelsFromUrl("http://api-proxy:10001/v1/models", 1000, msg => logs.push(msg));

      await vi.advanceTimersByTimeAsync(4999);
      expect(fetchMock).not.toHaveBeenCalled();

      await vi.advanceTimersByTimeAsync(1);
      await run;

      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(logs.some(l => l.includes("delaying initial models probe"))).toBe(true);
    });
  });

  describe("fetchAWFReflect", () => {
    afterEach(() => {
      vi.unstubAllGlobals();
      vi.unstubAllEnvs();
    });

    it("skips network requests when reflection is disabled", async () => {
      const fetchMock = vi.fn();
      const logs = [];
      vi.stubGlobal("fetch", fetchMock);
      vi.stubEnv("GH_AW_SKIP_REFLECT", "true");

      await expect(
        fetchAWFReflect({
          reflectUrl: "http://api-proxy:10000/reflect",
          outputPath: "/tmp/gh-aw-test-noop.json",
          logger: msg => logs.push(msg),
        })
      ).resolves.toEqual({
        ok: false,
        reflectUrl: "http://api-proxy:10000/reflect",
        outputPath: "/tmp/gh-aw-test-noop.json",
        reason: "disabled",
      });
      expect(fetchMock).not.toHaveBeenCalled();
      expect(logs).toContain("awf-reflect: disabled by GH_AW_SKIP_REFLECT");
    });

    it("saves enriched reflect data when api-proxy returns null models for configured provider", async () => {
      const modelData = { data: [{ id: "gpt-4o" }, { id: "gpt-4o-mini" }] };
      const reflectPayload = {
        endpoints: [{ provider: "openai", port: 10000, configured: true, models: null, models_url: "http://api-proxy:10000/v1/models" }],
        models_fetch_complete: true,
      };

      vi.stubGlobal(
        "fetch",
        vi.fn().mockImplementation(url => {
          const body = String(url).includes("/reflect") ? reflectPayload : modelData;
          return Promise.resolve({ ok: true, status: 200, json: async () => body });
        })
      );

      const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), "awf-reflect-test-"));
      const outputPath = path.join(outputDir, "awf-reflect.json");
      const logs = [];

      try {
        const result = await fetchAWFReflect({
          reflectUrl: "http://api-proxy:10000/reflect",
          outputPath,
          timeoutMs: 3000,
          modelsTimeoutMs: 1000,
          logger: msg => logs.push(msg),
        });

        expect(result).toEqual({
          ok: true,
          reflectUrl: "http://api-proxy:10000/reflect",
          outputPath,
          bytesWritten: expect.any(Number),
          reflectData: expect.objectContaining({ endpoints: expect.any(Array) }),
        });
        const saved = JSON.parse(fs.readFileSync(outputPath, "utf8"));
        expect(saved.endpoints[0].models).toEqual(["gpt-4o", "gpt-4o-mini"]);
        expect(logs.some(l => l.includes("saved "))).toBe(true);
      } finally {
        fs.rmSync(outputDir, { recursive: true, force: true });
      }
    });

    it("does not throw when the reflect endpoint is unreachable", async () => {
      vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("ECONNREFUSED")));
      const logs = [];
      await expect(
        fetchAWFReflect({
          reflectUrl: "http://api-proxy:10000/reflect",
          outputPath: "/tmp/gh-aw-test-noop.json",
          timeoutMs: 500,
          logger: msg => logs.push(msg),
        })
      ).resolves.toEqual({
        ok: false,
        reflectUrl: "http://api-proxy:10000/reflect",
        outputPath: "/tmp/gh-aw-test-noop.json",
        reason: "request_failed",
        error: "ECONNREFUSED",
      });
      expect(logs.some(l => l.includes("request failed"))).toBe(true);
    });

    it("does not throw when the reflect endpoint returns non-ok status", async () => {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 503 }));
      const logs = [];
      await expect(
        fetchAWFReflect({
          reflectUrl: "http://api-proxy:10000/reflect",
          outputPath: "/tmp/gh-aw-test-noop.json",
          timeoutMs: 500,
          logger: msg => logs.push(msg),
        })
      ).resolves.toEqual({
        ok: false,
        reflectUrl: "http://api-proxy:10000/reflect",
        outputPath: "/tmp/gh-aw-test-noop.json",
        reason: "unexpected_status",
        status: 503,
      });
      expect(logs.some(l => l.includes("unexpected status 503"))).toBe(true);
    });

    it("returns reflect data even when persisting awf-reflect output fails", async () => {
      const reflectPayload = { endpoints: [{ provider: "copilot", configured: true, models: ["copilot/claude-sonnet-4.6"] }] };
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => reflectPayload }));
      const logs = [];

      await expect(
        fetchAWFReflect({
          reflectUrl: "http://api-proxy:10000/reflect",
          outputPath: "/tmp/gh-aw-test-read-only/awf-reflect.json",
          timeoutMs: 500,
          logger: msg => logs.push(msg),
          writeFileSync: () => {
            throw new Error("EROFS: read-only file system");
          },
        })
      ).resolves.toEqual({
        ok: true,
        reflectUrl: "http://api-proxy:10000/reflect",
        outputPath: "/tmp/gh-aw-test-read-only/awf-reflect.json",
        reflectData: reflectPayload,
      });
      expect(logs.some(l => l.includes("unable to persist reflect payload"))).toBe(true);
    });

    it("uses the caller-supplied logger for all messages", async () => {
      vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("ECONNREFUSED")));
      const collected = [];
      await fetchAWFReflect({
        reflectUrl: "http://api-proxy:10000/reflect",
        outputPath: "/tmp/gh-aw-test-noop.json",
        timeoutMs: 500,
        logger: msg => collected.push(msg),
      });
      expect(collected.length).toBeGreaterThan(0);
    });
  });

  describe("inferProviderTypeForModel", () => {
    it("returns 'anthropic' for anthropic endpoint provider", () => {
      expect(inferProviderTypeForModel("anthropic", "claude-sonnet-4.6", null)).toBe("anthropic");
    });

    it("returns 'azure' for azure endpoint provider", () => {
      expect(inferProviderTypeForModel("azure", "gpt-4o", null)).toBe("azure");
      expect(inferProviderTypeForModel("azure-openai", "gpt-4o", null)).toBe("azure");
    });

    it("returns 'openai' for openai endpoint provider", () => {
      expect(inferProviderTypeForModel("openai", "gpt-4o", null)).toBe("openai");
    });

    it("uses explicit copilot provider mapping (always openai)", () => {
      // GitHub Copilot provider is a multi-model proxy that always uses OpenAI wire protocol,
      // regardless of model name (even for claude/anthropic models)
      expect(inferProviderTypeForModel("copilot", "claude-sonnet-4.6", null)).toBe("openai");
      expect(inferProviderTypeForModel("copilot", "claude-opus-4-5", null)).toBe("openai");
      expect(inferProviderTypeForModel("github-copilot", "claude-haiku-4.5", null)).toBe("openai");
      // Non-copilot providers still use model name heuristics
      expect(inferProviderTypeForModel("", "claude-haiku-4.5", null)).toBe("anthropic");
    });

    it("uses model name heuristic for opus/haiku/sonnet suffix models when provider is not copilot", () => {
      // copilot provider always returns openai
      expect(inferProviderTypeForModel("copilot", "model-opus-4.6", null)).toBe("openai");
      expect(inferProviderTypeForModel("copilot", "model-haiku-4.5", null)).toBe("openai");
      expect(inferProviderTypeForModel("copilot", "model-sonnet-4", null)).toBe("openai");
      // Non-copilot providers use model name heuristics
      expect(inferProviderTypeForModel("", "model-opus-4.6", null)).toBe("anthropic");
      expect(inferProviderTypeForModel("", "model-haiku-4.5", null)).toBe("anthropic");
      expect(inferProviderTypeForModel("", "model-sonnet-4", null)).toBe("anthropic");
    });

    it("uses model name heuristic for gpt-* models", () => {
      expect(inferProviderTypeForModel("copilot", "gpt-5.4", null)).toBe("openai");
      expect(inferProviderTypeForModel("", "gpt-4o", null)).toBe("openai");
    });

    it("uses model name heuristic for o1/o3/o4 models", () => {
      expect(inferProviderTypeForModel("copilot", "o1-mini", null)).toBe("openai");
      expect(inferProviderTypeForModel("copilot", "o3-pro", null)).toBe("openai");
      expect(inferProviderTypeForModel("copilot", "o4-mini", null)).toBe("openai");
    });

    it("copilot provider always returns openai, even for anthropic models in catalog", () => {
      const modelsJson = {
        providers: {
          "github-copilot": {
            models: {
              "raptor-mini": { provider_type: "openai", cost: {} },
              "claude-sonnet-4": { provider_type: "anthropic", cost: {} },
            },
          },
        },
      };
      expect(inferProviderTypeForModel("copilot", "raptor-mini", modelsJson)).toBe("openai");
      // copilot provider mapping takes precedence over catalog provider_type
      expect(inferProviderTypeForModel("copilot", "claude-sonnet-4", modelsJson)).toBe("openai");
    });

    it("copilot provider always returns openai, even for anthropic model name heuristics", () => {
      const modelsJson = { providers: { "github-copilot": { models: {} } } };
      // copilot provider mapping takes precedence over model name heuristics
      expect(inferProviderTypeForModel("copilot", "claude-unknown-model", modelsJson)).toBe("openai");
    });

    it("returns 'openai' by default for unknown models", () => {
      expect(inferProviderTypeForModel("copilot", "gemini-2.5-pro", null)).toBe("openai");
      expect(inferProviderTypeForModel("", "raptor-mini", null)).toBe("openai");
    });
  });

  describe("getCatalogModelEntry", () => {
    it("matches model names case-insensitively", () => {
      const entry = getCatalogModelEntry(
        {
          providers: {
            "github-copilot": { models: { "gpt-5.5": { provider_type: "openai", wire_api: "responses", cost: {} } } },
          },
        },
        "GPT-5.5"
      );
      expect(entry).toEqual({ provider_type: "openai", wire_api: "responses", cost: {} });
    });

    it("looks up utility variants through their base model after exact matches", () => {
      const modelsJson = {
        providers: {
          "github-copilot": {
            models: {
              "gpt-5.6-luna": { wire_api: "responses" },
              "gpt-5.6-luna-utility": { wire_api: "completions" },
            },
          },
        },
      };
      expect(getCatalogModelEntry(modelsJson, "gpt-5.6-luna-utility?effort=high", "github-copilot")).toEqual({ wire_api: "completions" });
      delete modelsJson.providers["github-copilot"].models["gpt-5.6-luna-utility"];
      expect(getCatalogModelEntry(modelsJson, "gpt-5.6-luna-utility", "github-copilot")).toEqual({ wire_api: "responses" });
    });

    it("uses the requested provider when duplicate model names exist", () => {
      const modelsJson = {
        providers: {
          openai: { models: { "gpt-5.5": { provider_type: "openai", cost: {} } } },
          "github-copilot": { models: { "gpt-5.5": { provider_type: "openai", wire_api: "responses", cost: {} } } },
        },
      };
      expect(getCatalogModelEntry(modelsJson, "gpt-5.5", "github-copilot")).toEqual({
        provider_type: "openai",
        wire_api: "responses",
        cost: {},
      });
      expect(getCatalogModelEntry(modelsJson, "gpt-5.5", "openai")).toEqual({
        provider_type: "openai",
        cost: {},
      });
    });

    it("returns null for invalid catalog entries", () => {
      expect(
        getCatalogModelEntry(
          {
            providers: {
              "github-copilot": { models: { broken: null, arrayish: [] } },
            },
          },
          "broken"
        )
      ).toBeNull();
      expect(
        getCatalogModelEntry(
          {
            providers: {
              "github-copilot": { models: { broken: null, arrayish: [] } },
            },
          },
          "arrayish"
        )
      ).toBeNull();
    });
  });

  describe("inferWireApiForModel", () => {
    it("omits wireApi for Anthropic providers even when the catalog requests one", () => {
      expect(inferWireApiForModel("anthropic", "claude-opus-5", { wire_api: "responses" })).toBeUndefined();
    });

    it("defaults GPT-5+ models to Responses when the catalog value is invalid or absent", () => {
      expect(inferWireApiForModel("openai", "gpt-5.5", { wire_api: "grpc" })).toBe("responses");
      expect(inferWireApiForModel("openai", "gpt-5.5", null)).toBe("responses");
      expect(inferWireApiForModel("openai", "gpt-7-preview", null)).toBe("responses");
      expect(inferWireApiForModel("openai", "gpt-4.1", null)).toBe("completions");
    });

    it.each(["gpt-5-mini", "gpt-6-luna"])("uses the bundled Responses API metadata for %s", model => {
      const modelsJson = JSON.parse(fs.readFileSync(path.join(import.meta.dirname, "models.json"), "utf8"));
      const catalogEntry = getCatalogModelEntry(modelsJson, model, "openai");
      expect(catalogEntry.wire_api).toBe("responses");
      expect(inferWireApiForModel("openai", model, catalogEntry)).toBe("responses");
    });
  });

  describe("resolveMultiProviderFromReflect", () => {
    it("returns null when reflectData is null", () => {
      const logs = [];
      const result = resolveMultiProviderFromReflect({ reflectData: null, logger: msg => logs.push(msg) });
      expect(result).toBeNull();
      expect(logs.some(l => l.includes("no reflect data provided"))).toBe(true);
    });

    it("resolves with a single configured endpoint", () => {
      const result = resolveMultiProviderFromReflect({
        reflectData: { endpoints: [{ provider: "copilot", port: 10002, configured: true, models: ["gpt-5.4"] }] },
      });
      expect(result).not.toBeNull();
      expect(result.providers).toHaveLength(1);
      expect(result.model).toBe("gpt-5.4");
    });

    it("rewrites provider baseUrl to the host bridge in sbx HOSTALIASES mode", () => {
      const originalHostAliases = process.env.HOSTALIASES;
      const aliasesPath = path.join(os.tmpdir(), `awf-hostaliases-${Date.now()}-${Math.random().toString(36).slice(2)}`);
      fs.writeFileSync(aliasesPath, "api-proxy localhost\n", "utf8");
      process.env.HOSTALIASES = aliasesPath;
      try {
        const result = resolveMultiProviderFromReflect({
          reflectData: { endpoints: [{ provider: "copilot", port: 10002, configured: true, models: ["gpt-5.4"] }] },
        });
        expect(result.providers[0].baseUrl).toBe("http://host.docker.internal:10002");
      } finally {
        if (originalHostAliases === undefined) {
          delete process.env.HOSTALIASES;
        } else {
          process.env.HOSTALIASES = originalHostAliases;
        }
        fs.rmSync(aliasesPath, { force: true });
      }
    });

    it("returns null when no configured endpoints exist", () => {
      const result = resolveMultiProviderFromReflect({
        reflectData: {
          endpoints: [
            { provider: "openai", port: 10001, configured: false, models: ["gpt-4o"] },
            { provider: "anthropic", port: 10002, configured: false, models: ["claude-sonnet-4.6"] },
          ],
        },
      });
      expect(result).toBeNull();
    });

    it("builds providers and models from two configured endpoints", () => {
      const reflectData = {
        endpoints: [
          { provider: "openai", port: 10001, configured: true, models: ["gpt-4o"] },
          { provider: "anthropic", port: 10002, configured: true, models: ["claude-sonnet-4.6"] },
        ],
      };
      const result = resolveMultiProviderFromReflect({ reflectData });
      expect(result).not.toBeNull();
      expect(result.providers).toHaveLength(2);
      expect(result.models).toHaveLength(2);
      expect(result.providers[0]).toMatchObject({ name: "openai", type: "openai", baseUrl: "http://api-proxy:10001", wireApi: "completions" });
      expect(result.providers[1]).toMatchObject({ name: "anthropic", type: "anthropic", baseUrl: "http://api-proxy:10002" });
      expect(result.providers[1]).not.toHaveProperty("wireApi");
      expect(result.models[0]).toEqual({ id: "gpt-4o", provider: "openai" });
      expect(result.models[1]).toEqual({ id: "claude-sonnet-4.6", provider: "anthropic" });
    });

    it("sets primary model to first model when no configured model provided", () => {
      const reflectData = {
        endpoints: [
          { provider: "openai", port: 10001, configured: true, models: ["gpt-5.4"] },
          { provider: "anthropic", port: 10002, configured: true, models: ["claude-sonnet-4.6"] },
        ],
      };
      const result = resolveMultiProviderFromReflect({ reflectData });
      expect(result.model).toBe("gpt-5.4");
    });

    it("prefers the configured model when it appears in model list", () => {
      const reflectData = {
        endpoints: [
          { provider: "openai", port: 10001, configured: true, models: ["gpt-5.4"] },
          { provider: "anthropic", port: 10002, configured: true, models: ["claude-sonnet-4.6"] },
        ],
      };
      const result = resolveMultiProviderFromReflect({ reflectData, model: "claude-sonnet-4.6" });
      expect(result.model).toBe("claude-sonnet-4.6");
    });

    it("falls back to first model when configured model is not found in list", () => {
      const reflectData = {
        endpoints: [
          { provider: "openai", port: 10001, configured: true, models: ["gpt-5.4"] },
          { provider: "anthropic", port: 10002, configured: true, models: ["claude-sonnet-4.6"] },
        ],
      };
      const result = resolveMultiProviderFromReflect({ reflectData, model: "nonexistent-model" });
      expect(result.model).toBe("gpt-5.4");
    });

    it("derives provider baseUrl from models_url origin when available", () => {
      const reflectData = {
        endpoints: [
          { provider: "openai", port: 10001, configured: true, models: ["gpt-4o"], models_url: "http://172.30.0.10:10001/v1/models" },
          { provider: "anthropic", port: 10002, configured: true, models: ["claude-sonnet-4.6"], models_url: "http://172.30.0.11:10002/v1/models" },
        ],
      };
      const result = resolveMultiProviderFromReflect({ reflectData });
      expect(result.providers[0].baseUrl).toBe("http://172.30.0.10:10001");
      expect(result.providers[1].baseUrl).toBe("http://172.30.0.11:10002");
    });

    it("infers openai wireApi from modelsJson catalog for openai endpoint", () => {
      const reflectData = {
        endpoints: [
          { provider: "copilot", port: 10002, configured: true, models: ["gpt-5.5"] },
          { provider: "anthropic", port: 10003, configured: true, models: ["claude-sonnet-4.6"] },
        ],
      };
      const modelsJson = {
        providers: {
          "github-copilot": { models: { "gpt-5.5": { provider_type: "openai", wire_api: "responses", cost: {} } } },
        },
      };
      const result = resolveMultiProviderFromReflect({ reflectData, modelsJson });
      expect(result.providers[0]).toMatchObject({ name: "copilot", type: "openai", wireApi: "responses" });
      expect(result.providers[1]).toMatchObject({ name: "anthropic", type: "anthropic" });
      expect(result.providers[1]).not.toHaveProperty("wireApi");
    });

    it("infers wireApi from the base catalog model for a Copilot utility variant", () => {
      const reflectData = {
        endpoints: [{ provider: "copilot", port: 10002, configured: true, models: ["gpt-5.6-luna-utility"] }],
      };
      const modelsJson = {
        providers: {
          "github-copilot": { models: { "gpt-5.6-luna": { provider_type: "openai", wire_api: "responses", cost: {} } } },
        },
      };
      const result = resolveMultiProviderFromReflect({
        model: "gpt-5.6-luna-utility",
        reflectData,
        modelsJson,
      });
      expect(result.providers[0]).toMatchObject({ name: "copilot", type: "openai", wireApi: "responses" });
    });

    it.each([null, { providers: { "github-copilot": { models: { "gpt-6-luna": { wire_api: "completions" } } } } }])("honors an explicit Responses API for gpt-6-luna with a missing or stale catalog", modelsJson => {
      const result = resolveMultiProviderFromReflect({
        model: "gpt-6-luna",
        wireApi: "responses",
        reflectData: { endpoints: [{ provider: "copilot", port: 10002, configured: true, models: ["gpt-6-luna"] }] },
        modelsJson,
      });
      expect(result.model).toBe("gpt-6-luna");
      expect(result.providers[0]).toMatchObject({ name: "copilot", type: "openai", wireApi: "responses" });
    });

    it("applies the wire API override only to the selected provider", () => {
      const result = resolveMultiProviderFromReflect({
        model: "gpt-6-luna",
        wireApi: "responses",
        reflectData: {
          endpoints: [
            { provider: "copilot", port: 10001, configured: true, models: ["gpt-4o"] },
            { provider: "copilot", port: 10002, configured: true, models: ["gpt-6-luna"] },
          ],
        },
      });
      expect(result.providers[0].wireApi).toBe("completions");
      expect(result.providers[1]).toMatchObject({ name: "copilot-1", wireApi: "responses" });
    });

    it("honors an explicit Completions API over catalog inference", () => {
      const result = resolveMultiProviderFromReflect({
        model: "gpt-6-luna",
        wireApi: "completions",
        reflectData: { endpoints: [{ provider: "copilot", port: 10002, configured: true, models: ["gpt-6-luna"] }] },
        modelsJson: { providers: { "github-copilot": { models: { "gpt-6-luna": { wire_api: "responses" } } } } },
      });
      expect(result.providers[0].wireApi).toBe("completions");
    });

    it.each(["Responses", " responses "])("normalizes explicit wire API overrides (%s)", wireApi => {
      const result = resolveMultiProviderFromReflect({
        model: "gpt-6-luna",
        wireApi,
        reflectData: { endpoints: [{ provider: "copilot", port: 10002, configured: true, models: ["gpt-6-luna"] }] },
      });
      expect(result.providers[0].wireApi).toBe("responses");
    });

    it("applies an explicit wire API override to the fallback primary model", () => {
      const result = resolveMultiProviderFromReflect({
        model: "nonexistent-model",
        wireApi: "responses",
        reflectData: { endpoints: [{ provider: "copilot", port: 10002, configured: true, models: ["gpt-6-luna"] }] },
      });
      expect(result.model).toBe("gpt-6-luna");
      expect(result.providers[0].wireApi).toBe("responses");
    });

    it("infers the fallback primary model wire API instead of inheriting an automatic GPT default", () => {
      const result = resolveMultiProviderFromReflect({
        model: "gpt-6-luna",
        reflectData: { endpoints: [{ provider: "openai", port: 10002, configured: true, models: ["gpt-4.1"] }] },
        modelsJson: { providers: { openai: { models: { "gpt-4.1": { wire_api: "completions" } } } } },
      });
      expect(result.model).toBe("gpt-4.1");
      expect(result.providers[0].wireApi).toBe("completions");
    });

    it("ignores wire API overrides for Anthropic providers", () => {
      const result = resolveMultiProviderFromReflect({
        model: "claude-opus-5",
        wireApi: "responses",
        reflectData: { endpoints: [{ provider: "anthropic", port: 10002, configured: true, models: ["claude-opus-5"] }] },
      });
      expect(result.providers[0]).not.toHaveProperty("wireApi");
    });

    it.each(["", "grpc", undefined])("keeps catalog inference for an invalid or absent wire API override (%s)", wireApi => {
      const result = resolveMultiProviderFromReflect({
        model: "gpt-6-luna",
        wireApi,
        reflectData: { endpoints: [{ provider: "copilot", port: 10002, configured: true, models: ["gpt-6-luna"] }] },
        modelsJson: { providers: { "github-copilot": { models: { "gpt-6-luna": { wire_api: "responses" } } } } },
      });
      expect(result.providers[0].wireApi).toBe("responses");
    });

    it("handles duplicate provider names by appending a numeric suffix", () => {
      const reflectData = {
        endpoints: [
          { provider: "copilot", port: 10001, configured: true, models: ["gpt-5.4"] },
          { provider: "copilot", port: 10002, configured: true, models: ["gpt-5.5"] },
        ],
      };
      const result = resolveMultiProviderFromReflect({ reflectData });
      expect(result.providers[0].name).toBe("copilot");
      expect(result.providers[1].name).toBe("copilot-1");
      expect(result.models[0]).toEqual({ id: "gpt-5.4", provider: "copilot" });
      expect(result.models[1]).toEqual({ id: "gpt-5.5", provider: "copilot-1" });
    });

    it("skips endpoints with no resolvable baseUrl", () => {
      const logs = [];
      const reflectData = {
        endpoints: [
          { provider: "openai", port: 10001, configured: true, models: ["gpt-4o"] },
          // no port and no models_url — skipped
          { provider: "anthropic", configured: true, models: ["claude-sonnet-4.6"] },
          { provider: "azure", port: 10003, configured: true, models: ["gpt-4o-azure"] },
        ],
      };
      const result = resolveMultiProviderFromReflect({ reflectData, logger: msg => logs.push(msg) });
      expect(result).not.toBeNull();
      expect(result.providers).toHaveLength(2);
      expect(result.providers.map(p => p.name)).toEqual(["openai", "azure"]);
      expect(logs.some(l => l.includes("no resolvable baseUrl"))).toBe(true);
    });

    it("collects all models from all endpoints", () => {
      const reflectData = {
        endpoints: [
          { provider: "openai", port: 10001, configured: true, models: ["gpt-4o", "gpt-5.4"] },
          { provider: "anthropic", port: 10002, configured: true, models: ["claude-sonnet-4.6", "claude-opus-5"] },
        ],
      };
      const result = resolveMultiProviderFromReflect({ reflectData });
      expect(result.models).toHaveLength(4);
      expect(result.models).toContainEqual({ id: "gpt-4o", provider: "openai" });
      expect(result.models).toContainEqual({ id: "gpt-5.4", provider: "openai" });
      expect(result.models).toContainEqual({ id: "claude-sonnet-4.6", provider: "anthropic" });
      expect(result.models).toContainEqual({ id: "claude-opus-5", provider: "anthropic" });
    });
  });
});
