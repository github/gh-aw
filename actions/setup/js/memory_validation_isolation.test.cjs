import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import childProcess from "node:child_process";
import { runCustomMemoryValidation, validateMemoryJSONSchemas } from "./memory_custom_validation.cjs";

describe("encoded memory validator runtime isolation", () => {
  let directory;
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    if (directory) fs.rmSync(directory, { recursive: true, force: true });
  });

  it("relocates decoded executable paths without rewriting schemas or exposing context", () => {
    directory = fs.mkdtempSync(path.join(import.meta.dirname, ".memory-isolation-test-"));
    const memory = path.join(directory, "memory");
    fs.mkdirSync(memory);
    fs.writeFileSync(path.join(memory, "state.json"), JSON.stringify({ path: "/tmp/gh-aw/agent" }));
    vi.spyOn(os, "tmpdir").mockReturnValue(directory);
    vi.stubEnv("GH_AW_RUNTIME_ROOT", "/tmp/gh-aw-run.Test123");
    const writes = [];
    vi.spyOn(childProcess, "spawnSync").mockImplementation((_command, args, options) => {
      expect(options.env.GH_AW_RUNTIME_TOKEN).toBeUndefined();
      if (args[0] === "-e") {
        const input = JSON.parse(options.input);
        expect(input.schemas[0].schema.properties.path.enum).toEqual(["/tmp/gh-aw/agent"]);
        return { status: 0, stdout: validateMemoryJSONSchemas(input.memoryDir, input.schemas, input.kind, input.memoryId), stderr: "" };
      }
      const wrapper = fs.readFileSync(args[0], "utf8");
      vm.runInNewContext(wrapper, {
        require(name) {
          if (name === "fs") return { writeFileSync: (...values) => writes.push(values) };
          if (name === "path") return path;
          throw new Error(`Unexpected validator import ${name}`);
        },
        process: { env: {}, exit: () => {} },
        console: { error: () => {} },
      });
      return { status: 0, stdout: "", stderr: "" };
    });
    const script = 'fs.writeFileSync("/tmp/gh-aw/agent/result.txt", "validated");';
    const result = runCustomMemoryValidation({
      scriptBase64: Buffer.from(script).toString("base64"),
      jsonSchemas: [{ file: "state.json", schema: { type: "object", properties: { path: { enum: ["/tmp/gh-aw/agent"] } }, required: ["path"] } }],
      memoryDir: memory,
      kind: "repo",
      timeoutSeconds: 5,
    });
    expect(result.ok, result.stderr).toBe(true);
    expect(writes).toEqual([["/tmp/gh-aw-run.Test123/agent/result.txt", "validated"]]);
    expect(fs.readFileSync(path.join(memory, "state.json"), "utf8")).toContain("/tmp/gh-aw/agent");
  });
});
