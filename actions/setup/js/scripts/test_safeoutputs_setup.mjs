import { afterAll } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const tempDir = fs.mkdtempSync(path.join(process.env.TMPDIR || os.tmpdir(), "gh-aw-test-safeoutputs-"));
process.env.TMPDIR = tempDir;
process.env.TEMP = tempDir;
process.env.TMP = tempDir;
process.env.GH_AW_RUNTIME_DIR = path.join(tempDir, "runtime");
process.env.GH_AW_RUNTIME_ID = path.basename(tempDir).replace(/[^a-zA-Z0-9_-]/g, "");
fs.mkdirSync(process.env.GH_AW_RUNTIME_DIR, { mode: 0o700 });
if (!process.env.RUNNER_TEMP) process.env.RUNNER_TEMP = tempDir;
if (process.env.GH_AW_VERIFY_RENDERING === "true" && !process.env.GH_AW_RENDERING_INPUT) {
  process.env.GH_AW_RENDERING_INPUT = path.join(process.env.GH_AW_RUNTIME_DIR, "agent/rendering-verifier-input.json");
}
const cli = path.join(tempDir, "safeoutputs");
fs.copyFileSync(fileURLToPath(new URL("./fixtures/safeoutputs.cjs", import.meta.url)), cli);
fs.chmodSync(cli, 0o700);

// A local output path does not rebind the real CLI's production MCP gateway.
// Isolate both the CLI override and PATH for tests and their subprocesses.
process.env.GH_AW_SAFEOUTPUTS_CLI = cli;
process.env.PATH = `${tempDir}${path.delimiter}${process.env.PATH || ""}`;
process.env.GH_AW_SAFE_OUTPUTS = path.join(tempDir, "safeoutputs.jsonl");
delete process.env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE;
delete process.env.GH_AW_AWF_EXECUTION_COMPONENT;

afterAll(() => fs.rmSync(tempDir, { recursive: true, force: true }));
