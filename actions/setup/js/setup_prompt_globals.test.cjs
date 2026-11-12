import { afterEach, describe, expect, it } from "vitest";
import { setupPromptGlobals } from "./setup_prompt_globals.cjs";

const originalEnv = { ...process.env };
const originalCore = global.core;
const originalContext = global.context;

afterEach(() => {
  process.env = { ...originalEnv };
  if (originalCore === undefined) delete global.core;
  else global.core = originalCore;
  if (originalContext === undefined) delete global.context;
  else global.context = originalContext;
});

describe("setupPromptGlobals", () => {
  it("preserves selected bindings and runner context without inherited credentials", () => {
    process.env = {
      GH_AW_PROMPT_ENV: '["GH_AW_PROMPT_CONTENT_0"]',
      GH_AW_PROMPT_CONTENT_0: "prompt text",
      RUNNER_TEMP: "/tmp/runner",
      GITHUB_OUTPUT: "/tmp/output",
      GH_AW_ACTIONS_DIR: "/tmp/actions",
      GITHUB_TOKEN: "inherited-token",
      NODE_OPTIONS: "--require=injected.cjs",
      UNSELECTED: "inherited-value",
    };
    const core = {};
    const context = {};

    setupPromptGlobals(core, context);

    expect(process.env).toEqual({
      GH_AW_PROMPT_CONTENT_0: "prompt text",
      RUNNER_TEMP: "/tmp/runner",
      GITHUB_OUTPUT: "/tmp/output",
      GH_AW_ACTIONS_DIR: "/tmp/actions",
    });
    expect(global.core).toBe(core);
    expect(global.context).toBe(context);
  });

  it.each([undefined, "", "invalid json", "null", "{}", "[1]"])("rejects malformed configuration (%s)", config => {
    if (config === undefined) delete process.env.GH_AW_PROMPT_ENV;
    else process.env.GH_AW_PROMPT_ENV = config;
    expect(() => setupPromptGlobals({}, {})).toThrow("GH_AW_PROMPT_ENV");
  });
});
