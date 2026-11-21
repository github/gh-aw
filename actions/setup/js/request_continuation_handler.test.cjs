import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { main } = require("./request_continuation_handler.cjs");
const { isDependencyUnsupportedType } = require("./safe_output_submission.cjs");
const tools = require("./safe_outputs_tools.json");
const { extractCreatedItemFromResult } = require("./safe_output_manifest.cjs");
const { hasExpectedSafeOutputs, hasTerminalSafeOutput, hasNoopInSafeOutputs } = require("./safeoutputs_cli.cjs");

describe("request_continuation metadata handler", () => {
  beforeEach(() => {
    global.core = { info: vi.fn(), warning: vi.fn() };
    global.github = new Proxy(
      {},
      {
        get: () => {
          throw new Error("No GitHub calls allowed");
        },
      }
    );
  });
  afterEach(() => {
    delete global.core;
    delete global.github;
  });

  it("records metadata without claiming execution or exposing content in logs", async () => {
    const handler = await main();
    const result = await handler({ type: "request_continuation", message: "Continue private follow-up task" });
    expect(result).toEqual({ success: true, message: "Continue private follow-up task", timestamp: expect.any(String) });
    expect(result).not.toHaveProperty("url");
    expect(result).not.toHaveProperty("resumed");
    expect(JSON.stringify(global.core.info.mock.calls)).not.toContain("private follow-up");
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("continuation not confirmed"));
    expect(await handler({ message: "Second request" })).toEqual({ success: false, error: "Max count of 1 reached" });
  });

  it("rejects invalid messages without consuming the budget", async () => {
    const handler = await main();
    for (const message of [undefined, null, "", " \n ", 5, {}, []]) {
      expect(await handler({ message })).toEqual({ success: false, error: "Missing required field: message" });
    }
    expect((await handler({ message: "Continue testing" })).success).toBe(true);
  });

  it("honors an explicit max", async () => {
    const handler = await main({ max: 2 });
    expect((await handler({ message: "one" })).success).toBe(true);
    expect((await handler({ message: "two" })).success).toBe(true);
    expect((await handler({ message: "three" })).success).toBe(false);
  });

  it("honors a resolved integer string max", async () => {
    const handler = await main({ max: "2" });
    expect((await handler({ message: "one" })).success).toBe(true);
    expect((await handler({ message: "two" })).success).toBe(true);
    expect(await handler({ message: "three" })).toEqual({ success: false, error: "Max count of 2 reached" });
  });

  it("rejects invalid runtime maxima", async () => {
    for (const max of [0, -1, -2, 1.5, "2.0", "2e0", "", "2x"]) {
      await expect(main({ max })).rejects.toThrow("max must be a positive integer");
    }
  });

  it("is not a mutating manifest result, terminal task output, or retry-stopping noop", async () => {
    const handler = await main();
    const result = await handler({ message: "Continue remaining work" });
    expect(extractCreatedItemFromResult("request_continuation", result)).toBeNull();
    const readFileSync = () => JSON.stringify({ type: "request_continuation", message: "Continue remaining work" }) + "\n";
    const options = { readFileSync, logger: () => {} };
    expect(hasExpectedSafeOutputs("unused", options)).toBe(false);
    expect(hasTerminalSafeOutput("unused", options)).toBe(false);
    expect(hasTerminalSafeOutput("unused", { ...options, includeReportIncomplete: true, includeMissingData: true })).toBe(false);
    expect(hasNoopInSafeOutputs("unused", options)).toBe(false);
  });

  it("declares only message and rejects dependency participation", () => {
    const tool = tools.find(tool => tool.name === "request_continuation");
    expect(tool.inputSchema.required).toEqual(["message"]);
    expect(Object.keys(tool.inputSchema.properties)).toEqual(["message"]);
    expect(tool.inputSchema.additionalProperties).toBe(false);
    expect(tool.inputSchema.properties.message.pattern).toBe("\\S");
    expect(isDependencyUnsupportedType("request_continuation")).toBe(true);
  });
});
