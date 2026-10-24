import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { parseDocument } from "./work_queue_yaml.cjs";

const source = readFileSync(resolve(import.meta.dirname, "../../../.github/workflows/haiku-printer.yml"), "utf8");
const document = parseDocument(source, { version: "1.2", uniqueKeys: true });
if (document.errors.length) throw new AggregateError(document.errors, "Invalid haiku workflow");
const step = document.toJS().jobs["print-message"].steps[0];

describe("haiku printer input handling", () => {
  it("passes the dispatch message as data rather than shell source", () => {
    expect(step.env.MESSAGE).toBe("${{ inputs.message }}");
    expect(step.run).not.toContain("${{");
  });

  it.each(["A quiet test runs\nMessages stay literal\nShell sleeps undisturbed", '$(printf unexpected) `printf unexpected` "; printf unexpected; #', "-n backslashes\\nremain literal\n'quotes' \"quotes\" $HOME %s", ""])(
    "prints %j literally to stdout and the step summary",
    message => {
      const directory = mkdtempSync(join(tmpdir(), "haiku-printer-"));
      const summary = join(directory, "summary.md");
      try {
        const result = spawnSync("bash", ["--noprofile", "--norc", "-e", "-c", step.run], {
          env: { PATH: process.env.PATH, MESSAGE: message, GITHUB_STEP_SUMMARY: summary },
          encoding: "utf8",
          timeout: 5000,
        });
        expect(result.error).toBeUndefined();
        expect(result.status).toBe(0);
        expect(result.stderr).toBe("");
        expect(result.stdout).toBe(`📨 Received message:\n${message}\n\n`);
        expect(readFileSync(summary, "utf8")).toBe(`### 📨 Message\n\n${message}\n`);
      } finally {
        rmSync(directory, { recursive: true, force: true });
      }
    }
  );
});
