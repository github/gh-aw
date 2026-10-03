import { describe, it, expect } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { injectModelFlagAfterExec, normalizeCodexModel, normalizeCodexModelArgs } = require("./model_fallback.cjs");

describe("Codex model arguments", () => {
  it.each([{ flags: ["--model", "user-model"] }, { flags: ["-m", "user-model"] }, { flags: ["--model=user-model"] }])("respects an existing model flag %j", ({ flags }) => {
    const args = ["exec", ...flags, "-"];
    expect(injectModelFlagAfterExec(args, "env-model")).toEqual(args);
  });

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

  it.each([{ flags: ["--model", "copilot/gpt-5"] }, { flags: ["-m", "copilot/gpt-5"] }, { flags: ["--model=copilot/gpt-5"] }])("normalizes explicit flags as well as model environment values", ({ flags }) => {
    expect(normalizeCodexModelArgs(["exec", ...flags, "-"], "github").join(" ")).not.toContain("copilot/");
  });

  it("does not interpret a positional prompt as an option after the separator", () => {
    expect(normalizeCodexModelArgs(["exec", "--", "--model=copilot/gpt-5"], "openai")).toEqual(["exec", "--", "--model=copilot/gpt-5"]);
  });
});
