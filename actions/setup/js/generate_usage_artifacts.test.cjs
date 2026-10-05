import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "module";

const req = createRequire(import.meta.url);
const { main } = req("./generate_usage_artifacts.cjs");

describe("generate_usage_artifacts.cjs", () => {
  let tempDirectory;

  afterEach(() => {
    if (tempDirectory) {
      fs.rmSync(tempDirectory, { recursive: true, force: true });
      tempDirectory = undefined;
    }
  });

  it("warns on generator failures and still lists usage files", async () => {
    tempDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "usage-artifacts-"));
    fs.mkdirSync(path.join(tempDirectory, "nested"));
    fs.writeFileSync(path.join(tempDirectory, "nested", "session.jsonl"), "");
    fs.writeFileSync(path.join(tempDirectory, "summary.json"), "");

    const core = { info: vi.fn(), warning: vi.fn() };
    const generateSummary = vi.fn().mockRejectedValue(new Error("summary failed"));
    const generateSession = vi.fn().mockRejectedValue("session failed");

    await main({
      core,
      generateSummary,
      generateSession,
      usageDirectory: tempDirectory,
    });

    expect(generateSummary).toHaveBeenCalledOnce();
    expect(generateSession).toHaveBeenCalledOnce();
    expect(core.warning).toHaveBeenCalledWith("Unable to generate usage activity summary: summary failed");
    expect(core.warning).toHaveBeenCalledWith("Unable to generate unified session: session failed");
    expect(core.info.mock.calls.map(([file]) => file)).toEqual([path.join(tempDirectory, "nested", "session.jsonl"), path.join(tempDirectory, "summary.json")]);
  });

  it("reports a contextual error when the usage directory cannot be read", async () => {
    tempDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "usage-artifacts-"));
    const missingDirectory = path.join(tempDirectory, "missing");

    await expect(
      main({
        core: { info: vi.fn(), warning: vi.fn() },
        generateSummary: vi.fn(),
        generateSession: vi.fn(),
        usageDirectory: missingDirectory,
      })
    ).rejects.toThrow(`Failed to read usage artifact directory ${missingDirectory}`);
  });
});
