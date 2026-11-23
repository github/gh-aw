import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { audit, checkExpectations } from "../../../.github/skills/native-session-validation/scripts/audit.cjs";
import { MCP_GATEWAY_CONFIG_PATHS } from "./redact_secrets.cjs";

describe("deterministic native session validation", () => {
  it("reconstructs raw native evidence and checks independent falsey expectations", () => {
    const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "native-validation-"));
    try {
      const input = path.join(temporary, "input");
      fs.mkdirSync(input);
      const raw =
        [
          { type: "session.start", data: { sessionId: "fixture" } },
          { type: "assistant.message", data: { content: "exact\n" } },
          { type: "vendor.notice", data: { flag: false, count: 0, value: null, items: [] } },
        ]
          .map(event => JSON.stringify(event))
          .join("\n") + "\n";
      fs.writeFileSync(path.join(input, "agent-stdio.log"), raw);
      const report = audit({
        input,
        engine: "copilot",
        native: ["agent-stdio.log"],
        output: path.join(temporary, "output"),
        expectations: [
          { source: "native-0", type: "assistant.message", data: { content: "exact\n" }, count: 1 },
          { source: "native-0", type: "vendor.notice", data: { flag: false, count: 0, value: null, items: [] }, count: 1 },
        ],
      });
      expect(report.semantics.status).toBe("passed");
      expect(report.recorded.status).toBe("unavailable");
      expect(report.native[0].comparison.leftOnly).toBe(0);
      expect(JSON.stringify(report)).not.toContain("exact");
      expect(fs.readFileSync(path.join(input, "agent-stdio.log"), "utf8")).toBe(raw);
      expect(fs.statSync(path.join(temporary, "output")).mode & 0o777).toBe(0o700);
      expect(fs.statSync(path.join(temporary, "output", "native-0.jsonl")).mode & 0o777).toBe(0o600);
      expect(() => audit({ input, engine: "copilot", output: path.join(input, "output") })).toThrow("outside evidence");
      expect(() => audit({ input, engine: "copilot", output: path.join(temporary, "output") })).toThrow("new directory");
    } finally {
      fs.rmSync(temporary, { recursive: true, force: true });
    }
  });

  it("does not mistake structural validation or canonical input for native semantic validation", () => {
    const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "native-validation-"));
    try {
      const input = path.join(temporary, "input");
      fs.mkdirSync(input);
      expect(() => audit({ input, engine: "copilot", output: path.join(temporary, "empty") })).toThrow("No recognizable agent session evidence");
      fs.writeFileSync(path.join(input, "agent-session.jsonl"), '{"type":"assistant.message","data":{"content":"fixture"}}\n');
      const report = audit({ input, engine: "copilot", output: path.join(temporary, "output") });
      expect(report.semantics.status).toBe("not_checked");
      expect(report.native).toEqual([]);
      expect(() => audit({ input, engine: "copilot", native: ["agent-session.jsonl"], output: path.join(temporary, "other") })).toThrow("not native");
      expect(() => audit({ input, engine: "copilot", native: ["../input/agent-session.jsonl"], output: path.join(temporary, "other") })).toThrow("relative");
      fs.writeFileSync(path.join(input, "aw_info.json"), '{"engine_id":"pi"}');
      expect(() => audit({ input, engine: "copilot", output: path.join(temporary, "mismatch") })).toThrow("recorded metadata");
    } finally {
      fs.rmSync(temporary, { recursive: true, force: true });
    }
  });

  it("fails exact counts and refuses unavailable assertion sources without exposing content", () => {
    const sessions = new Map([["native-0", [{ type: "assistant.message", data: { content: "private" } }]]]);
    const checks = checkExpectations(sessions, [{ source: "native-0", type: "assistant.message", data: { content: "private" }, count: 2 }]);
    expect(checks[0]).toMatchObject({ passed: false, expected: 2, actual: 1 });
    expect(JSON.stringify(checks)).not.toContain("private");
    expect(() => checkExpectations(sessions, [{ source: "recorded", type: "assistant.message", data: {}, count: 0 }])).toThrow("Invalid");
  });

  it("ignores ambient memory and secret discovery without changing the caller environment", () => {
    const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "native-validation-"));
    const exists = fs.existsSync;
    try {
      const input = path.join(temporary, "input");
      fs.mkdirSync(input);
      fs.writeFileSync(path.join(input, "agent-session.jsonl"), '{"type":"assistant.message","data":{"content":"fixture"}}\n');
      const baseline = audit({ input, engine: "copilot", output: path.join(temporary, "baseline") });
      vi.stubEnv("GH_AW_MEMORY_SAVE_RESULTS", '{"unrelated":{"jobResult":"success","status":"unchanged"}}');
      vi.stubEnv("GH_AW_SECRET_NAMES", "OFFLINE_FIXTURE");
      vi.stubEnv("SECRET_OFFLINE_FIXTURE", "fixture");
      const probes = [];
      vi.spyOn(fs, "existsSync").mockImplementation(filename => {
        if (MCP_GATEWAY_CONFIG_PATHS.includes(String(filename))) {
          probes.push(filename);
          throw new Error("Ambient credential probe");
        }
        return exists(filename);
      });
      const isolated = audit({ input, engine: "copilot", output: path.join(temporary, "isolated") });
      expect(probes).toEqual([]);
      expect(isolated.reconstructed.sha256).toBe(baseline.reconstructed.sha256);
      expect(process.env.SECRET_OFFLINE_FIXTURE).toBe("fixture");
    } finally {
      vi.restoreAllMocks();
      vi.unstubAllEnvs();
      fs.rmSync(temporary, { recursive: true, force: true });
    }
  });

  it("refuses a directory collision after the preflight without writing output files", () => {
    const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "native-validation-"));
    const mkdir = fs.mkdirSync;
    try {
      const input = path.join(temporary, "input");
      const output = path.join(temporary, "output");
      fs.mkdirSync(input);
      fs.writeFileSync(path.join(input, "agent-session.jsonl"), '{"type":"assistant.message","data":{"content":"fixture"}}\n');
      vi.spyOn(fs, "mkdirSync").mockImplementation((directory, options) => {
        if (directory === output) mkdir(output, { mode: 0o700 });
        return mkdir(directory, options);
      });
      expect(() => audit({ input, engine: "copilot", output })).toThrow();
      expect(fs.readdirSync(output)).toEqual([]);
    } finally {
      vi.restoreAllMocks();
      fs.rmSync(temporary, { recursive: true, force: true });
    }
  });
});
