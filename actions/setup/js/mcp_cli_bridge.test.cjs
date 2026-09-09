import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import http from "http";
import os from "os";
import path from "path";

import {
  ensureSafeOutputsTools,
  auditLog,
  ensureAuditDir,
  formatResponse,
  getToolCallTimeoutMs,
  hasStdinJsonPayload,
  main,
  parseToolArgs,
  readStdinSync,
  shouldShowToolHelpForEmptyArgs,
  showHelp,
  showToolHelp,
  tryExtractJsonFieldFromStdin,
  unescapeCliStringArg,
  writeStdoutAndFlush,
} from "./mcp_cli_bridge.cjs";

describe("mcp_cli_bridge.cjs", () => {
  let originalCore;
  let stdoutSpy;
  let stderrSpy;
  /** @type {string[]} */
  let stdoutChunks;
  /** @type {string[]} */
  let stderrChunks;

  beforeEach(() => {
    originalCore = global.core;
    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setFailed: vi.fn(() => {
        process.exitCode = 1;
      }),
    };
    process.exitCode = 0;
    stdoutChunks = [];
    stderrChunks = [];
    stdoutSpy = vi.spyOn(process.stdout, "write").mockImplementation(chunk => {
      stdoutChunks.push(String(chunk));
      return true;
    });
    stderrSpy = vi.spyOn(process.stderr, "write").mockImplementation(chunk => {
      stderrChunks.push(String(chunk));
      return true;
    });
  });

  afterEach(() => {
    stdoutSpy.mockRestore();
    stderrSpy.mockRestore();
    global.core = originalCore;
    process.exitCode = 0;
  });

  it("coerces integer and array arguments based on tool schema", () => {
    const schemaProperties = {
      count: { type: "integer" },
      workflows: { type: ["null", "array"] },
    };

    const { args } = parseToolArgs(["--count", "3", "--workflows", "daily-issues-report"], schemaProperties);

    expect(args).toEqual({
      count: 3,
      workflows: ["daily-issues-report"],
    });
  });

  it("maps dashed arg names to underscored schema keys", () => {
    const schemaProperties = {
      issue_number: { type: "integer" },
    };

    const { args } = parseToolArgs(["--issue-number", "42"], schemaProperties);

    expect(args).toEqual({
      issue_number: 42,
    });
  });

  it("maps underscored arg names to dashed schema keys", () => {
    const schemaProperties = {
      "issue-number": { type: "integer" },
    };

    const { args } = parseToolArgs(["--issue_number=99"], schemaProperties);

    expect(args).toEqual({
      "issue-number": 99,
    });
  });

  it("keeps exact schema keys when normalized forms collide", () => {
    const schemaProperties = {
      "issue-number": { type: "integer" },
      issue_number: { type: "integer" },
    };

    const dashed = parseToolArgs(["--issue-number", "7"], schemaProperties);
    const underscored = parseToolArgs(["--issue_number", "8"], schemaProperties);

    expect(dashed.args).toEqual({
      "issue-number": 7,
    });
    expect(underscored.args).toEqual({
      issue_number: 8,
    });
  });

  it("falls back to raw key when normalized schema key is ambiguous", () => {
    const schemaProperties = {
      "issue-number": { type: "integer" },
      issue_number: { type: "integer" },
    };

    const { args } = parseToolArgs(["--issuenumber", "11"], schemaProperties);

    expect(args).toEqual({
      issuenumber: "11",
    });
  });

  it("keeps normalized key unresolved when 3+ schema keys collide", () => {
    const schemaProperties = {
      "issue-number": { type: "integer" },
      issue_number: { type: "integer" },
      issueNumber: { type: "integer" },
    };

    const { args } = parseToolArgs(["--issuenumber", "15"], schemaProperties);

    expect(args).toEqual({
      issuenumber: "15",
    });
  });

  it("keeps unknown argument keys unchanged", () => {
    const schemaProperties = {
      issue_number: { type: "integer" },
    };

    const { args } = parseToolArgs(["--custom-field", "value"], schemaProperties);

    expect(args).toEqual({
      "custom-field": "value",
    });
  });

  it("normalizes repeated mixed dash/underscore arguments for array schema", () => {
    const schemaProperties = {
      issue_number: { type: "array" },
    };

    const { args } = parseToolArgs(["--issue-number", "1", "--issue_number", "2"], schemaProperties);

    expect(args).toEqual({
      issue_number: ["1", "2"],
    });
  });

  it("falls back to numeric coercion when schema properties are unavailable", () => {
    const { args } = parseToolArgs(["--count", "3", "--max_tokens", "3000"], {});

    expect(args).toEqual({
      count: 3,
      max_tokens: 3000,
    });
  });

  it("recovers empty safeoutputs schema from fallback tools path", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "bridge-safeoutputs-"));
    const fallbackPath = path.join(tempDir, "tools.json");
    fs.writeFileSync(fallbackPath, JSON.stringify([{ name: "report_incomplete" }]), "utf8");
    const originalPath = process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
    process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = fallbackPath;
    try {
      const recovered = ensureSafeOutputsTools([], "safeoutputs", path.join(tempDir, "empty.json"));
      expect(recovered).toHaveLength(1);
      expect(recovered[0].name).toBe("report_incomplete");
      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("recovered"));
    } finally {
      if (originalPath === undefined) {
        delete process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
      } else {
        process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = originalPath;
      }
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("fails fast when safeoutputs schema is empty", () => {
    const originalPath = process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
    const originalRunnerTemp = process.env.RUNNER_TEMP;
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "bridge-empty-safeoutputs-"));
    delete process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
    process.env.RUNNER_TEMP = tempDir;
    try {
      expect(() => ensureSafeOutputsTools([], "safeoutputs", path.join(tempDir, "safeoutputs.json"))).toThrow(/tool schema is empty/);
    } finally {
      if (originalPath !== undefined) {
        process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = originalPath;
      }
      if (originalRunnerTemp === undefined) {
        delete process.env.RUNNER_TEMP;
      } else {
        process.env.RUNNER_TEMP = originalRunnerTemp;
      }
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("allows zero-argument tools to proceed — only shows help when required fields are declared", () => {
    // Empty schema (zero-input custom tool) — must NOT show help; empty call is valid
    const emptySchemaTools = { inputSchema: { type: "object", properties: {}, additionalProperties: false } };
    expect(shouldShowToolHelpForEmptyArgs("safeoutputs", {}, emptySchemaTools)).toBe(false);

    // Optional-only tool (required array absent) — must NOT show help
    const optionalOnlyTool = { inputSchema: { type: "object", properties: { flag: { type: "boolean" } } } };
    expect(shouldShowToolHelpForEmptyArgs("safeoutputs", {}, optionalOnlyTool)).toBe(false);

    // Optional-only tool (required array present but empty) — must NOT show help
    const emptyRequiredTool = { inputSchema: { required: [] } };
    expect(shouldShowToolHelpForEmptyArgs("safeoutputs", {}, emptyRequiredTool)).toBe(false);

    // Tool with required fields and empty args — MUST show help (probe detection)
    const requiredFieldTool = { inputSchema: { required: ["title"] } };
    expect(shouldShowToolHelpForEmptyArgs("safeoutputs", {}, requiredFieldTool)).toBe(true);

    // Missing matchedTool (e.g. unknown tool) — treated as no-required; must NOT show help
    expect(shouldShowToolHelpForEmptyArgs("safeoutputs", {}, null)).toBe(false);
    expect(shouldShowToolHelpForEmptyArgs("safeoutputs", {}, undefined)).toBe(false);

    // Non-empty args are never affected
    expect(shouldShowToolHelpForEmptyArgs("safeoutputs", { title: "Bug report" }, requiredFieldTool)).toBe(false);

    // Non-safeoutputs servers are never affected
    expect(shouldShowToolHelpForEmptyArgs("other-server", {}, requiredFieldTool)).toBe(false);
  });

  it("coerces scientific notation when schema properties are unavailable", () => {
    const { args } = parseToolArgs(["--max_tokens", "1e3", "--threshold", "-2E-4"], {});

    expect(args).toEqual({
      max_tokens: 1000,
      threshold: -0.0002,
    });
  });

  it("preserves non-numeric values when schema properties are unavailable", () => {
    const { args } = parseToolArgs(["--start_date", "-1d", "--workflow_name", "daily-issues-report"], {});

    expect(args).toEqual({
      start_date: "-1d",
      workflow_name: "daily-issues-report",
    });
  });

  it("uses default 120s timeout for non-logs tools", () => {
    expect(getToolCallTimeoutMs("audit", {})).toBe(120000);
  });

  it("writes owner-only audit metadata without arguments, responses, or errors", () => {
    const auditDir = fs.mkdtempSync(path.join(os.tmpdir(), "bridge-audit-"));
    const sentinel = "sentinel-secret";
    try {
      ensureAuditDir(auditDir);
      auditLog(
        "server/../name",
        {
          event: "tools_call_done",
          tool: "example",
          statusCode: 200,
          elapsedMs: 12,
          argumentBytes: 42,
          arguments: { apiKey: sentinel },
          response: { secret: sentinel },
          error: sentinel,
        },
        auditDir
      );

      const files = fs.readdirSync(auditDir);
      expect(files).toEqual(["server_.._name.jsonl"]);
      const logPath = path.join(auditDir, files[0]);
      const record = JSON.parse(fs.readFileSync(logPath, "utf8"));
      expect(record).toMatchObject({
        server: "server/../name",
        event: "tools_call_done",
        tool: "example",
        statusCode: 200,
        elapsedMs: 12,
        argumentBytes: 42,
      });
      expect(record).not.toHaveProperty("arguments");
      expect(record).not.toHaveProperty("response");
      expect(record).not.toHaveProperty("error");
      expect(fs.statSync(auditDir).mode & 0o777).toBe(0o700);
      expect(fs.statSync(logPath).mode & 0o777).toBe(0o600);
      expect(fs.readFileSync(logPath, "utf8")).not.toContain(sentinel);
    } finally {
      fs.rmSync(auditDir, { recursive: true, force: true });
    }
  });

  it("removes audit records older than the 24-hour retention window", () => {
    const auditDir = fs.mkdtempSync(path.join(os.tmpdir(), "bridge-audit-"));
    const staleLog = path.join(auditDir, "stale.jsonl");
    const currentLog = path.join(auditDir, "current.jsonl");
    try {
      fs.writeFileSync(staleLog, "{}\n");
      fs.writeFileSync(currentLog, "{}\n");
      const staleTime = new Date(Date.now() - 25 * 60 * 60 * 1000);
      fs.utimesSync(staleLog, staleTime, staleTime);

      ensureAuditDir(auditDir);

      expect(fs.existsSync(staleLog)).toBe(false);
      expect(fs.existsSync(currentLog)).toBe(true);
    } finally {
      fs.rmSync(auditDir, { recursive: true, force: true });
    }
  });

  it("uses a longer timeout for logs calls without explicit timeout (default count=100, no filter)", () => {
    // effectiveCount=100, base=ceil(100/40)=3, no workflow_name → max(5,3)=5 minutes
    expect(getToolCallTimeoutMs("logs", {})).toBe(315000);
  });

  it("scales logs timeout from count when no explicit timeout is set (count=250, no filter)", () => {
    // effectiveCount=250, base=ceil(250/40)=7, no workflow_name → max(5,7)=7 minutes
    expect(getToolCallTimeoutMs("logs", { count: 250 })).toBe(435000);
  });

  it("scales logs timeout from count with workflow_name filter (count=250, filtered)", () => {
    // effectiveCount=250, base=ceil(250/40)=7, workflow_name present → 7 minutes (no min floor applied)
    expect(getToolCallTimeoutMs("logs", { count: 250, workflow_name: "ci" })).toBe(435000);
  });

  it("clamps count-based timeout to global minimum for small filtered counts", () => {
    // effectiveCount=40, base=ceil(40/40)=1, workflow_name present → 1 minute → 75000ms < 120000ms → clamped
    expect(getToolCallTimeoutMs("logs", { count: 40, workflow_name: "ci" })).toBe(120000);
  });

  it("applies 5-minute no-filter floor for small unfiltered counts", () => {
    // effectiveCount=40, base=1, no workflow_name → max(5,1)=5 minutes
    expect(getToolCallTimeoutMs("logs", { count: 40 })).toBe(315000);
  });

  it("applies 5-minute floor when engine filter is present, even with workflow_name", () => {
    // effectiveCount=40, base=1, workflow_name present but engine present too → max(5,1)=5 minutes
    expect(getToolCallTimeoutMs("logs", { count: 40, workflow_name: "ci", engine: "claude" })).toBe(315000);
  });

  it("applies 5-minute floor when engine filter is present without workflow_name", () => {
    // effectiveCount=40, base=1, no workflow_name, engine present → max(5,1)=5 minutes
    expect(getToolCallTimeoutMs("logs", { count: 40, engine: "claude" })).toBe(315000);
  });

  it("uses logs timeout argument with bridge buffer when provided", () => {
    // timeout=10min, floor=5min (default count=100, no filter) → max(120000, 315000, 615000) = 615000
    expect(getToolCallTimeoutMs("logs", { timeout: 10 })).toBe(615000);
  });

  it("floors small explicit timeout to the count-derived minimum", () => {
    // timeout=2.5min → explicit=165000ms; floor=5min → 315000ms; floor wins
    expect(getToolCallTimeoutMs("logs", { timeout: 2.5 })).toBe(315000);
  });

  it("caps explicit timeout at LOGS_TOOL_MAX_EXPLICIT_TIMEOUT_MINUTES (60)", () => {
    // timeout=999min → clamped to 60min → 3615000ms; floor=315000ms; capped value wins
    expect(getToolCallTimeoutMs("logs", { timeout: 999 })).toBe(3615000);
  });

  it("rejects non-numeric timeout types and falls back to count-derived timeout", () => {
    // typeof-check rejects strings and booleans even when Number() would accept them
    expect(getToolCallTimeoutMs("logs", { timeout: 0 })).toBe(315000);
    expect(getToolCallTimeoutMs("logs", { timeout: -5 })).toBe(315000);
    expect(getToolCallTimeoutMs("logs", { timeout: "invalid" })).toBe(315000);
    expect(getToolCallTimeoutMs("logs", { timeout: "5" })).toBe(315000);
    expect(getToolCallTimeoutMs("logs", { timeout: true })).toBe(315000);
  });

  it("treats MCP result envelopes with isError=true as errors", async () => {
    await formatResponse(
      {
        result: {
          isError: true,
          content: [{ type: "text", text: '{"error":"failed to audit workflow run"}' }],
        },
      },
      "agenticworkflows"
    );

    expect(stdoutChunks.join("")).toBe("");
    expect(stderrChunks.join("")).toContain("failed to audit workflow run");
    expect(process.exitCode).toBe(1);
  });

  it("prints progress notifications to stderr and final text result to stdout for SSE responses", async () => {
    const sseBody = [
      'data: {"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"abc","progress":1,"total":3,"message":"Step 1/3"}}',
      'data: {"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"done"}]}}',
      "",
    ].join("\n");

    await formatResponse(sseBody, "agenticworkflows");

    expect(stderrChunks.join("")).toContain("Step 1/3");
    expect(stdoutChunks.join("")).toBe("done\n");
    expect(process.exitCode).toBe(0);
  });

  it("prints numeric progress to stderr when progress notification has no message", async () => {
    const sseBody = ['data: {"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"abc","progress":2,"total":5}}', 'data: {"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"ok"}]}}', ""].join("\n");

    await formatResponse(sseBody, "agenticworkflows");

    expect(stderrChunks.join("")).toContain("Progress: 2/5");
    expect(stdoutChunks.join("")).toBe("ok\n");
    expect(process.exitCode).toBe(0);
  });

  it("adds a non-retry hint for safeoutputs empty-argument rejections", async () => {
    await formatResponse(
      {
        error: {
          code: -32602,
          message: "Empty arguments are not allowed — this tool is write-once, not a discovery probe.",
        },
      },
      "safeoutputs",
      "create_issue"
    );

    const stderr = stderrChunks.join("");
    expect(stderr).toContain("Error [-32602]: Empty arguments are not allowed");
    expect(stderr).toContain("do not retry 'safeoutputs create_issue' with empty arguments");
    expect(stderr).toContain("safeoutputs create_issue --help");
    expect(process.exitCode).toBe(1);
  });

  it("omits non-retry hint when toolName is absent", async () => {
    await formatResponse(
      {
        error: {
          code: -32602,
          message: "Empty arguments are not allowed — this tool is write-once, not a discovery probe.",
        },
      },
      "safeoutputs"
      // toolName omitted → defaults to ""
    );

    const stderr = stderrChunks.join("");
    expect(stderr).toContain("Error [-32602]");
    expect(stderr).not.toContain("do not retry");
    expect(process.exitCode).toBe(1);
  });

  it("does not add a non-retry hint for -32602 errors from non-safeoutputs servers", async () => {
    await formatResponse(
      {
        error: {
          code: -32602,
          message: "Empty arguments are not allowed — this tool is write-once, not a discovery probe.",
        },
      },
      "agenticworkflows",
      "some_tool"
    );

    const stderr = stderrChunks.join("");
    expect(stderr).toContain("Error [-32602]");
    expect(stderr).not.toContain("do not retry");
    expect(stderr).not.toContain("--help");
    expect(process.exitCode).toBe(1);
  });

  it("keeps top-level help compact for many commands", () => {
    const tools = Array.from({ length: 25 }, (_, i) => ({
      name: `tool_${i + 1}`,
      description: `Description for command ${i + 1} that is intentionally verbose for truncation checks.`,
    }));

    showHelp("safeoutputs", tools);

    const outputLines = stdoutChunks.join("").trimEnd().split("\n");
    const output = outputLines.join("\n");
    expect(outputLines.length).toBeLessThanOrEqual(20);
    expect(output).not.toMatch(/\.\.\. \+\d+ more command\(s\)/);
    for (const tool of tools) {
      expect(output).toContain(tool.name);
    }
  });

  it("does not truncate top-level help when commands exactly fit the line budget", () => {
    const tools = Array.from({ length: 14 }, (_, i) => ({
      name: `tool_${i + 1}`,
      description: `Description for command ${i + 1}.`,
    }));

    showHelp("safeoutputs", tools);

    const outputLines = stdoutChunks.join("").trimEnd().split("\n");
    const output = outputLines.join("\n");
    expect(outputLines.length).toBeLessThanOrEqual(20);
    expect(output).not.toMatch(/\.\.\. \+\d+ more command\(s\)/);
    for (const tool of tools) {
      expect(output).toContain(tool.name);
    }
  });

  it("keeps command help compact for many options", () => {
    const properties = {};
    for (let i = 1; i <= 24; i++) {
      properties[`field_${i}`] = { type: "string", description: `Field ${i} description with additional details for truncation.` };
    }

    showToolHelp("safeoutputs", "create_issue", [
      {
        name: "create_issue",
        description: "Create an issue with many available fields and optional metadata.",
        inputSchema: {
          properties,
          required: ["field_1", "field_2"],
        },
      },
    ]);

    const outputLines = stdoutChunks.join("").trimEnd().split("\n");
    const output = outputLines.join("\n");
    expect(outputLines.length).toBeLessThanOrEqual(30);
    expect(output).not.toMatch(/\.\.\. \+\d+ more option\(s\)/);
    expect(output).toContain("Required options are marked with *.");
    for (let i = 1; i <= 24; i++) {
      expect(output).toContain(`--field_${i}`);
    }
    expect(output).toContain("--field_1*");
    expect(output).toContain("--field_2*");
  });

  it("does not truncate command help when options exactly fit the line budget", () => {
    const properties = {};
    for (let i = 1; i <= 13; i++) {
      properties[`field_${i}`] = { type: "string", description: `Field ${i}.` };
    }

    showToolHelp("safeoutputs", "create_issue", [
      {
        name: "create_issue",
        description: "Create an issue.",
        inputSchema: {
          properties,
          required: ["field_1"],
        },
      },
    ]);

    const outputLines = stdoutChunks.join("").trimEnd().split("\n");
    const output = outputLines.join("\n");
    expect(outputLines.length).toBeLessThanOrEqual(30);
    expect(output).not.toMatch(/\.\.\. \+\d+ more option\(s\)/);
    expect(output).toContain("Required options are marked with *.");
    for (let i = 1; i <= 13; i++) {
      expect(output).toContain(`--field_${i}`);
    }
  });

  it("keeps required note when required options are in the compact list", () => {
    const properties = {};
    for (let i = 1; i <= 24; i++) {
      properties[`field_${i}`] = { type: "string", description: `Field ${i}.` };
    }

    showToolHelp("safeoutputs", "create_issue", [
      {
        name: "create_issue",
        description: "Create an issue.",
        inputSchema: {
          properties,
          required: ["field_23", "field_24"],
        },
      },
    ]);

    const outputLines = stdoutChunks.join("").trimEnd().split("\n");
    const output = outputLines.join("\n");
    expect(output).not.toMatch(/\.\.\. \+\d+ more option\(s\)/);
    expect(output).toContain("Required options are marked with *.");
    expect(output).toContain("--field_23*");
    expect(output).toContain("--field_24*");
  });

  describe("stdin placeholder removed — '-' is always a literal value", () => {
    it("passes '--key -' as literal '-' (space-separated form)", () => {
      const schemaProperties = { body: { type: "string" } };
      const stdinContent = "some stdin content";

      const { args } = parseToolArgs(["--body", "-"], schemaProperties, stdinContent);

      expect(args).toEqual({ body: "-" });
    });

    it("passes '--key=-' as literal '-' (equals form)", () => {
      const schemaProperties = { body: { type: "string" } };
      const stdinContent = "some stdin content";

      const { args } = parseToolArgs(["--body=-"], schemaProperties, stdinContent);

      expect(args).toEqual({ body: "-" });
    });

    it("throws when stdin exceeds maximum allowed size", () => {
      const fs = require("fs");
      // Simulate reading more than 10 MB total by making readSync return data repeatedly
      const STDIN_MAX_BYTES = 10 * 1024 * 1024;
      const callCount = { n: 0 };
      const readSyncSpy = vi.spyOn(fs, "readSync").mockImplementation((_fd, buf, _offset, length) => {
        callCount.n++;
        // Each call fills the buffer until we exceed the limit
        if (callCount.n > STDIN_MAX_BYTES / length + 1) return 0;
        buf.fill(0x41, 0, length); // fill with 'A'
        return length;
      });

      try {
        expect(() => readStdinSync()).toThrow(/exceeds maximum allowed size/);
      } finally {
        readSyncSpy.mockRestore();
      }
    });

    it("returns empty string when readSync errors before any bytes are read", () => {
      const fs = require("fs");
      const readSyncSpy = vi.spyOn(fs, "readSync").mockImplementation(() => {
        throw new Error("EBADF: bad file descriptor");
      });

      try {
        expect(readStdinSync()).toBe("");
      } finally {
        readSyncSpy.mockRestore();
      }
    });

    it("rethrows readSync errors that occur after some bytes have already been read", () => {
      const fs = require("fs");
      let callCount = 0;
      const readSyncSpy = vi.spyOn(fs, "readSync").mockImplementation((_fd, buf, _offset, length) => {
        callCount++;
        if (callCount === 1) {
          // First call: return some data
          buf.fill(0x41, 0, length);
          return length;
        }
        // Second call: simulate a mid-stream read error
        throw new Error("EIO: i/o error");
      });

      try {
        expect(() => readStdinSync()).toThrow(/EIO/);
      } finally {
        readSyncSpy.mockRestore();
      }
    });
  });

  describe("stdin JSON payload support", () => {
    it("returns true for '.' sentinel", () => {
      expect(hasStdinJsonPayload(["."])).toBe(true);
    });

    it("returns true for empty args when stdin is not a TTY", () => {
      const origIsTTY = process.stdin.isTTY;
      process.stdin.isTTY = undefined;
      try {
        expect(hasStdinJsonPayload([])).toBe(true);
      } finally {
        process.stdin.isTTY = origIsTTY;
      }
    });

    it("returns false for empty args when stdin is a TTY", () => {
      const origIsTTY = process.stdin.isTTY;
      // @ts-ignore
      process.stdin.isTTY = true;
      try {
        expect(hasStdinJsonPayload([])).toBe(false);
      } finally {
        process.stdin.isTTY = origIsTTY;
      }
    });

    it("returns false when args contain flags", () => {
      expect(hasStdinJsonPayload(["--body", "hello"])).toBe(false);
    });

    it("returns false when args has more than just '.'", () => {
      expect(hasStdinJsonPayload([".", "--extra", "value"])).toBe(false);
    });

    it("returns true for '--key .' (per-field stdin marker, space-separated)", () => {
      expect(hasStdinJsonPayload(["--body", "."])).toBe(true);
    });

    it("returns true for '--key=.' (per-field stdin marker, equals-separated)", () => {
      expect(hasStdinJsonPayload(["--body=."])).toBe(true);
    });

    it("returns true for mixed flags when one uses the per-field sentinel", () => {
      expect(hasStdinJsonPayload(["--title", "My title", "--body", "."])).toBe(true);
    });

    it("returns false when flag value is not '.' (not a sentinel)", () => {
      expect(hasStdinJsonPayload(["--body", "hello"])).toBe(false);
    });

    it("parses stdin JSON object when '.' sentinel is used", () => {
      const schemaProperties = {
        issue_number: { type: "integer" },
        body: { type: "string" },
      };
      const stdinContent = '{"issue_number": 42, "body": "hello world"}';

      const { args } = parseToolArgs(["."], schemaProperties, stdinContent);

      expect(args).toEqual({ issue_number: 42, body: "hello world" });
    });

    it("parses stdin JSON object when no args and stdinContent is provided", () => {
      const schemaProperties = {
        issue_number: { type: "integer" },
        body: { type: "string" },
      };
      const stdinContent = '{"issue_number": 7, "body": "test body"}';

      const { args } = parseToolArgs([], schemaProperties, stdinContent);

      expect(args).toEqual({ issue_number: 7, body: "test body" });
    });

    it("preserves types from JSON payload without coercion", () => {
      const schemaProperties = {
        count: { type: "integer" },
        enabled: { type: "boolean" },
        tags: { type: "array" },
      };
      const stdinContent = '{"count": 5, "enabled": true, "tags": ["a", "b"]}';

      const { args } = parseToolArgs(["."], schemaProperties, stdinContent);

      expect(args).toEqual({ count: 5, enabled: true, tags: ["a", "b"] });
    });

    it("normalizes dashed JSON keys to schema underscore keys", () => {
      const schemaProperties = {
        issue_number: { type: "integer" },
      };
      const stdinContent = '{"issue-number": 99}';

      const { args } = parseToolArgs(["."], schemaProperties, stdinContent);

      expect(args).toEqual({ issue_number: 99 });
    });

    it("falls through to empty args when stdinContent is null and sentinel is used", () => {
      const { args } = parseToolArgs(["."], {}, null);

      expect(args).toEqual({});
    });

    it("throws a parse error when explicit JSON payload mode receives empty stdin", () => {
      expect(() => parseToolArgs(["."], {}, "")).toThrow(/stdin is not valid JSON/i);
      expect(() => parseToolArgs(["."], {}, "")).toThrow(/requested with '\.'/i);
    });

    it("throws a parse error when explicit JSON payload mode receives invalid JSON", () => {
      const schemaProperties = { body: { type: "string" } };

      expect(() => parseToolArgs(["."], schemaProperties, "not json at all")).toThrow(/stdin is not valid JSON/i);
      expect(() => parseToolArgs(["."], schemaProperties, "not json at all")).toThrow(/requested with '\.'/i);
    });

    it("throws when JSON payload mode receives non-object JSON", () => {
      expect(() => parseToolArgs(["."], {}, '["a","b","c"]')).toThrow(/payload must be an object/i);
    });

    it("throws a parse error when no-flag piped stdin payload is invalid JSON", () => {
      expect(() => parseToolArgs([], {}, "{invalid json")).toThrow(/stdin is not valid JSON/i);
      expect(() => parseToolArgs([], {}, "{invalid json")).toThrow(/from piped stdin with no flags/i);
    });

    it("throws a parse error when no-flag piped stdin payload is whitespace-only", () => {
      expect(() => parseToolArgs([], {}, "   \n   ")).toThrow(/stdin is not valid JSON/i);
      expect(() => parseToolArgs([], {}, "   \n   ")).toThrow(/from piped stdin with no flags/i);
    });

    it("falls through to empty args for no-flag mode when stdin is truly empty", () => {
      const { args } = parseToolArgs([], {}, "");

      expect(args).toEqual({});
    });

    it("handles multiline JSON payload", () => {
      const schemaProperties = { body: { type: "string" } };
      const stdinContent = `{
  "body": "### Title\\n\\nLine one.\\n\\nLine two."
}`;

      const { args } = parseToolArgs(["."], schemaProperties, stdinContent);

      expect(args).toEqual({ body: "### Title\n\nLine one.\n\nLine two." });
    });
  });

  describe("per-field stdin marker ('.')", () => {
    it("substitutes '--body .' with stdin content (space-separated form)", () => {
      const schemaProperties = { title: { type: "string" }, body: { type: "string" } };
      const stdinContent = "This is a long body from stdin.";

      const { args } = parseToolArgs(["--title", "My issue", "--body", "."], schemaProperties, stdinContent);

      expect(args).toEqual({ title: "My issue", body: "This is a long body from stdin." });
    });

    it("substitutes '--body=.' with stdin content (equals-separated form)", () => {
      const schemaProperties = { body: { type: "string" } };
      const stdinContent = "Body content from stdin.";

      const { args } = parseToolArgs(["--body=."], schemaProperties, stdinContent);

      expect(args).toEqual({ body: "Body content from stdin." });
    });

    it("trims leading/trailing whitespace from stdin content", () => {
      const schemaProperties = { body: { type: "string" } };
      const stdinContent = "  \n  Trimmed content.  \n  ";

      const { args } = parseToolArgs(["--body", "."], schemaProperties, stdinContent);

      expect(args).toEqual({ body: "Trimmed content." });
    });

    it("falls back to literal '.' when stdinContent is null", () => {
      const schemaProperties = { body: { type: "string" } };

      const { args } = parseToolArgs(["--body", "."], schemaProperties, null);

      expect(args).toEqual({ body: "." });
    });

    it("falls back to literal '.' when stdinContent is empty", () => {
      const schemaProperties = { body: { type: "string" } };

      const { args } = parseToolArgs(["--body", "."], schemaProperties, "");

      expect(args).toEqual({ body: "." });
    });

    it("falls back to literal '.' when stdinContent is whitespace-only", () => {
      const schemaProperties = { body: { type: "string" } };

      const { args } = parseToolArgs(["--body", "."], schemaProperties, "   \n   ");

      expect(args).toEqual({ body: "." });
    });

    it("all fields using '.' receive the same stdin content", () => {
      const schemaProperties = { title: { type: "string" }, body: { type: "string" } };
      const stdinContent = "Shared stdin content.";

      const { args } = parseToolArgs(["--title", ".", "--body", "."], schemaProperties, stdinContent);

      expect(args).toEqual({ title: "Shared stdin content.", body: "Shared stdin content." });
    });
  });

  describe("per-field stdin mode with JSON stdin — field extraction", () => {
    it("extracts matching field from JSON stdin when --body . is used (space-separated)", () => {
      // Root cause of gh-aw-workshop#2118: agent piped JSON payload and used --body .
      // expecting the body field to be extracted, but the entire JSON string ended up as body.
      const schemaProperties = { title: { type: "string" }, body: { type: "string" } };
      const stdinContent = '{"title":"Fix bug","body":"This PR fixes the issue."}';

      const { args } = parseToolArgs(["--title", "Fix bug", "--body", "."], schemaProperties, stdinContent);

      expect(args).toEqual({ title: "Fix bug", body: "This PR fixes the issue." });
    });

    it("extracts matching field from JSON stdin when --body=. is used (equals-separated)", () => {
      const schemaProperties = { title: { type: "string" }, body: { type: "string" } };
      const stdinContent = '{"title":"Fix bug","body":"Details here."}';

      const { args } = parseToolArgs(["--title", "Fix bug", "--body=."], schemaProperties, stdinContent);

      expect(args).toEqual({ title: "Fix bug", body: "Details here." });
    });

    it("extracts both title and body when --title . --body . used with JSON stdin", () => {
      const schemaProperties = { title: { type: "string" }, body: { type: "string" } };
      const stdinContent = '{"title":"Fix: Bug #123","body":"This PR fixes bug #123."}';

      const { args } = parseToolArgs(["--title", ".", "--body", "."], schemaProperties, stdinContent);

      expect(args).toEqual({ title: "Fix: Bug #123", body: "This PR fixes bug #123." });
    });

    it("falls back to raw stdin when JSON does not contain the target key", () => {
      const schemaProperties = { body: { type: "string" } };
      const stdinContent = '{"other_field":"value"}';

      const { args } = parseToolArgs(["--body", "."], schemaProperties, stdinContent);

      // No 'body' key in JSON → use raw stdin content
      expect(args).toEqual({ body: stdinContent });
    });

    it("falls back to raw stdin when stdin is not a JSON object", () => {
      const schemaProperties = { body: { type: "string" } };
      const stdinContent = "This is a long body from stdin.";

      const { args } = parseToolArgs(["--body", "."], schemaProperties, stdinContent);

      expect(args).toEqual({ body: stdinContent });
    });

    it("falls back to raw stdin when stdin is a JSON array", () => {
      const schemaProperties = { body: { type: "string" } };
      const stdinContent = '["item1","item2"]';

      const { args } = parseToolArgs(["--body", "."], schemaProperties, stdinContent);

      expect(args).toEqual({ body: stdinContent });
    });

    it("falls back to raw stdin when stdin is invalid JSON", () => {
      const schemaProperties = { body: { type: "string" } };
      const stdinContent = '{"body": not-valid-json}';

      const { args } = parseToolArgs(["--body", "."], schemaProperties, stdinContent);

      expect(args).toEqual({ body: stdinContent });
    });

    it("resolves dash/underscore aliased JSON key to canonical schema key", () => {
      const schemaProperties = { issue_number: { type: "integer" } };
      const stdinContent = '{"issue-number":42}';

      const { args } = parseToolArgs(["--issue_number", "."], schemaProperties, stdinContent);

      expect(args).toEqual({ issue_number: 42 });
    });

    it("preserves non-string JSON values extracted from stdin (e.g. boolean, number)", () => {
      const schemaProperties = { draft: { type: "boolean" }, count: { type: "integer" } };
      const stdinContent = '{"draft":true,"count":5}';

      const { args: draftArgs } = parseToolArgs(["--draft", "."], schemaProperties, stdinContent);
      const { args: countArgs } = parseToolArgs(["--count", "."], schemaProperties, stdinContent);

      expect(draftArgs).toEqual({ draft: true });
      expect(countArgs).toEqual({ count: 5 });
    });

    it("handles multiline JSON payload with --body . correctly", () => {
      const schemaProperties = { title: { type: "string" }, body: { type: "string" } };
      const stdinContent = `{
  "title": "Fix bug",
  "body": "### Summary\\n\\nDetails here."
}`;

      const { args } = parseToolArgs(["--title", "Fix bug", "--body", "."], schemaProperties, stdinContent);

      expect(args).toEqual({ title: "Fix bug", body: "### Summary\n\nDetails here." });
    });
  });

  describe("tryExtractJsonFieldFromStdin", () => {
    it("extracts a field value from a JSON object string", () => {
      const schemaProperties = { body: { type: "string" } };
      const result = tryExtractJsonFieldFromStdin(
        '{"title":"Fix","body":"PR description"}',
        "body",
        schemaProperties,
        new Map([
          ["title", "title"],
          ["body", "body"],
        ]),
        new Set()
      );
      expect(result).toBe("PR description");
    });

    it("returns undefined when the key is absent from the JSON", () => {
      const schemaProperties = { body: { type: "string" } };
      const result = tryExtractJsonFieldFromStdin('{"title":"Fix"}', "body", schemaProperties, new Map([["title", "title"]]), new Set());
      expect(result).toBeUndefined();
    });

    it("returns undefined for non-object JSON (array)", () => {
      const result = tryExtractJsonFieldFromStdin('["a","b"]', "body", {}, new Map(), new Set());
      expect(result).toBeUndefined();
    });

    it("returns undefined for invalid JSON", () => {
      const result = tryExtractJsonFieldFromStdin("{not valid}", "body", {}, new Map(), new Set());
      expect(result).toBeUndefined();
    });

    it("returns undefined for plain text (not starting with {)", () => {
      const result = tryExtractJsonFieldFromStdin("plain text", "body", {}, new Map(), new Set());
      expect(result).toBeUndefined();
    });

    it("preserves null value extracted from JSON stdin (does not fall back to raw stdin)", () => {
      const schemaProperties = { body: { type: "string" } };
      const result = tryExtractJsonFieldFromStdin('{"body":null}', "body", schemaProperties, new Map([["body", "body"]]), new Set());
      expect(result).toBeNull();
    });

    it("preserves false boolean extracted from JSON stdin", () => {
      const schemaProperties = { draft: { type: "boolean" } };
      const result = tryExtractJsonFieldFromStdin('{"draft":false}', "draft", schemaProperties, new Map([["draft", "draft"]]), new Set());
      expect(result).toBe(false);
    });

    it("preserves 0 number extracted from JSON stdin", () => {
      const schemaProperties = { count: { type: "number" } };
      const result = tryExtractJsonFieldFromStdin('{"count":0}', "count", schemaProperties, new Map([["count", "count"]]), new Set());
      expect(result).toBe(0);
    });
  });

  describe("unescapeCliStringArg", () => {
    it("converts \\n to an actual newline", () => {
      expect(unescapeCliStringArg("Hello\\nWorld")).toBe("Hello\nWorld");
    });

    it("converts \\t to a tab character", () => {
      expect(unescapeCliStringArg("col1\\tcol2")).toBe("col1\tcol2");
    });

    it("converts \\r to a carriage return", () => {
      expect(unescapeCliStringArg("line1\\rline2")).toBe("line1\rline2");
    });

    it("converts \\b to a backspace character", () => {
      expect(unescapeCliStringArg("abc\\bdef")).toBe("abc\bdef");
    });

    it("converts \\f to a form-feed character", () => {
      expect(unescapeCliStringArg("page1\\fpage2")).toBe("page1\fpage2");
    });

    it("converts \\\\ to a single backslash", () => {
      expect(unescapeCliStringArg("path\\\\to\\\\file")).toBe("path\\to\\file");
    });

    it("converts \\uXXXX escapes to their Unicode code points", () => {
      expect(unescapeCliStringArg("quote:\\u2019")).toBe("quote:’");
    });

    it("converts \\\\n to a literal backslash followed by n (not a newline)", () => {
      // \\n in the CLI arg should become \n (backslash + n), not a newline
      expect(unescapeCliStringArg("Hello\\\\nWorld")).toBe("Hello\\nWorld");
    });

    it("leaves unknown escape sequences unchanged", () => {
      expect(unescapeCliStringArg("value\\xunknown")).toBe("value\\xunknown");
    });

    it("handles multiple escape sequences in the same string", () => {
      expect(unescapeCliStringArg("line1\\nline2\\nline3")).toBe("line1\nline2\nline3");
    });

    it("returns a plain string unchanged when no escape sequences are present", () => {
      expect(unescapeCliStringArg("no escapes here")).toBe("no escapes here");
    });
  });

  describe("parseToolArgs — body-like string escape unescaping", () => {
    it("unescapes \\n in body CLI flag arguments", () => {
      const schemaProperties = { body: { type: "string" } };
      const { args } = parseToolArgs(["--body", "Hello\\nWorld"], schemaProperties);
      expect(args).toEqual({ body: "Hello\nWorld" });
    });

    it("unescapes \\n in body --key=value arguments", () => {
      const schemaProperties = { body: { type: "string" } };
      const { args } = parseToolArgs(["--body=Hello\\nWorld"], schemaProperties);
      expect(args).toEqual({ body: "Hello\nWorld" });
    });

    it("unescapes \\n for nullable draft body fields", () => {
      const schemaProperties = { draft_body: { type: ["string", "null"] } };
      const { args } = parseToolArgs(["--draft-body", "line1\\nline2"], schemaProperties);
      expect(args).toEqual({ draft_body: "line1\nline2" });
    });

    it("does not unescape generic string fields like paths", () => {
      const schemaProperties = { path: { type: "string" } };
      const { args } = parseToolArgs(["--path", "C:\\temp\\new_file"], schemaProperties);
      expect(args).toEqual({ path: "C:\\temp\\new_file" });
    });

    it("does not unescape \\n when schema type is integer", () => {
      // A value with \n for an integer field should not be unescaped (it would fail coercion anyway)
      const schemaProperties = { count: { type: "integer" } };
      const { args } = parseToolArgs(["--count", "5\\n"], schemaProperties);
      // "5\n" is not a valid integer, falls through to rawValue
      expect(args).toEqual({ count: "5\\n" });
    });

    it("produces actual newlines in body fields matching JSON stdin mode behaviour", () => {
      // Verify that --body "title\n\nbody" (CLI flags) gives the same result as
      // JSON stdin with {"body":"title\n\nbody"}
      const schemaProperties = { body: { type: ["string", "null"] } };

      const { args: cliArgs } = parseToolArgs(["--body", "title\\n\\nbody"], schemaProperties);
      const { args: jsonArgs } = parseToolArgs(["."], schemaProperties, '{"body":"title\\n\\nbody"}');

      expect(cliArgs).toEqual(jsonArgs);
    });
  });

  describe("writeStdoutAndFlush", () => {
    it("resolves immediately when stdout.write returns true (no backpressure)", async () => {
      // The beforeEach mock captures chunks and returns true (no backpressure).
      // writeStdoutAndFlush should resolve synchronously in this case.
      await writeStdoutAndFlush("hello world\n");

      expect(stdoutChunks[0]).toBe("hello world\n");
    });

    it("waits for drain event when stdout.write returns false (pipe buffer full)", async () => {
      // Arrange: stdout.write returns false (simulates full pipe buffer like a ~64KiB payload)
      /** @type {any} */
      let drainCb = null;
      stdoutSpy.mockImplementation(chunk => {
        stdoutChunks.push(String(chunk));
        return false; // signal backpressure
      });
      const onceStub = vi.spyOn(process.stdout, "once").mockImplementation((event, cb) => {
        if (event === "drain") {
          drainCb = cb;
        }
        return process.stdout;
      });

      try {
        const writePromise = writeStdoutAndFlush("large payload\n");

        // Drain callback not yet called — promise should still be pending
        let resolved = false;
        writePromise.then(() => {
          resolved = true;
        });

        // Let microtasks run; drain hasn't fired yet so still pending
        await Promise.resolve();
        expect(resolved).toBe(false);
        expect(drainCb).not.toBeNull();

        // Fire the drain event
        drainCb();

        await writePromise;
        expect(resolved).toBe(true);
        expect(stdoutChunks).toContain("large payload\n");
      } finally {
        onceStub.mockRestore();
      }
    });

    it("rejects when stdout emits error while waiting for drain (EPIPE)", async () => {
      // Arrange: stdout.write returns false, then stdout emits an error
      stdoutSpy.mockImplementation(chunk => {
        stdoutChunks.push(String(chunk));
        return false; // signal backpressure
      });
      const error = new Error("EPIPE");
      /** @type {any} */
      let errorCb = null;
      const onceStub = vi.spyOn(process.stdout, "once").mockImplementation((event, cb) => {
        if (event === "error") {
          errorCb = cb;
        }
        return process.stdout;
      });

      try {
        const writePromise = writeStdoutAndFlush("data\n");
        // Verify the error callback was registered before firing it
        expect(errorCb).not.toBeNull();
        // Fire the error event asynchronously (simulates broken pipe)
        Promise.resolve().then(() => errorCb(error));
        await expect(writePromise).rejects.toThrow("EPIPE");
      } finally {
        onceStub.mockRestore();
      }
    });

    it("formatResponse awaits stdout drain before writing to stderr (no interleaving)", async () => {
      // This test verifies that core.info (→ stderr) is NOT called until after
      // stdout has been fully drained. Before the fix, process.stdout.write()
      // returning false would allow subsequent core.info calls to reach stderr
      // while stdout was still buffering, corrupting combined output.
      const callOrder = [];
      /** @type {any} */
      let drainCb = null;

      stdoutSpy.mockImplementation(chunk => {
        callOrder.push({ stream: "stdout", data: String(chunk) });
        return false; // simulate pipe buffer full
      });
      const onceStub = vi.spyOn(process.stdout, "once").mockImplementation((event, cb) => {
        if (event === "drain") {
          drainCb = cb;
        }
        return process.stdout;
      });
      global.core.info = vi.fn(msg => {
        callOrder.push({ stream: "stderr-info", data: String(msg) });
      });

      const body = {
        result: {
          content: [{ type: "text", text: "large json output" }],
        },
      };

      try {
        const formatPromise = formatResponse(body, "agenticworkflows");

        // Yield to microtasks — stdout write is queued, drain not yet fired
        await Promise.resolve();

        // core.info should NOT have been called yet (stdout hasn't drained)
        expect(callOrder.filter(e => e.stream === "stderr-info")).toHaveLength(0);

        // Now fire the drain event
        if (drainCb) drainCb();
        await formatPromise;

        // After drain: stdout write AND then core.info (order preserved)
        const stdoutIdx = callOrder.findIndex(e => e.stream === "stdout");
        const infoIdx = callOrder.findIndex(e => e.stream === "stderr-info");
        expect(stdoutIdx).toBeGreaterThanOrEqual(0);
        expect(infoIdx).toBeGreaterThan(stdoutIdx);
      } finally {
        onceStub.mockRestore();
      }
    });
  });

  describe("main — zero-argument tool routing via local MCP server", () => {
    /** @type {import("http").Server} */
    let server;
    /** @type {string} */
    let serverUrl;
    /** @type {object[]} */
    let recordedBodies;
    /** @type {string} */
    let toolsFile;
    /** @type {string[]} */
    let savedArgv;

    /** @type {Array<{name: string, description: string, inputSchema: object}>} */
    const zeroInputTools = [
      {
        name: "dispatch_code_factory",
        description: "Record a dispatch code factory safe-output item",
        inputSchema: {
          type: "object",
          properties: {},
          additionalProperties: false,
        },
      },
    ];

    /** @type {Array<{name: string, description: string, inputSchema: object}>} */
    const requiredInputTools = [
      {
        name: "create_issue",
        description: "Create an issue",
        inputSchema: {
          type: "object",
          properties: { title: { type: "string" } },
          required: ["title"],
          additionalProperties: false,
        },
      },
    ];

    /**
     * Start a minimal MCP HTTP server that records request bodies and returns
     * appropriate responses for each protocol step.
     *
     * @returns {Promise<void>}
     */
    async function startMcpServer() {
      recordedBodies = [];
      server = await new Promise(resolve => {
        const s = http.createServer((req, res) => {
          let body = "";
          req.on("data", chunk => {
            body += chunk;
          });
          req.on("end", () => {
            let parsed;
            try {
              parsed = JSON.parse(body);
            } catch {
              parsed = {};
            }
            recordedBodies.push(parsed);

            res.setHeader("Content-Type", "application/json");

            if (parsed.method === "initialize") {
              res.setHeader("mcp-session-id", "test-session-001");
              res.end(JSON.stringify({ jsonrpc: "2.0", id: parsed.id, result: { capabilities: {} } }));
            } else if (parsed.method === "tools/call") {
              res.end(
                JSON.stringify({
                  jsonrpc: "2.0",
                  id: parsed.id,
                  result: { content: [{ type: "text", text: "ok" }] },
                })
              );
            } else {
              // notifications/initialized, ping, etc.
              res.end(JSON.stringify({ jsonrpc: "2.0", result: {} }));
            }
          });
        });
        s.listen(0, "127.0.0.1", () => resolve(s));
      });

      const addr = /** @type {import("net").AddressInfo} */ server.address();
      serverUrl = `http://127.0.0.1:${addr.port}`;
    }

    beforeEach(async () => {
      savedArgv = process.argv;
      await startMcpServer();
    });

    afterEach(async () => {
      process.argv = savedArgv;
      if (toolsFile && fs.existsSync(toolsFile)) {
        fs.unlinkSync(toolsFile);
      }
      await new Promise(resolve => server.close(resolve));
    });

    /**
     * Write a tools file and configure process.argv for a main() call.
     *
     * @param {object[]} tools
     * @param {string[]} userArgs
     */
    function setupMainCall(tools, userArgs) {
      toolsFile = path.join(os.tmpdir(), `test-bridge-tools-${Date.now()}-${Math.random().toString(36).slice(2)}.json`);
      fs.writeFileSync(toolsFile, JSON.stringify(tools));
      process.argv = ["node", "mcp_cli_bridge.cjs", "--server-name", "safeoutputs", "--server-url", serverUrl, "--tools-file", toolsFile, "--api-key", "test-key", ...userArgs];
    }

    it("reaches MCP tools/call with {} for a zero-input tool (bare invocation)", async () => {
      setupMainCall(zeroInputTools, ["dispatch_code_factory"]);

      await main();

      const toolsCallBody = recordedBodies.find(b => b.method === "tools/call");
      expect(toolsCallBody).toBeDefined();
      expect(toolsCallBody.params.name).toBe("dispatch_code_factory");
      expect(toolsCallBody.params.arguments).toEqual({});
    });

    it("does not include tool argument values in live logs", async () => {
      const sentinel = "sentinel-tool-argument";
      setupMainCall(requiredInputTools, ["create_issue", "--title", sentinel]);

      await main();

      const toolsCallBody = recordedBodies.find(b => b.method === "tools/call");
      expect(toolsCallBody.params.arguments).toEqual({ title: sentinel });
      expect(JSON.stringify(global.core.info.mock.calls)).not.toContain(sentinel);
    });

    it("reaches MCP tools/call with {} for a zero-input tool (piped {} via . sentinel)", async () => {
      setupMainCall(zeroInputTools, ["dispatch_code_factory", "."]);

      // Simulate piped `{}` via the . sentinel with stdinContent = "{}"
      const readStdinSyncSpy = vi.spyOn(/** @type {any} */ require("./mcp_cli_bridge.cjs"), "readStdinSync");
      // readStdinSync is a module-level function already called inside main(); we need
      // to intercept at the module level. Since we cannot easily do that here, simulate
      // the piped-stdin path by using process.stdin.isTTY = undefined so hasStdinJsonPayload
      // returns true for the empty-args path, and by spying on fs.readSync to return "{}".
      const origIsTTY = process.stdin.isTTY;
      // @ts-ignore
      process.stdin.isTTY = undefined;

      const fsReadSyncSpy = vi.spyOn(fs, "readSync").mockImplementationOnce((_fd, buf, _offset, length) => {
        const encoded = Buffer.from("{}");
        encoded.copy(/** @type {Buffer} */ buf, 0, 0, Math.min(encoded.length, length));
        return Math.min(encoded.length, length);
      });
      fsReadSyncSpy.mockImplementationOnce(() => 0); // EOF on second read

      try {
        setupMainCall(zeroInputTools, ["dispatch_code_factory"]);
        await main();

        const toolsCallBody = recordedBodies.find(b => b.method === "tools/call");
        expect(toolsCallBody).toBeDefined();
        expect(toolsCallBody.params.name).toBe("dispatch_code_factory");
        expect(toolsCallBody.params.arguments).toEqual({});
      } finally {
        process.stdin.isTTY = origIsTTY;
        fsReadSyncSpy.mockRestore();
        readStdinSyncSpy.mockRestore?.();
      }
    });

    it("shows help and does NOT reach MCP tools/call for a required-input tool called with no args", async () => {
      setupMainCall(requiredInputTools, ["create_issue"]);

      await main();

      const toolsCallBody = recordedBodies.find(b => b.method === "tools/call");
      expect(toolsCallBody).toBeUndefined();

      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("No arguments provided for 'create_issue'"));
    });

    it("fails with JSON parse diagnostics instead of help when '.' payload is invalid JSON", async () => {
      setupMainCall(requiredInputTools, ["create_issue", "."]);

      const fsReadSyncSpy = vi.spyOn(fs, "readSync").mockImplementationOnce((_fd, buf, _offset, length) => {
        const encoded = Buffer.from('{"title":"broken "json"}');
        encoded.copy(/** @type {Buffer} */ buf, 0, 0, Math.min(encoded.length, length));
        return Math.min(encoded.length, length);
      });
      fsReadSyncSpy.mockImplementationOnce(() => 0); // EOF

      try {
        await expect(main()).resolves.toBeUndefined();
      } finally {
        fsReadSyncSpy.mockRestore();
      }

      const toolsCallBody = recordedBodies.find(b => b.method === "tools/call");
      expect(toolsCallBody).toBeUndefined();
      expect(global.core.warning).not.toHaveBeenCalledWith(expect.stringContaining("No arguments provided for 'create_issue'"));
      expect(stderrChunks.join("")).toContain("stdin is not valid JSON");
      expect(global.core.setFailed).toHaveBeenCalledWith(expect.stringContaining("Argument parsing failed"));
    });

    it("fails with JSON parse diagnostics instead of help when '.' payload is whitespace-only", async () => {
      setupMainCall(requiredInputTools, ["create_issue", "."]);

      const fsReadSyncSpy = vi.spyOn(fs, "readSync").mockImplementationOnce((_fd, buf, _offset, length) => {
        const encoded = Buffer.from("   \n   ");
        encoded.copy(/** @type {Buffer} */ buf, 0, 0, Math.min(encoded.length, length));
        return Math.min(encoded.length, length);
      });
      fsReadSyncSpy.mockImplementationOnce(() => 0); // EOF

      try {
        await expect(main()).resolves.toBeUndefined();
      } finally {
        fsReadSyncSpy.mockRestore();
      }

      const toolsCallBody = recordedBodies.find(b => b.method === "tools/call");
      expect(toolsCallBody).toBeUndefined();
      expect(global.core.warning).not.toHaveBeenCalledWith(expect.stringContaining("No arguments provided for 'create_issue'"));
      expect(stderrChunks.join("")).toContain("stdin is not valid JSON");
      expect(global.core.setFailed).toHaveBeenCalledWith(expect.stringContaining("Argument parsing failed"));
    });

    it("fails with JSON parse diagnostics for no-flag whitespace-only piped stdin", async () => {
      const origIsTTY = process.stdin.isTTY;
      // @ts-ignore
      process.stdin.isTTY = undefined;
      setupMainCall(requiredInputTools, ["create_issue"]);

      const fsReadSyncSpy = vi.spyOn(fs, "readSync").mockImplementationOnce((_fd, buf, _offset, length) => {
        const encoded = Buffer.from("   \n   ");
        encoded.copy(/** @type {Buffer} */ buf, 0, 0, Math.min(encoded.length, length));
        return Math.min(encoded.length, length);
      });
      fsReadSyncSpy.mockImplementationOnce(() => 0); // EOF

      try {
        await expect(main()).resolves.toBeUndefined();
      } finally {
        process.stdin.isTTY = origIsTTY;
        fsReadSyncSpy.mockRestore();
      }

      const toolsCallBody = recordedBodies.find(b => b.method === "tools/call");
      expect(toolsCallBody).toBeUndefined();
      expect(global.core.warning).not.toHaveBeenCalledWith(expect.stringContaining("No arguments provided for 'create_issue'"));
      expect(stderrChunks.join("")).toContain("stdin is not valid JSON");
      expect(global.core.setFailed).toHaveBeenCalledWith(expect.stringContaining("Argument parsing failed"));
    });

    it("still shows help for no-flag piped stdin when stdin is truly empty", async () => {
      const origIsTTY = process.stdin.isTTY;
      // @ts-ignore
      process.stdin.isTTY = undefined;
      setupMainCall(requiredInputTools, ["create_issue"]);

      const fsReadSyncSpy = vi.spyOn(fs, "readSync").mockImplementationOnce(() => 0); // EOF immediately

      try {
        await main();
      } finally {
        process.stdin.isTTY = origIsTTY;
        fsReadSyncSpy.mockRestore();
      }

      const toolsCallBody = recordedBodies.find(b => b.method === "tools/call");
      expect(toolsCallBody).toBeUndefined();
      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("No arguments provided for 'create_issue'"));
    });
  });
});
