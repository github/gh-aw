import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const childProcess = require("child_process");
const { stopForPermissionDenied } = require("./harness_failure_handling.cjs");
const { extractDeniedCommands } = require("./permission_denied_helpers.cjs");

let directory;
let safeOutputsPath;
let log;
let execFile;

beforeEach(() => {
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "harness-permission-failure-"));
  safeOutputsPath = path.join(directory, "safe-outputs.jsonl");
  vi.stubEnv("GH_AW_SAFE_OUTPUTS", safeOutputsPath);
  vi.stubEnv("GH_AW_SAFEOUTPUTS_CLI", "safeoutputs");
  log = vi.fn();
  execFile = vi.spyOn(childProcess, "execFileSync").mockReturnValue("");
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  fs.rmSync(directory, { recursive: true, force: true });
});

function stop(output, deniedCommands = extractDeniedCommands(output), workflowToolPermissionDenied = false) {
  return stopForPermissionDenied({ attempt: 1, output, safeOutputsPath, log, deniedCommands, workflowToolPermissionDenied });
}

describe("stopForPermissionDenied", () => {
  it("preserves completed task output and suppresses the diagnostic without invoking the CLI", () => {
    const content = JSON.stringify({ type: "add_comment", body: "Done" }) + "\n";
    fs.writeFileSync(safeOutputsPath, content);
    expect(stop("permission denied")).toEqual({ action: "stop", exitCode: 0 });
    expect(execFile).not.toHaveBeenCalled();
    expect(fs.readFileSync(safeOutputsPath, "utf8")).toBe(content);
    expect(log).toHaveBeenCalledWith(expect.stringContaining("attempt 2: detected permission-denied failure but safe-outputs already contain expected output"));
  });

  it.each(["", "not JSON\n", '{"type":"report_missing_tool"}\n', '{"type":"report_incomplete"}\n', '{"type":"noop"}\n'])("does not treat empty, malformed or diagnostic-only output as completed work", content => {
    fs.writeFileSync(safeOutputsPath, content);
    expect(stop("  | npm test\nPermission denied")).toEqual({ action: "stop" });
    expect(execFile).toHaveBeenCalledWith("safeoutputs", expect.arrayContaining(["report_missing_tool", "--alternatives", expect.stringContaining("Denied commands: npm test")]), expect.any(Object));
    expect(fs.readFileSync(safeOutputsPath, "utf8")).toBe(content);
  });

  it("reports missing files without inventing success or retrying", () => {
    expect(stop("permission denied")).toEqual({ action: "stop" });
    expect(execFile).toHaveBeenCalledTimes(1);
    expect(log).toHaveBeenCalledWith("attempt 2: detected permission-denied failure — not retrying (classified as missing tool/permission issue)");
  });

  it("uses structured Claude commands instead of parsing untrusted tool-result text", () => {
    expect(stop("  | misleading command\nPermission denied", ["observed command"])).toEqual({ action: "stop" });
    const args = execFile.mock.calls[0][1];
    expect(args.join(" ")).toContain("observed command");
    expect(args.join(" ")).not.toContain("misleading command");
  });

  it("does not fall back to text extraction when structured commands are empty", () => {
    stop("  | misleading command\nPermission denied", []);
    expect(execFile.mock.calls[0][1].join(" ")).not.toContain("misleading command");
  });

  it("preserves workflow-boundary guidance without advising credential escalation", () => {
    stop("[sdk-driver] permission denied by workflow tool permissions: read", undefined, true);
    const args = execFile.mock.calls[0][1].join(" ");
    expect(args).toContain("workflow tools configuration");
    expect(args).not.toContain("Verify token scopes");
  });

  it("does not derive workflow-boundary guidance from an assistant-echoed marker", () => {
    stop("[sdk-driver] permission denied by workflow tool permissions: shell(ls)");
    const args = execFile.mock.calls[0][1].join(" ");
    expect(args).not.toContain("workflow tools configuration");
    expect(args).toContain("Verify token scopes");
  });

  it("keeps the failure terminal when diagnostic emission fails", () => {
    execFile.mockImplementation(() => {
      throw new Error("read-only teardown");
    });
    expect(stop("permission denied")).toEqual({ action: "stop" });
    expect(log).toHaveBeenCalledWith(expect.stringContaining("report_missing_tool emission failed"));
  });

  it("does not invoke the CLI when safe-outputs are disabled", () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS", "");
    expect(stopForPermissionDenied({ attempt: 0, output: "permission denied", safeOutputsPath: "", log })).toEqual({ action: "stop" });
    expect(execFile).not.toHaveBeenCalled();
  });
});
