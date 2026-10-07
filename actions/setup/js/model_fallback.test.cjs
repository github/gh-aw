import { describe, it, expect } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { injectModelFlagAfterExec, normalizeCodexModel, normalizeCodexModelArgs, normalizeClaudeModelArgs } = require("./model_fallback.cjs");

describe("Claude model arguments", () => {
  const autoReflect = {
    endpoints: [{ provider: "copilot", configured: true, models: ["gpt-6.1-sol", "claude-sonnet-4.6", "claude-opus-5.5", "claude-sonnet-5"] }],
  };

  it.each(["auto", "copilot/auto"])("resolves %s in separate and equals-form flags to an advertised Claude model", model => {
    const options = { reflectData: autoReflect };
    expect(normalizeClaudeModelArgs(["--print", "--model", model], "github", {}, options)).toEqual(["--print", "--model", "claude-sonnet-5"]);
    expect(normalizeClaudeModelArgs([`--model=${model}`], "github", {}, options)).toEqual(["--model=claude-sonnet-5"]);
    expect(normalizeClaudeModelArgs(["--", "--model", model], "github", {}, options)).toEqual(["--", "--model", model]);
  });

  it.each(["auto", "copilot/auto"])("maps %s to the native sonnet alias when Anthropic is explicitly configured", model => {
    expect(normalizeClaudeModelArgs(["--model", model], "anthropic", { GH_AW_LLM_PROVIDER_EXPLICIT: "1" })).toEqual(["--model", "sonnet"]);
  });

  it("preserves auto query parameters and selects only the configured Copilot endpoint", () => {
    const reflectData = {
      endpoints: [{ provider: "anthropic", configured: true, models: ["claude-sonnet-99"] }, { provider: "copilot", configured: false, models: ["claude-sonnet-98"] }, ...autoReflect.endpoints],
    };
    expect(normalizeClaudeModelArgs(["--model", "copilot/auto?effort=high"], "github", {}, { reflectData })).toEqual(["--model", "claude-sonnet-5?effort=high"]);
  });

  it.each([
    [["claude-opus-4.6", "claude-opus-5.5", "claude-haiku-4.5"], "claude-opus-5.5"],
    [["claude-haiku-4.5"], "claude-haiku-4.5"],
  ])("selects an advertised alternate Claude family from %j", (models, expected) => {
    expect(normalizeClaudeModelArgs(["--model", "auto"], "github", {}, { reflectData: { endpoints: [{ provider: "github-copilot", configured: true, models }] } })).toEqual(["--model", expected]);
  });

  it.each(["auto", "copilot/auto"])("rejects %s before startup when Copilot advertises no Claude models", model => {
    expect(() => normalizeClaudeModelArgs(["--model", model], "github", {}, { reflectData: { endpoints: [{ provider: "copilot", configured: true, models: ["gpt-6.1-sol", "auto"] }] } })).toThrow("requires an advertised Claude");
    expect(() => normalizeClaudeModelArgs(["--model", model], "github", {})).toThrow("requires an advertised Claude");
  });

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
    ["auto", "github", "auto"],
    ["copilot/auto", "github", "auto"],
    ["auto", "openai", "auto"],
    ["copilot/gpt-5", "github", "gpt-5"],
    ["openai/gpt-5", "openai", "gpt-5"],
    ["gpt-5", "github", "gpt-5"],
  ])("normalizes a model with a matching provisioned provider", (model, provider, expected) => {
    expect(normalizeCodexModel(model, provider)).toBe(expected);
  });

  it.each(["auto", "copilot/auto"])("preserves %s as the Codex gateway picker in every model flag form", model => {
    for (const flags of [["--model", model], ["-m", model], [`--model=${model}`], [`-m=${model}`], [`-m${model}`]]) {
      expect(normalizeCodexModelArgs(["exec", ...flags, "-"], "github")).toEqual(["exec", ...flags.map(flag => flag.replace("copilot/", "")), "-"]);
    }
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
