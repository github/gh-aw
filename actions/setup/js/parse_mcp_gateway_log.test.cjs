// @ts-check
/// <reference types="@actions/github-script" />

const { generateGatewayLogSummary, generatePlainTextGatewaySummary, generatePlainTextLegacySummary, printAllGatewayFiles } = require("./parse_mcp_gateway_log.cjs");

describe("parse_mcp_gateway_log", () => {
  // Note: The main() function now checks for gateway.md first before falling back to log files.
  // If gateway.md exists, its content is written directly to the step summary.
  // These tests focus on the fallback generateGatewayLogSummary function used when gateway.md is not present.

  describe("generatePlainTextGatewaySummary", () => {
    test("generates plain text summary from markdown content", () => {
      const gatewayMdContent = `<details>
<summary>MCP Gateway Summary</summary>

**Statistics**

| Metric | Count |
|--------|-------|
| Requests | 42 |

**Details**

Some *italic* and **bold** text with \`code\`.

[Link text](http://example.com)

\`\`\`json
{"key": "value"}
\`\`\`

</details>`;

      const summary = generatePlainTextGatewaySummary(gatewayMdContent);

      expect(summary).toContain("=== MCP Gateway Logs ===");
      expect(summary).toContain("MCP Gateway Summary");
      expect(summary).toContain("Statistics");
      expect(summary).toContain("Requests");
      expect(summary).toContain("42");
      expect(summary).toContain("Details");
      expect(summary).toContain("Some italic and bold text with code");
      expect(summary).toContain("Link text");
      expect(summary).toContain('{"key": "value"}');

      // Should not contain markdown syntax
      expect(summary).not.toContain("<details>");
      expect(summary).not.toContain("**bold**");
      expect(summary).not.toContain("*italic*");
      expect(summary).not.toContain("`code`");
      expect(summary).not.toContain("[Link");
    });

    test("handles empty markdown content", () => {
      const summary = generatePlainTextGatewaySummary("");

      expect(summary).toContain("=== MCP Gateway Logs ===");
    });

    test("handles markdown with code blocks", () => {
      const gatewayMdContent = `\`\`\`bash
echo "Hello World"
\`\`\``;

      const summary = generatePlainTextGatewaySummary(gatewayMdContent);

      expect(summary).toContain('echo "Hello World"');
      expect(summary).not.toContain("```");
    });

    test("handles markdown with multiple sections", () => {
      const gatewayMdContent = `# Heading 1

## Heading 2

### Heading 3

Some content here.`;

      const summary = generatePlainTextGatewaySummary(gatewayMdContent);

      expect(summary).toContain("Heading 1");
      expect(summary).toContain("Heading 2");
      expect(summary).toContain("Heading 3");
      expect(summary).toContain("Some content here.");
      expect(summary).not.toContain("#");
    });
  });

  describe("generatePlainTextLegacySummary", () => {
    test("generates summary with both gateway.log and stderr.log", () => {
      const gatewayLogContent = "Gateway started\nServer listening on port 8080";
      const stderrLogContent = "Debug: connection accepted\nDebug: request processed";

      const summary = generatePlainTextLegacySummary(gatewayLogContent, stderrLogContent);

      expect(summary).toContain("=== MCP Gateway Logs ===");
      expect(summary).toContain("Gateway Log (gateway.log):");
      expect(summary).toContain("Gateway started");
      expect(summary).toContain("Server listening on port 8080");
      expect(summary).toContain("Gateway Log (stderr.log):");
      expect(summary).toContain("Debug: connection accepted");
      expect(summary).toContain("Debug: request processed");
    });

    test("generates summary with only gateway.log content", () => {
      const gatewayLogContent = "Gateway started\nServer ready";
      const stderrLogContent = "";

      const summary = generatePlainTextLegacySummary(gatewayLogContent, stderrLogContent);

      expect(summary).toContain("=== MCP Gateway Logs ===");
      expect(summary).toContain("Gateway Log (gateway.log):");
      expect(summary).toContain("Gateway started");
      expect(summary).not.toContain("Gateway Log (stderr.log):");
    });

    test("generates summary with only stderr.log content", () => {
      const gatewayLogContent = "";
      const stderrLogContent = "Error: connection failed\nRetrying...";

      const summary = generatePlainTextLegacySummary(gatewayLogContent, stderrLogContent);

      expect(summary).toContain("=== MCP Gateway Logs ===");
      expect(summary).not.toContain("Gateway Log (gateway.log):");
      expect(summary).toContain("Gateway Log (stderr.log):");
      expect(summary).toContain("Error: connection failed");
    });

    test("handles empty log content for both files", () => {
      const gatewayLogContent = "";
      const stderrLogContent = "";

      const summary = generatePlainTextLegacySummary(gatewayLogContent, stderrLogContent);

      expect(summary).toContain("=== MCP Gateway Logs ===");
    });

    test("trims whitespace from log content", () => {
      const gatewayLogContent = "\n\n  Gateway log with whitespace  \n\n";
      const stderrLogContent = "\n\n  Stderr log with whitespace  \n\n";

      const summary = generatePlainTextLegacySummary(gatewayLogContent, stderrLogContent);

      expect(summary).toContain("Gateway log with whitespace");
      expect(summary).toContain("Stderr log with whitespace");
      expect(summary).not.toContain("\n\n  Gateway log");
      expect(summary).not.toContain("\n\n  Stderr log");
    });
  });

  describe("generateGatewayLogSummary", () => {
    test("generates summary with both gateway.log and stderr.log", () => {
      const gatewayLogContent = "Gateway started\nServer listening on port 8080";
      const stderrLogContent = "Debug: connection accepted\nDebug: request processed";

      const summary = generateGatewayLogSummary(gatewayLogContent, stderrLogContent);

      // Check gateway.log section
      expect(summary).toContain("<summary>MCP Gateway Log (gateway.log)</summary>");
      expect(summary).toContain("Gateway started");
      expect(summary).toContain("Server listening on port 8080");

      // Check stderr.log section
      expect(summary).toContain("<summary>MCP Gateway Log (stderr.log)</summary>");
      expect(summary).toContain("Debug: connection accepted");
      expect(summary).toContain("Debug: request processed");

      // Check structure
      expect(summary).toContain("<details>");
      expect(summary).toContain("```");
      expect(summary).toContain("</details>");
    });

    test("generates summary with only gateway.log content", () => {
      const gatewayLogContent = "Gateway started\nServer ready";
      const stderrLogContent = "";

      const summary = generateGatewayLogSummary(gatewayLogContent, stderrLogContent);

      expect(summary).toContain("<summary>MCP Gateway Log (gateway.log)</summary>");
      expect(summary).toContain("Gateway started");
      expect(summary).not.toContain("<summary>MCP Gateway Log (stderr.log)</summary>");
    });

    test("generates summary with only stderr.log content", () => {
      const gatewayLogContent = "";
      const stderrLogContent = "Error: connection failed\nRetrying...";

      const summary = generateGatewayLogSummary(gatewayLogContent, stderrLogContent);

      expect(summary).not.toContain("<summary>MCP Gateway Log (gateway.log)</summary>");
      expect(summary).toContain("<summary>MCP Gateway Log (stderr.log)</summary>");
      expect(summary).toContain("Error: connection failed");
    });

    test("handles empty log content for both files", () => {
      const gatewayLogContent = "";
      const stderrLogContent = "";

      const summary = generateGatewayLogSummary(gatewayLogContent, stderrLogContent);

      expect(summary).toBe("");
    });

    test("trims whitespace from log content", () => {
      const gatewayLogContent = "\n\n  Gateway log with whitespace  \n\n";
      const stderrLogContent = "\n\n  Stderr log with whitespace  \n\n";

      const summary = generateGatewayLogSummary(gatewayLogContent, stderrLogContent);

      expect(summary).toContain("Gateway log with whitespace");
      expect(summary).toContain("Stderr log with whitespace");
      expect(summary).not.toContain("\n\n  Gateway log");
      expect(summary).not.toContain("\n\n  Stderr log");
    });

    test("preserves internal line breaks", () => {
      const gatewayLogContent = "Line 1\nLine 2\nLine 3";
      const stderrLogContent = "Error 1\nError 2";

      const summary = generateGatewayLogSummary(gatewayLogContent, stderrLogContent);

      const lines = summary.split("\n");

      // Find gateway.log code block - look for summary line with gateway.log
      const gatewaySummaryIndex = lines.findIndex(line => line.includes("gateway.log"));
      expect(gatewaySummaryIndex).toBeGreaterThan(-1);

      // Find the code block start after the gateway summary
      const gatewayCodeBlockIndex = lines.findIndex((line, index) => index > gatewaySummaryIndex && line === "```");
      expect(gatewayCodeBlockIndex).toBeGreaterThan(-1);

      // Find stderr.log code block - look for summary line with stderr.log
      const stderrSummaryIndex = lines.findIndex(line => line.includes("stderr.log"));
      expect(stderrSummaryIndex).toBeGreaterThan(-1);

      // Find the code block start after the stderr summary
      const stderrCodeBlockIndex = lines.findIndex((line, index) => index > stderrSummaryIndex && line === "```");
      expect(stderrCodeBlockIndex).toBeGreaterThan(-1);

      // Verify both sections exist and contain content
      expect(summary).toContain("Line 1");
      expect(summary).toContain("Line 2");
      expect(summary).toContain("Line 3");
      expect(summary).toContain("Error 1");
      expect(summary).toContain("Error 2");
    });
  });

  describe("main function behavior", () => {
    // These tests verify that when gateway.md exists, gateway.log is printed to core.info
    // and gateway.md is printed to step summary
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    test("when gateway.md and gateway.log both exist, prints gateway.log to core.info", async () => {
      // Create a temporary directory for test files
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const gatewayMdPath = path.join(tmpDir, "gateway.md");
      const gatewayLogPath = path.join(tmpDir, "gateway.log");

      try {
        // Write test files
        fs.writeFileSync(gatewayMdPath, "# Gateway Summary\n\nSome markdown content");
        fs.writeFileSync(gatewayLogPath, "Gateway log line 1\nGateway log line 2");

        // Mock core and fs for the test
        const mockCore = {
          info: vi.fn(),
          startGroup: vi.fn(),
          endGroup: vi.fn(),
          notice: vi.fn(),
          warning: vi.fn(),
          error: vi.fn(),
          setFailed: vi.fn(),
          summary: {
            addRaw: vi.fn().mockReturnThis(),
            write: vi.fn(),
          },
        };

        // Mock fs.existsSync and fs.readFileSync to use our test files
        const originalExistsSync = fs.existsSync;
        const originalReadFileSync = fs.readFileSync;

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") return true;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.log") return true;
          return originalExistsSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") {
            return fs.readFileSync(gatewayMdPath, encoding);
          }
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.log") {
            return fs.readFileSync(gatewayLogPath, encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        // Make core available globally for the test
        global.core = mockCore;

        // Run the main function
        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();

        // Verify gateway.log was printed to core.info
        const infoCall = mockCore.info.mock.calls.find(call => call[0].includes("Gateway log line 1"));
        expect(infoCall).toBeDefined();
        expect(infoCall[0]).toContain("Gateway log line 1");
        expect(infoCall[0]).toContain("Gateway log line 2");

        // Verify gateway.md was written to step summary
        expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Gateway Summary"));
        expect(mockCore.summary.write).toHaveBeenCalled();

        // Restore original functions
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
      } finally {
        // Clean up test files
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });
  });

  describe("printAllGatewayFiles", () => {
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    test("prints all files in gateway directories with content", () => {
      // Create a temporary directory structure
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const logsDir = path.join(tmpDir, "mcp-logs");

      try {
        // Create directory structure
        fs.mkdirSync(logsDir, { recursive: true });

        // Create test files
        fs.writeFileSync(path.join(logsDir, "gateway.log"), "Gateway log content\nLine 2");
        fs.writeFileSync(path.join(logsDir, "stderr.log"), "Error message");
        fs.writeFileSync(path.join(logsDir, "gateway.md"), "# Gateway Summary");

        // Mock core
        const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn() };
        global.core = mockCore;

        // Mock fs to redirect to our test directories
        const originalExistsSync = fs.existsSync;
        const originalReaddirSync = fs.readdirSync;
        const originalStatSync = fs.statSync;
        const originalReadFileSync = fs.readFileSync;

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs") return true;
          return originalExistsSync(filepath);
        });

        fs.readdirSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs") return originalReaddirSync(logsDir);
          return originalReaddirSync(filepath);
        });

        fs.statSync = vi.fn(filepath => {
          if (filepath.startsWith("/tmp/gh-aw/mcp-logs/")) {
            const filename = filepath.replace("/tmp/gh-aw/mcp-logs/", "");
            return originalStatSync(path.join(logsDir, filename));
          }
          return originalStatSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath.startsWith("/tmp/gh-aw/mcp-logs/")) {
            const filename = filepath.replace("/tmp/gh-aw/mcp-logs/", "");
            return originalReadFileSync(path.join(logsDir, filename), encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        // Call the function
        printAllGatewayFiles();

        // Verify the output
        const infoMessages = mockCore.info.mock.calls.map(call => call[0]);
        const allOutput = infoMessages.join("\n");
        const startGroupCalls = mockCore.startGroup.mock.calls.map(call => call[0]);
        const allGroups = startGroupCalls.join("\n");

        // Check header group was started
        expect(allGroups).toContain("=== Listing All Gateway-Related Files ===");

        // Check directories are listed
        expect(allGroups).toContain("/tmp/gh-aw/mcp-logs");

        // Check files are listed
        expect(allOutput).toContain("gateway.log");
        expect(allOutput).toContain("stderr.log");
        expect(allOutput).toContain("gateway.md");

        // Check file contents are printed for .log files
        expect(allOutput).toContain("Gateway log content");
        expect(allOutput).toContain("Error message");
        
        // Check .md file is listed but content is not displayed
        expect(allOutput).toContain("gateway.md");
        expect(allOutput).toContain("content not displayed for .md files");
        expect(allOutput).not.toContain("# Gateway Summary");

        // Restore original functions
        fs.existsSync = originalExistsSync;
        fs.readdirSync = originalReaddirSync;
        fs.statSync = originalStatSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
      } finally {
        // Clean up test files
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("handles missing directories gracefully", () => {
      // Mock core
      const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), notice: vi.fn(), warning: vi.fn(), error: vi.fn() };
      global.core = mockCore;

      // Mock fs to return false for directory existence
      const fs = require("fs");
      const originalExistsSync = fs.existsSync;

      fs.existsSync = vi.fn(() => false);

      try {
        // Call the function
        printAllGatewayFiles();

        // Verify the output
        const noticeMessages = mockCore.notice.mock.calls.map(call => call[0]);
        const allOutput = noticeMessages.join("\n");

        // Check that it reports missing directories
        expect(allOutput).toContain("Directory does not exist");
      } finally {
        // Restore original functions
        fs.existsSync = originalExistsSync;
        delete global.core;
      }
    });

    test("handles empty directories", () => {
      const fs = require("fs");
      const path = require("path");
      const os = require("os");

      // Create empty directories
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const logsDir = path.join(tmpDir, "mcp-logs");

      try {
        fs.mkdirSync(logsDir, { recursive: true });

        // Mock core
        const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), notice: vi.fn(), warning: vi.fn(), error: vi.fn() };
        global.core = mockCore;

        // Mock fs to use our test directories
        const originalExistsSync = fs.existsSync;
        const originalReaddirSync = fs.readdirSync;

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs") return true;
          return originalExistsSync(filepath);
        });

        fs.readdirSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs") return originalReaddirSync(logsDir);
          return originalReaddirSync(filepath);
        });

        // Call the function
        printAllGatewayFiles();

        // Verify the output
        const infoMessages = mockCore.info.mock.calls.map(call => call[0]);
        const allOutput = infoMessages.join("\n");

        // Check that it reports empty directories
        expect(allOutput).toContain("(empty directory)");

        // Restore original functions
        fs.existsSync = originalExistsSync;
        fs.readdirSync = originalReaddirSync;
        delete global.core;
      } finally {
        // Clean up
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("truncates files larger than 64KB", () => {
      const fs = require("fs");
      const path = require("path");
      const os = require("os");

      // Create test directory
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const logsDir = path.join(tmpDir, "mcp-logs");

      try {
        fs.mkdirSync(logsDir, { recursive: true });

        // Create a large file (70KB)
        const largeContent = "A".repeat(70 * 1024);
        fs.writeFileSync(path.join(logsDir, "large.log"), largeContent);

        // Mock core
        const mockCore = { info: vi.fn(), startGroup: vi.fn(), endGroup: vi.fn(), notice: vi.fn(), warning: vi.fn(), error: vi.fn() };
        global.core = mockCore;

        // Mock fs to use our test directories
        const originalExistsSync = fs.existsSync;
        const originalReaddirSync = fs.readdirSync;
        const originalStatSync = fs.statSync;
        const originalReadFileSync = fs.readFileSync;

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs") return true;
          return originalExistsSync(filepath);
        });

        fs.readdirSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs") return originalReaddirSync(logsDir);
          return originalReaddirSync(filepath);
        });

        fs.statSync = vi.fn(filepath => {
          if (filepath.startsWith("/tmp/gh-aw/mcp-logs/")) {
            const filename = filepath.replace("/tmp/gh-aw/mcp-logs/", "");
            return originalStatSync(path.join(logsDir, filename));
          }
          return originalStatSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath.startsWith("/tmp/gh-aw/mcp-logs/")) {
            const filename = filepath.replace("/tmp/gh-aw/mcp-logs/", "");
            return originalReadFileSync(path.join(logsDir, filename), encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        // Call the function
        printAllGatewayFiles();

        // Verify the output
        const infoMessages = mockCore.info.mock.calls.map(call => call[0]);
        const allOutput = infoMessages.join("\n");

        // Check that file was truncated
        expect(allOutput).toContain("...");
        expect(allOutput).toContain("truncated");
        expect(allOutput).toContain("65536 bytes");
        expect(allOutput).toContain("71680 total");

        // Restore original functions
        fs.existsSync = originalExistsSync;
        fs.readdirSync = originalReaddirSync;
        fs.statSync = originalStatSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
      } finally {
        // Clean up
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });
  });
});
