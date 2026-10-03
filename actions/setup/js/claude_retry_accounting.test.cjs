import { describe, it, expect } from "vitest";
const { normalizeClaudeSession } = require("./claude_session.cjs");
const { projectSessionResult } = require("./agent_session.cjs");

const result = (session_id, total_cost_usd, input_tokens, num_turns, extra = {}) => ({
  type: "result",
  session_id,
  total_cost_usd,
  num_turns,
  usage: { input_tokens },
  ...extra,
});

describe("Claude retry accounting", () => {
  it("uses the final cumulative snapshot per session and sums independent fresh sessions", () => {
    const events = normalizeClaudeSession([result("a", 0.1, 1000, 3), result("a", 0.2, 2000, 4), result("b", 0.3, 3000, 5), result("child", 10, 100000, 50, { parent_tool_use_id: "task-1" })]);
    expect(projectSessionResult(events)).toMatchObject({ total_cost_usd: 0.5, num_turns: 9, usage: { input_tokens: 5000 } });
  });
  it("does not assign fresh-session response usage to an earlier session", () => {
    const events = normalizeClaudeSession([result("a", 0.1, 1000, 3), { type: "assistant", session_id: "b", message: { id: "b-1", content: [], usage: { input_tokens: 2000, output_tokens: 20 } } }]);
    expect(projectSessionResult(events).usage).toMatchObject({ input_tokens: 3000, output_tokens: 20 });
  });
  it("preserves existing snapshot semantics when session identifiers are unavailable", () => {
    expect(projectSessionResult(normalizeClaudeSession([result(undefined, 0.1, 1000, 3), result(undefined, 0.2, 2000, 4)]))).toMatchObject({ total_cost_usd: 0.2, num_turns: 4, usage: { input_tokens: 2000 } });
  });
});
