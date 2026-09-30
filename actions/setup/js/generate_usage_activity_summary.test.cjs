import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "module";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const req = createRequire(import.meta.url);
const {
  parseFirewallLogs,
  parseSessionLogs,
  parseSteeringEvents,
  parseGatewayActivity,
  parseSafeOutputsManifest,
  parseLedgerCompaction,
  ledgerActivityFromSafeOutputs,
  parseExperimentsData,
  calculateWorkingSetFromJSONL,
  parseWorkingSetMetrics,
  buildFrictionSummary,
  buildFrictionStepSummary,
  writeFrictionStepSummary,
  readTokenUsageContent,
  MANIFEST_FILE_PATH,
} = req("./generate_usage_activity_summary.cjs");

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

    it("aggregates named skill tool invocations without retaining trace payloads", () => {
      const sessionRoot = fs.mkdtempSync(path.join(os.tmpdir(), "session-skills-test-"));
      try {
        const sessionDir = path.join(sessionRoot, "session-1");
        fs.mkdirSync(sessionDir, { recursive: true });
        const events = [
          { type: "tool.execution_start", timestamp: "2026-09-26T00:00:00Z", data: { toolCallId: "call-1", toolName: "skill", input: { skill: "documentation" } } },
          { type: "tool.execution_complete", timestamp: "2026-09-26T00:00:01Z", data: { toolCallId: "call-1", toolName: "skill", success: true } },
          { type: "tool.execution_start", timestamp: "2026-09-26T00:00:02Z", data: { toolCallId: "call-2", toolName: "skill", arguments: { skill: "documentation" } } },
          { type: "tool.execution_complete", timestamp: "2026-09-26T00:00:03Z", data: { toolCallId: "call-2", toolName: "skill", success: false } },
          { type: "tool.execution_start", timestamp: "2026-09-26T00:00:04Z", data: { toolCallId: "call-3", toolName: "view", input: { path: "/tmp/file" } } },
        ];
        fs.writeFileSync(path.join(sessionDir, "events.jsonl"), events.map(JSON.stringify).join("\n"));

        expect(parseSessionLogs([sessionRoot])).toMatchObject({
          tool_execution_starts: 3,
          tool_execution_completes: 2,
          skills: {
            total_invocations: 2,
            unique_skills: 1,
            items: [
              {
                name: "documentation",
                invocation_count: 2,
                failed_count: 1,
                first_timestamp: "2026-09-26T00:00:00Z",
                last_timestamp: "2026-09-26T00:00:02Z",
              },
            ],
          },
        });
      } finally {
        fs.rmSync(sessionRoot, { recursive: true, force: true });
      }
    });

    it("correlates ID-less failures and orders skill timestamps chronologically", () => {
      const sessionRoot = fs.mkdtempSync(path.join(os.tmpdir(), "session-skills-order-test-"));
      try {
        for (const [name, events] of [
          [
            "a-newer",
            [
              { type: "tool.execution_start", timestamp: "2026-09-26T00:00:03Z", data: { toolName: "skill", input: { skill: "documentation" } } },
              { type: "tool.execution_complete", timestamp: "2026-09-26T00:00:04Z", data: { toolName: "skill", success: true } },
            ],
          ],
          [
            "z-older",
            [
              { type: "tool.execution_start", timestamp: "2026-09-26T00:00:01Z", data: { toolName: "skill", input: { skill: "documentation" } } },
              { type: "tool.execution_complete", timestamp: "2026-09-26T00:00:02Z", data: { toolName: "skill", success: false } },
            ],
          ],
        ]) {
          const sessionDir = path.join(sessionRoot, name);
          fs.mkdirSync(sessionDir, { recursive: true });
          fs.writeFileSync(path.join(sessionDir, "events.jsonl"), events.map(JSON.stringify).join("\n"));
        }

        expect(parseSessionLogs([sessionRoot]).skills.items).toEqual([
          {
            name: "documentation",
            invocation_count: 2,
            failed_count: 1,
            first_timestamp: "2026-09-26T00:00:01Z",
            last_timestamp: "2026-09-26T00:00:03Z",
          },
        ]);
      } finally {
        fs.rmSync(sessionRoot, { recursive: true, force: true });
      }
    });
  });

  describe("parseSteeringEvents", () => {
    it("aggregates steering counters from the first available AWF event log", () => {
      const root = fs.mkdtempSync(path.join(os.tmpdir(), "steering-events-test-"));
      const missingPath = path.join(root, "missing.jsonl");
      const eventsPath = path.join(root, "events.jsonl");
      fs.writeFileSync(eventsPath, ['{"event":"token_steering"}', '{"type":"TOKEN_STEERING"}', '{"event_name":"timeout_steering"}', '{"eventName":"model_steering"}', '{"event":"request"}'].join("\n"));

      try {
        expect(parseSteeringEvents([missingPath, eventsPath])).toEqual({
          total_events: 4,
          event_counts: {
            model_steering: 1,
            timeout_steering: 1,
            token_steering: 2,
          },
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("falls back past empty and steering-free event logs", () => {
      const root = fs.mkdtempSync(path.join(os.tmpdir(), "steering-events-fallback-test-"));
      const emptyPath = path.join(root, "event-logs.jsonl");
      const unrelatedPath = path.join(root, "unrelated.jsonl");
      const eventsPath = path.join(root, "events.jsonl");
      fs.writeFileSync(emptyPath, "");
      fs.writeFileSync(unrelatedPath, '{"event":"request"}\n');
      fs.writeFileSync(eventsPath, '{"event":"token_steering"}\n');

      try {
        expect(parseSteeringEvents([emptyPath, unrelatedPath, eventsPath])).toEqual({
          total_events: 1,
          event_counts: { token_steering: 1 },
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
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
          avg_input_size: Math.round((Buffer.byteLength(JSON.stringify(firstArguments)) + Buffer.byteLength(JSON.stringify(secondArguments))) / 2),
          max_input_size: Buffer.byteLength(JSON.stringify(firstArguments)),
          total_output_size: Buffer.byteLength(JSON.stringify(firstResult)) + Buffer.byteLength(JSON.stringify(secondResult)),
          avg_output_size: Math.round((Buffer.byteLength(JSON.stringify(firstResult)) + Buffer.byteLength(JSON.stringify(secondResult))) / 2),
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
            avg_input_size: Math.round((Buffer.byteLength(JSON.stringify(firstArguments)) + Buffer.byteLength(JSON.stringify(secondArguments))) / 2),
            avg_output_size: Math.round((Buffer.byteLength(JSON.stringify(firstResult)) + Buffer.byteLength(JSON.stringify(secondResult))) / 2),
            max_input_size: Buffer.byteLength(JSON.stringify(firstArguments)),
            max_output_size: Buffer.byteLength(JSON.stringify(secondResult)),
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
      expect(ledgerActivityFromSafeOutputs(result)).toBeNull();
    });

    it("returns zero-item result when the manifest file is empty", () => {
      fs.writeFileSync(manifestPath, "");
      const result = parseSafeOutputsManifest(manifestPath);
      expect(result).toEqual({ total_items: 0, items_by_type: {}, items: [] });
      expect(ledgerActivityFromSafeOutputs(result)).toEqual({ transactions_added: 0 });
    });

    it("returns zero-item result when the manifest contains only blank lines", () => {
      fs.writeFileSync(manifestPath, "\n\n\n");
      const result = parseSafeOutputsManifest(manifestPath);
      expect(result).toEqual({ total_items: 0, items_by_type: {}, items: [] });
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
      expect(result.items).toEqual(lines.split("\n").map(line => JSON.parse(line)));
      expect(ledgerActivityFromSafeOutputs(result)).toEqual({ transactions_added: 0 });
    });

    it("counts only recorded ledger mutations as added transactions", () => {
      const lines = [{ type: "ledger_mutation" }, { type: "ledger_append" }, { type: "create_issue" }, { type: "ledger_mutation" }, { type: "ledger_mutation" }];
      fs.writeFileSync(manifestPath, lines.map(JSON.stringify).join("\n"));
      expect(ledgerActivityFromSafeOutputs(parseSafeOutputsManifest(manifestPath))).toEqual({ transactions_added: 3 });
    });

    it("collects compaction stats independently of the safe-output manifest", () => {
      const compaction = {
        before: 4,
        after: 2,
        selected: 3,
        records: 12,
        replacement: "abc123",
        retired: 3,
        changed: true,
      };
      expect(parseLedgerCompaction(JSON.stringify(compaction))).toEqual(compaction);
      expect(ledgerActivityFromSafeOutputs(null, compaction)).toEqual({ compaction });
      expect(ledgerActivityFromSafeOutputs({ items_by_type: {} }, compaction)).toEqual({
        transactions_added: 0,
        compaction,
      });
      expect(parseLedgerCompaction('{"before":-1}')).toBeNull();
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
      expect(result).toEqual({ total_items: 0, items_by_type: {}, items: [] });
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

  describe("friction-cost section", () => {
    let frictionDir;

    beforeEach(() => {
      frictionDir = fs.mkdtempSync(path.join(__dirname, ".friction-test-"));
    });

    afterEach(() => {
      fs.rmSync(frictionDir, { recursive: true, force: true });
    });

    const writeTokenUsage = lines => {
      const target = path.join(frictionDir, "token_usage.jsonl");
      fs.writeFileSync(target, lines.map(line => JSON.stringify(line)).join("\n"));
      return target;
    };

    it("reports an unavailable section when no activity source exists", () => {
      const friction = buildFrictionSummary({ gateway: null, integrity: null, session: null, firewall: null }, path.join(frictionDir, "missing.jsonl"));
      expect(friction).not.toHaveProperty("schema");
      expect(friction.canonical_unit).toBe("aic");
      expect(friction.measurement_state).toBe("unavailable");
      expect(friction.cost.aic).toBe(0);
    });

    it("computes friction from parsed activity sections and the token-usage file", () => {
      const tokenUsagePath = writeTokenUsage([
        { timestamp: "2026-01-01T00:00:01Z", input_tokens: 100, output_tokens: 10, ai_credits_this_response: 0.5, duration_ms: 100 },
        { timestamp: "2026-01-01T00:00:03Z", input_tokens: 100, output_tokens: 10, ai_credits_this_response: 0.25, duration_ms: 100 },
      ]);
      const friction = buildFrictionSummary(
        {
          gateway: { tool_calls: [{ tool_call_id: "call-1", timestamp: "2026-01-01T00:00:02Z", server_name: "github", tool_name: "issue_read", outcome: "failure", duration_ms: 42 }] },
          integrity: null,
          session: null,
          firewall: null,
        },
        tokenUsagePath
      );
      expect(friction.measurement_state).toBe("causal");
      expect(friction.cost.aic).toBe(0.25);
      expect(friction.cost.tool_calls).toBe(1);
      expect(friction.sources).toContain("mcp_gateway");
      expect(friction.sources).toContain("agent_token_usage");
    });

    it("reads token usage content only when the file exists", () => {
      expect(readTokenUsageContent(path.join(frictionDir, "missing.jsonl"))).toEqual({ content: "", available: false });
      const tokenUsagePath = writeTokenUsage([{ timestamp: "2026-01-01T00:00:01Z", input_tokens: 1 }]);
      const { content, available } = readTokenUsageContent(tokenUsagePath);
      expect(available).toBe(true);
      expect(content).toContain("input_tokens");
    });

    it("serializes to JSON without losing aggregate or event-level detail", () => {
      const friction = buildFrictionSummary(
        {
          gateway: null,
          integrity: null,
          session: null,
          firewall: { requests_by_domain: { "blocked.example": { allowed: 0, blocked: 2 } } },
        },
        path.join(frictionDir, "missing.jsonl")
      );
      const roundTripped = JSON.parse(JSON.stringify(friction));
      expect(roundTripped.total_occurrences).toBe(2);
      expect(roundTripped.events[0].driver).toBe("firewall_block");
      expect(roundTripped.drivers[0].driver).toBe("firewall_block");
      expect(roundTripped.groups[0].group_id).toBe("network_block");
    });

    it("renders friction cost and driver data in a collapsed step summary section", () => {
      const summary = buildFrictionStepSummary({
        measurement_state: "causal",
        counted_occurrences: 2,
        friction_ratio: 0.125,
        cost: {
          aic: 0.25,
          tokens: { total: 1234 },
          turns: 1,
          tool_calls: 2,
          latency_ms: 500,
        },
        drivers: [
          {
            driver: "mcp_tool_error",
            source: "mcp_gateway",
            state: "causal",
            counted_occurrences: 2,
            cost: { aic: 0.25 },
          },
        ],
      });

      expect(summary).toContain("<details>\n<summary>Friction Cost: 0.25 AIC (causal)</summary>");
      expect(summary).toContain("### Friction Cost");
      expect(summary).toContain("| 0.25 | 12.5% | 2 | 1,234 | 1 | 2 | 500 ms |");
      expect(summary).toContain("| `mcp_tool_error` | `mcp_gateway` | `causal` | 2 | 0.25 |");
      expect(summary).toContain("</details>");
    });

    it("renders unavailable friction measurements without implying a measured ratio", () => {
      const summary = buildFrictionStepSummary({
        measurement_state: "unavailable",
        counted_occurrences: 0,
        cost: {
          aic: 0,
          tokens: { total: 0 },
          turns: 0,
          tool_calls: 0,
          latency_ms: 0,
        },
        drivers: [],
      });

      expect(summary).toContain("<summary>Friction Cost: 0 AIC (unavailable)</summary>");
      expect(summary).toContain("| 0 | unavailable | 0 | 0 | 0 | 0 | 0 ms |");
    });

    it("suppresses the ratio for an unavailable aggregate even when friction_ratio is finite", () => {
      // Mirrors the mixed measured API-error plus unavailable firewall-block case in
      // friction_cost_metrics.test.cjs, where computeFrictionCost can emit an unavailable
      // aggregate alongside a finite friction_ratio.
      const summary = buildFrictionStepSummary({
        measurement_state: "unavailable",
        counted_occurrences: 1,
        friction_ratio: 0.4,
        cost: {
          aic: 0.1,
          tokens: { total: 100 },
          turns: 1,
          tool_calls: 1,
          latency_ms: 10,
        },
        drivers: [],
      });

      expect(summary).toContain("<summary>Friction Cost: 0.1 AIC (unavailable)</summary>");
      expect(summary).toContain("| 0.1 | unavailable | 1 | 100 | 1 | 1 | 10 ms |");
    });

    describe("writeFrictionStepSummary", () => {
      let summaryFile;
      let previousSummaryEnv;

      beforeEach(() => {
        summaryFile = path.join(frictionDir, "step-summary.md");
        previousSummaryEnv = process.env.GITHUB_STEP_SUMMARY;
      });

      afterEach(() => {
        if (previousSummaryEnv === undefined) {
          delete process.env.GITHUB_STEP_SUMMARY;
        } else {
          process.env.GITHUB_STEP_SUMMARY = previousSummaryEnv;
        }
      });

      it("appends the rendered section to GITHUB_STEP_SUMMARY without using core.summary", async () => {
        process.env.GITHUB_STEP_SUMMARY = summaryFile;
        fs.writeFileSync(summaryFile, "existing content\n", "utf8");

        await writeFrictionStepSummary("<details>friction</details>");

        expect(fs.readFileSync(summaryFile, "utf8")).toBe("existing content\n<details>friction</details>");
      });

      it("inserts a newline separator when existing content has no trailing newline", async () => {
        process.env.GITHUB_STEP_SUMMARY = summaryFile;
        fs.writeFileSync(summaryFile, "existing content", "utf8");

        await writeFrictionStepSummary("<details>friction</details>");

        expect(fs.readFileSync(summaryFile, "utf8")).toBe("existing content\n<details>friction</details>");
      });

      it("does not prepend a blank line when the summary file is missing or empty", async () => {
        process.env.GITHUB_STEP_SUMMARY = summaryFile;

        await writeFrictionStepSummary("<details>friction</details>");

        expect(fs.readFileSync(summaryFile, "utf8")).toBe("<details>friction</details>");
      });

      it("is a no-op when the section is empty", async () => {
        process.env.GITHUB_STEP_SUMMARY = summaryFile;
        fs.writeFileSync(summaryFile, "existing content\n", "utf8");

        await writeFrictionStepSummary("");

        expect(fs.readFileSync(summaryFile, "utf8")).toBe("existing content\n");
      });
    });
  });
});
