import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";

// Mock the global core object that GitHub Actions provides
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

// Set up global variables
global.core = mockCore;

describe("load_agent_output.cjs", () => {
  let loadAgentOutputModule;
  let tempDir;
  let originalEnv;

  beforeEach(async () => {
    // Save original environment
    originalEnv = process.env.GH_AW_AGENT_OUTPUT;

    // Create temp directory for test files
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "load-agent-output-test-"));

    // Clear all mocks
    vi.clearAllMocks();

    // Dynamically import the module (fresh for each test)
    loadAgentOutputModule = await import("./load_agent_output.cjs");
  });

  afterEach(() => {
    // Restore original environment
    if (originalEnv !== undefined) {
      process.env.GH_AW_AGENT_OUTPUT = originalEnv;
    } else {
      delete process.env.GH_AW_AGENT_OUTPUT;
    }

    // Clean up temp directory
    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  describe("loadAgentOutput", () => {
    it("should return success: false when GH_AW_AGENT_OUTPUT is not set", () => {
      delete process.env.GH_AW_AGENT_OUTPUT;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(false);
      expect(result.items).toBeUndefined();
      expect(mockCore.info).toHaveBeenCalledWith("No GH_AW_AGENT_OUTPUT environment variable found");
    });

    it("should return success: false and log info when file cannot be read", () => {
      process.env.GH_AW_AGENT_OUTPUT = "/nonexistent/file.json";

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(false);
      expect(result.error).toMatch(/Error reading agent output file/);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Error reading agent output file"));
    });

    it("should return success: false when file content is empty", () => {
      const emptyFile = path.join(tempDir, "empty.json");
      fs.writeFileSync(emptyFile, "");
      process.env.GH_AW_AGENT_OUTPUT = emptyFile;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(false);
      expect(result.items).toBeUndefined();
      expect(mockCore.info).toHaveBeenCalledWith("Agent output content is empty");
    });

    it("should return success: false when file content is only whitespace", () => {
      const whitespaceFile = path.join(tempDir, "whitespace.json");
      fs.writeFileSync(whitespaceFile, "   \n\t   ");
      process.env.GH_AW_AGENT_OUTPUT = whitespaceFile;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(false);
      expect(result.items).toBeUndefined();
      expect(mockCore.info).toHaveBeenCalledWith("Agent output content is empty");
    });

    it("should return success: false and log error when JSON is invalid", () => {
      const invalidJsonFile = path.join(tempDir, "invalid.json");
      const invalidContent = "{ invalid json }";
      fs.writeFileSync(invalidJsonFile, invalidContent);
      process.env.GH_AW_AGENT_OUTPUT = invalidJsonFile;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(false);
      expect(result.error).toMatch(/Error parsing agent output JSON/);
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Error parsing agent output JSON"));
      expect(mockCore.info).toHaveBeenCalledWith(`Failed to parse content:\n${invalidContent}`);
    });

    it("should return success: false when items field is missing", () => {
      const noItemsFile = path.join(tempDir, "no-items.json");
      const content = { other: "data" };
      fs.writeFileSync(noItemsFile, JSON.stringify(content));
      process.env.GH_AW_AGENT_OUTPUT = noItemsFile;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(false);
      expect(result.items).toBeUndefined();
      expect(mockCore.info).toHaveBeenCalledWith("No valid items found in agent output");
      expect(mockCore.info).toHaveBeenCalledWith(`Parsed content: ${JSON.stringify(content)}`);
    });

    it("should return success: false when items field is not an array", () => {
      const invalidItemsFile = path.join(tempDir, "invalid-items.json");
      const content = { items: "not-an-array" };
      fs.writeFileSync(invalidItemsFile, JSON.stringify(content));
      process.env.GH_AW_AGENT_OUTPUT = invalidItemsFile;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(false);
      expect(result.items).toBeUndefined();
      expect(mockCore.info).toHaveBeenCalledWith("No valid items found in agent output");
      expect(mockCore.info).toHaveBeenCalledWith(`Parsed content: ${JSON.stringify(content)}`);
    });

    it("should return success: true with empty items array", () => {
      const emptyItemsFile = path.join(tempDir, "empty-items.json");
      fs.writeFileSync(emptyItemsFile, JSON.stringify({ items: [] }));
      process.env.GH_AW_AGENT_OUTPUT = emptyItemsFile;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(true);
      expect(result.items).toEqual([]);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Agent output content length:"));
    });

    it("should return success: true with valid items", () => {
      const validFile = path.join(tempDir, "valid.json");
      const items = [
        { type: "create_issue", title: "Test Issue" },
        { type: "add_comment", body: "Test Comment" },
      ];
      fs.writeFileSync(validFile, JSON.stringify({ items }));
      process.env.GH_AW_AGENT_OUTPUT = validFile;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(true);
      expect(result.items).toEqual(items);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Agent output content length:"));
    });

    it("preserves validated collector diagnostics metadata", () => {
      const outputFile = path.join(tempDir, "collector-diagnostics.json");
      fs.writeFileSync(
        outputFile,
        JSON.stringify({
          items: [{ type: "report_incomplete", reason: "engine_driver_failure" }],
          collectorFailureCause: "engine_outage",
          collectorDriverExitCode: 139,
          collectorRetryCount: 2,
          collectorEngineErrorType: "sandbox_runtime_crash",
        })
      );
      process.env.GH_AW_AGENT_OUTPUT = outputFile;

      expect(loadAgentOutputModule.loadAgentOutput()).toMatchObject({
        success: true,
        collectorFailureCause: "engine_outage",
        collectorDriverExitCode: 139,
        collectorRetryCount: 2,
        collectorEngineErrorType: "sandbox_runtime_crash",
      });
    });

    it("should log file content length on successful parse", () => {
      const validFile = path.join(tempDir, "valid.json");
      const content = JSON.stringify({ items: [{ type: "test" }] });
      fs.writeFileSync(validFile, content);
      process.env.GH_AW_AGENT_OUTPUT = validFile;

      loadAgentOutputModule.loadAgentOutput();

      expect(mockCore.info).toHaveBeenCalledWith(`Agent output content length: ${content.length}`);
    });

    it("should handle complex nested items structure", () => {
      const complexFile = path.join(tempDir, "complex.json");
      const items = [
        {
          type: "create_issue",
          title: "Complex Issue",
          labels: ["bug", "high-priority"],
          metadata: { nested: { data: "value" } },
        },
      ];
      fs.writeFileSync(complexFile, JSON.stringify({ items }));
      process.env.GH_AW_AGENT_OUTPUT = complexFile;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(true);
      expect(result.items).toEqual(items);
    });

    it("should truncate large content when logging JSON parse failure", () => {
      const largeInvalidFile = path.join(tempDir, "large-invalid.json");
      // Create content larger than MAX_LOG_CONTENT_LENGTH
      const largeContent = "x".repeat(15000);
      fs.writeFileSync(largeInvalidFile, largeContent);
      process.env.GH_AW_AGENT_OUTPUT = largeInvalidFile;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(false);
      expect(result.error).toMatch(/Error parsing agent output JSON/);
      // Verify truncation happened
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("... (truncated, total length: 15000)"));
    });

    it("should truncate large content when logging invalid items structure", () => {
      const largeInvalidItemsFile = path.join(tempDir, "large-invalid-items.json");
      // Create a JSON object with large string that exceeds MAX_LOG_CONTENT_LENGTH when stringified
      const largeData = { other: "x".repeat(15000) };
      fs.writeFileSync(largeInvalidItemsFile, JSON.stringify(largeData));
      process.env.GH_AW_AGENT_OUTPUT = largeInvalidItemsFile;

      const result = loadAgentOutputModule.loadAgentOutput();

      expect(result.success).toBe(false);
      // Verify truncation happened
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("... (truncated, total length:"));
    });
  });

  describe("truncateForLogging", () => {
    it("should not truncate content under the limit", () => {
      const content = "short content";
      const result = loadAgentOutputModule.truncateForLogging(content);
      expect(result).toBe(content);
    });

    it("should truncate content over the limit", () => {
      const content = "x".repeat(15000);
      const result = loadAgentOutputModule.truncateForLogging(content);

      expect(result.length).toBeLessThan(content.length);
      expect(result).toContain("... (truncated, total length: 15000)");
      expect(result.startsWith("x".repeat(loadAgentOutputModule.MAX_LOG_CONTENT_LENGTH))).toBe(true);
    });

    it("should not truncate content exactly at the limit", () => {
      const content = "x".repeat(loadAgentOutputModule.MAX_LOG_CONTENT_LENGTH);
      const result = loadAgentOutputModule.truncateForLogging(content);
      expect(result).toBe(content);
    });
  });
});
