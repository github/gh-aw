import { describe, expect, it } from "vitest";
import { mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const { createLedgerServer, resolveSchemaPath } = require("./ledger_mcp_server.cjs");
const __dirname = path.dirname(fileURLToPath(import.meta.url));

function withMemory(test) {
  const memoryDir = mkdtempSync(path.join(__dirname, ".ledger-mcp-test-"));
  const priorRunId = process.env.GITHUB_RUN_ID;
  const priorTransactionLog = process.env.GH_AW_LEDGER_TRANSACTION_LOG;
  process.env.GITHUB_RUN_ID = "991";
  process.env.GH_AW_LEDGER_TRANSACTION_LOG = path.join(memoryDir, "safe-output-items.jsonl");
  try {
    test(memoryDir);
  } finally {
    if (priorRunId === undefined) delete process.env.GITHUB_RUN_ID;
    else process.env.GITHUB_RUN_ID = priorRunId;
    if (priorTransactionLog === undefined) delete process.env.GH_AW_LEDGER_TRANSACTION_LOG;
    else process.env.GH_AW_LEDGER_TRANSACTION_LOG = priorTransactionLog;
    rmSync(memoryDir, { recursive: true, force: true });
  }
}

function invoke(server, name, args = {}) {
  return server.tools[name].handler(args);
}

function output(response) {
  return JSON.parse(response.content[0].text);
}

describe("ledger MCP server", () => {
  it("registers exactly the four ledger tools and supports append/get/query/status", () => {
    withMemory(memoryDir => {
      const server = createLedgerServer({ memoryDir });
      expect(Object.keys(server.tools).sort()).toEqual(["ledger_append", "ledger_get", "ledger_query", "ledger_status"]);
      const append = output(invoke(server, "ledger_append", { type: "build", payload: { ok: true, score: 5, label: "alpha" } }));
      expect(append.id).toEqual(expect.any(String));
      expect(JSON.parse(readFileSync(path.join(memoryDir, "safe-output-items.jsonl"), "utf8"))).toMatchObject({
        type: "ledger_mutation",
        operation: "append",
        record: { id: append.id, sha: append.sha },
      });
      expect(output(invoke(server, "ledger_get", { id: append.id }))).toEqual(append);
      expect(output(invoke(server, "ledger_get", { sha: append.sha }))).toEqual(append);
      expect(invoke(server, "ledger_get", {}).isError).toBe(true);
      expect(invoke(server, "ledger_get", { id: append.id, sha: append.sha }).isError).toBe(true);
      expect(output(invoke(server, "ledger_get", { id: "ldg-00000000-0000-4000-8000-000000000000" }))).toBeNull();
      expect(JSON.stringify(output(invoke(server, "ledger_query", { type: "build" })))).toContain("build");
      expect(output(invoke(server, "ledger_query", { type: "build", where: { "payload.ok": { eq: true } } })).rows.map(record => record.id)).toContain(append.id);
      expect(output(invoke(server, "ledger_query", { type: "build", where: { "payload.ok": { eq: false } } })).rows).toEqual([]);
      for (const [field, predicate] of [
        ["payload.ok", { in: [false, true] }],
        ["payload.ok", { exists: true }],
        ["payload.label", { prefix: "alp" }],
        ["payload.score", { gt: 4 }],
        ["payload.score", { gte: 5 }],
        ["payload.score", { lt: 6 }],
        ["payload.score", { lte: 5 }],
      ]) {
        expect(output(invoke(server, "ledger_query", { type: "build", where: { [field]: predicate } })).rows.map(record => record.id)).toContain(append.id);
      }
      expect(output(invoke(server, "ledger_status"))).toMatchObject({
        records: expect.any(Number),
        validRecords: expect.any(Number),
        invalidRecords: expect.any(Number),
        incompleteRecords: expect.any(Number),
        heads: expect.any(Array),
        shards: expect.any(Number),
        diagnostics: expect.any(Array),
      });
    });
  });

  it("does not expose filesystem details in errors or permit caller run provenance", () => {
    withMemory(memoryDir => {
      const server = createLedgerServer({ memoryDir });
      const invalid = invoke(server, "ledger_append", { type: "", payload: true });
      expect(invalid.isError).toBe(true);
      expect(invalid.content[0].text).not.toContain(memoryDir);
      const oversized = invoke(server, "ledger_append", { type: "build", payload: { data: "x".repeat(40 * 1024) } });
      expect(oversized.isError).toBe(true);
      expect(oversized.content[0].text).toContain("patch-size limit");
      expect(oversized.content[0].text).not.toContain("xxx");
      const missing = invoke(server, "ledger_append", { type: "build", payload: true, runId: "untrusted" });
      expect(JSON.stringify(output(missing))).not.toContain("untrusted");
      expect(invoke(server, "ledger_query", { limit: -1 }).isError).toBe(true);
      const blockedDir = path.join(memoryDir, "blocked");
      writeFileSync(blockedDir, "not a directory");
      const blocked = createLedgerServer({ memoryDir: blockedDir });
      const failed = invoke(blocked, "ledger_append", { type: "build", payload: true });
      expect(failed.isError).toBe(true);
      expect(failed.content[0].text).not.toContain(blockedDir);
    });
  });

  it("only accepts a schema located within the repository workspace", () => {
    withMemory(workspace => {
      const schema = path.join(workspace, "schema.json");
      writeFileSync(schema, '{"type":"object"}');
      expect(resolveSchemaPath("schema.json", workspace)).toBe(schema);
      expect(() => resolveSchemaPath(schema, workspace)).toThrow();
      expect(() => resolveSchemaPath("../schema.json", workspace)).toThrow();
      expect(() => resolveSchemaPath("sub/../schema.json", workspace)).toThrow();
      expect(() => resolveSchemaPath(path.dirname(workspace), workspace)).toThrow();
      withMemory(outside => {
        symlinkSync(path.join(outside, "schema.json"), path.join(workspace, "escaped.json"));
        writeFileSync(path.join(outside, "schema.json"), "{}");
        expect(() => resolveSchemaPath("escaped.json", workspace)).toThrow();
      });
      symlinkSync(schema, path.join(workspace, "linked.json"));
      expect(() => resolveSchemaPath("linked.json", workspace)).toThrow();
      withMemory(outside => {
        const linkedRoot = path.join(outside, "linked-root");
        symlinkSync(workspace, linkedRoot);
        expect(() => resolveSchemaPath("schema.json", linkedRoot)).toThrow();
      });
    });
  });

  it("rejects events not matching the configured application schema", () => {
    withMemory(workspace => {
      withMemory(schemaRoot => {
        writeFileSync(path.join(schemaRoot, "schema.json"), JSON.stringify({ type: "object", properties: { approved: { type: "boolean" } }, required: ["approved"], additionalProperties: false }));
        const server = createLedgerServer({ memoryDir: workspace, workspace, schemaRoot, schemaPath: "schema.json" });
        expect(invoke(server, "ledger_append", { type: "build", payload: { approved: "no" } }).isError).toBe(true);
        expect(output(invoke(server, "ledger_append", { type: "build", payload: { approved: true } })).id).toEqual(expect.any(String));
      });
    });
  });

  it("passes the configured shard limit to the ledger", () => {
    withMemory(memoryDir => {
      const prior = process.env.GH_AW_LEDGER_MAX_SHARDS;
      process.env.GH_AW_LEDGER_MAX_SHARDS = "1";
      try {
        const shardDir = path.join(memoryDir, "ledger", "shards");
        require("node:fs").mkdirSync(shardDir, { recursive: true });
        require("node:fs").writeFileSync(path.join(shardDir, "00000000-0000-4000-8000-000000000000.jsonl"), "");
        const server = createLedgerServer({ memoryDir });
        const result = invoke(server, "ledger_append", { type: "build", payload: true });
        expect(result.isError).toBe(true);
        expect(result.content[0].text).toContain("file-count limit");
      } finally {
        if (prior === undefined) delete process.env.GH_AW_LEDGER_MAX_SHARDS;
        else process.env.GH_AW_LEDGER_MAX_SHARDS = prior;
      }
    });
  });

  it("converts configured ledger size limits from KiB to bytes", () => {
    withMemory(memoryDir => {
      const names = ["GH_AW_LEDGER_MAX_SEGMENT_KB", "GH_AW_LEDGER_MAX_RECORD_KB", "GH_AW_LEDGER_MAX_PATCH_KB"];
      const previous = names.map(name => process.env[name]);
      process.env.GH_AW_LEDGER_MAX_SEGMENT_KB = "2";
      process.env.GH_AW_LEDGER_MAX_RECORD_KB = "1";
      process.env.GH_AW_LEDGER_MAX_PATCH_KB = "2";
      try {
        const server = createLedgerServer({ memoryDir });
        const response = invoke(server, "ledger_append", { type: "build", payload: { data: "x".repeat(1500) } });
        expect(response.isError).toBe(true);
        expect(response.content[0].text).toContain("patch-size limit");
        expect(require("node:fs").existsSync(path.join(memoryDir, "ledger", "shards"))).toBe(false);

        process.env.GH_AW_LEDGER_MAX_RECORD_KB = "2";
        process.env.GH_AW_LEDGER_MAX_PATCH_KB = "1";
        const patchBounded = createLedgerServer({ memoryDir });
        const patchResponse = invoke(patchBounded, "ledger_append", { type: "build", payload: { data: "x".repeat(1500) } });
        expect(patchResponse.isError).toBe(true);
        expect(patchResponse.content[0].text).toContain("patch-size limit");
        expect(require("node:fs").existsSync(path.join(memoryDir, "ledger", "shards"))).toBe(false);
      } finally {
        names.forEach((name, index) => {
          if (previous[index] === undefined) delete process.env[name];
          else process.env[name] = previous[index];
        });
      }
    });
  });

  it("is copied into setup actions directory alongside its local dependencies", () => {
    const setup = readFileSync(path.join(__dirname, "../setup.sh"), "utf8");
    expect(setup).toContain('for file in "${JS_SOURCE_DIR}"/*.cjs; do');
    expect(setup).toContain('cp "$file" "${DESTINATION}/${filename}"');
  });

  it("starts over stdio and announces all four tools", () => {
    withMemory(memoryDir => {
      const schemaPath = path.join(memoryDir, "schema.json");
      writeFileSync(schemaPath, JSON.stringify({ type: "object", properties: { ok: { type: "boolean" } }, required: ["ok"] }));
      const request = [
        { jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2025-03-26", capabilities: {}, clientInfo: { name: "test", version: "1" } } },
        { jsonrpc: "2.0", id: 2, method: "tools/list" },
        { jsonrpc: "2.0", id: 3, method: "tools/call", params: { name: "ledger_status", arguments: {} } },
        { jsonrpc: "2.0", id: 4, method: "tools/call", params: { name: "ledger_append", arguments: { type: "smoke", payload: { ok: true } } } },
        { jsonrpc: "2.0", id: 5, method: "tools/call", params: { name: "ledger_query", arguments: { type: "smoke" } } },
        { jsonrpc: "2.0", id: 6, method: "tools/call", params: { name: "ledger_append", arguments: { type: "smoke", payload: { ok: "no" } } } },
        { jsonrpc: "2.0", id: 7, method: "tools/call", params: { name: "ledger_get", arguments: { sha: `sha256:${"0".repeat(64)}` } } },
      ];
      const child = spawnSync(process.execPath, [path.join(__dirname, "ledger_mcp_server.cjs")], {
        input: request.map(item => JSON.stringify(item)).join("\n") + "\n",
        encoding: "utf8",
        env: { ...process.env, GH_AW_MEMORY_DIR: memoryDir, GH_AW_LEDGER_SCHEMA: "schema.json", GH_AW_LEDGER_SCHEMA_ROOT: memoryDir, GITHUB_WORKSPACE: memoryDir, GITHUB_RUN_ID: "991" },
        timeout: 5000,
      });
      expect(child.status).toBe(0);
      const responses = child.stdout.trim().split("\n").map(JSON.parse);
      expect(responses.map(value => value.id)).toEqual([1, 2, 3, 4, 5, 6, 7]);
      expect(responses[1].result.tools.map(tool => tool.name).sort()).toEqual(["ledger_append", "ledger_get", "ledger_query", "ledger_status"]);
      expect(JSON.parse(responses[2].result.content[0].text)).toEqual(expect.any(Object));
      expect(JSON.parse(responses[3].result.content[0].text).id).toEqual(expect.any(String));
      expect(responses[4].result.content[0].text).toContain("smoke");
      expect(responses[5].result.isError).toBe(true);
      expect(responses[5].result.content[0].text).not.toContain(memoryDir);
      expect(JSON.parse(responses[6].result.content[0].text)).toBeNull();
    });
  });
});
