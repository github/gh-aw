import { describe, it, expect } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const {
  injectModelFlagAfterExec,
  normalizeCodexModel,
  normalizeCodexModelArgs,
  normalizeClaudeModelArgs,
  readFallbackModels,
  isModelFallbackFailure,
  resolveFallbackSelection,
  recordFallbackModel,
  getFallbackModel,
  replaceModelArgs,
} = require("./model_fallback.cjs");

describe("ordered engine model fallback", () => {
  it("requires explicitly configured model alternatives", () => {
    expect(readFallbackModels({})).toEqual([]);
    expect(readFallbackModels({ GH_AW_FALLBACK_MODELS: '["secondary","last"]' })).toEqual(["secondary", "last"]);
    for (const value of ["[]", "null", '[""]', '["model\\nINJECT=value"]', '["${{ inputs.model }}"]', "{}"]) {
      expect(() => readFallbackModels({ GH_AW_FALLBACK_MODELS: value })).toThrow();
    }
  });

  it.each([
    [{ status: 503, message: "Service unavailable" }, true],
    [{ errorType: "timeout_error" }, true],
    [{ code: "model_not_supported" }, true],
    [{ status: 400, param: "model", message: "invalid model" }, true],
    [{ status: 400, message: "invalid request" }, false],
    [{ status: 429, message: "model unavailable" }, false],
    [{ status: 401, message: "503 Service Unavailable" }, false],
    [{ status: 403, message: "The requested model is not supported" }, false],
    [{ errorType: "authentication_error", status: 503 }, false],
    [{ errorType: "model_policy_violation", status: 503 }, false],
  ])("classifies only model/provider failures: %j", (error, expected) => {
    const output = JSON.stringify({ type: "session.error", data: error });
    expect(isModelFallbackFailure({ exitCode: 1, output })).toBe(expected);
  });

  it("ignores quoted transcript and tool failures", () => {
    for (const output of [
      JSON.stringify({ type: "assistant", message: "503 Service Unavailable" }),
      JSON.stringify({ type: "tool.result", data: { status: 503 } }),
      "assistant:\nThe requested model is not supported",
      "```text\nCAPIError: 503\n```",
    ]) {
      expect(isModelFallbackFailure({ exitCode: 1, output })).toBe(false);
    }
  });

  it.each(["cancelled", "runtimeGuardFired", "watchdogFired"])("does not switch after %s", guard => {
    expect(isModelFallbackFailure({ exitCode: 1, output: "CAPIError: 503", [guard]: true })).toBe(false);
  });

  it("resolves provider-qualified models without silently selecting another endpoint", () => {
    const reflect = {
      endpoints: [
        { provider: "openai", configured: true, port: 10001 },
        { provider: "copilot", configured: true, port: 10002 },
        { provider: "anthropic", configured: true, port: 10003 },
      ],
    };
    expect(resolveFallbackSelection("openai/secondary", "github", reflect)).toMatchObject({ provider: "openai", model: "secondary" });
    expect(resolveFallbackSelection("copilot/secondary", "anthropic", reflect)).toMatchObject({ provider: "github", model: "secondary" });
    expect(resolveFallbackSelection("secondary", "anthropic", reflect)).toMatchObject({ provider: "anthropic", model: "secondary" });
    expect(() => resolveFallbackSelection("anthropic/secondary", "github", { endpoints: [reflect.endpoints[0]] })).toThrow("no configured AWF endpoint");
    expect(() => resolveFallbackSelection("openai/secondary", "github", null)).toThrow("requires AWF");
  });

  it("replaces model flags without rewriting prompt values or positional arguments", () => {
    expect(replaceModelArgs(["exec", "-mprimary", "-"], "secondary")).toEqual(["exec", "-msecondary", "-"]);
    expect(replaceModelArgs(["exec", "-"], "secondary")).toEqual(["exec", "--model", "secondary", "-"]);
    expect(replaceModelArgs(["--prompt", "--model=primary", "--model=primary"], "secondary")).toEqual(["--prompt", "--model=primary", "--model=secondary"]);
    expect(replaceModelArgs(["--", "--model=primary"], "secondary")).toEqual(["--model", "secondary", "--", "--model=primary"]);
  });

  it("resolves aliases to concrete models on provisioned providers", () => {
    const reflect = {
      endpoints: [
        { provider: "openai", configured: true, port: 10001, models: ["secondary"] },
        { provider: "anthropic", configured: true, port: 10003, models: ["last"] },
      ],
    };
    expect(resolveFallbackSelection("recovery", "openai", reflect, { recovery: ["anthropic/last"] })).toMatchObject({
      provider: "anthropic",
      model: "last",
      resolvedModel: "anthropic/last",
    });
    expect(resolveFallbackSelection("recovery", "openai", reflect, { recovery: ["secondary"] })).toMatchObject({
      provider: "openai",
      model: "secondary",
    });
    expect(() => resolveFallbackSelection("recovery", "openai", reflect, { recovery: ["missing"] })).toThrow("model-catalog retrieval");
    expect(() => resolveFallbackSelection("recovery", "openai", null, { recovery: ["secondary"] })).toThrow("model-catalog retrieval");
  });

  it("records the selected model without overwriting main-agent metadata during detection", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-fallback-info-"));
    try {
      const infoPath = path.join(dir, "aw_info.json");
      const envPath = path.join(dir, "github-env");
      const env = { GITHUB_ENV: envPath };
      fs.writeFileSync(infoPath, '{"model":"primary","workflow_name":"example"}');
      recordFallbackModel("openai/secondary", env, infoPath);
      expect(JSON.parse(fs.readFileSync(infoPath, "utf8"))).toMatchObject({ model: "openai/secondary", workflow_name: "example" });
      expect(getFallbackModel(infoPath)).toBe("openai/secondary");
      expect(env.GH_AW_INFO_MODEL).toBe("openai/secondary");
      expect(fs.existsSync(envPath)).toBe(false);
      recordFallbackModel("detector", { GH_AW_PHASE: "detection" }, infoPath);
      expect(getFallbackModel(infoPath)).toBe("openai/secondary");
      expect(getFallbackModel(infoPath, "detection")).toBe("detector");
      expect(() => recordFallbackModel("bad\nINJECT=value", env, infoPath)).toThrow("Invalid");
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});

describe("Claude model arguments", () => {
  it.each(["github", "copilot", "github-copilot", "github_models", " COPILOT "])("normalizes repository-variable models for provider %s", provider => {
    const env = { GH_AW_MODEL_AGENT_CLAUDE: "copilot/claude-haiku-4.5" };
    const args = ["--print", "--model", env.GH_AW_MODEL_AGENT_CLAUDE];
    expect(normalizeClaudeModelArgs(args, provider, env)).toEqual(["--print", "--model", "claude-haiku-4.5"]);
    expect(args[2]).toBe("copilot/claude-haiku-4.5");
    expect(env.GH_AW_MODEL_AGENT_CLAUDE).toBe("copilot/claude-haiku-4.5");
  });

  it("normalizes equals-form model flags while preserving explicit provider overrides", () => {
    expect(normalizeClaudeModelArgs(["--model=copilot/claude-haiku-4.5"], "anthropic", { GH_AW_LLM_PROVIDER_EXPLICIT: "1" })).toEqual(["--model=claude-haiku-4.5"]);
  });

  it("rejects an unprovisioned dynamic provider switch", () => {
    expect(() => normalizeClaudeModelArgs(["--model", "copilot/claude-haiku-4.5"], "anthropic", {})).toThrow("engine.model-provider: github");
  });

  it("preserves custom slugs, missing values and positional arguments after the separator", () => {
    expect(normalizeClaudeModelArgs(["--model", "anthropic/custom-model"], "anthropic", {})).toEqual(["--model", "anthropic/custom-model"]);
    expect(normalizeClaudeModelArgs(["--model"], "anthropic", {})).toEqual(["--model"]);
    expect(normalizeClaudeModelArgs(["--", "--model=copilot/claude-haiku-4.5"], "anthropic", {})).toEqual(["--", "--model=copilot/claude-haiku-4.5"]);
  });
});

describe("Codex model arguments", () => {
  it.each([{ flags: ["--model", "user-model"] }, { flags: ["-m", "user-model"] }, { flags: ["--model=user-model"] }, { flags: ["-m=user-model"] }, { flags: ["-muser-model"] }, { flags: ["--model="] }, { flags: ["-m="] }])(
    "respects an existing model flag %j",
    ({ flags }) => {
      const args = ["exec", ...flags, "-"];
      expect(injectModelFlagAfterExec(args, "env-model")).toEqual(args);
    }
  );

  it.each([
    ["copilot/gpt-5", "github", "gpt-5"],
    ["openai/gpt-5", "openai", "gpt-5"],
    ["gpt-5", "github", "gpt-5"],
  ])("normalizes a model with a matching provisioned provider", (model, provider, expected) => {
    expect(normalizeCodexModel(model, provider)).toBe(expected);
  });

  it("rejects a prefix requiring different credentials or a different proxy", () => {
    expect(() => normalizeCodexModel("copilot/gpt-5", "openai", { env: {} })).toThrow("does not match");
  });

  it.each(["copilot", "github", "github-copilot", "github_models", "COPILOT"])("honors an explicit provider override for the %s alias without changing or logging credentials", prefix => {
    const env = { GH_AW_LLM_PROVIDER_EXPLICIT: "1", GH_AW_LLM_PROVIDER: "openai", OPENAI_API_KEY: "test-secret" };
    const before = { ...env };
    const logs = [];
    const options = { env, logger: message => logs.push(message) };
    expect(normalizeCodexModel(`${prefix}/gpt-5`, "openai", options)).toBe("gpt-5");
    expect(normalizeCodexModelArgs(["exec", `--model=${prefix}/gpt-5`, "-"], "openai", options)).toEqual(["exec", "--model=gpt-5", "-"]);
    expect(env).toEqual(before);
    expect(logs.join("\n")).toContain("explicitly configured provider");
    expect(logs.join("\n")).not.toContain("test-secret");
    expect(() => normalizeCodexModel(`${prefix}/gpt-5`, "openai", { env: {} })).toThrow("does not match");
  });

  it.each([{ flags: ["--model", "copilot/gpt-5"] }, { flags: ["-m", "copilot/gpt-5"] }, { flags: ["--model=copilot/gpt-5"] }, { flags: ["-m=copilot/gpt-5"] }, { flags: ["-mcopilot/gpt-5"] }])(
    "normalizes explicit flags as well as model environment values",
    ({ flags }) => {
      expect(normalizeCodexModelArgs(["exec", ...flags, "-"], "github").join(" ")).not.toContain("copilot/");
    }
  );

  it("does not interpret a positional prompt as an option after the separator", () => {
    expect(normalizeCodexModelArgs(["exec", "--", "--model=copilot/gpt-5"], "openai")).toEqual(["exec", "--", "--model=copilot/gpt-5"]);
  });
});
