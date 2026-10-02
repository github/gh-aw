// @ts-check
import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { main } from "./write_dispatch_work_coordinator_snapshot.cjs";

const tempDirectories = [];

afterEach(() => {
  for (const directory of tempDirectories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("write dispatch coordinator activation snapshot", () => {
  it("writes the Git-backed log and head in an artifact-ready envelope", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "dispatch-coordinator-activation-"));
    tempDirectories.push(directory);
    const snapshotPath = path.join(directory, "snapshot.json");
    const githubClient = {
      rest: {
        git: {
          getRef: async () => {
            throw Object.assign(new Error("Not Found"), { status: 404 });
          },
        },
      },
    };
    const messages = [];

    await main({
      githubClient,
      context: { repo: { owner: "owner", repo: "repo" } },
      snapshotPath,
      core: { info: message => messages.push(message) },
    });

    expect(JSON.parse(fs.readFileSync(snapshotPath, "utf8"))).toEqual({
      version: 1,
      sha: null,
      transactionLog: "",
    });
    expect(messages).toEqual(["Captured dispatch coordinator snapshot (0 transactions)"]);
    expect(fs.statSync(snapshotPath).mode & 0o777).toBe(0o444);
  });
});
