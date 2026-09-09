import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "module";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const req = createRequire(import.meta.url);
const { parseFirewallLogs, parseSessionLogs, parseGatewayActivity, parseSafeOutputsManifest, parseExperimentsData, calculateWorkingSetFromJSONL, parseWorkingSetMetrics, MANIFEST_FILE_PATH } = req("./generate_usage_activity_summary.cjs");

describe("generate_usage_activity_summary.cjs", () => {
  /** Unique directory for each test to avoid cross-test interference */
  let squidLogDir;
  let experimentStateDir;
  const origExperimentStateDir = process.env.GH_AW_EXPERIMENT_STATE_DIR;

  beforeEach(() => {
    squidLogDir = path.join("/tmp/gh-aw", `squid-logs-unit-test-${Date.now()}`);
    experimentStateDir = path.join("/tmp/gh-aw", `experiment-unit-test-${Date.now()}`);
    fs.mkdirSync(squidLogDir, { recursive: true });
    fs.mkdirSync(experimentStateDir, { recursive: true });
    process.env.GH_AW_EXPERIMENT_STATE_DIR = experimentStateDir;
  });

  afterEach(() => {
    if (fs.existsSync(squidLogDir)) {
      fs.rmSync(squidLogDir, { recursive: true, force: true });
    }
    if (fs.existsSync(experimentStateDir)) {
      fs.rmSync(experimentStateDir, { recursive: true, force: true });
    }
    if (origExperimentStateDir === undefined) {
      delete process.env.GH_AW_EXPERIMENT_STATE_DIR;
    } else {
      process.env.GH_AW_EXPERIMENT_STATE_DIR = origExperimentStateDir;
    }
  });

  describe("parseFirewallLogs", () => {
    it("skips Squid diagnostic lines (WARNING:, DNS, Accepting) and does not treat them as domain names", () => {
      const logContent = [
        // Squid startup/diagnostic messages that should be skipped
        'WARNING: 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"',
        'DNS 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"',
        'Accepting 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"',
        // A valid access log entry that should be counted
        '1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"',
      ].join("\n");

      fs.writeFileSync(path.join(squidLogDir, "access.log"), logContent);

      const result = parseFirewallLogs();

      expect(result).not.toBeNull();
      expect(result.total_requests).toBe(1);
      expect(result.allowed_domains).toContain("api.github.com:443");
      // Diagnostic keywords must not appear as domain names
      expect(result.allowed_domains).not.toContain("WARNING:");
      expect(result.allowed_domains).not.toContain("DNS");
      expect(result.allowed_domains).not.toContain("Accepting");
    });

    it("returns null when only non-Squid diagnostic lines are present", () => {
      const logContent = [
        'WARNING: 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"',
        "DNS resolver ready - some extra fields here to pass length check x y z",
        "Accepting connections on port 3128 x y z",
      ].join("\n");

      fs.writeFileSync(path.join(squidLogDir, "access.log"), logContent);

      const result = parseFirewallLogs();

      expect(result).toBeNull();
    });

    it("counts valid Squid access log entries correctly", () => {
      const logContent = [
        '1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"',
        '1761332531.000 172.30.0.20:35289 blocked.example.com:443 1.2.3.4:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"',
      ].join("\n");

      fs.writeFileSync(path.join(squidLogDir, "access.log"), logContent);

      const result = parseFirewallLogs();

      expect(result).not.toBeNull();
      expect(result.total_requests).toBe(2);
      expect(result.allowed_requests).toBe(1);
      expect(result.blocked_requests).toBe(1);
      expect(result.allowed_domains).toContain("api.github.com:443");
      expect(result.blocked_domains).toContain("blocked.example.com:443");
    });
  });

  describe("parseSessionLogs", () => {
    it("matches events.jsonl one directory below the session-state directory", () => {
      const sessionRoot = fs.mkdtempSync(path.join(os.tmpdir(), "session-logs-test-"));
      try {
        fs.mkdirSync(path.join(sessionRoot, "session-1"), { recursive: true });
        fs.writeFileSync(path.join(sessionRoot, "session-1", "events.jsonl"), `${JSON.stringify({ type: "session.start" })}\n`);
        fs.mkdirSync(path.join(sessionRoot, "nested", "too-deep"), { recursive: true });
        fs.writeFileSync(path.join(sessionRoot, "nested", "too-deep", "events.jsonl"), `${JSON.stringify({ type: "assistant.message" })}\n`);

        expect(parseSessionLogs([sessionRoot])).toMatchObject({
          total_events: 1,
          session_starts: 1,
          assistant_messages: 0,
        });
      } finally {
        fs.rmSync(sessionRoot, { recursive: true, force: true });
      }
    });
  });

  describe("parseGatewayActivity", () => {
    it("aggregates RPC v2 tool calls, payload sizes, durations, failures, and integrity filtering", () => {
      const root = fs.mkdtempSync(path.join(os.tmpdir(), "gateway-activity-test-"));
      const logsDir = path.join(root, "mcp-logs");
      fs.mkdirSync(logsDir, { recursive: true });
      const firstArguments = { query: "is:open" };
      const secondArguments = { number: 1 };
      const firstResult = { items: [1, 2] };
      const secondResult = { isError: true, content: [{ type: "text", text: "denied" }] };
      const records = [
        {
          timestamp: "2026-08-15T23:48:42.000Z",
          event: "rpc_request",
          _schema: "rpc-message/v2",
          direction: "OUT",
          server_id: "github",
          payload: { jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "list_issues", arguments: firstArguments } },
        },
        { timestamp: "2026-08-15T23:48:42.025Z", event: "rpc_response", _schema: "rpc-message/v2", direction: "IN", server_id: "github", payload: { jsonrpc: "2.0", id: 1, result: firstResult } },
        {
          timestamp: "2026-08-15T23:48:42.100Z",
          event: "rpc_request",
          _schema: "rpc-message/v2",
          direction: "OUT",
          server_id: "github",
          payload: { jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "issue_read", arguments: secondArguments } },
        },
        { timestamp: "2026-08-15T23:48:42.140Z", event: "rpc_response", _schema: "rpc-message/v2", direction: "IN", server_id: "github", payload: { jsonrpc: "2.0", id: 2, result: secondResult } },
        { timestamp: "2026-08-15T23:48:42.150Z", event: "difc_filtered", _schema: "rpc-message/v2", server_id: "github", tool_name: "issue_read", reason: "integrity" },
      ];
      fs.writeFileSync(path.join(logsDir, "rpc-messages.jsonl"), records.map(JSON.stringify).join("\n"));

      try {
        const { gateway, integrity } = parseGatewayActivity([root]);
        expect(gateway).toMatchObject({
          total_calls: 2,
          failed_calls: 1,
          total_input_size: Buffer.byteLength(JSON.stringify(firstArguments)) + Buffer.byteLength(JSON.stringify(secondArguments)),
          max_input_size: Buffer.byteLength(JSON.stringify(firstArguments)),
          total_output_size: Buffer.byteLength(JSON.stringify(firstResult)) + Buffer.byteLength(JSON.stringify(secondResult)),
          max_output_size: Buffer.byteLength(JSON.stringify(secondResult)),
          total_duration_ms: 65,
          max_duration_ms: 40,
        });
        expect(gateway.servers).toEqual([
          expect.objectContaining({
            server_name: "github",
            request_count: 2,
            tool_call_count: 2,
            failed_calls: 1,
            avg_duration_ms: 32.5,
          }),
        ]);
        expect(gateway.tools).toEqual([
          expect.objectContaining({ server_name: "github", tool_name: "issue_read", call_count: 1, failed_calls: 1, avg_duration_ms: 40 }),
          expect.objectContaining({ server_name: "github", tool_name: "list_issues", call_count: 1, failed_calls: 0, avg_duration_ms: 25 }),
        ]);
        expect(gateway.tool_calls).toEqual([
          {
            tool_call_id: "call-1",
            timestamp: "2026-08-15T23:48:42.000Z",
            server_name: "github",
            tool_name: "list_issues",
            request_size: Buffer.byteLength(JSON.stringify(firstArguments)),
            response_size: Buffer.byteLength(JSON.stringify(firstResult)),
            duration_ms: 25,
            outcome: "success",
          },
          {
            tool_call_id: "call-2",
            timestamp: "2026-08-15T23:48:42.100Z",
            server_name: "github",
            tool_name: "issue_read",
            request_size: Buffer.byteLength(JSON.stringify(secondArguments)),
            response_size: Buffer.byteLength(JSON.stringify(secondResult)),
            duration_ms: 40,
            outcome: "failure",
          },
        ]);
        expect(JSON.stringify(gateway.tool_calls)).not.toContain('"id":1');
        expect(JSON.stringify(gateway.tool_calls)).not.toContain("is:open");
        expect(integrity).toEqual({
          total_filtered: 1,
          filtered_server_counts: { github: 1 },
          filtered_tool_counts: { issue_read: 1 },
          filtered_reason_counts: { integrity: 1 },
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("prefers gateway.jsonl over rpc-messages.jsonl in the same log root", () => {
      const root = fs.mkdtempSync(path.join(os.tmpdir(), "gateway-activity-test-"));
      const logsDir = path.join(root, "mcp-logs");
      fs.mkdirSync(logsDir, { recursive: true });
      fs.writeFileSync(path.join(logsDir, "gateway.jsonl"), JSON.stringify({ event: "tool_call", server_name: "github", tool_name: "issue_read", tool_call_id: "secret-tool-id", input_size: 10, output_size: 20, duration: 5 }));
      fs.writeFileSync(path.join(logsDir, "rpc-messages.jsonl"), JSON.stringify({ event: "difc_filtered", server_id: "github", tool_name: "issue_read", reason: "integrity" }));

      try {
        const { gateway, integrity } = parseGatewayActivity([root]);
        expect(gateway.total_calls).toBe(1);
        expect(gateway.total_input_size).toBe(10);
        expect(gateway.total_output_size).toBe(20);
        expect(gateway.tool_calls).toEqual([{ tool_call_id: "call-1", timestamp: "", server_name: "github", tool_name: "issue_read", request_size: 10, response_size: 20, duration_ms: 5, outcome: "success" }]);
        expect(JSON.stringify(gateway)).not.toContain("secret-tool-id");
        expect(integrity).toBeNull();
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("reports incomplete RPC calls without exposing identifiers or payloads", () => {
      const root = fs.mkdtempSync(path.join(os.tmpdir(), "gateway-activity-test-"));
      const logsDir = path.join(root, "mcp-logs");
      fs.mkdirSync(logsDir, { recursive: true });
      const secretID = "******";
      const secretArgument = "private-value";
      fs.writeFileSync(
        path.join(logsDir, "rpc-messages.jsonl"),
        JSON.stringify({
          timestamp: "2026-08-15T23:48:42.000Z",
          event: "rpc_request",
          direction: "OUT",
          server_id: "github",
          payload: { jsonrpc: "2.0", id: secretID, method: "tools/call", params: { name: "issue_read", arguments: { token: secretArgument } } },
        })
      );

      try {
        const { gateway } = parseGatewayActivity([root]);
        expect(gateway.tool_calls).toEqual([
          {
            tool_call_id: "call-1",
            timestamp: "2026-08-15T23:48:42.000Z",
            server_name: "github",
            tool_name: "issue_read",
            request_size: Buffer.byteLength(JSON.stringify({ token: secretArgument })),
            response_size: 0,
            duration_ms: 0,
            outcome: "incomplete",
          },
        ]);
        expect(JSON.stringify(gateway)).not.toContain(secretID);
        expect(JSON.stringify(gateway)).not.toContain(secretArgument);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
  });

  describe("parseSafeOutputsManifest", () => {
    /** Unique manifest file path per test to avoid cross-test interference */
    let manifestPath;

    beforeEach(() => {
      const testTmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "safe-outputs-test-"));
      manifestPath = path.join(testTmpDir, "safe-output-items.jsonl");
    });

    afterEach(() => {
      const dir = path.dirname(manifestPath);
      if (fs.existsSync(dir)) {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("returns null when the manifest file does not exist", () => {
      const result = parseSafeOutputsManifest(manifestPath);
      expect(result).toBeNull();
    });

    it("returns zero-item result when the manifest file is empty", () => {
      fs.writeFileSync(manifestPath, "");
      const result = parseSafeOutputsManifest(manifestPath);
      expect(result).toEqual({ total_items: 0, items_by_type: {} });
    });

    it("returns zero-item result when the manifest contains only blank lines", () => {
      fs.writeFileSync(manifestPath, "\n\n\n");
      const result = parseSafeOutputsManifest(manifestPath);
      expect(result).toEqual({ total_items: 0, items_by_type: {} });
    });

    it("throws when the manifest file exists but cannot be read", () => {
      fs.writeFileSync(manifestPath, JSON.stringify({ type: "create_issue" }));
      fs.chmodSync(manifestPath, 0o000);
      try {
        expect(() => parseSafeOutputsManifest(manifestPath)).toThrow();
      } finally {
        // Restore permissions so afterEach cleanup can remove the file.
        fs.chmodSync(manifestPath, 0o644);
      }
    });

    it("counts items by type from a valid manifest", () => {
      const lines = [
        JSON.stringify({ type: "create_issue", url: "https://github.com/owner/repo/issues/1" }),
        JSON.stringify({ type: "create_issue", url: "https://github.com/owner/repo/issues/2" }),
        JSON.stringify({ type: "add_comment", url: "https://github.com/owner/repo/issues/1#issuecomment-1" }),
      ].join("\n");
      fs.writeFileSync(manifestPath, lines);

      const result = parseSafeOutputsManifest(manifestPath);

      expect(result).not.toBeNull();
      expect(result.total_items).toBe(3);
      expect(result.items_by_type).toEqual({ create_issue: 2, add_comment: 1 });
    });

    it("skips lines with missing or empty type field", () => {
      const lines = [
        JSON.stringify({ type: "create_issue", url: "https://github.com/owner/repo/issues/1" }),
        JSON.stringify({ url: "https://example.com" }), // no type field
        JSON.stringify({ type: "", url: "https://example.com" }), // empty type
        "not json at all",
      ].join("\n");
      fs.writeFileSync(manifestPath, lines);

      const result = parseSafeOutputsManifest(manifestPath);

      expect(result).not.toBeNull();
      expect(result.total_items).toBe(1);
      expect(result.items_by_type).toEqual({ create_issue: 1 });
    });

    it("returns zero-item result when all lines are invalid JSON or have no type", () => {
      const lines = ["not json at all", JSON.stringify({ url: "https://example.com" })].join("\n");
      fs.writeFileSync(manifestPath, lines);
      const result = parseSafeOutputsManifest(manifestPath);
      expect(result).toEqual({ total_items: 0, items_by_type: {} });
    });
  });

  describe("parseExperimentsData", () => {
    it("returns null when no assignments file exists", () => {
      // No assignments.json in experimentStateDir
      const result = parseExperimentsData();
      expect(result).toBeNull();
    });

    it("returns null when assignments file is empty object", () => {
      fs.writeFileSync(path.join(experimentStateDir, "assignments.json"), JSON.stringify({}));
      const result = parseExperimentsData();
      expect(result).toBeNull();
    });

    it("returns assignments when file contains experiment data", () => {
      const assignments = { style: "concise", caveman: "yes" };
      fs.writeFileSync(path.join(experimentStateDir, "assignments.json"), JSON.stringify(assignments));
      const result = parseExperimentsData();
      expect(result).not.toBeNull();
      expect(result.assignments).toEqual(assignments);
    });

    it("returns null when assignments file is invalid JSON", () => {
      fs.writeFileSync(path.join(experimentStateDir, "assignments.json"), "not json");
      const result = parseExperimentsData();
      expect(result).toBeNull();
    });
  });

  describe("working-set rebuild metrics", () => {
    const record = inputTokens => JSON.stringify({ input_tokens: inputTokens, cache_read_tokens: 999999, cache_write_tokens: 888888 });

    it.each([
      { name: "one invocation", inputs: [100_000], cumulative: 100_000, peak: 100_000, excess: 0, factor: 1 },
      { name: "two identical invocations", inputs: [100_000, 100_000], cumulative: 200_000, peak: 100_000, excess: 100_000, factor: 2 },
      { name: "increasing invocations", inputs: [10_000, 20_000, 40_000], cumulative: 70_000, peak: 40_000, excess: 30_000, factor: 1.75 },
      { name: "many small calls plus one large call", inputs: [1_000, 1_000, 1_000, 100_000], cumulative: 103_000, peak: 100_000, excess: 3_000, factor: 1.03 },
      { name: "paper-inspired fixture", inputs: [100_000, 150_000, 200_000, 200_000, 224_000], cumulative: 874_000, peak: 224_000, excess: 650_000, factor: 3.9017857142857144 },
    ])("$name", ({ inputs, cumulative, peak, excess, factor }) => {
      const { workingSet } = calculateWorkingSetFromJSONL(inputs.map(record).join("\n"));
      expect(workingSet).toEqual({
        measurement_state: "measured",
        rebuild_factor: factor,
        cumulative_input_tokens: cumulative,
        peak_input_tokens: peak,
        rebuild_excess_tokens: excess,
        invocations: inputs.length,
      });
    });

    it("uses canonical input_tokens without adding cache token fields", () => {
      const { workingSet } = calculateWorkingSetFromJSONL([record(10), record(20)].join("\n"));
      expect(workingSet.cumulative_input_tokens).toBe(30);
      expect(workingSet.rebuild_factor).toBe(1.5);
    });

    it("counts valid zero-token records without fabricating a factor", () => {
      const { workingSet } = calculateWorkingSetFromJSONL([record(0), record(0)].join("\n"));
      expect(workingSet).toEqual({
        measurement_state: "unavailable",
        cumulative_input_tokens: 0,
        peak_input_tokens: 0,
        rebuild_excess_tokens: 0,
        invocations: 2,
      });
      expect(workingSet).not.toHaveProperty("rebuild_factor");
    });

    it("marks mixed malformed and valid records partial", () => {
      const { workingSet, ignoredRecords } = calculateWorkingSetFromJSONL(`${record(50)}\nnot-json\n${JSON.stringify({ output_tokens: 3 })}\n${record(100)}`);
      expect(ignoredRecords).toBe(2);
      expect(workingSet.measurement_state).toBe("partial");
      expect(workingSet.rebuild_factor).toBe(1.5);
      expect(workingSet.invocations).toBe(2);
    });

    it("returns unavailable for missing and empty files", () => {
      const missingPath = path.join(os.tmpdir(), `missing-token-usage-${Date.now()}.jsonl`);
      expect(parseWorkingSetMetrics(missingPath).workingSet.measurement_state).toBe("unavailable");

      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "empty-token-usage-"));
      const emptyPath = path.join(tmpDir, "token_usage.jsonl");
      fs.writeFileSync(emptyPath, "");
      try {
        expect(parseWorkingSetMetrics(emptyPath).workingSet.measurement_state).toBe("unavailable");
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("handles cumulative counts above the safe integer boundary without overflow", () => {
      const large = Number.MAX_SAFE_INTEGER;
      const { workingSet } = calculateWorkingSetFromJSONL([record(large), record(large)].join("\n"));
      expect(workingSet.rebuild_factor).toBe(2);
      expect(Number.isFinite(workingSet.rebuild_factor)).toBe(true);
      expect(workingSet.rebuild_factor).toBeGreaterThanOrEqual(1);
      expect(workingSet.cumulative_input_tokens).toBe(Number(BigInt(large) * 2n));
    });
  });
});
