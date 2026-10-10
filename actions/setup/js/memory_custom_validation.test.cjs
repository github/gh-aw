import { describe, it, expect, afterEach, beforeEach } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { clearValidationMarker, formatJSONFiles, getValidationMarkerPath, runCustomMemoryValidation, writeValidationMarker } from "./memory_custom_validation.cjs";
import { validateSchemaContract } from "./memory_schema_contract.cjs";
import schemaContractFixtures from "./memory_schema_contract.fixtures.json";

describe("memory_custom_validation", () => {
  let tempDir;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-memory-validation-test-"));
  });

  afterEach(() => {
    fs.rmSync(tempDir, { recursive: true, force: true });
    clearValidationMarker("repo", "default");
  });

  it("runs a successful validator with memory globals", () => {
    fs.writeFileSync(path.join(tempDir, "state.json"), JSON.stringify({ ok: true }));
    const result = runCustomMemoryValidation({
      script: `
        const state = JSON.parse(fs.readFileSync(path.join(memoryRoot, "state.json"), "utf8"));
        if (!state.ok || memoryKind !== "repo" || memoryId !== "default") throw new Error("bad context");
        console.log("domain ok");
      `,
      memoryDir: tempDir,
      memoryId: "default",
      kind: "repo",
      timeoutSeconds: 5,
    });

    expect(result.ok).toBe(true);
    expect(result.stdout).toContain("domain ok");
  });

  it("filters the disposable validation copy without changing sibling files", () => {
    fs.mkdirSync(path.join(tempDir, "nested"));
    fs.mkdirSync(path.join(tempDir, "sibling-only"));
    fs.mkdirSync(path.join(tempDir, ".git"));
    fs.writeFileSync(path.join(tempDir, "state.json"), '{"ok":true}');
    fs.writeFileSync(path.join(tempDir, "sibling.json"), "not JSON");
    fs.writeFileSync(path.join(tempDir, "nested", "state.json"), '{"nested":true}');
    fs.writeFileSync(path.join(tempDir, "nested", "sibling.json"), "not JSON");
    fs.writeFileSync(path.join(tempDir, "sibling-only", "sibling.json"), "not JSON");
    fs.writeFileSync(path.join(tempDir, ".git", "state.json"), "not JSON");

    const result = runCustomMemoryValidation({
      script: `
        for (const relativePath of ["state.json", "nested/state.json"]) {
          JSON.parse(fs.readFileSync(path.join(memoryRoot, relativePath), "utf8"));
        }
        if (fs.existsSync(path.join(memoryRoot, "sibling.json"))
          || fs.existsSync(path.join(memoryRoot, "nested", "sibling.json"))
          || fs.existsSync(path.join(memoryRoot, "sibling-only"))
          || fs.existsSync(path.join(memoryRoot, ".git"))) throw new Error("ineligible files copied");
      `,
      memoryDir: tempDir,
      kind: "repo",
      timeoutSeconds: 5,
      isEligibleFile: relativePath => relativePath === "state.json" || relativePath === "nested/state.json" || relativePath === ".git/state.json",
    });

    expect(result.ok).toBe(true);
    expect(fs.readFileSync(path.join(tempDir, "sibling.json"), "utf8")).toBe("not JSON");
    expect(fs.readFileSync(path.join(tempDir, "nested", "sibling.json"), "utf8")).toBe("not JSON");
    expect(fs.readFileSync(path.join(tempDir, "sibling-only", "sibling.json"), "utf8")).toBe("not JSON");
  });

  it("reports a nonzero validator separately from stdout", () => {
    const result = runCustomMemoryValidation({
      script: `
        console.log("generic-looking stdout");
        console.error("domain schema failed");
        return false;
      `,
      memoryDir: tempDir,
      memoryId: "default",
      kind: "cache",
      timeoutSeconds: 5,
    });
    expect(result.ok).toBe(false);
    expect(result.stdout).toContain("generic-looking stdout");
    expect(result.stderr).toContain("domain schema failed");
  });

  it("validates nested JSON schemas without rewriting data", () => {
    const file = path.join(tempDir, "state.json");
    const contents = '{"items":["open",null]}';
    fs.writeFileSync(file, contents);
    const result = runCustomMemoryValidation({
      jsonSchemas: [
        {
          file: "state.json",
          format: "json",
          schema: {
            type: "object",
            required: ["items"],
            additionalProperties: false,
            properties: { items: { type: "array", items: { enum: ["open", "closed", null] } } },
          },
        },
      ],
      memoryDir: tempDir,
      memoryId: "schema-test",
      kind: "cache",
      timeoutSeconds: 5,
    });

    expect(result.ok).toBe(true);
    expect(fs.readFileSync(file, "utf8")).toBe(contents);
  });

  it("validates every file matched by a recursive schema glob", () => {
    fs.mkdirSync(path.join(tempDir, "events", "nested"), { recursive: true });
    fs.writeFileSync(path.join(tempDir, "events", "first.json"), '{"ok":true}');
    fs.writeFileSync(path.join(tempDir, "events", "nested", "second.json"), '{"ok":true}');
    const jsonSchemas = [
      {
        file: "events/**/*.json",
        format: "json",
        schema: { type: "object", required: ["ok"], properties: { ok: { type: "boolean" } } },
      },
    ];
    expect(runCustomMemoryValidation({ jsonSchemas, memoryDir: tempDir, kind: "repo" }).ok).toBe(true);

    fs.writeFileSync(path.join(tempDir, "events", "nested", "second.json"), '{"ok":"no"}');
    const result = runCustomMemoryValidation({ jsonSchemas, memoryDir: tempDir, kind: "repo" });
    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("events/nested/second.json");
    expect(result.stderr).toContain("ok");
  });

  it("fails when a schema glob matches no files", () => {
    const result = runCustomMemoryValidation({
      jsonSchemas: [{ file: "events/**/*.json", format: "json", schema: { type: "object" } }],
      memoryDir: tempDir,
      kind: "repo",
    });

    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("events/**/*.json");
    expect(result.stderr).toContain("matched no files");
  });

  it("infers formats independently for JSON and JSONL wildcard matches", () => {
    fs.mkdirSync(path.join(tempDir, "nested"));
    fs.writeFileSync(path.join(tempDir, "state.JSON"), '{"ok":true}');
    fs.writeFileSync(path.join(tempDir, "nested", "events.JSONL"), '{"ok":true}\n{"ok":false}\n');
    const jsonSchemas = [{ file: "**/*", schema: { type: "object", required: ["ok"], properties: { ok: { type: "boolean" } } } }];
    expect(runCustomMemoryValidation({ jsonSchemas, memoryDir: tempDir, kind: "repo" }).ok).toBe(true);

    fs.writeFileSync(path.join(tempDir, "nested", "events.JSONL"), '{"ok":true}\n{"ok":"bad"}\n');
    const result = runCustomMemoryValidation({ jsonSchemas, memoryDir: tempDir, kind: "repo" });
    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("nested/events.JSONL");
    expect(result.stderr).toContain("line 2");
    expect(result.stderr).toContain("ok");
  });

  it.each(["state.json", "events.jsonl"])("infers the format of an exact %s target", file => {
    fs.writeFileSync(path.join(tempDir, file), file.endsWith(".jsonl") ? "{}\n{}\n" : "{}");
    const result = runCustomMemoryValidation({
      jsonSchemas: [{ file, schema: { type: "object" } }],
      memoryDir: tempDir,
      kind: "cache",
    });
    expect(result.ok).toBe(true);
  });

  it("requires an explicit format for unknown extensions", () => {
    fs.writeFileSync(path.join(tempDir, "state.txt"), "{}");
    const declaration = { file: "state.txt", schema: { type: "object" } };
    const result = runCustomMemoryValidation({ jsonSchemas: [declaration], memoryDir: tempDir, kind: "repo" });
    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("state.txt");
    expect(result.stderr).toContain("set format to json or jsonl");
    expect(runCustomMemoryValidation({ jsonSchemas: [{ ...declaration, format: "json" }], memoryDir: tempDir, kind: "repo" }).ok).toBe(true);
  });

  it.each(["", null, "yaml"])("rejects an invalid explicit format %s rather than inferring it", format => {
    fs.writeFileSync(path.join(tempDir, "state.json"), "{}");
    const result = runCustomMemoryValidation({
      jsonSchemas: [{ file: "state.json", format, schema: { type: "object" } }],
      memoryDir: tempDir,
      kind: "repo",
    });
    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("unsupported format");
  });

  it("reports nested schema paths and checks schemas before custom scripts", () => {
    fs.writeFileSync(path.join(tempDir, "state.json"), '{"items":[1]}');
    const result = runCustomMemoryValidation({
      script: 'throw new Error("script should run only after schema validation");',
      jsonSchemas: [
        {
          file: "state.json",
          format: "json",
          schema: { type: "object", properties: { items: { type: "array", items: { type: "string" } } } },
        },
      ],
      memoryDir: tempDir,
      memoryId: "schema-test",
      kind: "repo",
    });

    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("state.json");
    expect(result.stderr).toContain("items[0]");
    expect(result.stderr).not.toContain("script should run only");
  });

  it("validates JSONL records independently and reports physical line numbers", () => {
    fs.writeFileSync(path.join(tempDir, "events.jsonl"), '{"id":1}\r\n{"id":"bad"}\r\n');
    const result = runCustomMemoryValidation({
      jsonSchemas: [{ file: "events.jsonl", format: "jsonl", schema: { type: "object", properties: { id: { type: "integer" } } } }],
      memoryDir: tempDir,
      memoryId: "schema-test",
      kind: "drive",
    });

    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("events.jsonl");
    expect(result.stderr).toContain("line 2");
    expect(result.stderr).toContain("id");
  });

  it("accepts an empty JSONL file but rejects blank and malformed records", () => {
    const file = path.join(tempDir, "events.jsonl");
    const declaration = [{ file: "events.jsonl", format: "jsonl", schema: { type: "object" } }];
    fs.writeFileSync(file, "");
    expect(runCustomMemoryValidation({ jsonSchemas: declaration, memoryDir: tempDir, kind: "repo" }).ok).toBe(true);
    fs.writeFileSync(file, "{}\n\n");
    expect(runCustomMemoryValidation({ jsonSchemas: declaration, memoryDir: tempDir, kind: "repo" }).stderr).toContain("line 2");
    fs.writeFileSync(file, "{broken}\n");
    expect(runCustomMemoryValidation({ jsonSchemas: declaration, memoryDir: tempDir, kind: "repo" }).stderr).toContain("line 1");
  });

  it("streams JSONL records and rejects lossy numbers used with numeric enums", () => {
    const file = path.join(tempDir, "events.jsonl");
    fs.writeFileSync(file, `${'{"status":"open"}\n'.repeat(10000)}`);
    const declaration = [{ file: "events.jsonl", format: "jsonl", schema: { type: "object", properties: { status: { enum: ["open", "closed"] } } } }];
    expect(runCustomMemoryValidation({ jsonSchemas: declaration, memoryDir: tempDir, kind: "repo" }).ok).toBe(true);

    fs.writeFileSync(file, '{"count":1.0000000000000001}\n');
    const numericEnum = [{ file: "events.jsonl", format: "jsonl", schema: { type: "object", properties: { count: { enum: [1] } } } }];
    const result = runCustomMemoryValidation({ jsonSchemas: numericEnum, memoryDir: tempDir, kind: "repo" });
    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("line 1");
    expect(result.stderr).toContain("cannot be represented exactly");
  });

  it("fails closed for missing, malformed, unsupported, or escaping schema targets", () => {
    const validSchema = { type: "object" };
    const missing = runCustomMemoryValidation({
      jsonSchemas: [{ file: "missing.json", format: "json", schema: validSchema }],
      memoryDir: tempDir,
      kind: "repo",
    });
    expect(missing.ok).toBe(false);
    expect(missing.stderr).toContain("missing.json");

    const unsupported = runCustomMemoryValidation({
      jsonSchemas: [{ file: "missing.jsonl", format: "jsonl", schema: { type: "object", format: "date-time" } }],
      memoryDir: tempDir,
      kind: "repo",
    });
    expect(unsupported.ok).toBe(false);
    expect(unsupported.stderr).toContain("Unsupported memory schema keyword");

    const traversal = runCustomMemoryValidation({
      jsonSchemas: [{ file: "../outside.json", format: "json", schema: validSchema }],
      memoryDir: tempDir,
      kind: "repo",
    });
    expect(traversal.ok).toBe(false);
    expect(traversal.stderr).toContain("relative path");

    const required = runCustomMemoryValidation({ requireJSONSchemas: true, memoryDir: tempDir, kind: "repo" });
    expect(required.ok).toBe(false);
    expect(required.stderr).toContain("missing or empty");
  });

  it("rejects schema targets that are directories or external symlinks", () => {
    fs.mkdirSync(path.join(tempDir, "folder"));
    fs.writeFileSync(path.join(tempDir, "external.json"), '{"ok":true}');
    const outsideDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-schema-outside-"));
    const outsideFile = path.join(outsideDir, "outside.json");
    fs.writeFileSync(outsideFile, '{"ok":true}');
    fs.symlinkSync(outsideFile, path.join(tempDir, "external-link.json"));
    try {
      for (const file of ["folder", "external-link.json"]) {
        const result = runCustomMemoryValidation({
          jsonSchemas: [{ file, format: "json", schema: { type: "object" } }],
          memoryDir: tempDir,
          kind: "repo",
        });
        expect(result.ok).toBe(false);
        expect(result.stderr).toContain(file);
      }
      const wildcardResult = runCustomMemoryValidation({
        jsonSchemas: [{ file: "external-*.json", format: "json", schema: { type: "object" } }],
        memoryDir: tempDir,
        kind: "repo",
      });
      expect(wildcardResult.ok).toBe(false);
      expect(wildcardResult.stderr).toContain("external-link.json");
      expect(wildcardResult.stderr).toContain("outside the memory directory");
    } finally {
      fs.rmSync(outsideDir, { recursive: true, force: true });
    }
  });

  it("keeps compiler and runtime schema contract checks aligned", () => {
    for (const fixture of schemaContractFixtures) {
      if (fixture.valid) {
        expect(() => validateSchemaContract(fixture.schema, "Memory"), fixture.name).not.toThrow();
      } else {
        expect(() => validateSchemaContract(fixture.schema, "Memory"), fixture.name).toThrow();
      }
    }
  });

  it("rejects validators that modify memory files", () => {
    const statePath = path.join(tempDir, "state.json");
    fs.writeFileSync(statePath, JSON.stringify({ ok: true }));

    const result = runCustomMemoryValidation({
      script: `fs.writeFileSync(path.join(memoryRoot, "state.json"), JSON.stringify({ ok: false }));`,
      memoryDir: tempDir,
      memoryId: "default",
      kind: "cache",
      timeoutSeconds: 5,
    });

    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("must not modify memory files");
  });

  it("keeps the working directory intact when a validator removes its memory root", () => {
    const statePath = path.join(tempDir, "state.json");
    fs.writeFileSync(statePath, JSON.stringify({ ok: true }));

    const result = runCustomMemoryValidation({
      script: `fs.rmSync(memoryRoot, { recursive: true, force: true });`,
      memoryDir: tempDir,
      memoryId: "default",
      kind: "repo",
      timeoutSeconds: 5,
    });

    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("Unable to snapshot memory after custom validation");
    expect(fs.existsSync(tempDir)).toBe(true);
    expect(fs.readFileSync(statePath, "utf8")).toBe(JSON.stringify({ ok: true }));
  });

  it("times out long-running validators", () => {
    const result = runCustomMemoryValidation({
      script: "while (true) {}",
      memoryDir: tempDir,
      memoryId: "default",
      kind: "repo",
      timeoutSeconds: 1,
    });

    expect(result.ok).toBe(false);
    expect(result.timedOut).toBe(true);
  });

  it("formats JSON before validation can inspect it", () => {
    const file = path.join(tempDir, "state.json");
    fs.writeFileSync(file, '{"b":2,"a":1}');

    const formatted = formatJSONFiles(tempDir, 1024);

    expect(formatted).toEqual(["state.json"]);
    expect(fs.readFileSync(file, "utf8")).toBe('{\n  "b": 2,\n  "a": 1\n}\n');
  });

  it("writes and clears validation markers", () => {
    const marker = writeValidationMarker("repo", "default");
    expect(marker).toBe(getValidationMarkerPath("repo", "default"));
    expect(fs.existsSync(marker)).toBe(true);

    clearValidationMarker("repo", "default");

    expect(fs.existsSync(marker)).toBe(false);
  });
});
