import { describe, it, expect } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const {
  countPermissionDeniedIssues,
  hasNumerousPermissionDeniedIssues,
  extractDeniedCommands,
  hasWorkflowToolPermissionDenial,
  WORKFLOW_TOOL_PERMISSION_DENIED_EXIT_CODE,
  buildMissingToolPermissionIssuePayload,
} = require("./permission_denied_helpers.cjs");

describe("permission_denied_helpers.cjs", () => {
  it("counts repeated permission-denied signals", () => {
    const output = "permission denied\nEACCES: permission denied\nEPERM operation not permitted\npermissions denied";
    expect(countPermissionDeniedIssues(output)).toBe(5);
  });

  it("detects numerous permission-denied issues at threshold", () => {
    const output = "permission denied\npermission denied\npermission denied";
    expect(hasNumerousPermissionDeniedIssues(output)).toBe(true);
  });

  it("extracts denied commands from pipe-marked output", () => {
    const output = ["  \u2502 go version 2>&1", "  Permission denied", "  \u2502 ls /usr/local/go", "  Permission denied"].join("\n");
    expect(extractDeniedCommands(output)).toEqual(["go version 2>&1", "ls /usr/local/go"]);
  });

  it("extracts denied commands from Copilot SDK driver tool denial logs", () => {
    const output = [
      "[copilot-sdk-driver] [sdk-driver] permission denied by workflow tool permissions: shell(agenticworkflows --help 2>&1 | head -60)",
      "[copilot-sdk-driver] [sdk-driver] tool denial 1/5: permission denied: shell(agenticworkflows --help 2>&1 | head -60)",
      "[copilot-sdk-driver] [sdk-driver] permission denied by workflow tool permissions: read",
      "[copilot-sdk-driver] [sdk-driver] tool denial 3/5: permission denied: read",
    ].join("\n");
    expect(extractDeniedCommands(output)).toEqual(["shell(agenticworkflows --help 2>&1 | head -60)", "read"]);
  });

  it.each(["url(https://example.com)", "workflow"])("recognizes explicit workflow permission summaries for %s", summary => {
    const output = `[sdk-driver] permission denied by workflow tool permissions: ${summary}`;
    expect(hasWorkflowToolPermissionDenial(output)).toBe(false);
    expect(hasWorkflowToolPermissionDenial(WORKFLOW_TOOL_PERMISSION_DENIED_EXIT_CODE)).toBe(true);
    const payload = JSON.parse(buildMissingToolPermissionIssuePayload(extractDeniedCommands(output), true));
    expect(payload.alternatives).toContain("workflow tools configuration");
    expect(payload.denied_commands).toEqual([summary]);
  });

  it.each([
    "[sdk-driver] permission denied by workflow tool permissions: shell(ls)",
    '{"source":"sdk-driver","event":"permission-denied-by-workflow-tool-permissions"}',
    '{"component":"sdk-driver","type":"permission-denied-by-workflow-tool-permissions"}',
    '{"type":"guard.tool_denials_exceeded","data":{"denialCount":5,"threshold":5}}',
    String(WORKFLOW_TOOL_PERMISSION_DENIED_EXIT_CODE),
  ])("does not classify assistant-echoed denial text: %s", output => {
    expect(hasWorkflowToolPermissionDenial(output)).toBe(false);
  });

  it.each([undefined, null, 0, 1, 79, { exitCode: WORKFLOW_TOOL_PERMISSION_DENIED_EXIT_CODE }])("rejects non-denial exit values: %j", exitCode => {
    expect(hasWorkflowToolPermissionDenial(exitCode)).toBe(false);
  });

  it("builds report_missing_tool payload with default denied commands", () => {
    const payload = JSON.parse(buildMissingToolPermissionIssuePayload());
    expect(payload).toEqual({
      type: "report_missing_tool",
      tool: "tool/permission",
      reason: "missing tool/permission issue: numerous permission denied errors detected",
      alternatives: "Verify token scopes, repository permissions, and MCP/tool access configuration.",
      denied_commands: [],
    });
  });

  it("reports workflow tool denials without advising credential escalation", () => {
    const output = [
      "[copilot-sdk-driver] [sdk-driver] permission denied by workflow tool permissions: shell(node --test scripts/pr-sous-chef.test.mjs)",
      "[copilot-sdk-driver] [sdk-driver] tool denial 3/3: permission denied: shell(node --test scripts/pr-sous-chef.test.mjs)",
    ].join("\n");
    const payload = JSON.parse(buildMissingToolPermissionIssuePayload(extractDeniedCommands(output), true));
    expect(payload.denied_commands).toEqual(["shell(node --test scripts/pr-sous-chef.test.mjs)"]);
    expect(payload.alternatives).toContain("commands allowed by the workflow tools configuration");
    expect(payload.alternatives).toContain("does not establish missing token scopes");
    expect(payload.alternatives).not.toContain("Verify token scopes");
    expect(payload.reason).toBe("missing tool/permission issue: workflow tool permission denial detected");
  });

  it.each(["shell(node test.js)", "read(/workspace/file)", "write(/workspace/file)", "mcp(github.get_file_contents)", "custom-tool(example)"])("preserves generic permission guidance for tool-like denial prefixes: %s", command => {
    const output = `tool denial 1/3: permission denied: ${command}`;
    const payload = JSON.parse(buildMissingToolPermissionIssuePayload([command], output));
    expect(payload.alternatives).toContain("Verify token scopes");
    expect(payload.alternatives).not.toContain("workflow tools configuration");
    expect(payload.denied_commands).toEqual([command]);
  });

  it("preserves generic permission guidance when no workflow boundary is identified", () => {
    const payload = JSON.parse(buildMissingToolPermissionIssuePayload(["go version"], "permission denied: go version"));
    expect(payload.alternatives).toContain("Verify token scopes");
    expect(payload.denied_commands).toEqual(["go version"]);
  });
});
