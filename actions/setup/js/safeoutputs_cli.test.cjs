import { describe, it, expect } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";
import { compileFunction } from "node:vm";

const require = createRequire(import.meta.url);
const { runSafeOutputsCLI, buildMissingToolAlternatives, emitMissingToolPermissionIssue, emitInfrastructureIncomplete, hasExpectedSafeOutputs, hasTerminalSafeOutput, hasNoopInSafeOutputs } = require("./safeoutputs_cli.cjs");

describe("safeoutputs_cli.cjs", () => {
  it("executes the CommonJS module in strict mode", () => {
    const source = fs.readFileSync(require.resolve("./safeoutputs_cli.cjs"), "utf8");
    const execute = compileFunction(`${source}\nreturn (function () { return this; })();`, ["require", "module", "exports"]);
    const module = { exports: {} };
    expect(execute(require, module, module.exports)).toBeUndefined();
  });

  describe("runSafeOutputsCLI", () => {
    it("throws when an argument key contains invalid characters", () => {
      process.env.GH_AW_SAFEOUTPUTS_CLI = "true"; // 'true' exits 0, accepts any args
      expect(() => runSafeOutputsCLI("report_missing_tool", { "bad key!": "value" })).toThrow("invalid safeoutputs argument key");
      delete process.env.GH_AW_SAFEOUTPUTS_CLI;
    });

    it("skips empty string values", () => {
      // 'true' succeeds without checking args, so empty-value keys must be skipped silently
      process.env.GH_AW_SAFEOUTPUTS_CLI = "true";
      expect(() => runSafeOutputsCLI("report_missing_tool", { tool: "", reason: "test" })).not.toThrow();
      delete process.env.GH_AW_SAFEOUTPUTS_CLI;
    });

    it("wraps CLI errors with tool name and key summary", () => {
      process.env.GH_AW_SAFEOUTPUTS_CLI = "false"; // 'false' exits 1
      expect(() => runSafeOutputsCLI("report_missing_tool", { tool: "tool/permission", reason: "test" })).toThrow("safeoutputs report_missing_tool(tool, reason) failed");
      delete process.env.GH_AW_SAFEOUTPUTS_CLI;
    });
  });

  describe("buildMissingToolAlternatives", () => {
    const BASE = "Verify token scopes, repository permissions, and MCP/tool access configuration.";

    it("returns base alternatives unchanged when denied command list is empty", () => {
      expect(buildMissingToolAlternatives(BASE, [])).toBe(BASE);
    });

    it("returns base alternatives unchanged when denied commands is null", () => {
      expect(buildMissingToolAlternatives(BASE, null)).toBe(BASE);
    });

    it("appends denied commands when list is non-empty", () => {
      const result = buildMissingToolAlternatives(BASE, ["go version"]);
      expect(result).toContain("Denied commands: go version");
    });

    it("caps result to 512 characters", () => {
      const base = "base";
      const deniedCommands = Array.from({ length: 30 }, (_, i) => `command-${i}-${"x".repeat(30)}`);
      const result = buildMissingToolAlternatives(base, deniedCommands);
      expect(result.length).toBeLessThanOrEqual(512);
    });

    it("includes overflow marker when commands are truncated", () => {
      const base = "base";
      const deniedCommands = Array.from({ length: 30 }, (_, i) => `command-${i}-${"x".repeat(30)}`);
      const result = buildMissingToolAlternatives(base, deniedCommands);
      expect(result).toContain("... and");
      expect(result).toContain("more");
    });

    it("truncates a base alternatives string that already exceeds 512 chars", () => {
      const longBase = "x".repeat(600);
      const result = buildMissingToolAlternatives(longBase, ["go version"]);
      expect(result.length).toBe(512);
    });
  });

  describe("emitMissingToolPermissionIssue", () => {
    it("invokes safeoutputs CLI with correct tool and reason", () => {
      const calls = [];
      const logs = [];
      emitMissingToolPermissionIssue({
        safeOutputsPath: "/tmp/safeoutputs.jsonl",
        deniedCommands: ["go version"],
        runSafeOutputsCLI: (toolName, args) => calls.push({ toolName, args }),
        logger: message => logs.push(message),
      });
      expect(calls).toHaveLength(1);
      expect(calls[0].toolName).toBe("report_missing_tool");
      expect(calls[0].args.tool).toBe("tool/permission");
      expect(calls[0].args.reason).toContain("missing tool/permission issue");
      expect(calls[0].args.alternatives).toContain("Denied commands: go version");
      expect(logs.some(m => m.includes("report_missing_tool emitted"))).toBe(true);
    });

    it("skips emission when safeOutputsPath is empty", () => {
      const calls = [];
      const logs = [];
      emitMissingToolPermissionIssue({
        safeOutputsPath: "",
        runSafeOutputsCLI: () => calls.push("call"),
        logger: message => logs.push(message),
      });
      expect(calls).toHaveLength(0);
      expect(logs.some(m => m.includes("skipped"))).toBe(true);
    });

    it("preserves workflow allowlist guidance through the actual CLI emission path", () => {
      const calls = [];
      emitMissingToolPermissionIssue({
        safeOutputsPath: "/tmp/safeoutputs.jsonl",
        deniedCommands: ["shell(node --test scripts/pr-sous-chef.test.mjs)"],
        permissionOutput: "[sdk-driver] permission denied by workflow tool permissions: shell(node --test scripts/pr-sous-chef.test.mjs)",
        runSafeOutputsCLI: (toolName, args) => calls.push({ toolName, args }),
        logger: () => {},
      });
      expect(calls).toHaveLength(1);
      expect(calls[0].args.alternatives).toContain("workflow tools configuration");
      expect(calls[0].args.alternatives).toContain("Denied commands: shell(node --test scripts/pr-sous-chef.test.mjs)");
      expect(calls[0].args.alternatives).not.toContain("Verify token scopes");
      expect(calls[0].args.alternatives.length).toBeLessThanOrEqual(512);
    });

    it("logs error when CLI invocation fails", () => {
      const logs = [];
      emitMissingToolPermissionIssue({
        safeOutputsPath: "/tmp/safeoutputs.jsonl",
        runSafeOutputsCLI: () => {
          throw new Error("EROFS: read-only file system");
        },
        logger: message => logs.push(message),
      });
      expect(logs.some(m => m.includes("report_missing_tool emission failed"))).toBe(true);
      expect(logs.some(m => m.includes("EROFS"))).toBe(true);
    });
  });

  describe("emitInfrastructureIncomplete", () => {
    it("invokes safeoutputs CLI with reason and details", () => {
      const calls = [];
      const logs = [];
      emitInfrastructureIncomplete("temporary outage", {
        safeOutputsPath: "/tmp/safeoutputs.jsonl",
        runSafeOutputsCLI: (toolName, args) => calls.push({ toolName, args }),
        logger: message => logs.push(message),
      });
      expect(calls).toEqual([
        {
          toolName: "report_incomplete",
          args: { reason: "infrastructure_error", details: "temporary outage" },
        },
      ]);
      expect(logs.some(m => m.includes("report_incomplete emitted"))).toBe(true);
    });

    it("skips emission when safeOutputsPath is empty", () => {
      const calls = [];
      const logs = [];
      emitInfrastructureIncomplete("temporary outage", {
        safeOutputsPath: "",
        runSafeOutputsCLI: () => calls.push("call"),
        logger: message => logs.push(message),
      });
      expect(calls).toHaveLength(0);
      expect(logs.some(m => m.includes("skipped"))).toBe(true);
    });

    it("logs error when CLI invocation fails", () => {
      const logs = [];
      emitInfrastructureIncomplete("temporary outage", {
        safeOutputsPath: "/tmp/safeoutputs.jsonl",
        runSafeOutputsCLI: () => {
          throw new Error("EROFS: read-only file system");
        },
        logger: message => logs.push(message),
      });
      expect(logs.some(m => m.includes("report_incomplete emission failed"))).toBe(true);
      expect(logs.some(m => m.includes("EROFS"))).toBe(true);
    });
  });

  describe("hasExpectedSafeOutputs", () => {
    function makeTempFile(content) {
      const p = path.join(os.tmpdir(), `safeoutputs-expected-test-${Date.now()}-${Math.random().toString(36).slice(2)}.jsonl`);
      fs.writeFileSync(p, content, "utf8");
      return p;
    }

    it("returns true when the file contains a non-diagnostic entry", () => {
      const filePath = makeTempFile('{"type":"add_comment","body":"done"}\n');
      try {
        expect(hasExpectedSafeOutputs(filePath)).toBe(true);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns true for a submit_pull_request_review entry", () => {
      const filePath = makeTempFile('{"type":"submit_pull_request_review","event":"APPROVE"}\n');
      try {
        expect(hasExpectedSafeOutputs(filePath)).toBe(true);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns false when all entries are diagnostic types", () => {
      const filePath = makeTempFile('{"type":"noop","message":"nothing to do"}\n{"type":"report_missing_tool","tool":"x"}\n{"type":"report_incomplete","reason":"infra"}\n');
      try {
        expect(hasExpectedSafeOutputs(filePath)).toBe(false);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns false when the file contains only a noop entry", () => {
      const filePath = makeTempFile('{"type":"noop","message":"nothing to do"}\n');
      try {
        expect(hasExpectedSafeOutputs(filePath)).toBe(false);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns false when the file contains only a report_missing_tool entry", () => {
      const filePath = makeTempFile('{"type":"report_missing_tool","tool":"tool/permission"}\n');
      try {
        expect(hasExpectedSafeOutputs(filePath)).toBe(false);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns false when the file contains only a report_incomplete entry", () => {
      const filePath = makeTempFile('{"type":"report_incomplete","reason":"infrastructure_error"}\n');
      try {
        expect(hasExpectedSafeOutputs(filePath)).toBe(false);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns true when an expected entry is mixed with diagnostic entries", () => {
      const filePath = makeTempFile('{"type":"report_missing_tool","tool":"x"}\n{"type":"add_comment","body":"done"}\n');
      try {
        expect(hasExpectedSafeOutputs(filePath)).toBe(true);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns false when the file does not exist", () => {
      expect(hasExpectedSafeOutputs("/tmp/nonexistent-safe-outputs-expected.jsonl")).toBe(false);
    });

    it("returns false when safeOutputsPath is empty", () => {
      expect(hasExpectedSafeOutputs("")).toBe(false);
    });

    it("returns false for an empty file", () => {
      const filePath = makeTempFile("");
      try {
        expect(hasExpectedSafeOutputs(filePath)).toBe(false);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("skips malformed lines and returns false when no valid expected entry exists", () => {
      const filePath = makeTempFile('not-json\n{"type":"noop"}\n');
      try {
        expect(hasExpectedSafeOutputs(filePath)).toBe(false);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("skips malformed lines and returns true when a valid non-diagnostic entry follows them", () => {
      const filePath = makeTempFile('not-json\n{"type":"add_comment","body":"done"}\n');
      try {
        expect(hasExpectedSafeOutputs(filePath)).toBe(true);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("uses injected readFileSync for testability", () => {
      const logs = [];
      const result = hasExpectedSafeOutputs("/fake/path.jsonl", {
        readFileSync: () => '{"type":"update_pull_request","title":"fix"}\n',
        logger: m => logs.push(m),
      });
      expect(result).toBe(true);
      expect(logs.some(m => m.includes("non-diagnostic entry found"))).toBe(true);
    });

    it("returns false when injected readFileSync returns only diagnostic entries", () => {
      const result = hasExpectedSafeOutputs("/fake/path.jsonl", {
        readFileSync: () => '{"type":"noop","message":"nothing"}\n',
      });
      expect(result).toBe(false);
    });

    it("returns false when injected readFileSync throws", () => {
      const result = hasExpectedSafeOutputs("/fake/path.jsonl", {
        readFileSync: () => {
          throw new Error("ENOENT");
        },
      });
      expect(result).toBe(false);
    });
  });

  describe("hasTerminalSafeOutput", () => {
    it.each(["create_pull_request_review_comment", "create-pull-request-review-comment"])("excludes %s only when completed-review semantics are requested", type => {
      const options = { readFileSync: () => JSON.stringify({ type, body: "Partial finding" }) + "\n" };
      expect(hasTerminalSafeOutput("/fake/path.jsonl", options)).toBe(true);
      expect(hasTerminalSafeOutput("/fake/path.jsonl", { ...options, excludePartialReviewComments: true })).toBe(false);
    });

    it.each(["submit_pull_request_review", "add_comment"])("accepts %s after partial findings with completed-review semantics", type => {
      const options = {
        excludePartialReviewComments: true,
        readFileSync: () => [JSON.stringify({ type: "create_pull_request_review_comment", body: "Partial finding" }), JSON.stringify({ type, body: "Complete" })].join("\n") + "\n",
      };
      expect(hasTerminalSafeOutput("/fake/path.jsonl", options)).toBe(true);
    });

    function makeTempFile(content) {
      const p = path.join(os.tmpdir(), `safeoutputs-terminal-test-${Date.now()}-${Math.random().toString(36).slice(2)}.jsonl`);
      fs.writeFileSync(p, content, "utf8");
      return p;
    }

    it("returns true for noop and non-diagnostic entries", () => {
      const filePath = makeTempFile('{"type":"report_missing_tool","tool":"x"}\n{"type":"noop","message":"done"}\n');
      try {
        expect(hasTerminalSafeOutput(filePath)).toBe(true);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns false for report_incomplete unless explicitly included", () => {
      const filePath = makeTempFile('{"type":"report_incomplete","reason":"blocked"}\n');
      try {
        expect(hasTerminalSafeOutput(filePath)).toBe(false);
        expect(hasTerminalSafeOutput(filePath, { includeReportIncomplete: true })).toBe(true);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns false for report_missing_data unless explicitly included", () => {
      const filePath = makeTempFile('{"type":"report_missing_data","reason":"metadata unavailable"}\n');
      try {
        expect(hasTerminalSafeOutput(filePath)).toBe(false);
        expect(hasTerminalSafeOutput(filePath, { includeMissingData: true })).toBe(true);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("scopes detection to content after byteOffset", () => {
      const firstLine = '{"type":"noop","message":"old"}\n';
      const filePath = makeTempFile(`${firstLine}{"type":"report_missing_tool","tool":"x"}\n`);
      try {
        expect(hasTerminalSafeOutput(filePath, { byteOffset: 0 })).toBe(true);
        expect(hasTerminalSafeOutput(filePath, { byteOffset: Buffer.byteLength(firstLine) })).toBe(false);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns false when byteOffset is used with an injected string reader", () => {
      const logs = [];
      expect(
        hasTerminalSafeOutput("/fake/path.jsonl", {
          byteOffset: 1,
          logger: message => logs.push(message),
          readFileSync: () => '{"type":"noop","message":"old"}\n',
        })
      ).toBe(false);
      expect(logs.some(message => message.includes("byteOffset is unsupported"))).toBe(true);
    });
  });

  describe("hasNoopInSafeOutputs", () => {
    function makeTempFile(content) {
      const p = path.join(os.tmpdir(), `safeoutputs-noop-test-${Date.now()}-${Math.random().toString(36).slice(2)}.jsonl`);
      fs.writeFileSync(p, content, "utf8");
      return p;
    }

    it("returns true when the file contains a noop entry", () => {
      const filePath = makeTempFile('{"type":"noop","message":"nothing to do"}\n');
      try {
        expect(hasNoopInSafeOutputs(filePath)).toBe(true);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns true when noop is mixed with other entries", () => {
      const filePath = makeTempFile('{"type":"add_comment","body":"done"}\n{"type":"noop","message":"nothing"}\n');
      try {
        expect(hasNoopInSafeOutputs(filePath)).toBe(true);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns false when the file has no noop entry", () => {
      const filePath = makeTempFile('{"type":"add_comment","body":"done"}\n');
      try {
        expect(hasNoopInSafeOutputs(filePath)).toBe(false);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("returns false when the file does not exist", () => {
      expect(hasNoopInSafeOutputs("/tmp/nonexistent-safe-outputs-file.jsonl")).toBe(false);
    });

    it("returns false when safeOutputsPath is empty", () => {
      expect(hasNoopInSafeOutputs("")).toBe(false);
    });

    it("returns false for an empty file", () => {
      const filePath = makeTempFile("");
      try {
        expect(hasNoopInSafeOutputs(filePath)).toBe(false);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("skips malformed lines and returns false when no valid noop exists", () => {
      const filePath = makeTempFile('not-json\n{"type":"add_comment"}\n');
      try {
        expect(hasNoopInSafeOutputs(filePath)).toBe(false);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("skips malformed lines and returns true when a valid noop follows them", () => {
      const filePath = makeTempFile('not-json\n{"type":"noop","message":"x"}\n');
      try {
        expect(hasNoopInSafeOutputs(filePath)).toBe(true);
      } finally {
        fs.rmSync(filePath);
      }
    });

    it("uses injected readFileSync for testability", () => {
      const logs = [];
      const result = hasNoopInSafeOutputs("/fake/path.jsonl", {
        readFileSync: () => '{"type":"noop","message":"injected"}\n',
        logger: m => logs.push(m),
      });
      expect(result).toBe(true);
      expect(logs.some(m => m.includes("noop entry found"))).toBe(true);
    });

    it("returns false when injected readFileSync returns no noop entries", () => {
      const result = hasNoopInSafeOutputs("/fake/path.jsonl", {
        readFileSync: () => '{"type":"add_comment","body":"hi"}\n',
      });
      expect(result).toBe(false);
    });

    it("returns false when injected readFileSync throws", () => {
      const result = hasNoopInSafeOutputs("/fake/path.jsonl", {
        readFileSync: () => {
          throw new Error("ENOENT");
        },
      });
      expect(result).toBe(false);
    });
  });
});
