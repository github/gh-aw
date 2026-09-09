// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const {
  generateGatewayLogSummary,
  generatePlainTextGatewaySummary,
  generatePlainTextLegacySummary,
  parseGatewayJsonlForDifcFiltered,
  parseGatewayJsonlForTokenSteering,
  parseGatewayJsonlForModelAliasResolution,
  generateDifcFilteredSummary,
  generateTokenSteeringSummary,
  generateModelAliasResolutionSummary,
  parseRpcMessagesJsonl,
  getRpcMessageType,
  getRpcRequestLabel,
  generateRpcMessagesSummary,
  printAllGatewayFiles,
  parseTokenUsageJsonl,
  generateTokenUsageSummary,
  formatDurationMs,
  writeStepSummaryWithTokenUsage,
} = require("./parse_mcp_gateway_log.cjs");

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
    // These tests verify that when gateway.md exists, it is written to step summary
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    test("when gateway.md exists, writes it to step summary without gateway.log", async () => {
      // Create a temporary directory for test files
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const gatewayMdPath = path.join(tmpDir, "gateway.md");

      try {
        // Write test file
        fs.writeFileSync(gatewayMdPath, "# Gateway Summary\n\nSome markdown content");

        // Mock core and fs for the test
        const mockCore = {
          info: vi.fn(),
          debug: vi.fn(),
          startGroup: vi.fn(),
          endGroup: vi.fn(),
          notice: vi.fn(),
          warning: vi.fn(),
          error: vi.fn(),
          setFailed: vi.fn(),
          exportVariable: vi.fn(),
          setOutput: vi.fn(),
          summary: {
            addRaw: vi.fn().mockReturnThis(),
            addDetails: vi.fn().mockReturnThis(),
            write: vi.fn(),
          },
        };

        // Mock fs.existsSync and fs.readFileSync to use our test files
        const originalExistsSync = fs.existsSync;
        const originalReadFileSync = fs.readFileSync;

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") return true;
          return originalExistsSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") {
            return fs.readFileSync(gatewayMdPath, encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        // Make core available globally for the test
        global.core = mockCore;

        // Run the main function
        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();

        // Verify gateway.md was written to step summary
        expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Gateway Summary"));
        expect(mockCore.summary.write).toHaveBeenCalled();

        // Verify gateway.log content was NOT printed to core.info
        const infoMessages = mockCore.info.mock.calls.map(call => call[0]).join("\n");
        expect(infoMessages).not.toContain("Gateway log line");

        // Restore original functions
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
      } finally {
        // Clean up test files
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("strips leading null bytes from gateway.md before writing to step summary", async () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const gatewayMdPath = path.join(tmpDir, "gateway.md");

      // Simulate MCPG's behaviour: pre-allocate a fixed-size header region filled
      // with null bytes (1444 bytes, as seen in production runs) before real content.
      const nullBytes = "\x00".repeat(1444);
      const realContent = "- ✓ **backend**\n  Successfully connected to MCP backend server.\n";
      fs.writeFileSync(gatewayMdPath, nullBytes + realContent);

      const mockCore = {
        info: vi.fn(),
        debug: vi.fn(),
        startGroup: vi.fn(),
        endGroup: vi.fn(),
        notice: vi.fn(),
        warning: vi.fn(),
        error: vi.fn(),
        setFailed: vi.fn(),
        exportVariable: vi.fn(),
        setOutput: vi.fn(),
        summary: {
          addRaw: vi.fn().mockReturnThis(),
          addDetails: vi.fn().mockReturnThis(),
          write: vi.fn(),
        },
      };

      const originalExistsSync = fs.existsSync;
      const originalReadFileSync = fs.readFileSync;

      fs.existsSync = vi.fn(filepath => {
        if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") return true;
        return originalExistsSync(filepath);
      });

      fs.readFileSync = vi.fn((filepath, encoding) => {
        if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") {
          return originalReadFileSync(gatewayMdPath, encoding);
        }
        return originalReadFileSync(filepath, encoding);
      });

      global.core = mockCore;

      try {
        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();

        // Step summary must contain the real content without any null bytes
        const addRawCalls = mockCore.summary.addRaw.mock.calls;
        expect(addRawCalls.length).toBeGreaterThan(0);
        const writtenContent = addRawCalls.map(call => call[0]).join("");
        expect(writtenContent).not.toContain("\x00");
        expect(writtenContent).toContain("backend");
      } finally {
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("does not append token usage details when token usage file exists", async () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const gatewayMdPath = path.join(tmpDir, "gateway.md");
      const tokenUsagePath = path.join(tmpDir, "token-usage.jsonl");
      const originalExistsSync = fs.existsSync;
      const originalReadFileSync = fs.readFileSync;

      try {
        fs.writeFileSync(gatewayMdPath, "# Gateway Summary\n\nSome markdown content");
        fs.writeFileSync(
          tokenUsagePath,
          JSON.stringify({
            model: "claude-haiku-4-5-20251001",
            input_tokens: 42,
            output_tokens: 2765,
            cache_read_tokens: 141738,
            cache_write_tokens: 38170,
            duration_ms: 26500,
          })
        );

        const mockCore = {
          info: vi.fn(),
          debug: vi.fn(),
          startGroup: vi.fn(),
          endGroup: vi.fn(),
          notice: vi.fn(),
          warning: vi.fn(),
          error: vi.fn(),
          setFailed: vi.fn(),
          exportVariable: vi.fn(),
          setOutput: vi.fn(),
          summary: {
            addRaw: vi.fn().mockReturnThis(),
            addDetails: vi.fn().mockReturnThis(),
            write: vi.fn(),
          },
        };

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") return true;
          if (filepath === "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl") return true;
          return originalExistsSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") {
            return originalReadFileSync(gatewayMdPath, encoding);
          }
          if (filepath === "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl") {
            return originalReadFileSync(tokenUsagePath, encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        global.core = mockCore;

        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();

        expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Gateway Summary"));
        expect(mockCore.summary.addDetails).not.toHaveBeenCalledWith("Token Usage", expect.any(String));
        expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", expect.any(String));
        expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AMBIENT_CONTEXT", expect.any(String));
        expect(mockCore.summary.write).toHaveBeenCalled();
      } finally {
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("appends steering from rpc-messages.jsonl after gateway.md", async () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const gatewayMdPath = path.join(tmpDir, "gateway.md");
      const rpcMessagesPath = path.join(tmpDir, "rpc-messages.jsonl");
      const originalExistsSync = fs.existsSync;
      const originalReadFileSync = fs.readFileSync;

      try {
        fs.writeFileSync(gatewayMdPath, "# Gateway Summary\n\nSome markdown content");
        fs.writeFileSync(
          rpcMessagesPath,
          JSON.stringify({
            timestamp: "2026-03-18T17:30:01.123456789Z",
            type: "token_steering",
            request_id: "req-124",
            provider: "copilot",
            message: "[AWF TOKEN WARNING] You have used 95% of your effective token budget. Finalize and submit your work now.",
          })
        );

        const mockCore = {
          info: vi.fn(),
          debug: vi.fn(),
          startGroup: vi.fn(),
          endGroup: vi.fn(),
          notice: vi.fn(),
          warning: vi.fn(),
          error: vi.fn(),
          setFailed: vi.fn(),
          exportVariable: vi.fn(),
          setOutput: vi.fn(),
          summary: {
            addRaw: vi.fn().mockReturnThis(),
            addDetails: vi.fn().mockReturnThis(),
            write: vi.fn(),
          },
        };

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") return true;
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") return true;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.jsonl") return false;
          return originalExistsSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") {
            return originalReadFileSync(gatewayMdPath, encoding);
          }
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") {
            return originalReadFileSync(rpcMessagesPath, encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        global.core = mockCore;

        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();

        const summaryCalls = mockCore.summary.addRaw.mock.calls.map(call => call[0]);
        expect(summaryCalls[0]).toContain("Gateway Summary");
        expect(summaryCalls.join("\n")).toContain("Token Steering Events (1)");
        expect(summaryCalls.join("\n")).toContain("req-124");
        expect(mockCore.summary.write).toHaveBeenCalled();
      } finally {
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("renders token steering from rpc-messages.jsonl when gateway.md is absent", async () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const rpcMessagesPath = path.join(tmpDir, "rpc-messages.jsonl");
      const originalExistsSync = fs.existsSync;
      const originalReadFileSync = fs.readFileSync;

      try {
        fs.writeFileSync(
          rpcMessagesPath,
          [
            JSON.stringify({
              timestamp: "2026-03-18T17:30:00.123456789Z",
              direction: "OUT",
              type: "REQUEST",
              server_id: "github",
              payload: { method: "tools/call", params: { name: "list_issues", arguments: {} } },
            }),
            JSON.stringify({
              timestamp: "2026-03-18T17:30:01.123456789Z",
              type: "token_steering",
              request_id: "req-123",
              provider: "copilot",
              message: "[AWF TOKEN WARNING] You have used 90% of your effective token budget. Complete your current task and prepare final output.",
            }),
          ].join("\n")
        );

        const mockCore = {
          info: vi.fn(),
          debug: vi.fn(),
          startGroup: vi.fn(),
          endGroup: vi.fn(),
          notice: vi.fn(),
          warning: vi.fn(),
          error: vi.fn(),
          setFailed: vi.fn(),
          exportVariable: vi.fn(),
          setOutput: vi.fn(),
          summary: {
            addRaw: vi.fn().mockReturnThis(),
            addDetails: vi.fn().mockReturnThis(),
            write: vi.fn(),
          },
        };

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") return true;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") return false;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.jsonl") return false;
          return originalExistsSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") {
            return originalReadFileSync(rpcMessagesPath, encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        global.core = mockCore;

        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();

        const summaryOutput = mockCore.summary.addRaw.mock.calls.map(call => call[0]).join("\n");
        expect(summaryOutput).toContain("MCP Gateway Activity (1 request, 1 token_steering)");
        expect(summaryOutput).toContain("Token Steering Events (1)");
        expect(summaryOutput).toContain("req-123");
        expect(summaryOutput).toContain("[AWF TOKEN WARNING]");
        expect(mockCore.summary.write).toHaveBeenCalled();
      } finally {
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("renders model alias resolution events from rpc-messages.jsonl when gateway.md and gateway.jsonl are absent", async () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const rpcMessagesPath = path.join(tmpDir, "rpc-messages.jsonl");
      const originalExistsSync = fs.existsSync;
      const originalReadFileSync = fs.readFileSync;

      try {
        fs.writeFileSync(
          rpcMessagesPath,
          [
            JSON.stringify({
              timestamp: "2026-03-18T17:30:00.123456789Z",
              direction: "OUT",
              type: "REQUEST",
              server_id: "github",
              payload: { method: "tools/call", params: { name: "list_issues", arguments: {} } },
            }),
            JSON.stringify({
              timestamp: "2026-03-18T17:30:01.123456789Z",
              event: "MODEL_ALIAS_REWRITE",
              data: {
                request_id: "req-900",
                provider: "copilot",
                original_model: "sonnet",
                resolved_model: "claude-sonnet-4.6",
                fallback_index: 0,
              },
            }),
          ].join("\n")
        );

        const mockCore = {
          info: vi.fn(),
          debug: vi.fn(),
          startGroup: vi.fn(),
          endGroup: vi.fn(),
          notice: vi.fn(),
          warning: vi.fn(),
          error: vi.fn(),
          setFailed: vi.fn(),
          exportVariable: vi.fn(),
          setOutput: vi.fn(),
          summary: {
            addRaw: vi.fn().mockReturnThis(),
            addDetails: vi.fn().mockReturnThis(),
            write: vi.fn(),
          },
        };

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") return true;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") return false;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.jsonl") return false;
          return originalExistsSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") {
            return originalReadFileSync(rpcMessagesPath, encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        global.core = mockCore;

        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();

        const summaryOutput = mockCore.summary.addRaw.mock.calls.map(call => call[0]).join("\n");
        expect(summaryOutput).toContain("Model Alias Resolution Events (1)");
        expect(summaryOutput).toContain("| Time | Provider | Request ID | Alias | Resolved model |");
        expect(summaryOutput).toContain("req-900");
        expect(summaryOutput).toContain("sonnet");
        expect(summaryOutput).toContain("claude-sonnet-4.6");
        expect(summaryOutput).toContain('"event": "MODEL_ALIAS_REWRITE"');
        expect(mockCore.summary.write).toHaveBeenCalled();
      } finally {
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("warns and continues when rpc-messages.jsonl is zero bytes", async () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const rpcMessagesPath = path.join(tmpDir, "rpc-messages.jsonl");
      const originalExistsSync = fs.existsSync;
      const originalReadFileSync = fs.readFileSync;

      try {
        fs.writeFileSync(rpcMessagesPath, "");

        const mockCore = {
          info: vi.fn(),
          debug: vi.fn(),
          startGroup: vi.fn(),
          endGroup: vi.fn(),
          notice: vi.fn(),
          warning: vi.fn(),
          error: vi.fn(),
          setFailed: vi.fn(),
          exportVariable: vi.fn(),
          setOutput: vi.fn(),
          summary: {
            addRaw: vi.fn().mockReturnThis(),
            addDetails: vi.fn().mockReturnThis(),
            write: vi.fn(),
          },
        };

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") return true;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") return false;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.jsonl") return false;
          return originalExistsSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") {
            return originalReadFileSync(rpcMessagesPath, encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        global.core = mockCore;

        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();

        expect(mockCore.warning).toHaveBeenCalledWith("rpc-messages.jsonl is present but zero bytes; continuing without RPC summary");
        expect(mockCore.setFailed).not.toHaveBeenCalled();
        expect(mockCore.summary.write).toHaveBeenCalled();
      } finally {
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("does not false-positive ai_credits_rate_limit_error from rpc-messages.jsonl content with rate-limit branch names and ai-credits commit messages", async () => {
      // Regression test: rpc-messages.jsonl contains MCP tool call responses that include
      // arbitrary repository data (branch names, commit messages) which can contain keywords
      // like "rate-limit" and "ai-credits". These must NOT be treated as real rate limit errors.
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const rpcMessagesPath = path.join(tmpDir, "rpc-messages.jsonl");
      const originalExistsSync = fs.existsSync;
      const originalReadFileSync = fs.readFileSync;

      try {
        // Simulate a list_branches MCP response with a branch that contains "rate-limit"
        // followed later in the same response by a branch with "ai-credits" (pattern 2 match)
        const branchesPayload = JSON.stringify({
          timestamp: "2026-08-02T08:39:00Z",
          event: "message",
          direction: "IN",
          server_id: "github",
          payload: {
            result: {
              content: [
                {
                  type: "text",
                  text: JSON.stringify([
                    { name: "schema-coverage-rate-limit-6dc5507939c41e8a", sha: "abc123" },
                    { name: "signed/jsweep/ai-credits-context-5df6a8a73adbb3c8", sha: "def456" },
                  ]),
                },
              ],
            },
          },
        });
        // Simulate a list_workflow_runs MCP response with a commit message containing both
        // "AI credits" and "rate-limit" (pattern 1 match)
        const runsPayload = JSON.stringify({
          timestamp: "2026-08-02T08:42:00Z",
          event: "message",
          direction: "IN",
          server_id: "github",
          payload: {
            result: {
              content: [
                {
                  type: "text",
                  text: JSON.stringify([
                    {
                      id: 30740108209,
                      head_commit: {
                        message: "Fix AI credits throughput rate-limit errors being silently swallowed (#49360)",
                      },
                    },
                  ]),
                },
              ],
            },
          },
        });
        fs.writeFileSync(rpcMessagesPath, [branchesPayload, runsPayload].join("\n"));

        const mockCore = {
          info: vi.fn(),
          debug: vi.fn(),
          startGroup: vi.fn(),
          endGroup: vi.fn(),
          notice: vi.fn(),
          warning: vi.fn(),
          error: vi.fn(),
          setFailed: vi.fn(),
          exportVariable: vi.fn(),
          setOutput: vi.fn(),
          summary: {
            addRaw: vi.fn().mockReturnThis(),
            addDetails: vi.fn().mockReturnThis(),
            write: vi.fn(),
          },
        };

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") return true;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") return false;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.jsonl") return false;
          return originalExistsSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") {
            return originalReadFileSync(rpcMessagesPath, encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        global.core = mockCore;

        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();

        // ai_credits_rate_limit_error must NOT be set to true from branch names / commit messages
        const setOutputCalls = mockCore.setOutput.mock.calls;
        const rateLimitCall = setOutputCalls.find(([name]) => name === "ai_credits_rate_limit_error");
        expect(rateLimitCall).toBeDefined();
        expect(rateLimitCall[1]).toBe("false");
      } finally {
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("detects ai_credits_rate_limit_error from stderr.log even when rpc-messages.jsonl contains only false-positive-like content", async () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-"));
      const rpcMessagesPath = path.join(tmpDir, "rpc-messages.jsonl");
      const stderrLogPath = path.join(tmpDir, "stderr.log");
      const originalExistsSync = fs.existsSync;
      const originalReadFileSync = fs.readFileSync;

      try {
        const rpcPayload = JSON.stringify({
          timestamp: "2026-08-02T08:39:00Z",
          event: "message",
          direction: "IN",
          server_id: "github",
          payload: {
            result: {
              content: [{ type: "text", text: JSON.stringify([{ name: "schema-coverage-rate-limit-abc123" }]) }],
            },
          },
        });
        fs.writeFileSync(rpcMessagesPath, rpcPayload);
        fs.writeFileSync(stderrLogPath, "CAPIError: 429 Too Many Requests");

        const mockCore = {
          info: vi.fn(),
          debug: vi.fn(),
          startGroup: vi.fn(),
          endGroup: vi.fn(),
          notice: vi.fn(),
          warning: vi.fn(),
          error: vi.fn(),
          setFailed: vi.fn(),
          exportVariable: vi.fn(),
          setOutput: vi.fn(),
          summary: {
            addRaw: vi.fn().mockReturnThis(),
            addDetails: vi.fn().mockReturnThis(),
            write: vi.fn(),
          },
        };

        fs.existsSync = vi.fn(filepath => {
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") return true;
          if (filepath === "/tmp/gh-aw/mcp-logs/stderr.log") return true;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.md") return false;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.jsonl") return false;
          if (filepath === "/tmp/gh-aw/mcp-logs/gateway.log") return false;
          return originalExistsSync(filepath);
        });

        fs.readFileSync = vi.fn((filepath, encoding) => {
          if (filepath === "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl") {
            return originalReadFileSync(rpcMessagesPath, encoding);
          }
          if (filepath === "/tmp/gh-aw/mcp-logs/stderr.log") {
            return originalReadFileSync(stderrLogPath, encoding);
          }
          return originalReadFileSync(filepath, encoding);
        });

        global.core = mockCore;
        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();

        const setOutputCalls = mockCore.setOutput.mock.calls;
        const rateLimitCall = setOutputCalls.find(([name]) => name === "ai_credits_rate_limit_error");
        expect(rateLimitCall).toBeDefined();
        expect(rateLimitCall[1]).toBe("true");
      } finally {
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    test("calls setFailed when an unexpected error is thrown inside main", async () => {
      const originalExistsSync = fs.existsSync;
      const originalReadFileSync = fs.readFileSync;

      const mockCore = {
        info: vi.fn(),
        debug: vi.fn(),
        startGroup: vi.fn(),
        endGroup: vi.fn(),
        notice: vi.fn(),
        warning: vi.fn(),
        error: vi.fn(),
        setFailed: vi.fn(),
        exportVariable: vi.fn(),
        setOutput: vi.fn(),
        summary: {
          addRaw: vi.fn().mockReturnThis(),
          addDetails: vi.fn().mockReturnThis(),
          write: vi.fn().mockRejectedValue(new Error("summary write failure")),
        },
      };

      fs.existsSync = vi.fn(() => false);
      fs.readFileSync = vi.fn((p, enc) => originalReadFileSync(p, enc));
      global.core = mockCore;

      try {
        const { main } = require("./parse_mcp_gateway_log.cjs");
        await main();
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("summary write failure"));
      } finally {
        fs.existsSync = originalExistsSync;
        fs.readFileSync = originalReadFileSync;
        delete global.core;
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

        // Check files are listed (filenames appear in startGroup calls for files with content)
        expect(allGroups).toContain("gateway.log");
        expect(allGroups).toContain("stderr.log");
        expect(allGroups).toContain("gateway.md");

        // Check file contents are printed for .log files
        expect(allOutput).toContain("Gateway log content");
        expect(allOutput).toContain("Error message");

        // Check .md file content IS displayed (now supported)
        expect(allOutput).toContain("# Gateway Summary");

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

  describe("parseGatewayJsonlForDifcFiltered", () => {
    test("extracts DIFC_FILTERED events from JSONL content", () => {
      const jsonlContent = [
        JSON.stringify({
          timestamp: "2026-03-18T17:30:00.123456789Z",
          type: "DIFC_FILTERED",
          server_id: "github",
          tool_name: "list_issues",
          description: "resource:list_issues",
          reason: "Integrity check failed, missingTags=[approved:github/copilot-indexing-issues-prs]",
          secrecy_tags: ["private:github/copilot-indexing-issues-prs"],
          integrity_tags: ["none:github/copilot-indexing-issues-prs"],
          author_association: "NONE",
          author_login: "external-user",
          html_url: "https://github.com/github/copilot-indexing-issues-prs/issues/42",
          number: "42",
        }),
        JSON.stringify({ timestamp: "2026-03-18T17:30:01Z", type: "RESPONSE", server_id: "github" }),
        JSON.stringify({
          timestamp: "2026-03-18T17:31:00Z",
          type: "DIFC_FILTERED",
          server_id: "github",
          tool_name: "get_issue",
          reason: "Secrecy check failed",
          author_login: "user2",
        }),
      ].join("\n");

      const events = parseGatewayJsonlForDifcFiltered(jsonlContent);

      expect(events).toHaveLength(2);
      expect(events[0].tool_name).toBe("list_issues");
      expect(events[0].server_id).toBe("github");
      expect(events[0].author_login).toBe("external-user");
      expect(events[1].tool_name).toBe("get_issue");
    });

    test("returns empty array when no DIFC_FILTERED events", () => {
      const jsonlContent = [JSON.stringify({ timestamp: "2026-03-18T17:30:01Z", type: "RESPONSE", server_id: "github" }), JSON.stringify({ timestamp: "2026-03-18T17:30:02Z", type: "REQUEST", server_id: "github" })].join("\n");

      const events = parseGatewayJsonlForDifcFiltered(jsonlContent);
      expect(events).toHaveLength(0);
    });

    test("returns empty array for empty content", () => {
      expect(parseGatewayJsonlForDifcFiltered("")).toHaveLength(0);
    });

    test("skips malformed JSON lines", () => {
      const jsonlContent = ["not valid json", JSON.stringify({ type: "DIFC_FILTERED", tool_name: "valid_tool" }), "{broken}"].join("\n");

      const events = parseGatewayJsonlForDifcFiltered(jsonlContent);
      expect(events).toHaveLength(1);
      expect(events[0].tool_name).toBe("valid_tool");
    });

    test("skips blank lines", () => {
      const jsonlContent = "\n" + JSON.stringify({ type: "DIFC_FILTERED", tool_name: "t1" }) + "\n\n" + JSON.stringify({ type: "DIFC_FILTERED", tool_name: "t2" }) + "\n";

      const events = parseGatewayJsonlForDifcFiltered(jsonlContent);
      expect(events).toHaveLength(2);
    });

    test("extracts events using the schema rpc-message/v2 'event' field (no top-level type)", () => {
      const jsonlContent = JSON.stringify({
        timestamp: "2026-08-15T23:30:00Z",
        event: "difc_filtered",
        _schema: "rpc-message/v2",
        server_id: "github",
        tool_name: "list_issues",
        reason: "Integrity check failed",
      });

      const events = parseGatewayJsonlForDifcFiltered(jsonlContent);
      expect(events).toHaveLength(1);
      expect(events[0].tool_name).toBe("list_issues");
    });
  });

  describe("parseGatewayJsonlForTokenSteering", () => {
    test("extracts token_steering events from gateway.jsonl content", () => {
      const jsonlContent = [
        JSON.stringify({
          timestamp: "2026-03-18T17:30:00.123456789Z",
          level: "info",
          event: "token_steering",
          request_id: "req-123",
          provider: "copilot",
          message: "[AWF TOKEN WARNING] You have used 90% of your effective token budget. Complete your current task and prepare final output.",
        }),
        JSON.stringify({ timestamp: "2026-03-18T17:30:01Z", level: "info", event: "request_start", request_id: "req-124" }),
        JSON.stringify({
          timestamp: "2026-03-18T17:30:02Z",
          type: "token_steering",
          request_id: "req-125",
          provider: "anthropic",
          message: "[AWF TOKEN WARNING] You have used 95% of your effective token budget. Finalize and submit your work now.",
        }),
      ].join("\n");

      const events = parseGatewayJsonlForTokenSteering(jsonlContent);

      expect(events).toHaveLength(2);
      expect(events[0].request_id).toBe("req-123");
      expect(events[0].provider).toBe("copilot");
      expect(events[1].provider).toBe("anthropic");
    });

    test("returns empty array when no token steering events are present", () => {
      const jsonlContent = [JSON.stringify({ event: "request_start" }), JSON.stringify({ type: "RESPONSE" })].join("\n");

      expect(parseGatewayJsonlForTokenSteering(jsonlContent)).toHaveLength(0);
    });
  });

  describe("parseGatewayJsonlForModelAliasResolution", () => {
    test("extracts model alias rewrite/resolution events from gateway.jsonl content", () => {
      const jsonlContent = [
        JSON.stringify({
          timestamp: "2026-03-18T17:30:02Z",
          event: "model_alias_resolution",
          request_id: "req-200",
          provider: "copilot",
          alias: "sonnet",
          resolved_model: "claude-sonnet-4.6",
        }),
        JSON.stringify({ timestamp: "2026-03-18T17:30:03Z", event: "request_start", request_id: "req-201" }),
        JSON.stringify({
          timestamp: "2026-03-18T17:30:04Z",
          type: "model_alias_resolution",
          request_id: "req-202",
          provider: "openai",
          model_alias: "mini",
          resolved_model: "gpt-5-mini",
        }),
        JSON.stringify({
          timestamp: "2026-03-18T17:30:05Z",
          event: "MODEL_ALIAS_REWRITE",
          data: {
            provider: "anthropic",
            request_id: "req-203",
            original_model: "sonnet",
            resolved_model: "claude-sonnet-4.6",
          },
        }),
        JSON.stringify({
          timestamp: "2026-03-18T17:30:06Z",
          event: "model_rewrite",
          request_id: "req-204",
          provider: "copilot",
          original_model: "gpt-5-mini",
          resolved_model: "gpt-5.4-mini",
        }),
      ].join("\n");

      const events = parseGatewayJsonlForModelAliasResolution(jsonlContent);

      expect(events).toHaveLength(4);
      expect(events[0].request_id).toBe("req-200");
      expect(events[1].request_id).toBe("req-202");
      expect(events[2].data.request_id).toBe("req-203");
      expect(events[3].request_id).toBe("req-204");
    });

    test("returns empty array when no model alias resolution events are present", () => {
      const jsonlContent = [JSON.stringify({ event: "request_start" }), JSON.stringify({ type: "RESPONSE" })].join("\n");

      expect(parseGatewayJsonlForModelAliasResolution(jsonlContent)).toHaveLength(0);
    });
  });

  describe("generateDifcFilteredSummary", () => {
    const sampleEvents = [
      {
        timestamp: "2026-03-18T17:30:00.123456789Z",
        type: "DIFC_FILTERED",
        server_id: "github",
        tool_name: "list_issues",
        description: "resource:list_issues",
        reason: "Integrity check failed, missingTags=[approved:github/copilot-indexing-issues-prs]",
        secrecy_tags: ["private:github/copilot-indexing-issues-prs"],
        integrity_tags: ["none:github/copilot-indexing-issues-prs"],
        author_association: "NONE",
        author_login: "external-user",
        html_url: "https://github.com/github/copilot-indexing-issues-prs/issues/42",
        number: "42",
      },
    ];

    test("returns empty string for empty events array", () => {
      expect(generateDifcFilteredSummary([])).toBe("");
    });

    test("returns empty string for null/undefined", () => {
      expect(generateDifcFilteredSummary(null)).toBe("");
      expect(generateDifcFilteredSummary(undefined)).toBe("");
    });

    test("generates details/summary section with event count", () => {
      const summary = generateDifcFilteredSummary(sampleEvents);
      expect(summary).toContain("<details>");
      expect(summary).toContain("DIFC Filtered Events (1)");
      expect(summary).toContain("</details>");
    });

    test("includes tool name in code formatting", () => {
      const summary = generateDifcFilteredSummary(sampleEvents);
      expect(summary).toContain("`list_issues`");
    });

    test("includes server_id", () => {
      const summary = generateDifcFilteredSummary(sampleEvents);
      expect(summary).toContain("github");
    });

    test("includes reason for filtering", () => {
      const summary = generateDifcFilteredSummary(sampleEvents);
      expect(summary).toContain("Integrity check failed");
    });

    test("includes author login and association", () => {
      const summary = generateDifcFilteredSummary(sampleEvents);
      expect(summary).toContain("external-user");
      expect(summary).toContain("NONE");
    });

    test("renders resource as linked issue number", () => {
      const summary = generateDifcFilteredSummary(sampleEvents);
      expect(summary).toContain("[#42]");
      expect(summary).toContain("https://github.com/github/copilot-indexing-issues-prs/issues/42");
    });

    test("uses description as resource when html_url absent", () => {
      const events = [{ type: "DIFC_FILTERED", tool_name: "my_tool", description: "resource:my_tool" }];
      const summary = generateDifcFilteredSummary(events);
      expect(summary).toContain("resource:my_tool");
    });

    test("shows dash instead of #unknown when description resolves to #unknown", () => {
      const events = [{ type: "DIFC_FILTERED", tool_name: "search_issues", description: "github:#unknown", reason: "has lower integrity" }];
      const summary = generateDifcFilteredSummary(events);
      expect(summary).not.toContain("#unknown");
      expect(summary).toContain("| - |");
    });

    test("escapes pipe characters in reason", () => {
      const events = [{ type: "DIFC_FILTERED", tool_name: "t", reason: "failed | check" }];
      const summary = generateDifcFilteredSummary(events);
      expect(summary).toContain("failed \\| check");
    });

    test("generates correct table header", () => {
      const summary = generateDifcFilteredSummary(sampleEvents);
      expect(summary).toContain("| Time | Server | Tool | Reason | User | Resource |");
      expect(summary).toContain("|------|--------|------|--------|------|----------|");
    });

    test("shows event count in summary for multiple events", () => {
      const multiEvents = [
        { type: "DIFC_FILTERED", tool_name: "t1", reason: "r1" },
        { type: "DIFC_FILTERED", tool_name: "t2", reason: "r2" },
        { type: "DIFC_FILTERED", tool_name: "t3", reason: "r3" },
      ];
      const summary = generateDifcFilteredSummary(multiEvents);
      expect(summary).toContain("DIFC Filtered Events (3)");
    });
  });

  describe("generateTokenSteeringSummary", () => {
    test("returns empty string for empty events array", () => {
      expect(generateTokenSteeringSummary([])).toBe("");
    });

    test("renders a token steering summary table", () => {
      const summary = generateTokenSteeringSummary([
        {
          timestamp: "2026-03-18T17:30:00.123456789Z",
          provider: "copilot",
          request_id: "req-123",
          message: "[AWF TOKEN WARNING] You have used 90% of your effective token budget. Complete your current task and prepare final output.",
        },
      ]);

      expect(summary).toContain("Token Steering Events (1)");
      expect(summary).toContain("| Time | Provider | Request ID | Message |");
      expect(summary).toContain("2026-03-18 17:30:00Z");
      expect(summary).toContain("copilot");
      expect(summary).toContain("req-123");
      expect(summary).toContain("[AWF TOKEN WARNING] You have used 90% of your effective token budget.");
    });
  });

  describe("generateModelAliasResolutionSummary", () => {
    test("returns empty string for empty events array", () => {
      expect(generateModelAliasResolutionSummary([])).toBe("");
    });

    test("renders a model alias resolution summary table and raw event JSON", () => {
      const summary = generateModelAliasResolutionSummary([
        {
          timestamp: "2026-03-18T17:30:02.123456789Z",
          event: "model_alias_resolution",
          provider: "copilot",
          request_id: "req-200",
          alias: "sonnet",
          resolved_model: "claude-sonnet-4.6",
          candidates: ["claude-sonnet-4.6", "gpt-4o"],
        },
        {
          timestamp: "2026-03-18T17:30:03.123456789Z",
          event: "MODEL_ALIAS_REWRITE",
          data: {
            provider: "anthropic",
            request_id: "req-201",
            original_model: "sonnet",
            resolved_model: "claude-sonnet-4.6",
          },
        },
      ]);

      expect(summary).toContain("Model Alias Resolution Events (2)");
      expect(summary).toContain("| Time | Provider | Request ID | Alias | Resolved model |");
      expect(summary).toContain("2026-03-18 17:30:02Z");
      expect(summary).toContain("copilot");
      expect(summary).toContain("req-200");
      expect(summary).toContain("sonnet");
      expect(summary).toContain("claude-sonnet-4.6");
      expect(summary).toContain("anthropic");
      expect(summary).toContain("req-201");
      expect(summary).toContain("Raw events");
      expect(summary).toContain('"event": "model_alias_resolution"');
      expect(summary).toContain('"event": "MODEL_ALIAS_REWRITE"');
      expect(summary).toContain('"candidates"');
    });
  });

  describe("parseRpcMessagesJsonl", () => {
    test("returns empty arrays for empty content", () => {
      const result = parseRpcMessagesJsonl("");
      expect(result.requests).toHaveLength(0);
      expect(result.responses).toHaveLength(0);
      expect(result.other).toHaveLength(0);
    });

    test("categorizes REQUEST entries", () => {
      const content = [
        JSON.stringify({ timestamp: "2026-01-18T11:10:49Z", direction: "OUT", type: "REQUEST", server_id: "github", payload: { jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "list_issues", arguments: {} } } }),
        JSON.stringify({ timestamp: "2026-01-18T11:10:50Z", direction: "IN", type: "RESPONSE", server_id: "github", payload: { jsonrpc: "2.0", id: 1, result: {} } }),
      ].join("\n");

      const result = parseRpcMessagesJsonl(content);
      expect(result.requests).toHaveLength(1);
      expect(result.responses).toHaveLength(1);
      expect(result.other).toHaveLength(0);
      expect(result.requests[0].server_id).toBe("github");
    });

    test("excludes DIFC_FILTERED entries (handled separately)", () => {
      const content = [
        JSON.stringify({ type: "REQUEST", server_id: "github", payload: { method: "tools/call", params: { name: "list_issues" } } }),
        JSON.stringify({ type: "DIFC_FILTERED", server_id: "github", tool_name: "get_issue", reason: "blocked" }),
      ].join("\n");

      const result = parseRpcMessagesJsonl(content);
      expect(result.requests).toHaveLength(1);
      expect(result.other).toHaveLength(0);
    });

    test("captures unknown message types in other array", () => {
      const content = [
        JSON.stringify({ type: "SESSION_START", server_id: "github" }),
        JSON.stringify({ type: "SESSION_END", server_id: "github" }),
        JSON.stringify({ type: "REQUEST", server_id: "github", payload: { method: "initialize" } }),
      ].join("\n");

      const result = parseRpcMessagesJsonl(content);
      expect(result.requests).toHaveLength(1);
      expect(result.other).toHaveLength(2);
    });

    test("skips malformed JSON lines", () => {
      const content = ["not valid json", JSON.stringify({ type: "REQUEST", server_id: "github", payload: { method: "tools/call", params: { name: "list_issues" } } }), "{broken}"].join("\n");

      const result = parseRpcMessagesJsonl(content);
      expect(result.requests).toHaveLength(1);
    });

    test("skips entries without a type field", () => {
      const content = [JSON.stringify({ server_id: "github" }), JSON.stringify({ type: "REQUEST", server_id: "ok", payload: { method: "tools/list" } })].join("\n");

      const result = parseRpcMessagesJsonl(content);
      expect(result.requests).toHaveLength(1);
      expect(result.other).toHaveLength(0);
    });

    // Regression test for https://github.com/github/gh-aw/issues/53254: real-world
    // rpc-messages.jsonl files (schema "rpc-message/v2") use a top-level "event" field
    // ("rpc_request"/"rpc_response") instead of the legacy top-level "type" field.
    test("categorizes entries using the schema rpc-message/v2 'event' field", () => {
      const content = [
        JSON.stringify({
          timestamp: "2026-08-15T23:48:42.233Z",
          event: "rpc_request",
          _schema: "rpc-message/v2",
          direction: "OUT",
          server_id: "github",
          method: "tools/call",
          payload: { jsonrpc: "2.0", method: "tools/call", params: { name: "list_issues", arguments: {} } },
        }),
        JSON.stringify({
          timestamp: "2026-08-15T23:48:42.400Z",
          event: "rpc_response",
          _schema: "rpc-message/v2",
          direction: "IN",
          server_id: "github",
          payload: { jsonrpc: "2.0", id: 1, result: {} },
        }),
      ].join("\n");

      const result = parseRpcMessagesJsonl(content);
      expect(result.requests).toHaveLength(1);
      expect(result.responses).toHaveLength(1);
      expect(result.other).toHaveLength(0);
      expect(result.requests[0].server_id).toBe("github");
      expect(result.requests[0].type).toBe("REQUEST");
    });
  });

  describe("getRpcMessageType", () => {
    test("returns the legacy type field when present", () => {
      expect(getRpcMessageType({ type: "REQUEST", event: "rpc_response" })).toBe("REQUEST");
    });

    test("maps rpc_request to REQUEST", () => {
      expect(getRpcMessageType({ event: "rpc_request" })).toBe("REQUEST");
    });

    test("maps rpc_response to RESPONSE", () => {
      expect(getRpcMessageType({ event: "rpc_response" })).toBe("RESPONSE");
    });

    test("maps difc_filtered to DIFC_FILTERED", () => {
      expect(getRpcMessageType({ event: "difc_filtered" })).toBe("DIFC_FILTERED");
    });

    test("returns empty string when neither field is present", () => {
      expect(getRpcMessageType({ server_id: "github" })).toBe("");
    });
  });

  describe("getRpcRequestLabel", () => {
    test("returns tool name for tools/call requests", () => {
      const entry = { type: "REQUEST", payload: { method: "tools/call", params: { name: "list_issues" } } };
      expect(getRpcRequestLabel(entry)).toBe("list_issues");
    });

    test("returns method name for non-tools/call requests", () => {
      const entry = { type: "REQUEST", payload: { method: "tools/list" } };
      expect(getRpcRequestLabel(entry)).toBe("tools/list");
    });

    test("returns tools/call as fallback when params.name is missing", () => {
      const entry = { type: "REQUEST", payload: { method: "tools/call" } };
      expect(getRpcRequestLabel(entry)).toBe("tools/call");
    });

    test("returns unknown when payload is missing", () => {
      const entry = { type: "REQUEST" };
      expect(getRpcRequestLabel(entry)).toBe("unknown");
    });

    test("returns unknown when method is missing", () => {
      const entry = { type: "REQUEST", payload: {} };
      expect(getRpcRequestLabel(entry)).toBe("unknown");
    });
  });

  describe("generateRpcMessagesSummary", () => {
    const sampleRequests = [
      { timestamp: "2026-01-18T11:10:49Z", direction: "OUT", type: "REQUEST", server_id: "github", payload: { method: "tools/call", params: { name: "list_issues" } } },
      { timestamp: "2026-01-18T11:10:51Z", direction: "OUT", type: "REQUEST", server_id: "safeoutputs", payload: { method: "tools/call", params: { name: "add_comment" } } },
    ];
    const sampleResponses = [{ timestamp: "2026-01-18T11:10:50Z", direction: "IN", type: "RESPONSE", server_id: "github", payload: { jsonrpc: "2.0", result: {} } }];

    test("returns empty string for no messages", () => {
      expect(generateRpcMessagesSummary({ requests: [], responses: [], other: [] }, [])).toBe("");
    });

    test("generates details/summary with request and response counts", () => {
      const summary = generateRpcMessagesSummary({ requests: sampleRequests, responses: sampleResponses, other: [] }, []);
      expect(summary).toContain("<details>");
      expect(summary).toContain("MCP Gateway Activity (2 requests, 1 response)");
      expect(summary).toContain("</details>");
    });

    test("renders request table with time, server, and tool columns", () => {
      const summary = generateRpcMessagesSummary({ requests: sampleRequests, responses: [], other: [] }, []);
      expect(summary).toContain("#### REQUEST");
      expect(summary).toContain("| Time | Server | Tool / Method |");
      expect(summary).toContain("<code>list_issues</code>");
      expect(summary).toContain("<code>add_comment</code>");
      expect(summary).toContain("github");
      expect(summary).toContain("safeoutputs");
    });

    test("escapes request labels rendered as code", () => {
      const requests = [{ timestamp: "2026-01-18T11:10:49Z", direction: "OUT", type: "REQUEST", server_id: "github", payload: { method: "tools/call", params: { name: "list`issues<bad>" } } }];
      const summary = generateRpcMessagesSummary({ requests, responses: [], other: [] }, []);
      expect(summary).toContain("<code>list`issues&lt;bad&gt;</code>");
    });

    test("formats ISO timestamp as readable date-time", () => {
      const summary = generateRpcMessagesSummary({ requests: sampleRequests, responses: [], other: [] }, []);
      expect(summary).toContain("2026-01-18 11:10:49Z");
    });

    test("shows blocked count in summary when DIFC events present", () => {
      const difcEvents = [{ type: "DIFC_FILTERED", tool_name: "get_issue", reason: "blocked" }];
      const summary = generateRpcMessagesSummary({ requests: sampleRequests, responses: [], other: [] }, difcEvents);
      expect(summary).toContain("1 blocked");
    });

    test("renders response rows with status and details", () => {
      const responses = [
        { timestamp: "2026-01-18T11:10:50Z", direction: "IN", type: "RESPONSE", server_id: "github", payload: { jsonrpc: "2.0", id: 1, result: { tools: [{ name: "list_issues" }] } } },
        { timestamp: "2026-01-18T11:10:52Z", direction: "IN", type: "RESPONSE", server_id: "safeoutputs", payload: { jsonrpc: "2.0", id: 2, error: { code: -32603, message: "Tool failed" } } },
      ];

      const summary = generateRpcMessagesSummary({ requests: sampleRequests, responses, other: [] }, []);
      expect(summary).toContain("#### RESPONSE");
      expect(summary).toContain("| Time | Server | Direction | Status | Details |");
      expect(summary).toContain("1 tool");
      expect(summary).toContain("error");
      expect(summary).toContain("Tool failed");
    });

    test("includes DIFC_FILTERED table when events are present", () => {
      const difcEvents = [{ type: "DIFC_FILTERED", tool_name: "get_issue", server_id: "github", reason: "Integrity check failed", author_login: "user1", author_association: "MEMBER" }];
      const summary = generateRpcMessagesSummary({ requests: sampleRequests, responses: [], other: [] }, difcEvents);
      expect(summary).toContain("DIFC Filtered Events");
      expect(summary).toContain("`get_issue`");
    });

    test("renders other message types section", () => {
      const other = [
        { timestamp: "2026-01-18T11:10:40Z", direction: "OUT", type: "SESSION_START", server_id: "github", session_id: "abc123" },
        { timestamp: "2026-01-18T11:10:41Z", direction: "OUT", type: "SESSION_START", server_id: "github", session_id: "def456" },
        { timestamp: "2026-01-18T11:10:55Z", direction: "IN", type: "SESSION_END", server_id: "github", reason: "completed" },
      ];
      const summary = generateRpcMessagesSummary({ requests: [], responses: [], other }, []);
      expect(summary).toContain("MCP Gateway Activity (2 SESSION_START, 1 SESSION_END)");
      expect(summary).toContain("#### SESSION_START");
      expect(summary).toContain("#### SESSION_END");
      expect(summary).toContain("session_id=abc123");
      expect(summary).toContain("reason=completed");
    });

    test("sanitizes other message type labels in headings and summary", () => {
      const other = [{ timestamp: "2026-01-18T11:10:40Z", direction: "OUT", type: "SESSION_<bad>\nSTART", server_id: "github", session_id: "abc123" }];
      const summary = generateRpcMessagesSummary({ requests: [], responses: [], other }, []);
      expect(summary).toContain("MCP Gateway Activity (1 SESSION_&lt;bad&gt; START)");
      expect(summary).toContain("#### SESSION_&lt;bad&gt; START");
      expect(summary).not.toContain("#### SESSION_<bad>\nSTART");
    });

    test("shows minimal header when only DIFC events exist (no requests)", () => {
      const difcEvents = [{ type: "DIFC_FILTERED", tool_name: "list_issues", reason: "blocked" }];
      const summary = generateRpcMessagesSummary({ requests: [], responses: [], other: [] }, difcEvents);
      expect(summary).toContain("1 blocked");
      expect(summary).toContain("All tool calls were blocked");
    });
  });

  describe("formatDurationMs", () => {
    test("formats sub-second durations as milliseconds", () => {
      expect(formatDurationMs(0)).toBe("0ms");
      expect(formatDurationMs(500)).toBe("500ms");
      expect(formatDurationMs(999)).toBe("999ms");
    });

    test("formats second-range durations with one decimal place", () => {
      expect(formatDurationMs(1000)).toBe("1.0s");
      expect(formatDurationMs(2500)).toBe("2.5s");
      expect(formatDurationMs(59999)).toBe("60.0s");
    });

    test("formats minute-range durations as MmSs", () => {
      expect(formatDurationMs(60000)).toBe("1m0s");
      expect(formatDurationMs(90000)).toBe("1m30s");
      expect(formatDurationMs(120000)).toBe("2m0s");
    });
  });

  describe("parseTokenUsageJsonl", () => {
    test("returns null for empty content", () => {
      expect(parseTokenUsageJsonl("")).toBeNull();
      expect(parseTokenUsageJsonl("   \n  ")).toBeNull();
    });

    test("prefers the exact AWF-reported total from run 33157406852", () => {
      const content = fs.readFileSync(path.join(__dirname, "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"), "utf8");
      const summary = parseTokenUsageJsonl(content);

      expect(summary).not.toBeNull();
      expect(summary.totalRequests).toBe(5);
      expect(summary.totalInputTokens).toBe(39376);
      expect(summary.totalCacheReadTokens).toBe(57984);
      expect(summary.totalOutputTokens).toBe(175);
      expect(summary.aiCreditsSource).toBe("awf_reported");
      expect(summary.entries.map(entry => entry.deltaAIC)).toEqual([0.29142, 0.2928, 0.15018, 0.1506, 0.15102]);
      expect(summary.totalAIC).toBe(1.03602);
      expect(generateTokenUsageSummary(summary)).toContain("1.03602");
    });

    test("retains legacy recomputation when AWF-reported fields are absent", () => {
      const content = fs
        .readFileSync(path.join(__dirname, "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"), "utf8")
        .trim()
        .split("\n")
        .map(line => {
          const record = JSON.parse(line);
          delete record.ai_credits_this_response;
          delete record.ai_credits_total;
          return JSON.stringify(record);
        })
        .join("\n");
      const summary = parseTokenUsageJsonl(content);

      expect(summary.aiCreditsSource).toBe("recomputed");
      expect(summary.totalAIC).toBeCloseTo(0.44538, 6);
    });

    test("falls back to legacy pricing and warns for malformed AWF-reported AIC fields", () => {
      const content = JSON.stringify({
        provider: "copilot",
        model: "gpt-4o-mini-2024-07-18",
        input_tokens: 19288,
        output_tokens: 35,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        ai_credits_this_response: "0.29142",
        ai_credits_total: -1,
      });
      const summary = parseTokenUsageJsonl(content);

      expect(summary.aiCreditsSource).toBe("recomputed");
      expect(summary.entries[0].deltaAIC).toBeCloseTo(0.29142, 6);
      expect(summary.totalAIC).toBeCloseTo(0.29142, 6);
      expect(summary.aiCreditsWarnings).toEqual([expect.stringContaining("1 token usage record(s)")]);
      expect(generateTokenUsageSummary(summary)).toContain("Warning:");
    });

    test("treats null AWF-reported AIC fields as malformed rather than zero", () => {
      const summary = parseTokenUsageJsonl(
        JSON.stringify({
          provider: "copilot",
          model: "gpt-4o-mini-2024-07-18",
          input_tokens: 19288,
          output_tokens: 35,
          cache_read_tokens: 0,
          cache_write_tokens: 0,
          ai_credits_this_response: null,
          ai_credits_total: null,
        })
      );

      expect(summary.aiCreditsSource).toBe("recomputed");
      expect(summary.totalAIC).toBeCloseTo(0.29142, 6);
      expect(summary.aiCreditsWarnings).toEqual([expect.stringContaining("fallback accounting")]);
    });

    test("extends the last valid AWF-reported total with fallback pricing when a later total is malformed", () => {
      const records = fs
        .readFileSync(path.join(__dirname, "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"), "utf8")
        .trim()
        .split("\n")
        .map(line => JSON.parse(line));
      records.at(-1).ai_credits_total = "1.03602";
      const summary = parseTokenUsageJsonl(records.map(record => JSON.stringify(record)).join("\n"));

      expect(summary.aiCreditsSource).toBe("awf_reported");
      expect(summary.entries.at(-1).deltaAIC).toBe(0.15102);
      expect(summary.totalAIC).toBe(1.03602);
      expect(summary.aiCreditsWarnings).toEqual([expect.stringContaining("1 token usage record(s)")]);
    });

    test("exports the exact AWF-reported total to the main job output", async () => {
      const tokenUsagePath = "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl";
      const content = fs.readFileSync(path.join(__dirname, "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"), "utf8");
      const existsSpy = vi.spyOn(fs, "existsSync").mockImplementation(filePath => filePath === tokenUsagePath);
      const readSpy = vi.spyOn(fs, "readFileSync").mockImplementation((filePath, encoding) => {
        if (filePath === tokenUsagePath) return content;
        return "";
      });
      const coreObj = {
        debug: vi.fn(),
        info: vi.fn(),
        exportVariable: vi.fn(),
        setOutput: vi.fn(),
        summary: {
          addRaw: vi.fn(),
          write: vi.fn().mockResolvedValue(undefined),
        },
      };

      try {
        await writeStepSummaryWithTokenUsage(coreObj);
      } finally {
        existsSpy.mockRestore();
        readSpy.mockRestore();
      }

      expect(coreObj.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", "1.03602");
      expect(coreObj.setOutput).toHaveBeenCalledWith("aic", "1.03602");
      expect(coreObj.info).toHaveBeenCalledWith("AI Credits: 1.03602");
    });

    test.each([
      [true, 0.018],
      [false, 0.0255],
    ])("propagates explicit input_tokens_include_cache=%s through legacy repricing", (inputTokensIncludeCache, expectedAIC) => {
      const summary = parseTokenUsageJsonl(
        JSON.stringify({
          provider: "copilot",
          model: "gpt-4o-mini-2024-07-18",
          input_tokens: 1000,
          output_tokens: 100,
          cache_read_tokens: 400,
          cache_write_tokens: 100,
          input_tokens_include_cache: inputTokensIncludeCache,
        })
      );

      expect(summary.entries[0].inputTokensIncludeCache).toBe(inputTokensIncludeCache);
      expect(summary.entries[0].deltaAIC).toBeCloseTo(expectedAIC, 6);
      expect(summary.totalAIC).toBeCloseTo(expectedAIC, 6);
    });

    test("distinguishes zero AWF-reported credits from absent fields", () => {
      const summary = parseTokenUsageJsonl(
        JSON.stringify({
          provider: "copilot",
          model: "gpt-4o-mini-2024-07-18",
          input_tokens: 1000,
          output_tokens: 100,
          cache_read_tokens: 0,
          cache_write_tokens: 0,
          ai_credits_this_response: 0,
          ai_credits_total: 0,
        })
      );

      expect(summary.aiCreditsSource).toBe("awf_reported");
      expect(summary.entries[0].deltaAIC).toBe(0);
      expect(summary.totalAIC).toBe(0);
      expect(summary.aiCreditsWarnings).toEqual([]);
      expect(generateTokenUsageSummary(summary)).toContain("| **Total** | | **1,000** | **100** | **0** | **0** | | **0** |");
    });

    test("exports AWF-reported zero to the main job output", async () => {
      const tokenUsagePath = "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl";
      const content = JSON.stringify({
        provider: "copilot",
        model: "gpt-4o-mini-2024-07-18",
        input_tokens: 1000,
        output_tokens: 100,
        ai_credits_this_response: 0,
        ai_credits_total: 0,
      });
      const existsSpy = vi.spyOn(fs, "existsSync").mockImplementation(filePath => filePath === tokenUsagePath);
      const readSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
        if (filePath === tokenUsagePath) return content;
        return "";
      });
      const coreObj = {
        debug: vi.fn(),
        info: vi.fn(),
        exportVariable: vi.fn(),
        setOutput: vi.fn(),
        summary: {
          addRaw: vi.fn(),
          write: vi.fn().mockResolvedValue(undefined),
        },
      };

      try {
        await writeStepSummaryWithTokenUsage(coreObj);
      } finally {
        existsSpy.mockRestore();
        readSpy.mockRestore();
      }

      expect(coreObj.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", "0");
      expect(coreObj.setOutput).toHaveBeenCalledWith("aic", "0");
      expect(coreObj.info).toHaveBeenCalledWith("AI Credits: 0");
    });

    test("aggregates AWF-reported credits by model without changing the run total", () => {
      const content = [
        {
          model: "gpt-4o-mini",
          provider: "copilot",
          input_tokens: 10,
          output_tokens: 1,
          ai_credits_this_response: 0.2,
          ai_credits_total: 0.2,
        },
        {
          model: "claude-sonnet-4-6",
          provider: "copilot",
          input_tokens: 20,
          output_tokens: 2,
          ai_credits_this_response: 0.8,
          ai_credits_total: 1,
        },
      ]
        .map(record => JSON.stringify(record))
        .join("\n");

      const summary = parseTokenUsageJsonl(content);

      expect(summary.byModel["gpt-4o-mini"].aic).toBe(0.2);
      expect(summary.byModel["claude-sonnet-4-6"].aic).toBe(0.8);
      expect(summary.totalAIC).toBe(1);
    });

    test("keeps model names such as __proto__ as data without polluting object prototypes", () => {
      const summary = parseTokenUsageJsonl(
        JSON.stringify({
          model: "__proto__",
          provider: "copilot",
          input_tokens: 10,
          output_tokens: 1,
          ai_credits_this_response: 0.1,
          ai_credits_total: 0.1,
        })
      );

      expect(Object.getPrototypeOf(summary.byModel)).toBeNull();
      expect(summary.byModel["__proto__"].aic).toBe(0.1);
      expect(Object.prototype).not.toHaveProperty("aic");
    });

    test("deduplicates repeated request IDs before aggregating reported credits", () => {
      const record = {
        request_id: "same-request",
        model: "gpt-4o-mini",
        provider: "copilot",
        input_tokens: 10,
        output_tokens: 1,
        ai_credits_this_response: 0.2,
        ai_credits_total: 0.2,
      };
      const summary = parseTokenUsageJsonl(`${JSON.stringify(record)}\n${JSON.stringify(record)}\n`);

      expect(summary.totalRequests).toBe(1);
      expect(summary.totalInputTokens).toBe(10);
      expect(summary.byModel["gpt-4o-mini"].aic).toBe(0.2);
      expect(summary.totalAIC).toBe(0.2);
      expect(summary.aiCreditsWarnings).toEqual([expect.stringContaining("1 duplicate token usage record(s)")]);
    });

    test("warns and uses legacy provider semantics for invalid input_tokens_include_cache", () => {
      const summary = parseTokenUsageJsonl(
        JSON.stringify({
          provider: "copilot",
          model: "gpt-4o-mini-2024-07-18",
          input_tokens: 1000,
          output_tokens: 100,
          cache_read_tokens: 400,
          cache_write_tokens: 100,
          input_tokens_include_cache: "invalid",
        })
      );

      expect(summary.totalAIC).toBeCloseTo(0.0195, 6);
      expect(summary.aiCreditsWarnings).toEqual([expect.stringContaining("invalid input_tokens_include_cache")]);
    });

    test("does not warn for invalid input_tokens_include_cache when AWF delta is valid", () => {
      const summary = parseTokenUsageJsonl(
        JSON.stringify({
          provider: "copilot",
          model: "gpt-4o-mini-2024-07-18",
          input_tokens: 1000,
          output_tokens: 100,
          cache_read_tokens: 400,
          cache_write_tokens: 100,
          input_tokens_include_cache: "invalid",
          ai_credits_this_response: 0.123,
          ai_credits_total: 0.123,
        })
      );

      expect(summary.totalAIC).toBe(0.123);
      expect(summary.aiCreditsWarnings).toEqual([]);
    });

    test("uses the chronologically last valid AWF-reported total", () => {
      const content = [
        {
          timestamp: "2026-08-28T09:00:02Z",
          request_id: "third",
          model: "gpt-4o-mini",
          provider: "copilot",
          input_tokens: 300,
          output_tokens: 1,
          ai_credits_this_response: 1,
          ai_credits_total: 3,
        },
        {
          timestamp: "2026-08-28T09:00:00Z",
          request_id: "first",
          model: "gpt-4o-mini",
          provider: "copilot",
          input_tokens: 100,
          output_tokens: 1,
          ai_credits_this_response: 1,
          ai_credits_total: 1,
        },
        {
          timestamp: "2026-08-28T09:00:01Z",
          request_id: "second",
          model: "gpt-4o-mini",
          provider: "copilot",
          input_tokens: 200,
          output_tokens: 1,
          ai_credits_this_response: 1,
          ai_credits_total: 2,
        },
      ]
        .map(record => JSON.stringify(record))
        .join("\n");

      const summary = parseTokenUsageJsonl(content);

      expect(summary.entries.map(entry => entry.runningAIC)).toEqual([1, 2, 3]);
      expect(summary.totalAIC).toBe(3);
      expect(summary.ambientContextTokens).toBe(100);
      expect(summary.aiCreditsWarnings).toEqual([]);
    });

    test("warns when AWF cumulative and per-request reported credits diverge", () => {
      const content = [
        {
          request_id: "one",
          model: "gpt-4o-mini",
          provider: "copilot",
          input_tokens: 1,
          output_tokens: 1,
          ai_credits_this_response: 0.2,
          ai_credits_total: 0.2,
        },
        {
          request_id: "two",
          model: "gpt-4o-mini",
          provider: "copilot",
          input_tokens: 1,
          output_tokens: 1,
          ai_credits_this_response: 0.8,
          ai_credits_total: 0.9,
        },
      ]
        .map(record => JSON.stringify(record))
        .join("\n");

      const summary = parseTokenUsageJsonl(content);

      expect(summary.totalAIC).toBe(0.9);
      expect(summary.aiCreditsWarnings).toEqual([expect.stringContaining("differs from the sum")]);
    });

    test("keeps large AWF-reported totals compact in the human-readable table", () => {
      const summary = parseTokenUsageJsonl(
        JSON.stringify({
          request_id: "large",
          model: "gpt-4o-mini",
          provider: "copilot",
          input_tokens: 1,
          output_tokens: 1,
          ai_credits_this_response: 1234.56789,
          ai_credits_total: 1234.56789,
        })
      );

      expect(generateTokenUsageSummary(summary)).toContain("| 1.2K | 1.2K |");
    });

    test("parses a single entry and aggregates totals", () => {
      const content = JSON.stringify({
        timestamp: "2026-04-01T17:56:38.042Z",
        request_id: "abc-123",
        provider: "anthropic",
        model: "claude-sonnet-4-6",
        path: "/v1/messages",
        status: 200,
        streaming: true,
        input_tokens: 100,
        output_tokens: 200,
        cache_read_tokens: 5000,
        cache_write_tokens: 3000,
        duration_ms: 2500,
        response_bytes: 1500,
      });
      const summary = parseTokenUsageJsonl(content);
      expect(summary).not.toBeNull();
      expect(summary.totalInputTokens).toBe(100);
      expect(summary.totalOutputTokens).toBe(200);
      expect(summary.totalCacheReadTokens).toBe(5000);
      expect(summary.totalCacheWriteTokens).toBe(3000);
      expect(summary.totalRequests).toBe(1);
      expect(summary.totalDurationMs).toBe(2500);
      expect(summary.ambientContextTokens).toBe(900);
    });

    test("aggregates multiple entries across models", () => {
      const lines = [
        JSON.stringify({ provider: "anthropic", model: "claude-sonnet-4-6", input_tokens: 10, output_tokens: 20, cache_read_tokens: 100, cache_write_tokens: 50, duration_ms: 1000 }),
        JSON.stringify({ provider: "anthropic", model: "claude-sonnet-4-6", input_tokens: 5, output_tokens: 15, cache_read_tokens: 200, cache_write_tokens: 0, duration_ms: 500 }),
        JSON.stringify({ provider: "openai", model: "gpt-4o", input_tokens: 30, output_tokens: 40, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 2000 }),
      ];
      const summary = parseTokenUsageJsonl(lines.join("\n"));
      expect(summary).not.toBeNull();
      expect(summary.totalRequests).toBe(3);
      expect(summary.totalInputTokens).toBe(45);
      expect(summary.totalOutputTokens).toBe(75);
      expect(summary.totalCacheReadTokens).toBe(300);
      expect(summary.totalCacheWriteTokens).toBe(50);
      expect(summary.totalDurationMs).toBe(3500);
      expect(summary.byModel["claude-sonnet-4-6"].requests).toBe(2);
      expect(summary.byModel["claude-sonnet-4-6"].inputTokens).toBe(15);
      expect(summary.byModel["gpt-4o"].requests).toBe(1);
      expect(summary.ambientContextTokens).toBe(25);
    });

    test("skips malformed lines and still parses valid ones", () => {
      const content = `{"model":"gpt-4o","input_tokens":10,"output_tokens":5,"cache_read_tokens":0,"cache_write_tokens":0,"duration_ms":100}
not-json
{"model":"gpt-4o","input_tokens":20,"output_tokens":10,"cache_read_tokens":0,"cache_write_tokens":0,"duration_ms":200}`;
      const summary = parseTokenUsageJsonl(content);
      expect(summary).not.toBeNull();
      expect(summary.totalRequests).toBe(2);
      expect(summary.totalInputTokens).toBe(30);
    });

    test("uses 'unknown' for entries without a model field", () => {
      const content = JSON.stringify({ input_tokens: 10, output_tokens: 5, duration_ms: 100 });
      const summary = parseTokenUsageJsonl(content);
      expect(summary).not.toBeNull();
      expect(summary.byModel["unknown"]).toBeDefined();
    });

    test("does not compute cache efficiency", () => {
      const content = JSON.stringify({ model: "m", input_tokens: 100, output_tokens: 10, cache_read_tokens: 900, cache_write_tokens: 0, duration_ms: 100 });
      const summary = parseTokenUsageJsonl(content);
      expect(summary).not.toBeNull();
      expect(summary).not.toHaveProperty("cacheEfficiency");
    });

    test("populates per-turn entries array in order", () => {
      const lines = [
        JSON.stringify({ model: "m1", input_tokens: 10, output_tokens: 5, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 100 }),
        JSON.stringify({ model: "m2", input_tokens: 20, output_tokens: 10, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 200 }),
      ];
      const summary = parseTokenUsageJsonl(lines.join("\n"));
      expect(summary.entries).toHaveLength(2);
      expect(summary.entries[0].model).toBe("m1");
      expect(summary.entries[0].inputTokens).toBe(10);
      expect(summary.entries[0].durationMs).toBe(100);
      expect(summary.entries[1].model).toBe("m2");
    });
  });

  describe("generateTokenUsageSummary", () => {
    test("returns empty string for null or zero-request summary", () => {
      expect(generateTokenUsageSummary(null)).toBe("");
      expect(generateTokenUsageSummary({ totalRequests: 0, byModel: {} })).toBe("");
    });

    test("renders header and table columns", () => {
      const summary = parseTokenUsageJsonl(JSON.stringify({ model: "claude-sonnet-4-6", provider: "anthropic", input_tokens: 100, output_tokens: 200, cache_read_tokens: 5000, cache_write_tokens: 3000, duration_ms: 2500 }));
      const md = generateTokenUsageSummary(summary);
      expect(md).toContain("| # | Alias | Input | Output | Cache Read | Cache Write | ΔAI Credits | AI Credits | Duration |");
      expect(md).toContain("sonnet46");
    });

    test("includes totals row", () => {
      const summary = parseTokenUsageJsonl(JSON.stringify({ model: "m", input_tokens: 10, output_tokens: 20, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 1000 }));
      const md = generateTokenUsageSummary(summary);
      expect(md).toContain("**Total**");
    });

    test("does not include cache efficiency", () => {
      const content = JSON.stringify({ model: "m", input_tokens: 100, output_tokens: 10, cache_read_tokens: 900, cache_write_tokens: 0, duration_ms: 100 });
      const summary = parseTokenUsageJsonl(content);
      const md = generateTokenUsageSummary(summary);
      expect(md).not.toContain("Cache efficiency");
    });

    test("renders rows in chronological (input) order", () => {
      const lines = [
        JSON.stringify({ model: "first-model", input_tokens: 5, output_tokens: 5, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 100 }),
        JSON.stringify({ model: "second-model", input_tokens: 1000, output_tokens: 500, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 500 }),
      ];
      const summary = parseTokenUsageJsonl(lines.join("\n"));
      const md = generateTokenUsageSummary(summary);
      const { formatModelEmojiAlias } = require("./model_aliases.cjs");
      const firstIdx = md.indexOf(formatModelEmojiAlias("first-model"));
      const secondIdx = md.indexOf(formatModelEmojiAlias("second-model"));
      expect(firstIdx).toBeLessThan(secondIdx);
    });

    test("does not include effective token columns in table", () => {
      const content = JSON.stringify({ model: "m", input_tokens: 100, output_tokens: 200, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 1000 });
      const summary = parseTokenUsageJsonl(content);
      const md = generateTokenUsageSummary(summary);
      expect(md).not.toContain("| ΔET |");
      expect(md).not.toContain("| ET |");
    });

    test("includes AI credits columns in table header", () => {
      const content = JSON.stringify({ model: "m", input_tokens: 100, output_tokens: 200, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 1000 });
      const summary = parseTokenUsageJsonl(content);
      const md = generateTokenUsageSummary(summary);
      expect(md).toContain("| ΔAI Credits |");
      expect(md).toContain("| AI Credits |");
      expect(md).not.toContain("effective token");
    });

    test("renders AIC value in totals row for known model with pricing", () => {
      const content = JSON.stringify({ model: "claude-sonnet-4-6", provider: "anthropic", input_tokens: 100, output_tokens: 200, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 1000 });
      const summary = parseTokenUsageJsonl(content);
      const md = generateTokenUsageSummary(summary);
      if (summary.totalAIC > 0) {
        // When pricing is available the totals row should contain a bold AIC value
        expect(md).toContain("**Total**");
        const { formatAIC } = require("./model_costs.cjs");
        expect(md).toContain(formatAIC(summary.totalAIC));
      }
    });

    test("compounded AIC equals sum of per-turn delta AIC values", () => {
      const lines = [
        JSON.stringify({ model: "claude-sonnet-4-6", provider: "anthropic", input_tokens: 100, output_tokens: 50, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 100 }),
        JSON.stringify({ model: "claude-sonnet-4-6", provider: "anthropic", input_tokens: 200, output_tokens: 100, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 200 }),
      ];
      const summary = parseTokenUsageJsonl(lines.join("\n"));
      const totalAIC = summary.entries.reduce((sum, e) => sum + (e.deltaAIC || 0), 0);
      expect(Math.abs(totalAIC - summary.totalAIC)).toBeLessThan(0.0001);
    });

    test("includes an AI credits legend", () => {
      const content = JSON.stringify({ model: "m", input_tokens: 100, output_tokens: 200, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 1000 });
      const summary = parseTokenUsageJsonl(content);
      const md = generateTokenUsageSummary(summary);
      expect(md).toContain("Legend:");
      expect(md).toContain("current AI credits pricing model");
      expect(md).not.toContain("effective token");
    });

    test("does not include cache efficiency or effective token wording", () => {
      const content = JSON.stringify({ model: "m", input_tokens: 100, output_tokens: 10, cache_read_tokens: 900, cache_write_tokens: 0, duration_ms: 100 });
      const summary = parseTokenUsageJsonl(content);
      const md = generateTokenUsageSummary(summary);
      expect(md).not.toContain("●");
      expect(md).not.toContain("Cache efficiency");
      expect(md).not.toContain("effective token");
    });
  });
});
