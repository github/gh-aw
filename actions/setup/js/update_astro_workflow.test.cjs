import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { readDocumentMetadata } from "../../../scripts/markdown_metadata_test_adapter.cjs";

const source = readFileSync(resolve(import.meta.dirname, "../../../.github/workflows/update-astro.md"), "utf8");
const [, frontmatter] = source.match(/^---\r?\n([\s\S]*?)\r?\n---/);
const workflow = readDocumentMetadata(frontmatter, { version: "1.2", uniqueKeys: true }).value;

describe("Update Astro workflow completion", () => {
  it("fails the preliminary check when npm-check-updates fails instead of skipping the agent", () => {
    const dir = mkdtempSync(resolve(tmpdir(), "update-astro-test-"));
    try {
      const npx = resolve(dir, "npx");
      const output = resolve(dir, "output");
      const script = workflow.jobs.check_updates.steps.find(step => step.id === "check").run;
      writeFileSync(npx, '#!/bin/sh\nprintf \'%s\\n\' "$NCU_OUTPUT"\nexit "$NCU_EXIT"\n');
      chmodSync(npx, 0o755);
      for (const [status, result, expected, expectedOutput] of [
        ["0", "{}", 0, "has_updates=false\n"],
        ["0", '{"astro":"7.4.13"}', 0, "has_updates=true\nupdates_summary=astro: 7.4.13\n"],
        ["1", "", 1, ""],
      ]) {
        writeFileSync(output, "");
        const run = spawnSync("bash", ["-e", "-o", "pipefail", "-c", script], {
          cwd: dir,
          env: { ...process.env, PATH: `${dir}:${process.env.PATH}`, GITHUB_OUTPUT: output, NCU_EXIT: status, NCU_OUTPUT: result },
        });
        expect(run.status).toBe(expected);
        expect(readFileSync(output, "utf8")).toBe(expectedOutput);
      }
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });

  it("distinguishes no updates from blocked or unverified work", () => {
    expect(source).toContain("`noop` only when no dependency changes are needed");
    expect(source).toContain("`report_incomplete` with the concrete blocker");
    expect(source).toContain("it is diagnostic only and does not replace `report_incomplete`");
    expect(source).toContain("Do not create a PR or use `noop` in this case");
    expect(source).toContain("Do not create a pull request if the build is failing");
  });

  it("preserves TLS verification when npm downloads fail", () => {
    expect(source).toContain("NODE_USE_SYSTEM_CA=1");
    expect(source).toContain("Keep TLS verification enabled");
    expect(source).toContain("never disable it or switch to an unapproved registry");
  });
});
