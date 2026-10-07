import { describe, it, expect, vi, afterEach } from "vitest";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import { createRequire } from "node:module";
import { validateNativeSchema, configureNativeSchema, createResponseCollector, correctionArguments, generateStructuredResponse } from "./gemini_structured_output.cjs";

const localRequire = createRequire(import.meta.url);
const runtimeSource = fs.readFileSync(new URL("./gemini_structured_output.cjs", import.meta.url), "utf8");
const schema = {
  type: "object",
  properties: { engine: { type: "string", enum: ["gemini"] }, count: { type: "integer" } },
  required: ["engine", "count"],
  additionalProperties: false,
};
const args = ["--yolo", "--skip-trust", "--output-format", "stream-json", "--prompt", "primary task"];
const valid = '{ "engine": "gemini", "count": 2 }';
const sessionId = "native-session-id";
const record = (collector, event) => collector.accept(JSON.stringify(event));

afterEach(() => vi.unstubAllEnvs());

describe("Gemini native schema configuration", () => {
  it("accepts the documented native subset without requiring optional fields or closed objects", () => {
    expect(() => validateNativeSchema(schema)).not.toThrow();
    expect(() => validateNativeSchema({ type: "object", properties: { optional: { type: "string" } } })).not.toThrow();
    expect(() =>
      validateNativeSchema({
        type: "object",
        $defs: { result: { type: "string", enum: ["pass", "fail"] } },
        properties: { result: { $ref: "#/$defs/result" } },
      })
    ).not.toThrow();
  });

  it.each(["const", "pattern", "minLength", "maxLength", "allOf", "oneOf", "not", "if", "dependentRequired", "unevaluatedProperties", "definitions"])("rejects unsupported %s instead of relying on prompt-only enforcement", keyword => {
    expect(() => validateNativeSchema({ type: "object", properties: { field: { [keyword]: {} } } })).toThrow(`keyword "${keyword}"`);
  });

  it("rejects recursive and anchor references and unsupported $ref siblings", () => {
    expect(() => validateNativeSchema({ type: "object", properties: { child: { $ref: "#" } } })).toThrow("recursive");
    expect(() => validateNativeSchema({ type: "object", properties: { child: { $ref: "#named-anchor" } } })).toThrow("JSON Pointer");
    expect(() => validateNativeSchema({ type: "object", $defs: { value: { type: "string" } }, properties: { field: { $ref: "#/$defs/value", maxLength: 10 } } })).toThrow('keyword "maxLength"');
    expect(() => validateNativeSchema({ type: "object", $defs: { value: { type: "string" } }, properties: { field: { $ref: "#/$defs/value", type: "string" } } })).toThrow("alongside $ref");
  });

  it("rejects boolean and object enum members rather than silently accepting unsupported constraints", () => {
    expect(() => validateNativeSchema({ type: "object", properties: { field: { enum: [true] } } })).toThrow("strings and numbers");
  });

  it("passes the exact JSON schema through native primary-chat generation settings", () => {
    const original = {
      mcpServers: { github: { url: "http://localhost:3000" } },
      tools: { core: ["run_shell_command"] },
      modelConfigs: { customOverrides: [{ match: { model: "summarizer-shell" }, modelConfig: { generateContentConfig: { temperature: 0 } } }] },
    };
    const configured = configureNativeSchema(original, schema);
    expect(configured.mcpServers).toEqual(original.mcpServers);
    expect(configured.tools).toEqual(original.tools);
    expect(original.modelConfigs.customOverrides).toHaveLength(1);
    expect(configured.modelConfigs.customOverrides).toHaveLength(2);
    expect(configured.modelConfigs.customOverrides[1]).toEqual({
      match: { isChatModel: true, overrideScope: "core" },
      modelConfig: { generateContentConfig: { responseMimeType: "application/json", responseJsonSchema: schema } },
    });
    expect(configureNativeSchema(configured, schema)).toEqual(configured);
  });

  it("rejects execution paths that may bypass or replace the native schema", () => {
    expect(() => configureNativeSchema({ experimental: { adk: { agentSessionNoninteractiveEnabled: true } } }, schema)).toThrow("ADK");
    expect(() => configureNativeSchema({ hooks: { BeforeModel: [{ command: "custom-hook" }] } }, schema)).toThrow("BeforeModel");
    vi.stubEnv("GEMINI_CLI_EXP_AGENT", "true");
    expect(() => configureNativeSchema({}, schema)).toThrow("ADK");
  });

  it("rejects malformed native settings instead of silently dropping the schema", () => {
    expect(() => configureNativeSchema({ modelConfigs: { customOverrides: {} } }, schema)).toThrow("must be an array");
  });

  it("rejects existing schema overrides that could take precedence over the primary schema", () => {
    for (const field of ["responseSchema", "responseJsonSchema", "responseMimeType"]) {
      expect(() => configureNativeSchema({ modelConfigs: { customOverrides: [{ match: { model: "chat-base", isRetry: true }, modelConfig: { generateContentConfig: { [field]: "conflict" } } }] } }, schema)).toThrow(
        "conflicting native output overrides"
      );
    }
  });
});

describe("Gemini primary final response extraction", () => {
  it("extracts raw final JSON without session envelopes or commentary before tools", () => {
    const collector = createResponseCollector();
    collector.accept("CLI diagnostic, not a stream event");
    record(collector, { type: "init", session_id: sessionId });
    record(collector, { type: "message", role: "assistant", content: "I will inspect the repository." });
    record(collector, { type: "tool_use", tool_name: "run_shell_command" });
    record(collector, { type: "tool_result", output: '{"untrusted":"tool output"}' });
    record(collector, { type: "message", role: "assistant", content: valid.slice(0, 10), delta: true });
    record(collector, { type: "message", role: "assistant", content: valid.slice(10), delta: true });
    record(collector, { type: "result", status: "success", stats: { total_tokens: 10 } });
    expect(collector.finish()).toEqual({ text: valid, sessionId });
  });

  it("uses a fresh response after a new native session", () => {
    const collector = createResponseCollector();
    record(collector, { type: "message", role: "assistant", content: "old response" });
    record(collector, { type: "result", status: "success" });
    record(collector, { type: "init", session_id: sessionId });
    record(collector, { type: "message", role: "assistant", content: valid });
    record(collector, { type: "result", status: "success" });
    expect(collector.finish().text).toBe(valid);
  });

  it.each([undefined, "error"])("requires a successful terminal result (%s)", status => {
    const collector = createResponseCollector();
    record(collector, { type: "message", role: "assistant", content: valid });
    if (status) record(collector, { type: "result", status });
    expect(() => collector.finish()).toThrow("successful terminal result");
  });

  it("rejects an empty final response and a response exceeding the job-output limit", () => {
    const collector = createResponseCollector();
    record(collector, { type: "result", status: "success" });
    expect(() => collector.finish()).toThrow("no primary final response");
    expect(() => record(collector, { type: "message", role: "assistant", content: "x".repeat(256 * 1024 + 1) })).toThrow("256 KiB");
  });
});

describe("Gemini native constrained response correction", () => {
  it("returns the original raw JSON after validating its exact shape", async () => {
    const execute = vi.fn().mockResolvedValue({ text: valid, sessionId });
    await expect(generateStructuredResponse("gemini", args, schema, execute)).resolves.toBe(valid);
    expect(execute).toHaveBeenCalledOnce();
    expect(execute).toHaveBeenCalledWith("gemini", args);
  });

  describe("Gemini structured-output file contract", () => {
    function runtime(records, exitCode = 0, inputSchema = schema) {
      const workspace = ".gemini-native-schema-memory-fixture";
      const settingsPath = path.join(workspace, ".gemini", "settings.json");
      const files = new Map([
        ["schema.json", JSON.stringify(inputSchema)],
        ["structured.json", '{"stale":true}'],
        [settingsPath, JSON.stringify({ mcpServers: { github: { url: "http://localhost:3000" } } })],
      ]);
      const writes = [];
      const mockFs = {
        readFileSync: filename => {
          if (!files.has(filename)) throw new Error(`Missing fixture: ${filename}`);
          return files.get(filename);
        },
        rmSync: filename => files.delete(filename),
        writeFileSync: (filename, value, options) => {
          writes.push({ filename, value, options });
          files.set(filename, value);
        },
      };
      const spawn = vi.fn(() => {
        const child = new EventEmitter();
        child.stdout = new PassThrough();
        child.kill = vi.fn();
        setImmediate(() => {
          child.stdout.end(records.map(record => JSON.stringify(record)).join("\n") + "\n");
          setImmediate(() => child.emit("close", exitCode, null));
        });
        return child;
      });
      const module = { exports: {} };
      const shared = localRequire("./structured_output.cjs");
      vm.runInNewContext(runtimeSource, {
        module,
        require: name => {
          if (name === "node:fs") return mockFs;
          if (name === "node:child_process") return { spawn };
          if (name === "./structured_output.cjs") {
            return { ...shared, loadStructuredOutputSchema: filename => JSON.parse(mockFs.readFileSync(filename)) };
          }
          return localRequire(name);
        },
        process: { env: { ...process.env, GITHUB_WORKSPACE: workspace }, stdout: new PassThrough() },
        Buffer,
        Error,
        SyntaxError,
        JSON,
      });
      return { main: module.exports.main, files, writes, spawn, settingsPath };
    }

    it("writes RAW final JSON, not the stream envelope, only after native schema validation", async () => {
      const fixture = runtime([
        { type: "init", session_id: sessionId },
        { type: "message", role: "assistant", content: valid },
        { type: "result", status: "success" },
      ]);
      await fixture.main(["schema.json", "structured.json", "--", "gemini", ...args]);
      expect(fixture.files.get("structured.json")).toBe(valid + "\n");
      const configured = JSON.parse(fixture.files.get(fixture.settingsPath));
      expect(configured.modelConfigs.customOverrides[0].modelConfig.generateContentConfig.responseJsonSchema).toEqual(schema);
      expect(configured.mcpServers.github.url).toBe("http://localhost:3000");
      expect(fixture.writes.at(-1).options).toEqual({ mode: 0o600 });
      expect(fixture.spawn).toHaveBeenCalledOnce();
    });

    it("removes stale output and fails on a nonzero native CLI exit", async () => {
      const fixture = runtime(
        [
          { type: "message", role: "assistant", content: valid },
          { type: "result", status: "success" },
        ],
        1
      );
      await expect(fixture.main(["schema.json", "structured.json", "--", "gemini", ...args])).rejects.toThrow("exit 1");
      expect(fixture.files.has("structured.json")).toBe(false);
      expect(fixture.spawn).toHaveBeenCalledOnce();
    });

    it("fails without publishing an empty or missing primary final response", async () => {
      const fixture = runtime([{ type: "result", status: "success" }]);
      await expect(fixture.main(["schema.json", "structured.json", "--", "gemini", ...args])).rejects.toThrow("no primary final response");
      expect(fixture.files.has("structured.json")).toBe(false);
    });

    it("fails before execution if the JSON schema is invalid", async () => {
      const fixture = runtime([], 0, { type: "invalid-schema-type" });
      await expect(fixture.main(["schema.json", "structured.json", "--", "gemini", ...args])).rejects.toThrow("schema is invalid");
      expect(fixture.files.has("structured.json")).toBe(false);
      expect(fixture.spawn).not.toHaveBeenCalled();
    });

    it("fails before execution if a valid JSON schema has unsupported native constraints", async () => {
      const fixture = runtime([], 0, { type: "object", properties: { field: { type: "string", pattern: "^fixed$" } } });
      await expect(fixture.main(["schema.json", "structured.json", "--", "gemini", ...args])).rejects.toThrow('keyword "pattern"');
      expect(fixture.files.has("structured.json")).toBe(false);
      expect(fixture.spawn).not.toHaveBeenCalled();
    });
  });

  it.each(["```json\n" + valid + "\n```", '{"engine":"gemini","count":"wrong type"}', '{"engine":"gemini","count":2,"extra":true}'])("corrects an invalid primary response once without a prompt-only fallback (%s)", async text => {
    const execute = vi.fn().mockResolvedValueOnce({ text, sessionId }).mockResolvedValueOnce({ text: valid, sessionId });
    await expect(generateStructuredResponse("gemini", args, schema, execute)).resolves.toBe(valid);
    expect(execute).toHaveBeenCalledTimes(2);
    const correction = execute.mock.calls[1][1];
    expect(correction).toContain("--resume");
    expect(correction).toContain(sessionId);
    expect(correction).toContain("--output-format");
    expect(correction).not.toContain("primary task");
    expect(correction.at(-1)).toContain("configured native JSON schema");
    expect(correction.at(-1)).not.toContain(text);
  });

  it("fails after exactly one correction attempt", async () => {
    const execute = vi.fn().mockResolvedValue({ text: "invalid JSON", sessionId });
    await expect(generateStructuredResponse("gemini", args, schema, execute)).rejects.toThrow("after one correction attempt");
    expect(execute).toHaveBeenCalledTimes(2);
  });

  it("does not correct a native CLI or provider failure", async () => {
    const execute = vi.fn().mockRejectedValue(new Error("Gemini CLI failed during structured output"));
    await expect(generateStructuredResponse("gemini", args, schema, execute)).rejects.toThrow("Gemini CLI failed");
    expect(execute).toHaveBeenCalledOnce();
  });

  it("does not replace native schema errors with prompt instructions", async () => {
    const execute = vi.fn().mockResolvedValue({ text: valid, sessionId });
    await expect(generateStructuredResponse("gemini", args, { type: "invalid-schema-type" }, execute)).rejects.toThrow("schema could not be validated");
    expect(execute).toHaveBeenCalledOnce();
  });

  it("rejects missing or unsafe resume IDs instead of starting a replacement task", () => {
    for (const id of ["", "../../other-session", "session;injection"]) {
      expect(() => correctionArguments(args, id, "invalid JSON")).toThrow("valid native session ID");
    }
  });
});
