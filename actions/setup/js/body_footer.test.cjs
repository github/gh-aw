import { beforeEach, describe, expect, it } from "vitest";

const { appendConfiguredBodyFooter } = require("./body_footer.cjs");

describe("appendConfiguredBodyFooter", () => {
  beforeEach(() => {
    global.context = {
      serverUrl: "https://github.com",
      repo: { owner: "owner", repo: "repo" },
      runId: 123,
    };
    process.env.GH_AW_WORKFLOW_NAME = "Policy Workflow";
  });

  it("appends and renders the configured footer", () => {
    expect(appendConfiguredBodyFooter("Body", "From {workflow_name}: {run_url}")).toBe("Body\n\nFrom Policy Workflow: https://github.com/owner/repo/actions/runs/123");
  });

  it("preserves the body when no footer is configured", () => {
    expect(appendConfiguredBodyFooter("Body", undefined)).toBe("Body");
  });

  it.each([undefined, "Footer"])("preserves only explicitly allowed mentions while rendering with footer %s", footer => {
    const body = appendConfiguredBodyFooter("Please ask @copilot and @someone-else", footer, { allowedMentions: ["copilot"], maxLength: 100 });
    expect(body).toBe(`Please ask @copilot and \`@someone-else\`${footer ? "\n\nFooter" : ""}`);
  });

  it("preserves the sanitizer's runtime bot mention aliases", () => {
    expect(appendConfiguredBodyFooter("Please ask @copilot", "Footer", { allowedMentions: ["copilot-swe-agent"] })).toBe("Please ask @copilot\n\nFooter");
  });

  it("neutralizes all mentions by default", () => {
    expect(appendConfiguredBodyFooter("Please ask @copilot", "Footer")).toBe("Please ask `@copilot`\n\nFooter");
  });

  it("does not reactivate mentions neutralized by the caller's mention limit", () => {
    expect(appendConfiguredBodyFooter("Please ask @copilot and `@teammate`", "Footer", { allowedMentions: ["copilot", "teammate"] })).toBe("Please ask @copilot and `@teammate`\n\nFooter");
  });

  it("redacts oversized body text without shortening or omitting the footer", () => {
    const body = "x".repeat(100);
    expect(appendConfiguredBodyFooter(body, "Footer", { maxLength: 100 })).toBe("Content redacted to fit size limit.\n\nFooter");
  });

  it("fails explicitly when the footer alone cannot fit", () => {
    expect(() => appendConfiguredBodyFooter("Body", "f".repeat(101), { maxLength: 100 })).toThrow("protected markers, footers, and framework content");
  });

  it("appends the footer when the composed body fits the destination limit", () => {
    expect(appendConfiguredBodyFooter("Body", "Footer", { maxLength: 100 })).toBe("Body\n\nFooter");
  });
});
