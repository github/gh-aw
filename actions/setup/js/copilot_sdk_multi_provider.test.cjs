const { qualifyModelForMultiProvider, parseMultiProviderJson } = require("./copilot_sdk_multi_provider.cjs");

describe("qualifyModelForMultiProvider", () => {
  const config = {
    providers: [{ name: "copilot-responses" }, { name: "copilot-completions-1" }, { name: "openai" }],
    models: [
      { id: "gpt-5.6-luna", provider: "copilot-responses" },
      { id: "claude-haiku-4.5", provider: "copilot-completions-1" },
      { id: "org/model", provider: "openai" },
      { id: "orphan", provider: "missing" },
    ],
  };

  it.each([
    ["gpt-5.6-luna", "copilot-responses/gpt-5.6-luna"],
    ["copilot/gpt-5.6-luna", "copilot-responses/gpt-5.6-luna"],
    ["claude-haiku-4.5", "copilot-completions-1/claude-haiku-4.5"],
    ["copilot/claude-haiku-4.5", "copilot-completions-1/claude-haiku-4.5"],
    ["copilot-completions-1/claude-haiku-4.5", "copilot-completions-1/claude-haiku-4.5"],
    ["org/model", "openai/org/model"],
    ["openai/org/model", "openai/org/model"],
  ])("qualifies %s", (model, expected) => {
    expect(qualifyModelForMultiProvider(model, config)).toBe(expected);
  });

  it.each(["small", "medium", "large", "", "unknown", "orphan", "other/gpt-5.6-luna", "copilot/org/model", undefined])("does not guess %s", model => {
    expect(qualifyModelForMultiProvider(model, config)).toBeNull();
  });

  it("returns null without a catalog", () => {
    expect(qualifyModelForMultiProvider("gpt-5.6-luna", null)).toBeNull();
  });

  it.each(["shared", "copilot/shared"])("rejects ambiguous model %s and accepts explicit qualification", model => {
    const ambiguous = {
      providers: [{ name: "copilot-responses" }, { name: "copilot-completions" }],
      models: [
        { id: "shared", provider: "copilot-responses" },
        { id: "shared", provider: "copilot-completions" },
      ],
    };
    expect(() => qualifyModelForMultiProvider(model, ambiguous)).toThrow(/Ambiguous model.*explicit <provider>\/<model>/);
    expect(qualifyModelForMultiProvider("copilot-completions/shared", ambiguous)).toBe("copilot-completions/shared");
  });

  it("uses the same per-model mapping after JSON handoff", () => {
    const parsed = parseMultiProviderJson(
      JSON.stringify({
        model: "copilot-completions/claude-haiku-4.5",
        providers: [{ name: "copilot-completions", type: "openai", baseUrl: "http://api-proxy:10002", wireApi: "completions" }],
        models: [{ id: "claude-haiku-4.5", provider: "copilot-completions" }],
      })
    );
    expect(qualifyModelForMultiProvider("claude-haiku-4.5", parsed)).toBe(parsed.model);
  });
});
