import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

describe("pi_provider.cjs", () => {
  let module;
  let originalEnv;
  let originalFetch;
  let originalExitCode;
  let stderrOutput;

  beforeEach(async () => {
    originalEnv = { ...process.env };
    originalFetch = global.fetch;
    originalExitCode = process.exitCode;
    stderrOutput = [];
    vi.spyOn(process.stderr, "write").mockImplementation(msg => {
      stderrOutput.push(String(msg));
      return true;
    });
    module = await import("./pi_provider.cjs?" + Date.now());
  });

  afterEach(() => {
    process.env = originalEnv;
    global.fetch = originalFetch;
    process.exitCode = originalExitCode;
    vi.restoreAllMocks();
  });

  it("prefers GH_AW_PI_MODEL over PI_MODEL", () => {
    process.env.GH_AW_PI_MODEL = "copilot/claude-sonnet-4";
    process.env.PI_MODEL = "anthropic/claude-opus-4";

    expect(module.getConfiguredModel()).toBe("copilot/claude-sonnet-4");
  });

  it("registers configured providers and aliases from the environment", () => {
    process.env.COPILOT_GITHUB_TOKEN = "copilot-token";
    process.env.GITHUB_COPILOT_BASE_URL = "https://copilot.example.test";
    process.env.ANTHROPIC_API_KEY = "anthropic-token";
    process.env.ANTHROPIC_BASE_URL = "https://anthropic.example.test";
    process.env.CODEX_API_KEY = "codex-token";
    process.env.OPENAI_BASE_URL = "https://openai.example.test";

    const calls = [];
    const pi = {
      registerProvider: vi.fn((name, config) => {
        calls.push([name, config]);
      }),
      on: vi.fn(),
    };

    const count = module.registerConfiguredProviders(pi, () => {});

    expect(count).toBe(5);
    expect(calls).toEqual([
      ["github-copilot", { apiKey: "copilot-token", api: "openai-completions", baseUrl: "https://copilot.example.test" }],
      ["copilot", { apiKey: "copilot-token", api: "openai-completions", baseUrl: "https://copilot.example.test" }],
      ["anthropic", { apiKey: "anthropic-token", api: "anthropic", baseUrl: "https://anthropic.example.test" }],
      ["openai", { apiKey: "codex-token", api: "openai-responses", baseUrl: "https://openai.example.test" }],
      ["codex", { apiKey: "codex-token", api: "openai-responses", baseUrl: "https://openai.example.test" }],
    ]);
  });

  it("logs the configured provider using GH_AW_PI_MODEL during agent_start", async () => {
    process.env.GH_AW_PI_MODEL = "copilot/claude-sonnet-4";
    global.fetch = vi.fn().mockRejectedValue(new Error("network disabled"));

    const handlers = {};
    const pi = {
      registerProvider: vi.fn(),
      on: vi.fn((event, handler) => {
        handlers[event] = handler;
      }),
    };

    module.default(pi);
    await handlers.agent_start();

    expect(stderrOutput.some(line => line.includes("provider=copilot model=copilot/claude-sonnet-4"))).toBe(true);
  });

  it("logs provider request and response diagnostics for inference calls", async () => {
    process.env.GH_AW_PI_MODEL = "copilot/claude-sonnet-4";

    const handlers = {};
    const pi = {
      registerProvider: vi.fn(),
      on: vi.fn((event, handler) => {
        handlers[event] = handler;
      }),
    };
    const ctx = {
      model: {
        provider: "copilot",
        id: "claude-sonnet-4",
        api: "openai-completions",
        baseUrl: "http://api-proxy:10002/v1",
      },
    };

    module.default(pi);
    await handlers.before_provider_request({ type: "before_provider_request", payload: {} }, ctx);
    await handlers.after_provider_response(
      {
        type: "after_provider_response",
        status: 503,
        headers: {
          "content-type": "application/json",
          "x-request-id": "req-123",
        },
      },
      ctx
    );

    expect(stderrOutput.some(line => line.includes("provider_request provider=copilot model=claude-sonnet-4 api=openai-completions method=POST url=http://api-proxy:10002/v1/chat/completions"))).toBe(true);
    expect(stderrOutput.some(line => line.includes("provider_response provider=copilot model=claude-sonnet-4 status=503 method=POST url=http://api-proxy:10002/v1/chat/completions response_headers=content-type,x-request-id"))).toBe(true);
  });

  it("resolves native Anthropic requests to the Messages API", () => {
    expect(
      module.resolveProviderRequestTarget({
        api: "anthropic-messages",
        baseUrl: "http://api-proxy:10001",
      })
    ).toEqual({
      api: "anthropic-messages",
      method: "POST",
      url: "http://api-proxy:10001/v1/messages",
    });
  });

  it("keeps legacy Anthropic requests on the compatibility endpoint", () => {
    expect(
      module.resolveProviderRequestTarget({
        api: "anthropic",
        baseUrl: "http://anthropic.example.test",
      })
    ).toEqual({
      api: "anthropic",
      method: "POST",
      url: "http://anthropic.example.test/messages",
    });
  });

  it("fails the run when every provider request fails", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-provider-"));
    process.env.GH_AW_SAFE_OUTPUTS = path.join(tempDir, "outputs.jsonl");
    process.env.GH_AW_SAFEOUTPUTS_CLI = "true";

    const handlers = {};
    const pi = {
      registerProvider: vi.fn(),
      on: vi.fn((event, handler) => {
        handlers[event] = handler;
      }),
    };
    const ctx = {
      model: {
        provider: "aw-gateway",
        id: "claude-sonnet-5",
        api: "anthropic-messages",
        baseUrl: "http://api-proxy:10001",
      },
    };

    module.default(pi);
    await handlers.before_provider_request({ type: "before_provider_request", payload: {} }, ctx);
    await handlers.after_provider_response({ type: "after_provider_response", status: 404, headers: {} }, ctx);
    await handlers.agent_end();

    expect(process.exitCode).toBe(1);
    expect(stderrOutput.some(line => line.includes("report_incomplete emitted via safeoutputs CLI"))).toBe(true);
  });

  it("does not fail the run when a provider request succeeds after a failure", async () => {
    const handlers = {};
    const pi = {
      registerProvider: vi.fn(),
      on: vi.fn((event, handler) => {
        handlers[event] = handler;
      }),
    };
    const ctx = {
      model: {
        provider: "aw-gateway",
        id: "claude-sonnet-5",
        api: "anthropic-messages",
        baseUrl: "http://api-proxy:10001",
      },
    };

    module.default(pi);
    await handlers.before_provider_request({ type: "before_provider_request", payload: {} }, ctx);
    await handlers.after_provider_response({ type: "after_provider_response", status: 503, headers: {} }, ctx);
    await handlers.before_provider_request({ type: "before_provider_request", payload: {} }, ctx);
    await handlers.after_provider_response({ type: "after_provider_response", status: 200, headers: {} }, ctx);
    await handlers.agent_end();

    expect(process.exitCode).toBe(originalExitCode);
  });

  it("fails the run when a successful response ends with a stream error", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-provider-"));
    process.env.GH_AW_SAFE_OUTPUTS = path.join(tempDir, "outputs.jsonl");
    process.env.GH_AW_SAFEOUTPUTS_CLI = "true";

    const handlers = {};
    const pi = {
      registerProvider: vi.fn(),
      on: vi.fn((event, handler) => {
        handlers[event] = handler;
      }),
    };
    const ctx = {
      model: {
        provider: "aw-gateway",
        id: "claude-sonnet-5",
        api: "anthropic-messages",
        baseUrl: "http://api-proxy:10001",
      },
    };

    module.default(pi);
    await handlers.before_provider_request({}, ctx);
    await handlers.after_provider_response({ status: 200, headers: {} }, ctx);
    await handlers.message_end({
      message: {
        role: "assistant",
        provider: "aw-gateway",
        model: "claude-sonnet-5",
        stopReason: "error",
        errorMessage: "stream interrupted",
      },
    });
    await handlers.agent_end();

    expect(process.exitCode).toBe(1);
  });

  // Triggers the message_end infrastructure-error handler with a given stand-in
  // GH_AW_SAFEOUTPUTS_CLI override ('true' simulates a successful CLI call, 'false'
  // simulates a failed one) and returns the handlers/stderr output for assertions.
  async function triggerConnectionError(cliOverride) {
    process.env.GH_AW_PI_MODEL = "copilot/claude-sonnet-4";
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-provider-"));
    process.env.GH_AW_SAFE_OUTPUTS = path.join(tempDir, "outputs.jsonl");
    process.env.GH_AW_SAFEOUTPUTS_CLI = cliOverride;

    const handlers = {};
    const pi = {
      registerProvider: vi.fn(),
      on: vi.fn((event, handler) => {
        handlers[event] = handler;
      }),
    };
    const ctx = {
      model: {
        provider: "copilot",
        id: "claude-sonnet-4",
        api: "openai-completions",
        baseUrl: "http://api-proxy:10002/v1",
      },
    };

    module.default(pi);
    await handlers.before_provider_request({ type: "before_provider_request", payload: {} }, ctx);
    await handlers.message_end({
      type: "message_end",
      message: {
        role: "assistant",
        provider: "aw-gateway",
        model: "claude-sonnet-4",
        api: "openai-completions",
        stopReason: "error",
        errorMessage: "Connection error.",
      },
    });
    await handlers.agent_end();
  }

  it("logs assistant inference errors with the last request target", async () => {
    // 'true' is a stand-in safeoutputs CLI binary that always exits 0, simulating a
    // successful emission through the CLI channel instead of a direct fs append.
    await triggerConnectionError("true");

    expect(
      stderrOutput.some(line =>
        line.includes('provider_error provider=aw-gateway model=claude-sonnet-4 api=openai-completions status=no-response method=POST url=http://api-proxy:10002/v1/chat/completions response_headers=none error="Connection error."')
      )
    ).toBe(true);
    // Emission goes through the safeoutputs CLI channel (not a direct fs append), so it
    // survives the read-only sandbox mount that backs GH_AW_SAFE_OUTPUTS in production.
    expect(stderrOutput.some(line => line.includes("report_incomplete emitted via safeoutputs CLI"))).toBe(true);
  });

  it("logs a failure when the safeoutputs CLI channel is unavailable (e.g. read-only sandbox)", async () => {
    // 'false' is a stand-in safeoutputs CLI binary that always exits 1, simulating a
    // failed CLI invocation without ever touching the filesystem directly.
    await triggerConnectionError("false");

    expect(stderrOutput.some(line => line.includes("report_incomplete emission failed"))).toBe(true);
    expect(fs.existsSync(process.env.GH_AW_SAFE_OUTPUTS)).toBe(false);
  });

  it("skips synthetic report_incomplete emission when safe outputs already exist", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-provider-"));
    const safeOutputsPath = path.join(tempDir, "outputs.jsonl");
    process.env.GH_AW_SAFE_OUTPUTS = safeOutputsPath;
    fs.writeFileSync(safeOutputsPath, '{"type":"add_comment","body":"done"}\n');

    const logs = [];
    module.emitInfrastructureIncompleteIfNoSafeOutputs("temporary outage", message => logs.push(message));

    expect(fs.readFileSync(safeOutputsPath, "utf8")).toBe('{"type":"add_comment","body":"done"}\n');
    expect(logs.some(m => m.includes("skipped: safe outputs already recorded"))).toBe(true);
  });

  it("calls /reflect on the management port (10000) when AWF_REFLECT_ENABLED is set", async () => {
    process.env.GH_AW_PI_MODEL = "copilot/claude-sonnet-4";
    process.env.AWF_REFLECT_ENABLED = "1";
    const fetchedUrls = [];
    global.fetch = vi.fn().mockImplementation(url => {
      fetchedUrls.push(url);
      return Promise.reject(new Error("network disabled"));
    });

    const handlers = {};
    const pi = {
      registerProvider: vi.fn(),
      on: vi.fn((event, handler) => {
        handlers[event] = handler;
      }),
    };

    module.default(pi);
    await handlers.agent_start();
    await handlers.agent_end();

    expect(fetchedUrls.every(url => url === "http://api-proxy:10000/reflect")).toBe(true);
    expect(fetchedUrls.length).toBe(2);
  });

  it("logs reflect failure context when the /reflect call fails", async () => {
    process.env.GH_AW_PI_MODEL = "copilot/claude-sonnet-4";
    process.env.AWF_REFLECT_ENABLED = "1";
    global.fetch = vi.fn().mockRejectedValue(new Error("ECONNREFUSED"));

    const handlers = {};
    const pi = {
      registerProvider: vi.fn(),
      on: vi.fn((event, handler) => {
        handlers[event] = handler;
      }),
    };

    module.default(pi);
    await handlers.agent_start();

    const reflectOutputPath = path.join(process.env.RUNNER_TEMP || os.tmpdir(), "awf-reflect.json");
    expect(
      stderrOutput.some(line => line.includes(`reflect_failure phase=agent_start provider=copilot model=copilot/claude-sonnet-4 url=http://api-proxy:10000/reflect output=${reflectOutputPath} reason=request_failed error="ECONNREFUSED"`))
    ).toBe(true);
  });

  it("skips /reflect when AWF_REFLECT_ENABLED is not set", async () => {
    process.env.GH_AW_PI_MODEL = "copilot/claude-sonnet-4";
    delete process.env.AWF_REFLECT_ENABLED;
    global.fetch = vi.fn().mockRejectedValue(new Error("network disabled"));

    const handlers = {};
    const pi = {
      registerProvider: vi.fn(),
      on: vi.fn((event, handler) => {
        handlers[event] = handler;
      }),
    };

    module.default(pi);
    await handlers.agent_start();
    await handlers.agent_end();

    expect(global.fetch).not.toHaveBeenCalled();
  });
});
