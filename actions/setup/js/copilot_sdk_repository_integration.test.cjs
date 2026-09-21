import { describe, expect, it } from "vitest";
import { execFile } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";

const gatewayBinary = process.env.GH_AW_TEST_MCP_GATEWAY_BINARY;
const routes = ["/mcp/github", "/mcp/reference", "/mcp/safeoutputs"];

async function runFixture(mode) {
  const filename = fileURLToPath(new URL("./fixtures/copilot_sdk_repository.fixture.cjs", import.meta.url));
  const env = { ...process.env };
  delete env.GH_AW_TEST_MCP_GATEWAY_BINARY;
  if (mode === "gateway") env.GH_AW_TEST_MCP_GATEWAY_BINARY = gatewayBinary;
  const ambient = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-sdk-absent-go-caches-"));
  env.GOCACHE = path.join(ambient, "absent-build");
  env.GOMODCACHE = path.join(ambient, "absent-modules");
  let result;
  let executionError;
  try {
    result = await promisify(execFile)(process.execPath, [filename, `--${mode}`], { env, encoding: "utf8", timeout: 240_000, maxBuffer: 2 * 1024 * 1024, windowsHide: true });
    expect(fs.existsSync(env.GOCACHE), "fixture must not create or reuse the host build cache").toBe(false);
    expect(fs.existsSync(env.GOMODCACHE), "fixture must not create or reuse the host module cache").toBe(false);
  } catch (error) {
    executionError = error;
    throw error;
  } finally {
    try {
      fs.rmSync(ambient, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
    } catch (cleanupError) {
      throw new AggregateError(executionError ? [executionError, cleanupError] : [cleanupError], "SDK fixture host-cache cleanup failed", { cause: executionError });
    }
  }
  const lines = result.stdout.split(/\r?\n/).filter(value => value.startsWith("SDK_REPOSITORY_RESULT="));
  expect(lines, result.stderr).toHaveLength(1);
  const evidence = JSON.parse(lines[0].slice("SDK_REPOSITORY_RESULT=".length));
  console.info("SDK_REPOSITORY_TIMINGS=" + JSON.stringify({ mode, operations: evidence.operationTimings }));
  expect(evidence).toMatchObject({ mode, catalogVerified: true, authChecked: true, providerRequests: 0, unexpectedRequests: 0, backendAuthFailures: 0, backendErrors: [], reads: 1, forbiddenCalls: 0, permissionDenials: 0 });
  expect(evidence.outputs).toEqual(["create_pull_request", "noop"]);
  expect(evidence.actions).toEqual(["status", "prepare_branch", "validate", "commit", "validate", "commit", "format", "validate", "commit", "diff"]);
  expect(evidence.nativeFailures.map(failure => failure.action)).toEqual(["validate", "commit", "validate", "commit"]);
  for (const failure of evidence.nativeFailures) {
    expect(failure).toMatchObject({ resultType: "failure", completionRequestId: expect.any(String) });
    expect(failure.completionRequestId).not.toBe("");
    expect(Buffer.byteLength(JSON.stringify({ resultType: failure.resultType, textResultForLlm: failure.textResultForLlm, error: failure.error }), "utf8")).toBeLessThanOrEqual(8192);
  }
  expect(new Set(evidence.nativeFailures.map(failure => failure.completionRequestId)).size).toBe(4);
  for (const text of [evidence.nativeFailures[0].textResultForLlm, evidence.nativeFailures[0].error]) expect(text).toContain("untracked addition: validation-receipt.json");
  for (const text of [evidence.nativeFailures[2].textResultForLlm, evidence.nativeFailures[2].error]) {
    expect(text).toContain("go test failed with exit code 1");
    expect(text).toMatch(/stdout: [^\n]*NATIVE_STDOUT_DIAGNOSTIC/);
    expect(text).toMatch(/stderr: [^\n]*NATIVE_STDERR_DIAGNOSTIC/);
  }
  for (const index of [1, 3]) {
    expect(evidence.nativeFailures[index].textResultForLlm).toContain("Run validate successfully");
    expect(evidence.nativeFailures[index].error).toContain("Run validate successfully");
  }
  expect(evidence.operationTimings.map(timing => timing.action)).toEqual(evidence.actions);
  expect(evidence.nativeTools.length).toBeGreaterThan(30);
  for (const name of ["bash", "write_bash", "task", "noop", "github-delete_file"]) expect(evidence.nativeTools).not.toContain(name);
  expect(Object.keys(evidence.backendRequests).sort()).toEqual(routes);
  for (const route of routes) {
    expect(evidence.backendRequests[route].authenticatedRequests).toBeGreaterThan(0);
    expect(evidence.backendRequests[route].catalogRequests).toBeGreaterThan(0);
  }
  expect(evidence.backendRequests["/mcp/github"].calls).toEqual(["get_file_contents"]);
  expect(evidence.backendRequests["/mcp/safeoutputs"].calls).toEqual(["create_pull_request", "noop"]);
  expect(evidence.backendRequests["/mcp/reference"].calls).toEqual([]);
  for (const [serverName, toolName] of [
    ["github", "get_file_contents"],
    ["safeoutputs", "create_pull_request"],
    ["safeoutputs", "noop"],
  ]) {
    expect(evidence.permissionIdentities).toContainEqual({ kind: "mcp", serverName, toolName: `${serverName}-${toolName}` });
  }
  return evidence;
}

describe("promptless native SDK repository integration", () => {
  it("executes native inspection, fixed Go/Git and MCP declarations without inference (direct backend)", async () => {
    const evidence = await runFixture("direct");
    expect(evidence.gateway).toBeNull();
  }, 250_000);

  it("executes the same native catalog, permissions and repository sequence through the real MCP gateway without inference", async context => {
    if (gatewayBinary === undefined) {
      return context.skip("Set GH_AW_TEST_MCP_GATEWAY_BINARY to an absolute gateway executable path; the direct-backend fixture does not cover gateway negotiation.");
    }
    const evidence = await runFixture("gateway");
    expect(evidence.gateway).toMatchObject({ binary: gatewayBinary, version: expect.any(String), routesUsed: routes, stopped: true });
    expect(evidence.gateway.methods).toEqual(expect.arrayContaining(["server/discover", "initialize", "notifications/initialized", "tools/list", "tools/call"]));
    expect(evidence.gateway.authRejections).toEqual(Object.fromEntries(routes.map(route => [route, { missing: 401, invalid: 401, bearer: 401 }])));
  }, 250_000);
});
