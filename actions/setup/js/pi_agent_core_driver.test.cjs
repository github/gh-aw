import { afterEach, beforeEach, describe, expect, it } from "vitest";

const { buildModel } = await import("./pi_agent_core_driver.cjs");

describe("pi_agent_core_driver.cjs", () => {
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
  });

  afterEach(() => {
    process.env = originalEnv;
  });

  describe("buildModel", () => {
    it("routes the openai provider through openai-responses in native (no-gateway) mode", () => {
      process.env.OPENAI_API_KEY = "test-key";
      const model = buildModel(null, "openai/gpt-5.4");
      expect(model.api).toBe("openai-responses");
      expect(model.provider).toBe("openai");
    });

    it("routes the codex provider through openai-responses in native mode", () => {
      const model = buildModel(null, "codex/gpt-5.4");
      expect(model.api).toBe("openai-responses");
      expect(model.provider).toBe("openai");
    });

    it("keeps the anthropic provider on anthropic-messages in native mode", () => {
      const model = buildModel(null, "anthropic/claude-opus-4");
      expect(model.api).toBe("anthropic-messages");
      expect(model.provider).toBe("anthropic");
    });

    it("keeps the github-copilot provider on openai-completions in native mode", () => {
      const model = buildModel(null, "copilot/claude-sonnet-4");
      expect(model.api).toBe("openai-completions");
      expect(model.provider).toBe("github-copilot");
    });

    it("defaults to github-copilot when no provider prefix is present", () => {
      const model = buildModel(null, "claude-sonnet-4");
      expect(model.api).toBe("openai-completions");
      expect(model.provider).toBe("github-copilot");
    });
  });
});
