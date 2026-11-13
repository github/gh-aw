import { describe, it, expect } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { CYBERSECURITY_POLICY_REJECTION_PREFIX, isGitHubProviderCybersecurityPolicyRejection } = require("./github_provider_policy_rejection.cjs");

const message = `${CYBERSECURITY_POLICY_REJECTION_PREFIX} Try rephrasing your request.`;

describe("GitHub provider cybersecurity-policy rejection detection", () => {
  it.each([
    ["Copilot SDK query", "copilot", "github", { type: "session.error", data: { errorType: "query", message } }],
    ["Copilot SDK driver", "copilot", "copilot", { type: "session.error", data: { errorType: "sdk_driver", message } }],
    ["Claude CLI result", "claude", "github-copilot", { type: "result", is_error: true, result: message }],
    ["Codex failed turn", "codex", "github_models", { type: "turn.failed", error: { message } }],
  ])("recognizes %s on a GitHub provider", (_name, engine, provider, event) => {
    expect(isGitHubProviderCybersecurityPolicyRejection(JSON.stringify(event), provider, engine)).toBe(true);
  });

  it.each([
    ["non-GitHub provider", "claude", "anthropic", { type: "result", is_error: true, result: message }],
    ["quoted message", "claude", "github", { type: "result", is_error: true, result: `Quoted: ${message}` }],
    ["assistant text", "claude", "github", { type: "assistant", message }],
    ["wrong event type", "codex", "github", { type: "item.completed", error: { message } }],
    ["sub-agent event", "codex", "github", { type: "turn.failed", parentToolCallId: "child", error: { message } }],
    ["snake-case sub-agent event", "claude", "github", { type: "result", is_error: true, agent_id: "child", result: message }],
    ["unrelated error prefix", "copilot", "github", { type: "session.error", data: { errorType: "query", message: "422 Invalid request" } }],
  ])("ignores %s", (_name, engine, provider, event) => {
    expect(isGitHubProviderCybersecurityPolicyRejection(JSON.stringify(event), provider, engine)).toBe(false);
  });
});
