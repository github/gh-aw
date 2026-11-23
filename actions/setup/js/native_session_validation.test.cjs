import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { audit, checkExpectations } from "../../../.github/skills/native-session-validation/scripts/audit.cjs";
import { MCP_GATEWAY_CONFIG_PATHS } from "./redact_secrets.cjs";
import { parseEngineLog } from "./engine_log_parser.cjs";

describe("deterministic native session validation", () => {
  it("reconstructs raw native evidence and checks independent falsey expectations", () => {
    const temporary = fs.mkdtempSync(path.resolve(".native-validation-"));
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
      const normalized = fs
        .readFileSync(path.join(temporary, "output", "native-0.jsonl"), "utf8")
        .trim()
        .split("\n")
        .map(line => JSON.parse(line));
      expect(normalized[0].type).toBe("session.format");
      expect(normalized.filter(event => event.type === "session.start")).toHaveLength(1);
      expect(normalized.filter(event => event.type === "workflow.info")).toHaveLength(0);
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
    const temporary = fs.mkdtempSync(path.resolve(".native-validation-"));
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

  it("distinguishes partial objects from arrays and requires exact array values", () => {
    const sessions = new Map([["native-0", [{}, [], [1], [1, 2], { items: [] }, { items: [1] }].map(content => ({ type: "vendor.notice", data: { content } }))]]);
    const checks = checkExpectations(sessions, [
      { source: "native-0", type: "vendor.notice", data: { content: {} }, count: 3 },
      { source: "native-0", type: "vendor.notice", data: { content: [] }, count: 1 },
      { source: "native-0", type: "vendor.notice", data: { content: [1] }, count: 1 },
      { source: "native-0", type: "vendor.notice", data: { content: { items: [] } }, count: 1 },
    ]);
    expect(checks.every(check => check.passed)).toBe(true);
  });

  it("ignores ambient memory and secret discovery without changing the caller environment", () => {
    const temporary = fs.mkdtempSync(path.resolve(".native-validation-"));
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

  it.each([
    ["copilot", { type: "assistant.message", data: { content: "OFFLINE_NATIVE_SECRET SOURCE_NATIVE_MASK" } }],
    ["claude", { type: "assistant", message: { content: [{ type: "text", text: "OFFLINE_NATIVE_SECRET SOURCE_NATIVE_MASK" }] } }],
    ["codex", { type: "item.completed", item: { id: "message", type: "agent_message", text: "OFFLINE_NATIVE_SECRET SOURCE_NATIVE_MASK" } }],
    ["custom", { type: "assistant", message: { content: [{ type: "text", text: "OFFLINE_NATIVE_SECRET SOURCE_NATIVE_MASK" }] } }],
  ])("does not discover ambient credentials while reconstructing or independently parsing raw %s evidence", (engine, record) => {
    const temporary = fs.mkdtempSync(path.resolve(".native-validation-"));
    const exists = fs.existsSync;
    try {
      const input = path.join(temporary, "input");
      fs.mkdirSync(input);
      const raw = "::add-mask::SOURCE_NATIVE_MASK\n" + JSON.stringify(record) + "\n";
      fs.writeFileSync(path.join(input, "agent-stdio.log"), raw);
      const baseline = audit({ input, engine, native: ["agent-stdio.log"], output: path.join(temporary, "baseline") });
      vi.stubEnv("GH_AW_SECRET_NAMES", "OFFLINE_FIXTURE");
      vi.stubEnv("SECRET_OFFLINE_FIXTURE", "OFFLINE_NATIVE_SECRET");
      const probes = [];
      vi.spyOn(fs, "existsSync").mockImplementation(filename => {
        if (MCP_GATEWAY_CONFIG_PATHS.includes(String(filename))) {
          probes.push(filename);
          throw new Error("Ambient credential probe");
        }
        return exists(filename);
      });
      const isolated = audit({ input, engine, native: ["agent-stdio.log"], output: path.join(temporary, "isolated") });
      expect(probes).toEqual([]);
      expect(isolated.reconstructed.sha256).toBe(baseline.reconstructed.sha256);
      expect(isolated.native[0].sha256).toBe(baseline.native[0].sha256);
      const normalized = fs.readFileSync(path.join(temporary, "isolated", "native-0.jsonl"), "utf8");
      expect(normalized).toContain("OFFLINE_NATIVE_SECRET");
      expect(normalized).not.toContain("SOURCE_NATIVE_MASK");
      expect(process.env.SECRET_OFFLINE_FIXTURE).toBe("OFFLINE_NATIVE_SECRET");
      vi.restoreAllMocks();
      vi.spyOn(fs, "existsSync").mockImplementation(filename => {
        if (MCP_GATEWAY_CONFIG_PATHS.includes(String(filename))) probes.push(filename);
        return exists(filename);
      });
      const runtime = parseEngineLog(raw, engine);
      expect(probes.length).toBeGreaterThan(0);
      probes.length = 0;
      const offline = parseEngineLog(raw, engine, { render: false });
      expect(probes).toEqual([]);
      expect(runtime.markdown).toBeTruthy();
      expect(offline.markdown).not.toContain("OFFLINE_NATIVE_SECRET");
      expect(offline.logEntries).toEqual(runtime.logEntries);
    } finally {
      vi.restoreAllMocks();
      vi.unstubAllEnvs();
      fs.rmSync(temporary, { recursive: true, force: true });
    }
  });

  it("retains recorded workflow metadata in the independent native artifact", () => {
    const temporary = fs.mkdtempSync(path.resolve(".native-validation-"));
    try {
      const input = path.join(temporary, "input");
      fs.mkdirSync(input);
      fs.writeFileSync(path.join(input, "aw_info.json"), '{"engine_id":"copilot"}');
      fs.writeFileSync(path.join(input, "agent-stdio.log"), '{"type":"assistant.message","data":{"content":"fixture"}}\n');
      const report = audit({
        input,
        engine: "copilot",
        native: ["agent-stdio.log"],
        output: path.join(temporary, "output"),
        expectations: [{ source: "native-0", type: "workflow.info", data: { engineId: "copilot" }, count: 1 }],
      });
      expect(report.semantics.status).toBe("passed");
    } finally {
      fs.rmSync(temporary, { recursive: true, force: true });
    }
  });

  it("fails the CLI on malformed independent native evidence even when canonical reconstruction and assertions pass", () => {
    const temporary = fs.mkdtempSync(path.resolve(".native-validation-"));
    try {
      const input = path.join(temporary, "input");
      fs.mkdirSync(input);
      const canonical = '{"type":"assistant.message","data":{"content":"fixture"}}\n';
      fs.writeFileSync(path.join(input, "agent-session.jsonl"), canonical);
      fs.writeFileSync(path.join(input, "raw-native.log"), canonical + '{"type":broken\n');
      const expectations = [{ source: "native-0", type: "assistant.message", data: { content: "fixture" }, count: 1 }];
      const options = { input, engine: "copilot", native: ["raw-native.log"], expectations };
      const report = audit({ ...options, output: path.join(temporary, "direct") });
      expect(report.semantics.status).toBe("passed");
      expect(report.reconstructed.warnings).toBe(0);
      expect(report.native[0].warnings).toBe(1);
      const normalized = fs.readFileSync(path.join(temporary, "direct", "native-0.jsonl"), "utf8");
      expect(normalized).toContain('"code":"malformed_json","line":2');
      expect(normalized).not.toContain("broken");
      const expectationsFile = path.join(temporary, "expectations.json");
      fs.writeFileSync(expectationsFile, JSON.stringify(expectations));
      const cli = spawnSync(
        process.execPath,
        [
          path.resolve(__dirname, "../../../.github/skills/native-session-validation/scripts/audit.cjs"),
          "--root",
          input,
          "--engine",
          "copilot",
          "--native",
          "raw-native.log",
          "--expectations",
          expectationsFile,
          "--output",
          path.join(temporary, "cli"),
        ],
        { encoding: "utf8" }
      );
      expect(cli.status, cli.stderr).toBe(1);
      expect(cli.stdout, cli.stderr).not.toBe("");
      expect(JSON.parse(cli.stdout)).toMatchObject({ semantics: { status: "passed" }, native: [{ warnings: 1 }] });
    } finally {
      fs.rmSync(temporary, { recursive: true, force: true });
    }
  });

  it.each([
    '● Answer\n```json\n{"incomplete example":\n```\n',
    '[DEBUG] data: {\n  "type": "assistant.message",\n  "data": { "content": "fixture" }\n}\n',
    JSON.stringify([{ type: "assistant.message", data: { content: "fixture" } }], null, 2) + "\n",
  ])("does not treat engine plaintext or pretty-printed JSON as damaged JSONL (%#)", raw => {
    const temporary = fs.mkdtempSync(path.resolve(".native-validation-"));
    try {
      const input = path.join(temporary, "input");
      fs.mkdirSync(input);
      fs.writeFileSync(path.join(input, "agent-session.jsonl"), '{"type":"assistant.message","data":{"content":"fixture"}}\n');
      fs.writeFileSync(path.join(input, "raw-native.log"), raw);
      const report = audit({ input, engine: "copilot", native: ["raw-native.log"], output: path.join(temporary, "output") });
      expect(report.native[0].warnings).toBe(0);
      expect(report.native[0].records).toBeGreaterThan(1);
    } finally {
      fs.rmSync(temporary, { recursive: true, force: true });
    }
  });

  it("uses an existing native parser warning instead of adding a duplicate generic warning", () => {
    const temporary = fs.mkdtempSync(path.resolve(".native-validation-"));
    try {
      const input = path.join(temporary, "input");
      fs.mkdirSync(input);
      fs.writeFileSync(path.join(input, "agent-session.jsonl"), '{"type":"assistant.message","data":{"content":"fixture"}}\n');
      const record = { type: "message", message: { id: "message", role: "assistant", content: [{ type: "text", text: "fixture" }] } };
      fs.writeFileSync(path.join(input, "raw-native.log"), JSON.stringify(record) + '\n{"type":broken\n');
      const report = audit({ input, engine: "goose", native: ["raw-native.log"], output: path.join(temporary, "output") });
      expect(report.native[0].warnings).toBe(1);
      const normalized = fs.readFileSync(path.join(temporary, "output", "native-0.jsonl"), "utf8");
      expect(normalized).toContain('"code":"malformed_jsonl","line":2');
      expect(normalized).not.toContain('"code":"malformed_json"');
    } finally {
      fs.rmSync(temporary, { recursive: true, force: true });
    }
  });

  it.each(["error_max_turns", "success"])("preserves Claude protocol failure and max-turn metadata without presentation (%s)", subtype => {
    vi.stubEnv("GH_AW_MAX_TURNS", "3");
    try {
      const raw = JSON.stringify([
        {
          type: "system",
          subtype: "init",
          mcp_servers: [
            { name: "broken", status: "failed" },
            { name: "healthy", status: "connected" },
          ],
        },
        { type: "result", subtype, num_turns: subtype === "success" ? 3 : 1 },
      ]);
      const runtime = parseEngineLog(raw, "claude");
      const offline = parseEngineLog(raw, "claude", { render: false });
      expect(runtime.maxTurnsHit).toBe(true);
      expect(offline.maxTurnsHit).toBe(runtime.maxTurnsHit);
      expect(offline.mcpFailures).toEqual(runtime.mcpFailures);
      expect(offline.mcpFailures).toEqual(["broken"]);
      expect(offline.logEntries).toEqual(runtime.logEntries);
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it("parses legacy Codex without ambient presentation and preserves protocol failure evidence", () => {
    const raw = "model: example\nFailed to connect to MCP server 'broken': unavailable\nthinking\nfixture\ntotal_tokens: 0\n";
    const runtime = parseEngineLog(raw, "codex");
    const exists = fs.existsSync;
    try {
      vi.spyOn(fs, "existsSync").mockImplementation(filename => {
        if (MCP_GATEWAY_CONFIG_PATHS.includes(String(filename))) throw new Error("Ambient credential probe");
        return exists(filename);
      });
      const offline = parseEngineLog(raw, "codex", { render: false });
      expect(offline.markdown).toBe("");
      expect(offline.logEntries).toEqual(runtime.logEntries);
      expect(offline.mcpFailures).toEqual(runtime.mcpFailures);
      expect(offline.mcpFailures).toEqual(["broken"]);
      expect(offline.maxTurnsHit).toBe(runtime.maxTurnsHit);
    } finally {
      vi.restoreAllMocks();
    }
  });

  it("refuses a directory collision after the preflight without writing output files", () => {
    const temporary = fs.mkdtempSync(path.resolve(".native-validation-"));
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
