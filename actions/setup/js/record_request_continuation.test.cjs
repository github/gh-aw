import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { main } = require("./record_request_continuation.cjs");

describe("record_request_continuation", () => {
  let tempDir;
  let outputPath;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-continuation-"));
    outputPath = path.join(tempDir, "agent_output.json");
    process.env.GH_AW_AGENT_OUTPUT = outputPath;
    process.env.GH_AW_REQUEST_CONTINUATION_MAX = "1";
    global.core = { info: vi.fn(), warning: vi.fn() };
  });

  afterEach(() => {
    delete process.env.GH_AW_AGENT_OUTPUT;
    delete process.env.GH_AW_REQUEST_CONTINUATION_MAX;
    delete global.core;
    fs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("persists trusted timestamps without changing unrelated output", async () => {
    const source = {
      items: [
        { type: "request_continuation", message: "Follow-up work", timestamp: "agent-supplied" },
        { type: "custom_job", message: 42, timestamp: true },
      ],
    };
    fs.writeFileSync(outputPath, JSON.stringify(source));

    await main();

    const written = JSON.parse(fs.readFileSync(outputPath, "utf8"));
    expect(written.items[0].timestamp).not.toBe("agent-supplied");
    expect(Number.isNaN(Date.parse(written.items[0].timestamp))).toBe(false);
    expect(written.items[1]).toEqual(source.items[1]);
    expect(JSON.stringify(global.core.info.mock.calls)).not.toContain("Follow-up work");
  });

  it("only records metadata for a routed workflow and never routes", async () => {
    process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({ request_continuation: { max: 1, route: "dispatch", max_depth: 2, workflow: "summarize", workflow_file: "summarize.lock.yml" } });
    global.github = new Proxy(
      {},
      {
        get: () => {
          throw new Error("No GitHub calls allowed");
        },
      }
    );
    try {
      fs.writeFileSync(outputPath, JSON.stringify({ items: [{ type: "request_continuation", message: "Summarize page 2" }], errors: [] }));
      await main();
      const written = JSON.parse(fs.readFileSync(outputPath, "utf8"));
      expect(Number.isNaN(Date.parse(written.items[0].timestamp))).toBe(false);
      expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("continuation not confirmed"));
    } finally {
      delete process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;
      delete global.github;
    }
  });
});
