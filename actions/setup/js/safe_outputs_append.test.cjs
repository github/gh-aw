// @ts-check
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import { createAppendFunction } from "./safe_outputs_append.cjs";

describe("safe_outputs_append", () => {
  let testOutputFile;
  let appendFn;

  beforeEach(() => {
    // Use unique path for each test
    const testId = Math.random().toString(36).substring(7);
    testOutputFile = `/tmp/test-safe-outputs-append-${testId}/outputs.jsonl`;

    const outputDir = path.dirname(testOutputFile);
    fs.mkdirSync(outputDir, { recursive: true });

    appendFn = createAppendFunction(testOutputFile);
  });

  afterEach(() => {
    // Clean up test files
    try {
      if (fs.existsSync(testOutputFile)) {
        fs.unlinkSync(testOutputFile);
      }
      const testDir = path.dirname(testOutputFile);
      if (fs.existsSync(testDir)) {
        fs.rmSync(testDir, { recursive: true, force: true });
      }
    } catch (error) {
      // Ignore cleanup errors
    }
  });

  describe("createAppendFunction", () => {
    it("should append entry to output file", () => {
      const entry = {
        type: "test-type",
        data: "test-data",
      };

      appendFn(entry);

      const content = fs.readFileSync(testOutputFile, "utf8");
      const lines = content.trim().split("\n");
      expect(lines).toHaveLength(1);

      const parsedEntry = JSON.parse(lines[0]);
      expect(parsedEntry).toEqual({
        type: "test_type",
        data: "test-data",
      });
    });

    it("should normalize dashes to underscores in type", () => {
      const entry = {
        type: "create-pull-request",
        title: "Test PR",
      };

      appendFn(entry);

      const content = fs.readFileSync(testOutputFile, "utf8");
      const parsedEntry = JSON.parse(content.trim());
      expect(parsedEntry.type).toBe("create_pull_request");
    });

    it("should append multiple entries", () => {
      const entries = [
        { type: "type-1", data: "data-1" },
        { type: "type-2", data: "data-2" },
        { type: "type-3", data: "data-3" },
      ];

      entries.forEach(entry => appendFn(entry));

      const content = fs.readFileSync(testOutputFile, "utf8");
      const lines = content.trim().split("\n");
      expect(lines).toHaveLength(3);

      const parsedEntries = lines.map(line => JSON.parse(line));
      expect(parsedEntries[0].type).toBe("type_1");
      expect(parsedEntries[1].type).toBe("type_2");
      expect(parsedEntries[2].type).toBe("type_3");
    });

    it("should preserve entry fields", () => {
      const entry = {
        type: "upload-asset",
        path: "/test/file.png",
        sha: "abc123",
        size: 1024,
        url: "https://example.com/file.png",
      };

      appendFn(entry);

      const content = fs.readFileSync(testOutputFile, "utf8");
      const parsedEntry = JSON.parse(content.trim());
      expect(parsedEntry).toEqual({
        type: "upload_asset",
        path: "/test/file.png",
        sha: "abc123",
        size: 1024,
        url: "https://example.com/file.png",
      });
    });

    it("should throw error when output file is not configured", () => {
      const badAppendFn = createAppendFunction("");
      const entry = { type: "test", data: "test" };

      expect(() => badAppendFn(entry)).toThrow("No output file configured");
    });

    it("should throw error when write fails", () => {
      // Create a directory where the file should be (will cause write to fail)
      fs.mkdirSync(testOutputFile, { recursive: true });

      const entry = { type: "test", data: "test" };

      expect(() => appendFn(entry)).toThrow("Failed to write to output file");

      // Clean up the directory
      fs.rmSync(testOutputFile, { recursive: true, force: true });
    });

    it("should handle entries with nested objects", () => {
      const entry = {
        type: "complex-type",
        nested: {
          field1: "value1",
          field2: { subfield: "value2" },
        },
      };

      appendFn(entry);

      const content = fs.readFileSync(testOutputFile, "utf8");
      const parsedEntry = JSON.parse(content.trim());
      expect(parsedEntry.nested).toEqual({
        field1: "value1",
        field2: { subfield: "value2" },
      });
    });

    it("should handle entries with arrays", () => {
      const entry = {
        type: "array-type",
        items: ["item1", "item2", "item3"],
      };

      appendFn(entry);

      const content = fs.readFileSync(testOutputFile, "utf8");
      const parsedEntry = JSON.parse(content.trim());
      expect(parsedEntry.items).toEqual(["item1", "item2", "item3"]);
    });

    it("should create file if it doesn't exist", () => {
      expect(fs.existsSync(testOutputFile)).toBe(false);

      const entry = { type: "test", data: "test" };
      appendFn(entry);

      expect(fs.existsSync(testOutputFile)).toBe(true);
    });

    it("should append to existing file", () => {
      // Write initial entry
      fs.writeFileSync(testOutputFile, JSON.stringify({ type: "initial", data: "initial" }) + "\n");

      // Append new entry
      const entry = { type: "new", data: "new" };
      appendFn(entry);

      const content = fs.readFileSync(testOutputFile, "utf8");
      const lines = content.trim().split("\n");
      expect(lines).toHaveLength(2);
    });

    it.skipIf(process.platform !== "linux").each([false, true])("keeps the output file owner-only: %s", existing => {
      if (existing) {
        fs.writeFileSync(testOutputFile, "existing\n");
        fs.chmodSync(testOutputFile, 0o666);
      }

      appendFn({ type: "private-output", data: true });

      expect(fs.statSync(testOutputFile).mode & 0o777).toBe(0o600);
    });

    it.skipIf(process.platform !== "linux")("does not follow a symlink when appending output", () => {
      const target = `${testOutputFile}.target`;
      fs.writeFileSync(target, "untouched\n");
      fs.symlinkSync(target, testOutputFile);
      try {
        expect(() => appendFn({ type: "private-output", data: true })).toThrow("Failed to write to output file");
        expect(fs.readFileSync(target, "utf8")).toBe("untouched\n");
      } finally {
        fs.unlinkSync(target);
      }
    });

    it("should write single-line JSON without formatting (JSONL format)", () => {
      // Test that JSON is written as a single line, not formatted/pretty-printed
      const entry = {
        type: "complex-type",
        nested: {
          field1: "value1",
          field2: { subfield: "value2" },
        },
        array: ["item1", "item2", "item3"],
      };

      appendFn(entry);

      const content = fs.readFileSync(testOutputFile, "utf8");
      const lines = content.split("\n");

      // Should have exactly 2 lines: one JSON entry + one empty line from trailing \n
      expect(lines).toHaveLength(2);
      expect(lines[1]).toBe("");

      // The first line should be parseable JSON
      const parsed = JSON.parse(lines[0]);
      expect(parsed.type).toBe("complex_type");

      // Verify no internal newlines in the JSON (except the trailing one)
      const firstLine = lines[0];
      expect(firstLine).not.toContain("\n");
      expect(firstLine).not.toContain("\r");

      // Verify the line doesn't have indentation (which would indicate formatting)
      expect(firstLine).not.toMatch(/\n\s+/);
    });

    it("should handle entries with string fields containing newlines", () => {
      // Test that even if entry data contains newlines, the JSON line itself remains single-line
      const entry = {
        type: "text-with-newlines",
        message: "Line 1\nLine 2\nLine 3",
        description: "Multi-line\r\ntext\r\nhere",
      };

      appendFn(entry);

      const content = fs.readFileSync(testOutputFile, "utf8");
      const lines = content.split("\n");

      // Should have exactly 2 lines: one JSON entry + one empty line from trailing \n
      // The newlines in the data should be escaped in JSON, not literal
      expect(lines).toHaveLength(2);
      expect(lines[1]).toBe("");

      // The JSON should be parseable and preserve the newlines in the data
      const parsed = JSON.parse(lines[0]);
      expect(parsed.message).toBe("Line 1\nLine 2\nLine 3");
      expect(parsed.description).toBe("Multi-line\r\ntext\r\nhere");

      // Verify the line contains escaped newlines, not literal ones
      expect(lines[0]).toContain("\\n");
      expect(lines[0]).not.toContain("\nLine 2"); // Should not have literal newline
    });
  });
});
