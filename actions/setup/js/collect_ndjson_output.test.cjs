import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
describe("collect_ndjson_output.cjs", () => {
  let mockCore, collectScript;
  (beforeEach(() => {
    (fs.existsSync("/tmp/gh-aw") || fs.mkdirSync("/tmp/gh-aw", { recursive: !0 }),
      (global.originalConsole = global.console),
      (global.console = { log: vi.fn(), error: vi.fn() }),
      (mockCore = {
        debug: vi.fn(),
        info: vi.fn(),
        notice: vi.fn(),
        warning: vi.fn(),
        error: vi.fn(),
        setFailed: vi.fn(),
        setOutput: vi.fn(),
        exportVariable: vi.fn(),
        setSecret: vi.fn(),
        getInput: vi.fn(),
        getBooleanInput: vi.fn(),
        getMultilineInput: vi.fn(),
        getState: vi.fn(),
        saveState: vi.fn(),
        startGroup: vi.fn(),
        endGroup: vi.fn(),
        group: vi.fn(),
        addPath: vi.fn(),
        setCommandEcho: vi.fn(),
        isDebug: vi.fn().mockReturnValue(!1),
        getIDToken: vi.fn(),
        toPlatformPath: vi.fn(),
        toPosixPath: vi.fn(),
        toWin32Path: vi.fn(),
        summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
      }),
      (global.core = mockCore),
      (global.context = { eventName: "issues", actor: "test-actor", repo: { owner: "test-owner", repo: "test-repo" }, payload: {} }),
      (global.github = { rest: { repos: { listCollaborators: vi.fn().mockResolvedValue({ data: [] }) }, users: { getByUsername: vi.fn() } } }));
    const scriptPath = path.join(__dirname, "collect_ndjson_output.cjs");
    ((collectScript = fs.readFileSync(scriptPath, "utf8")),
      (global.fs = fs),
      fs.existsSync("/opt/gh-aw/safeoutputs") || fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
      fs.writeFileSync(
        path.join("/opt/gh-aw/safeoutputs", "validation.json"),
        JSON.stringify({
          create_issue: {
            defaultMax: 1,
            fields: {
              title: { required: !0, type: "string", sanitize: !0, maxLength: 128 },
              body: { required: !0, type: "string", sanitize: !0, maxLength: 65e3 },
              labels: { type: "array", itemType: "string", itemSanitize: !0, itemMaxLength: 128 },
              parent: { issueOrPRNumber: !0 },
              temporary_id: { type: "string" },
            },
          },
          add_comment: { defaultMax: 1, fields: { body: { required: !0, type: "string", sanitize: !0, maxLength: 65e3 }, item_number: { issueOrPRNumber: !0 } } },
          create_pull_request: {
            defaultMax: 1,
            fields: {
              title: { required: !0, type: "string", sanitize: !0, maxLength: 128 },
              body: { required: !0, type: "string", sanitize: !0, maxLength: 65e3 },
              branch: { required: !0, type: "string", sanitize: !0, maxLength: 256 },
              labels: { type: "array", itemType: "string", itemSanitize: !0, itemMaxLength: 128 },
            },
          },
          update_issue: {
            defaultMax: 1,
            customValidation: "requiresOneOf:status,title,body",
            fields: { status: { type: "string", enum: ["open", "closed"] }, title: { type: "string", sanitize: !0, maxLength: 128 }, body: { type: "string", sanitize: !0, maxLength: 65e3 }, issue_number: { issueOrPRNumber: !0 } },
          },
          create_pull_request_review_comment: {
            defaultMax: 1,
            customValidation: "startLineLessOrEqualLine",
            fields: {
              path: { required: !0, type: "string" },
              line: { required: !0, positiveInteger: !0 },
              body: { required: !0, type: "string", sanitize: !0, maxLength: 65e3 },
              start_line: { optionalPositiveInteger: !0 },
              side: { type: "string", enum: ["LEFT", "RIGHT"] },
            },
          },
          link_sub_issue: { defaultMax: 5, customValidation: "parentAndSubDifferent", fields: { parent_issue_number: { required: !0, issueNumberOrTemporaryId: !0 }, sub_issue_number: { required: !0, issueNumberOrTemporaryId: !0 } } },
          noop: { defaultMax: 1, fields: { message: { required: !0, type: "string", sanitize: !0, maxLength: 65e3 } } },
          missing_tool: {
            defaultMax: 20,
            fields: { tool: { required: !0, type: "string", sanitize: !0, maxLength: 128 }, reason: { required: !0, type: "string", sanitize: !0, maxLength: 256 }, alternatives: { type: "string", sanitize: !0, maxLength: 512 } },
          },
          create_code_scanning_alert: {
            defaultMax: 40,
            fields: {
              file: { required: !0, type: "string", sanitize: !0, maxLength: 512 },
              line: { required: !0, positiveInteger: !0 },
              severity: { required: !0, type: "string", enum: ["error", "warning", "info", "note"] },
              message: { required: !0, type: "string", sanitize: !0, maxLength: 2048 },
              column: { optionalPositiveInteger: !0 },
              ruleIdSuffix: { type: "string", pattern: "^[a-zA-Z0-9_-]+$", patternError: "must contain only alphanumeric characters, hyphens, and underscores", sanitize: !0, maxLength: 128 },
            },
          },
          assign_to_agent: {
            defaultMax: 1,
            customValidation: "requiresOneOf:issue_number,pull_number",
            fields: {
              issue_number: { issueNumberOrTemporaryId: !0 },
              pull_number: { optionalPositiveInteger: !0 },
              agent: { type: "string", sanitize: !0, maxLength: 128 },
            },
          },
          create_discussion: {
            defaultMax: 1,
            fields: { title: { required: !0, type: "string", sanitize: !0, maxLength: 128 }, body: { required: !0, type: "string", sanitize: !0, maxLength: 65e3 }, category: { type: "string", sanitize: !0, maxLength: 128 } },
          },
          update_release: {
            defaultMax: 1,
            fields: { tag: { type: "string", sanitize: !0, maxLength: 256 }, operation: { required: !0, type: "string", enum: ["replace", "append", "prepend"] }, body: { required: !0, type: "string", sanitize: !0, maxLength: 65e3 } },
          },
        })
      ));
  }),
    afterEach(() => {
      ["/tmp/gh-aw/test-ndjson-output.txt", "/tmp/gh-aw/agent_output.json"].forEach(file => {
        try {
          fs.existsSync(file) && fs.unlinkSync(file);
        } catch (error) {}
      });
      try {
        fs.existsSync("/tmp/gh-aw/safeoutputs") &&
          (fs.readdirSync("/tmp/gh-aw/safeoutputs").forEach(file => {
            const filePath = path.join("/tmp/gh-aw/safeoutputs", file);
            fs.statSync(filePath).isDirectory() ? fs.rmSync(filePath, { recursive: !0, force: !0 }) : fs.unlinkSync(filePath);
          }),
          fs.rmdirSync("/tmp/gh-aw/safeoutputs"));
      } catch (error) {}
      "undefined" != typeof global && (delete global.fs, delete global.core, global.originalConsole && ((global.console = global.originalConsole), delete global.originalConsole));
    }),
    it("should handle missing GH_AW_SAFE_OUTPUTS environment variable", async () => {
      (delete process.env.GH_AW_SAFE_OUTPUTS,
        await eval(`(async () => { ${collectScript}; await main(); })()`),
        expect(mockCore.setOutput).toHaveBeenCalledWith("output", ""),
        expect(mockCore.info).toHaveBeenCalledWith("GH_AW_SAFE_OUTPUTS not set, no output to collect"));
    }),
    it("should handle missing output file", async () => {
      ((process.env.GH_AW_SAFE_OUTPUTS = "/tmp/gh-aw/nonexistent-file.txt"),
        await eval(`(async () => { ${collectScript}; await main(); })()`),
        expect(mockCore.setOutput).toHaveBeenCalledWith("output", ""),
        expect(mockCore.info).toHaveBeenCalledWith("Output file does not exist: /tmp/gh-aw/nonexistent-file.txt"));
    }),
    it("should handle empty output file", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt";
      (fs.writeFileSync(testFile, ""),
        (process.env.GH_AW_SAFE_OUTPUTS = testFile),
        await eval(`(async () => { ${collectScript}; await main(); })()`),
        expect(mockCore.setOutput).toHaveBeenCalledWith("output", '{"items":[],"errors":[]}'),
        expect(mockCore.info).toHaveBeenCalledWith("Output file is empty"));
    }),
    it("should validate and parse valid JSONL content", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent = '{"type": "create_issue", "title": "Test Issue", "body": "Test body"}\n{"type": "add_comment", "body": "Test comment"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_issue": true, "add_comment": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(2), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.items[1].type).toBe("add_comment"), expect(parsedOutput.errors).toHaveLength(0));
    }),
    it("should reject items with unexpected output types", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent = '{"type": "create_issue", "title": "Test Issue", "body": "Test body"}\n{"type": "unexpected-type", "data": "some data"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_issue": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.errors).toHaveLength(1), expect(parsedOutput.errors[0]).toContain("Unexpected output type"));
    }),
    it("should validate required fields for create_issue type", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent = '{"type": "create_issue", "title": "Test Issue"}\n{"type": "create_issue", "body": "Test body"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_issue": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
        fs.writeFileSync(configPath, __config),
        await eval(`(async () => { ${collectScript}; await main(); })()`),
        expect(mockCore.warning).toHaveBeenCalled(),
        expect(mockCore.setFailed).not.toHaveBeenCalled());
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(0),
        expect(parsedOutput.errors.length).toBeGreaterThan(0),
        expect(parsedOutput.errors.some(e => e.includes("requires a 'body' field (string)"))).toBe(!0),
        expect(parsedOutput.errors.some(e => e.includes("requires a 'title' field (string)"))).toBe(!0));
    }),
    it("should validate required fields for add-labels type", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent = '{"type": "add_labels", "labels": ["bug", "enhancement"]}\n{"type": "add_labels", "labels": "not-an-array"}\n{"type": "add_labels", "labels": [1, 2, 3]}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"add_labels": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].labels).toEqual(["bug", "enhancement"]), expect(parsedOutput.errors).toHaveLength(2));
    }),
    it("should validate required fields for create-pull-request type", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent =
          '{"type": "create_pull_request", "title": "Test PR"}\n{"type": "create_pull_request", "body": "Test body"}\n{"type": "create_pull_request", "branch": "test-branch"}\n{"type": "create_pull_request", "title": "Complete PR", "body": "Test body", "branch": "feature-branch"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_pull_request": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(1),
        expect(parsedOutput.items[0].title).toBe("Complete PR"),
        expect(parsedOutput.items[0].body).toBe("Test body"),
        expect(parsedOutput.items[0].branch).toBe("feature-branch"),
        expect(parsedOutput.errors).toHaveLength(3),
        expect(parsedOutput.errors[0]).toContain("requires a 'body' field (string)"),
        expect(parsedOutput.errors[1]).toContain("requires a 'title' field (string)"),
        expect(parsedOutput.errors[2]).toContain("requires a 'title' field (string)"));
    }),
    it("should handle invalid JSON lines", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent = '{"type": "create_issue", "title": "Test Issue", "body": "Test body"}\n{invalid json}\n{"type": "add_comment", "body": "Test comment"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_issue": true, "add_comment": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(2), expect(parsedOutput.errors).toHaveLength(1), expect(parsedOutput.errors[0]).toContain("Invalid JSON"));
    }),
    it("should allow multiple items of supported types up to limits", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent = '{"type": "create_issue", "title": "First Issue", "body": "First body"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_issue": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].title).toBe("First Issue"), expect(parsedOutput.errors).toHaveLength(0));
    }),
    it("should respect max limits from config", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent =
          '{"type": "create_issue", "title": "First Issue", "body": "First body"}\n{"type": "create_issue", "title": "Second Issue", "body": "Second body"}\n{"type": "create_issue", "title": "Third Issue", "body": "Third body"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_issue": {"max": 2}}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(2),
        expect(parsedOutput.items[0].title).toBe("First Issue"),
        expect(parsedOutput.items[1].title).toBe("Second Issue"),
        expect(parsedOutput.errors).toHaveLength(1),
        expect(parsedOutput.errors[0]).toContain("Too many items of type 'create_issue'. Maximum allowed: 2"));
    }),
    it("should validate required fields for create-discussion type", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent = '{"type": "create_discussion", "title": "Test Discussion"}\n{"type": "create_discussion", "body": "Test body"}\n{"type": "create_discussion", "title": "Valid Discussion", "body": "Valid body"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_discussion": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(1),
        expect(parsedOutput.items[0].title).toBe("Valid Discussion"),
        expect(parsedOutput.items[0].body).toBe("Valid body"),
        expect(parsedOutput.errors).toHaveLength(2),
        expect(parsedOutput.errors[0]).toContain("requires a 'body' field (string)"),
        expect(parsedOutput.errors[1]).toContain("requires a 'title' field (string)"));
    }),
    it("should skip empty lines", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent = '{"type": "create_issue", "title": "Test Issue", "body": "Test body"}\n\n{"type": "add_comment", "body": "Test comment"}\n';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_issue": true, "add_comment": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(2), expect(parsedOutput.errors).toHaveLength(0));
    }),
    it("should validate required fields for create-pull-request-review-comment type", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent =
          '{"type": "create_pull_request_review_comment", "path": "src/file.js", "line": 10, "body": "Good code"}\n{"type": "create_pull_request_review_comment", "path": "src/file.js", "line": "invalid", "body": "Comment"}\n{"type": "create_pull_request_review_comment", "path": "src/file.js", "body": "Missing line"}\n{"type": "create_pull_request_review_comment", "line": 15}\n{"type": "create_pull_request_review_comment", "path": "src/file.js", "line": 20, "start_line": 25, "body": "Invalid range"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_pull_request_review_comment": {"max": 10}}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(1),
        expect(parsedOutput.items[0].path).toBe("src/file.js"),
        expect(parsedOutput.items[0].line).toBe(10),
        expect(parsedOutput.items[0].body).toBeDefined(),
        expect(parsedOutput.errors).toHaveLength(4),
        expect(parsedOutput.errors.some(e => e.includes("line' must be a valid positive integer"))).toBe(!0),
        expect(parsedOutput.errors.some(e => e.includes("'line' is required"))).toBe(!0),
        expect(parsedOutput.errors.some(e => e.includes("requires a 'path' field (string)"))).toBe(!0),
        expect(parsedOutput.errors.some(e => e.includes("start_line' must be less than or equal to 'line'"))).toBe(!0));
    }),
    it("should validate optional fields for create-pull-request-review-comment type", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent =
          '{"type": "create_pull_request_review_comment", "path": "src/file.js", "line": 20, "start_line": 15, "side": "LEFT", "body": "Multi-line comment"}\n{"type": "create_pull_request_review_comment", "path": "src/file.js", "line": 25, "side": "INVALID", "body": "Invalid side"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_pull_request_review_comment": {"max": 10}}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(1),
        expect(parsedOutput.items[0].side).toBe("LEFT"),
        expect(parsedOutput.items[0].start_line).toBe(15),
        expect(parsedOutput.errors).toHaveLength(1),
        expect(parsedOutput.errors[0]).toContain("side' must be 'LEFT' or 'RIGHT'"));
    }),
    it("should validate required fields for update_release type", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent =
          '{"type": "update_release", "tag": "v1.0.0", "operation": "replace", "body": "New release notes"}\n{"type": "update_release", "tag": "v1.0.0", "operation": "prepend", "body": "Prepended notes"}\n{"type": "update_release", "operation": "replace", "body": "Tag omitted - will be inferred"}\n{"type": "update_release", "tag": "v1.0.0", "operation": "invalid", "body": "Notes"}\n{"type": "update_release", "tag": "v1.0.0", "body": "Missing operation"}\n{"type": "update_release", "tag": "v1.0.0", "operation": "append"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"update_release": {"max": 10}}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(3),
        expect(parsedOutput.items[0].tag).toBe("v1.0.0"),
        expect(parsedOutput.items[0].operation).toBe("replace"),
        expect(parsedOutput.items[1].operation).toBe("prepend"),
        expect(parsedOutput.items[2].tag).toBeUndefined(),
        expect(parsedOutput.items[2].operation).toBe("replace"),
        expect(parsedOutput.items[0].body).toBeDefined(),
        expect(parsedOutput.errors).toHaveLength(3),
        expect(parsedOutput.errors.some(e => e.includes("operation' must be one of:"))).toBe(!0),
        expect(parsedOutput.errors.some(e => e.includes("requires a 'operation' field (string)"))).toBe(!0),
        expect(parsedOutput.errors.some(e => e.includes("requires a 'body' field (string)"))).toBe(!0));
    }),
    it("should respect max limits for create-pull-request-review-comment from config", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        items = [];
      for (let i = 1; i <= 12; i++) items.push(`{"type": "create_pull_request_review_comment", "path": "src/file.js", "line": ${i}, "body": "Comment ${i}"}`);
      const ndjsonContent = items.join("\n");
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_pull_request_review_comment": {"max": 5}}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(5), expect(parsedOutput.errors).toHaveLength(7), expect(parsedOutput.errors.every(e => e.includes("Too many items of type 'create_pull_request_review_comment'. Maximum allowed: 5"))).toBe(!0));
    }),
    describe("JSON repair functionality", () => {
      (it("should repair JSON with unescaped quotes in string values", async () => {
        const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
          ndjsonContent = '{"type": "create_issue", "title": "Issue with "quotes" inside", "body": "Test body"}';
        (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
        const __config = '{"create_issue": true}',
          configPath = "/opt/gh-aw/safeoutputs/config.json";
        (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
        const setOutputCalls = mockCore.setOutput.mock.calls,
          outputCall = setOutputCalls.find(call => "output" === call[0]);
        expect(outputCall).toBeDefined();
        const parsedOutput = JSON.parse(outputCall[1]);
        (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].title).toContain("quotes"), expect(parsedOutput.errors).toHaveLength(0));
      }),
        it("should repair JSON with missing quotes around object keys", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{type: "create_issue", title: "Test Issue", body: "Test body"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with trailing commas", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Test Issue", "body": "Test body",}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with single quotes", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{'type': 'create_issue', 'title': 'Test Issue', 'body': 'Test body'}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with missing closing braces", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Test Issue", "body": "Test body"';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with missing opening braces", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '"type": "create_issue", "title": "Test Issue", "body": "Test body"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with newlines in string values", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Test Issue", "body": "Line 1\\nLine 2\\nLine 3"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].body).toContain("Line 1"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with tabs and special characters", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Test\tIssue", "body": "Test\tbody"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with array syntax issues", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "add_labels", "labels": ["bug", "enhancement",}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"add_labels": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].labels).toEqual(["bug", "enhancement"]), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should handle complex repair scenarios with multiple issues", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{type: 'create_issue', title: 'Issue with \"quotes\" and trailing,', body: 'Multi\\nline\\ntext',";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should handle JSON broken across multiple lines (real multiline scenario)", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Test Issue", "body": "Line 1\nLine 2\nLine 3"}\n{"type": "add_comment", "body": "This is a valid line"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true, "add_comment": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("add_comment"),
            expect(parsedOutput.errors.length).toBeGreaterThan(0),
            expect(parsedOutput.errors.some(error => error.includes("JSON parsing failed"))).toBe(!0));
        }),
        it("should still report error if repair fails completely", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{completely broken json with no hope: of repair [[[}}}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, __config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors.length).toBeGreaterThan(0), expect(parsedOutput.errors.some(e => e.includes("JSON parsing failed"))).toBe(!0));
        }),
        it("should preserve valid JSON without modification", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Perfect JSON", "body": "This should not be modified"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].title).toBe("Perfect JSON"), expect(parsedOutput.items[0].body).toBe("This should not be modified"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair mixed quote types in same object", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{\"type\": 'create_issue', \"title\": 'Mixed quotes', 'body': \"Test body\"}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.items[0].title).toBe("Mixed quotes"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair arrays ending with wrong bracket type", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "add_labels", "labels": ["bug", "feature", "enhancement"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"add_labels": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].labels).toEqual(["bug", "feature", "enhancement"]), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should handle simple missing closing brackets with graceful repair", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "add_labels", "labels": ["bug", "feature"';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"add_labels": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          parsedOutput.items.length > 0
            ? (expect(parsedOutput.items[0].type).toBe("add_labels"), expect(parsedOutput.items[0].labels).toEqual(["bug", "feature"]), expect(parsedOutput.errors).toHaveLength(0))
            : (expect(mockCore.setFailed).not.toHaveBeenCalled(), expect(mockCore.warning).toHaveBeenCalled(), expect(parsedOutput.errors.length).toBeGreaterThan(0));
        }),
        it("should repair nested objects with multiple issues", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{type: 'create_issue', title: 'Nested test', body: 'Body text', labels: ['bug', 'priority',}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.items[0].labels).toEqual(["bug", "priority"]), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with Unicode characters and escape sequences", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{type: 'create_issue', title: 'Unicode test éñ', body: 'Body with \\u0040 symbols',";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.items[0].title).toContain("é"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with control characters (null, backspace, form feed)", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Test\0Issue", "body": "Body\bwith\fcontrolchars"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("create_issue"),
            expect(parsedOutput.items[0].title).toBe("TestIssue"),
            expect(parsedOutput.items[0].body).toBe("Bodywithcontrolchars"),
            expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with device control characters", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "DeviceControlTest", "body": "Texthere"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("create_issue"),
            expect(parsedOutput.items[0].title).toBe("DeviceControlTest"),
            expect(parsedOutput.items[0].body).toBe("Texthere"),
            expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON preserving valid escape sequences (newline, tab, carriage return)", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Valid\\tTab", "body": "Line1\\nLine2\\rCarriage"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("create_issue"),
            expect(parsedOutput.items[0].title).toBe("Valid\tTab"),
            expect(parsedOutput.items[0].body).toBe("Line1\nLine2\rCarriage"),
            expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with mixed control characters and regular escape sequences", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Mixed\0test\\nwith text", "body": "Bodywith\\ttabend"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("create_issue"),
            expect(parsedOutput.items[0].title).toMatch(/Mixedtest\nwith text/),
            expect(parsedOutput.items[0].body).toMatch(/Bodywith\ttabend/),
            expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with DEL character (0x7F) and other high control chars", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "TestDel", "body": "Bodywithcontrol"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("create_issue"),
            expect(parsedOutput.items[0].title).toBe("TestDel"),
            expect(parsedOutput.items[0].body).toBe("Bodywithcontrol"),
            expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with all ASCII control characters in sequence", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Control test\0\\t\\n", "body": "End of test"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"));
          const title = parsedOutput.items[0].title;
          (expect(title).toBe("Control test"), expect(parsedOutput.items[0].body).toBe("End of test"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should test control character repair in isolation using the repair function", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{type: \"create_issue\", title: 'Test\0with\bcontrol\fchars', body: 'Bodytext',}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("create_issue"),
            expect(parsedOutput.items[0].title).toBe("Testwithcontrolchars"),
            expect(parsedOutput.items[0].body).toBe("Bodytext"),
            expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should test repair function behavior with specific control character scenarios", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Control\0", "body": "Test\bend"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("create_issue"),
            expect(parsedOutput.items[0].title).toBe("Control"),
            expect(parsedOutput.items[0].body).toBe("Testend"),
            expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair JSON with numbers, booleans, and null values", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{type: 'create_issue', title: 'Complex types test', body: 'Body text', priority: 5, urgent: true, assignee: null,}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("create_issue"),
            expect(parsedOutput.items[0].priority).toBe(5),
            expect(parsedOutput.items[0].urgent).toBe(!0),
            expect(parsedOutput.items[0].assignee).toBe(null),
            expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should attempt repair but fail gracefully with excessive malformed JSON", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{,type: 'create_issue',, title: 'Extra commas', body: 'Test',, labels: ['bug',,],}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, __config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors.length).toBeGreaterThan(0), expect(parsedOutput.errors.some(e => e.includes("JSON parsing failed"))).toBe(!0));
        }),
        it("should repair very long strings with multiple issues", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            longBody = 'This is a very long body text that contains "quotes" and other\\nspecial characters including tabs\\t and newlines\\r\\n and more text that goes on and on.',
            ndjsonContent = `{type: 'create_issue', title: 'Long string test', body: '${longBody}',}`;
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.items[0].body).toContain("very long body"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair deeply nested structures", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{type: 'create_issue', title: 'Nested test', body: 'Body', metadata: {project: 'test', tags: ['important', 'urgent',}, version: 1.0,}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("create_issue"),
            expect(parsedOutput.items[0].metadata).toBeDefined(),
            expect(parsedOutput.items[0].metadata.project).toBe("test"),
            expect(parsedOutput.items[0].metadata.tags).toEqual(["important", "urgent"]),
            expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should handle complex backslash scenarios with graceful failure", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{type: 'create_issue', title: 'Escape test with \"quotes\" and \\\\backslashes', body: 'Test body',}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          1 === parsedOutput.items.length
            ? (expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.items[0].title).toContain("quotes"), expect(parsedOutput.errors).toHaveLength(0))
            : (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors).toHaveLength(1), expect(parsedOutput.errors[0]).toContain("JSON parsing failed"));
        }),
        it("should repair JSON with carriage returns and form feeds", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{type: 'create_issue', title: 'Special chars', body: 'Text with\\rcarriage\\fform feed',}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should gracefully handle repair attempts on fundamentally broken JSON", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{{{[[[type]]]}}} === \"broken\" &&& title ??? 'impossible to repair' @@@ body";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, __config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors.length).toBeGreaterThan(0), expect(parsedOutput.errors.some(e => e.includes("JSON parsing failed"))).toBe(!0));
        }),
        it("should handle repair of JSON with missing property separators", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{type 'create_issue', title 'Missing colons', body 'Test body'}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, __config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors.length).toBeGreaterThan(0), expect(parsedOutput.errors.some(e => e.includes("JSON parsing failed"))).toBe(!0));
        }),
        it("should repair arrays with mixed bracket types in complex structures", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "{type: 'add-labels', labels: ['priority', 'bug', 'urgent'}, extra: ['data', 'here'}";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"add_labels": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("add_labels"), expect(parsedOutput.items[0].labels).toEqual(["priority", "bug", "urgent"]), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should gracefully handle cases with multiple trailing commas", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Test", "body": "Test body",,,}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          parsedOutput.items.length > 0
            ? (expect(parsedOutput.items[0].type).toBe("create_issue"), expect(parsedOutput.items[0].title).toBe("Test"), expect(parsedOutput.errors).toHaveLength(0))
            : (expect(mockCore.setFailed).not.toHaveBeenCalled(), expect(mockCore.warning).toHaveBeenCalled(), expect(parsedOutput.errors.length).toBeGreaterThan(0));
        }),
        it("should repair JSON with simple missing closing brackets", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "add_labels", "labels": ["bug", "feature"]}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"add_labels": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("add_labels"), expect(parsedOutput.items[0].labels).toEqual(["bug", "feature"]), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should repair combination of unquoted keys and trailing commas", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{type: "create_issue", title: "Combined issues", body: "Test body", priority: 1,}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1),
            expect(parsedOutput.items[0].type).toBe("create_issue"),
            expect(parsedOutput.items[0].title).toBe("Combined issues"),
            expect(parsedOutput.items[0].priority).toBe(1),
            expect(parsedOutput.errors).toHaveLength(0));
        }));
    }),
    it("should store validated output in agent_output.json file and set GH_AW_AGENT_OUTPUT environment variable", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent = '{"type": "create_issue", "title": "Test Issue", "body": "Test body"}\n{"type": "add_comment", "body": "Test comment"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_issue": true, "add_comment": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`), expect(fs.existsSync("/tmp/gh-aw/agent_output.json")).toBe(!0));
      const agentOutputContent = fs.readFileSync("/tmp/gh-aw/agent_output.json", "utf8"),
        agentOutputJson = JSON.parse(agentOutputContent);
      (expect(agentOutputJson.items).toHaveLength(2),
        expect(agentOutputJson.items[0].type).toBe("create_issue"),
        expect(agentOutputJson.items[1].type).toBe("add_comment"),
        expect(agentOutputJson.errors).toHaveLength(0),
        expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AGENT_OUTPUT", "/tmp/gh-aw/agent_output.json"));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(2), expect(parsedOutput.errors).toHaveLength(0));
    }),
    it("should handle errors when writing agent_output.json file gracefully", async () => {
      const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
        ndjsonContent = '{"type": "create_issue", "title": "Test Issue", "body": "Test body"}';
      (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
      const __config = '{"create_issue": true}',
        configPath = "/opt/gh-aw/safeoutputs/config.json";
      (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config));
      const originalWriteFileSync = fs.writeFileSync;
      ((fs.writeFileSync = vi.fn((filePath, content, options) => {
        if ("/tmp/gh-aw/agent_output.json" === filePath) throw new Error("Permission denied");
        return originalWriteFileSync(filePath, content, options);
      })),
        await eval(`(async () => { ${collectScript}; await main(); })()`),
        (fs.writeFileSync = originalWriteFileSync),
        expect(mockCore.error).toHaveBeenCalledWith("Failed to write agent output file: Permission denied"));
      const setOutputCalls = mockCore.setOutput.mock.calls,
        outputCall = setOutputCalls.find(call => "output" === call[0]);
      expect(outputCall).toBeDefined();
      const parsedOutput = JSON.parse(outputCall[1]);
      (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.errors).toHaveLength(0), expect(mockCore.exportVariable).not.toHaveBeenCalled());
    }),
    describe("create_code_scanning_alert validation", () => {
      (it("should validate valid code scanning alert entries", async () => {
        const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
          ndjsonContent =
            '{"type": "create_code_scanning_alert", "file": "src/auth.js", "line": 42, "severity": "error", "message": "SQL injection vulnerability"}\n{"type": "create_code_scanning_alert", "file": "src/utils.js", "line": 25, "severity": "warning", "message": "XSS vulnerability", "column": 10, "ruleIdSuffix": "xss-check"}\n{"type": "create_code_scanning_alert", "file": "src/complete.js", "line": "30", "severity": "NOTE", "message": "Complete example", "column": "5", "ruleIdSuffix": "complete-rule"}';
        (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
        const __config = '{"create_code_scanning_alert": true}',
          configPath = "/opt/gh-aw/safeoutputs/config.json";
        (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
        const setOutputCalls = mockCore.setOutput.mock.calls,
          outputCall = setOutputCalls.find(call => "output" === call[0]);
        expect(outputCall).toBeDefined();
        const parsedOutput = JSON.parse(outputCall[1]);
        (expect(parsedOutput.items).toHaveLength(3),
          expect(parsedOutput.errors).toHaveLength(0),
          expect(parsedOutput.items[0]).toEqual({ type: "create_code_scanning_alert", file: "src/auth.js", line: 42, severity: "error", message: "SQL injection vulnerability" }),
          expect(parsedOutput.items[1]).toEqual({ type: "create_code_scanning_alert", file: "src/utils.js", line: 25, severity: "warning", message: "XSS vulnerability", column: 10, ruleIdSuffix: "xss-check" }),
          expect(parsedOutput.items[2]).toEqual({ type: "create_code_scanning_alert", file: "src/complete.js", line: 30, severity: "note", message: "Complete example", column: 5, ruleIdSuffix: "complete-rule" }));
      }),
        it("should reject code scanning alert entries with missing required fields", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent =
              '{"type": "create_code_scanning_alert", "severity": "error", "message": "Missing file field"}\n{"type": "create_code_scanning_alert", "file": "src/missing.js", "severity": "error", "message": "Missing line field"}\n{"type": "create_code_scanning_alert", "file": "src/missing2.js", "line": 10, "message": "Missing severity field"}\n{"type": "create_code_scanning_alert", "file": "src/missing3.js", "line": 10, "severity": "error"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_code_scanning_alert": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, __config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0),
            expect(parsedOutput.errors.length).toBeGreaterThan(0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert requires a 'file' field (string)"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'line' is required"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert requires a 'severity' field (string)"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert requires a 'message' field (string)"))).toBe(!0));
        }),
        it("should reject code scanning alert entries with invalid field types", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent =
              '{"type": "create_code_scanning_alert", "file": 123, "line": 10, "severity": "error", "message": "File should be string"}\n{"type": "create_code_scanning_alert", "file": "src/test.js", "line": null, "severity": "error", "message": "Line should be number or string"}\n{"type": "create_code_scanning_alert", "file": "src/test.js", "line": 10, "severity": 123, "message": "Severity should be string"}\n{"type": "create_code_scanning_alert", "file": "src/test.js", "line": 10, "severity": "error", "message": 123}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_code_scanning_alert": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, __config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0),
            expect(parsedOutput.errors.length).toBeGreaterThan(0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert requires a 'file' field (string)"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'line' is required"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert requires a 'severity' field (string)"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert requires a 'message' field (string)"))).toBe(!0));
        }),
        it("should reject code scanning alert entries with invalid severity levels", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent =
              '{"type": "create_code_scanning_alert", "file": "src/test.js", "line": 10, "severity": "invalid-level", "message": "Invalid severity"}\n{"type": "create_code_scanning_alert", "file": "src/test2.js", "line": 15, "severity": "critical", "message": "Unsupported severity"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_code_scanning_alert": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, __config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0),
            expect(parsedOutput.errors.length).toBeGreaterThan(0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'severity' must be one of: error, warning, info, note"))).toBe(!0));
        }),
        it("should reject code scanning alert entries with invalid optional fields", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent =
              '{"type": "create_code_scanning_alert", "file": "src/test.js", "line": 10, "severity": "error", "message": "Test", "column": "invalid"}\n{"type": "create_code_scanning_alert", "file": "src/test2.js", "line": 15, "severity": "error", "message": "Test", "ruleIdSuffix": 123}\n{"type": "create_code_scanning_alert", "file": "src/test3.js", "line": 20, "severity": "error", "message": "Test", "ruleIdSuffix": "bad rule!@#"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_code_scanning_alert": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, __config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0),
            expect(parsedOutput.errors.length).toBeGreaterThan(0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'column' must be a valid positive integer (got: invalid)"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'ruleIdSuffix' must be a string"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'ruleIdSuffix' must contain only alphanumeric characters, hyphens, and underscores"))).toBe(!0));
        }),
        it("should handle mixed valid and invalid code scanning alert entries", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent =
              '{"type": "create_code_scanning_alert", "file": "src/valid.js", "line": 10, "severity": "error", "message": "Valid entry"}\n{"type": "create_code_scanning_alert", "file": "src/missing.js", "severity": "error", "message": "Missing line field"}\n{"type": "create_code_scanning_alert", "file": "src/valid2.js", "line": 20, "severity": "warning", "message": "Another valid entry", "column": 5}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_code_scanning_alert": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(2),
            expect(parsedOutput.errors).toHaveLength(1),
            expect(parsedOutput.items[0].file).toBe("src/valid.js"),
            expect(parsedOutput.items[1].file).toBe("src/valid2.js"),
            expect(parsedOutput.errors).toContain("Line 2: create_code_scanning_alert 'line' is required"));
        }),
        it("should reject code scanning alert entries with invalid line and column values", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent =
              '{"type": "create_code_scanning_alert", "file": "src/test.js", "line": "invalid", "severity": "error", "message": "Invalid line string"}\n{"type": "create_code_scanning_alert", "file": "src/test2.js", "line": 0, "severity": "error", "message": "Zero line number"}\n{"type": "create_code_scanning_alert", "file": "src/test3.js", "line": -5, "severity": "error", "message": "Negative line number"}\n{"type": "create_code_scanning_alert", "file": "src/test4.js", "line": 10, "column": "abc", "severity": "error", "message": "Invalid column string"}\n{"type": "create_code_scanning_alert", "file": "src/test5.js", "line": 10, "column": 0, "severity": "error", "message": "Zero column number"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_code_scanning_alert": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, __config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0),
            expect(parsedOutput.errors.length).toBeGreaterThan(0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'line' must be a valid positive integer (got: invalid)"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'line' must be a valid positive integer (got: 0)"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'line' must be a valid positive integer (got: -5)"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'column' must be a valid positive integer (got: abc)"))).toBe(!0),
            expect(parsedOutput.errors.some(e => e.includes("create_code_scanning_alert 'column' must be a valid positive integer (got: 0)"))).toBe(!0));
        }));
    }),
    describe("Content sanitization functionality", () => {
      (it("should preserve command-line flags with colons", async () => {
        const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
          ndjsonContent = '{"type": "create_issue", "title": "Test issue", "body": "Use z3 -v:10 and z3 -memory:high for performance monitoring"}';
        (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
        const __config = '{"create_issue": true}',
          configPath = "/opt/gh-aw/safeoutputs/config.json";
        (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
          fs.writeFileSync(configPath, __config),
          await eval(`(async () => { ${collectScript}; await main(); })()`),
          expect(mockCore.setOutput).toHaveBeenCalledWith("output", expect.any(String)));
        const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
          parsedOutput = JSON.parse(outputCall[1]);
        (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].body).toBe("Use z3 -v:10 and z3 -memory:high for performance monitoring"), expect(parsedOutput.errors).toHaveLength(0));
      }),
        it("should preserve various command-line flag patterns", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "CLI Flags Test", "body": "Various flags: gcc -std:c++20, clang -target:x86_64, rustc -C:opt-level=3, javac -cp:lib/*, python -W:ignore, node --max-old-space-size:8192"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          expect(parsedOutput.items[0].body).toBe("Various flags: gcc -std:c++20, clang -target:x86_64, rustc -C:opt-level=3, javac -cp:lib/*, python -W:ignore, node --max-old-space-size:8192");
        }),
        it("should redact non-https protocols while preserving command flags", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Protocol Test", "body": "Use https://github.com/repo for code, avoid ftp://example.com/file and git://example.com/repo, but z3 -v:10 should work"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          expect(parsedOutput.items[0].body).toBe("Use https://github.com/repo for code, avoid (example.com/redacted) and (example.com/redacted) but z3 -v:10 should work");
        }),
        it("should handle mixed protocols and command flags in complex text", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent =
              '{"type": "create_issue", "title": "Complex Test", "body": "Install from https://github.com/z3prover/z3, then run: z3 -v:10 -memory:high -timeout:30000. Avoid ssh://git.example.com/repo.git or file://localhost/path"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          expect(parsedOutput.items[0].body).toBe("Install from https://github.com/z3prover/z3, then run: z3 -v:10 -memory:high -timeout:30000. Avoid (git.example.com/redacted) or (localhost/redacted)");
        }),
        it("should preserve allowed domains while redacting unknown ones", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Domain Test", "body": "GitHub URLs: https://github.com/repo, https://api.github.com/users, https://githubusercontent.com/file. External: https://example.com/page"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          expect(parsedOutput.items[0].body).toBe("GitHub URLs: https://github.com/repo, https://api.github.com/users, https://githubusercontent.com/file. External: (example.com/redacted)");
        }),
        it("should handle @mentions neutralization", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "@mention Test", "body": "Hey @username and @org/team, check this out! But preserve email@domain.com"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          expect(parsedOutput.items[0].body).toBe("Hey `@username` and `@org/team`, check this out! But preserve email@domain.com");
        }),
        it("should neutralize bot trigger phrases", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Bot Trigger Test", "body": "This fixes #123 and closes #456, also resolves #789"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          expect(parsedOutput.items[0].body).toBe("This `fixes #123` and `closes #456`, also `resolves #789`");
        }),
        it("should remove ANSI escape sequences", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            bodyWithAnsi = "[31mRed text[0m and [1mBold text[m",
            ndjsonContent = JSON.stringify({ type: "create_issue", title: "ANSI Test", body: bodyWithAnsi });
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          expect(parsedOutput.items[0].body).toBe("Red text and Bold text");
        }),
        it("should handle custom allowed domains from environment", async () => {
          const originalServerUrl = process.env.GITHUB_SERVER_URL,
            originalApiUrl = process.env.GITHUB_API_URL;
          (delete process.env.GITHUB_SERVER_URL, delete process.env.GITHUB_API_URL, (process.env.GH_AW_ALLOWED_DOMAINS = "example.com,test.org"));
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Custom Domains", "body": "Allowed: https://example.com/page, https://sub.example.com/file, https://test.org/doc. Blocked: https://github.com/repo, https://blocked.com/page"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items[0].body).toBe("Allowed: https://example.com/page, https://sub.example.com/file, https://test.org/doc. Blocked: (github.com/redacted), (blocked.com/redacted)"),
            delete process.env.GH_AW_ALLOWED_DOMAINS,
            originalServerUrl && (process.env.GITHUB_SERVER_URL = originalServerUrl),
            originalApiUrl && (process.env.GITHUB_API_URL = originalApiUrl));
        }),
        it("should handle edge cases with colons in different contexts", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Colon Edge Cases", "body": "Time 12:30 PM, ratio 3:1, IPv6 ::1, URL path/file:with:colons, command -flag:value, namespace::function"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          expect(parsedOutput.items[0].body).toBe("Time 12:30 PM, ratio 3:1, IPv6 ::1, URL path/file:with:colons, command -flag:value, namespace::function");
        }),
        it("should truncate excessively long content", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            longBody = "x".repeat(6e5),
            ndjsonContent = `{"type": "create_issue", "title": "Long Content Test", "body": "${longBody}"}`;
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items[0].body).toMatch(/\[Content truncated due to length\]$/), expect(parsedOutput.items[0].body.length).toBeLessThan(6e5));
        }),
        it("should truncate content with too many lines", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            manyLines = Array(66e3).fill("line").join("\n"),
            ndjsonContent = JSON.stringify({ type: "create_issue", title: "Many Lines Test", body: manyLines });
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          expect(parsedOutput.items[0].body).toMatch(/\[Content truncated due to line count\]$/);
          const lineCount = parsedOutput.items[0].body.split("\n").length;
          expect(lineCount).toBeLessThan(66e3);
        }),
        it("should preserve backticks and code blocks", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Code Test", "body": "Use `z3 -v:10` in terminal. Code block:\\n```\\nz3 -memory:high input.smt2\\nftp://should-not-be-redacted-in-code\\n```"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items[0].body).toContain("z3 -v:10"), expect(parsedOutput.items[0].body).toContain("z3 -memory:high"));
        }),
        it("should handle sanitization across multiple field types", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_pull_request", "title": "PR with z3 -v:10 flag", "body": "Testing https://github.com/repo and ftp://example.com", "branch": "feature/z3-timeout:5000", "labels": ["bug", "z3:solver"]}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_pull_request": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items[0].title).toBe("PR with z3 -v:10 flag"),
            expect(parsedOutput.items[0].body).toBe("Testing https://github.com/repo and (example.com/redacted)"),
            expect(parsedOutput.items[0].branch).toBe("feature/z3-timeout:5000"),
            expect(parsedOutput.items[0].labels).toEqual(["bug", "z3:solver"]));
        }),
        it("should remove XML comments to prevent content hiding", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent =
              '{"type": "create_issue", "title": "XML Comment Test", "body": "This is visible \x3c!-- This is hidden content --\x3e more visible text \x3c!--- This is also hidden ---\x3e and more text \x3c!--- malformed comment --!> final text"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]),
            parsedOutput = JSON.parse(outputCall[1]);
          expect(parsedOutput.items[0].body).toBe("This is visible  more visible text  and more text  final text");
        }));
    }),
    describe("Min validation tests", () => {
      (it("should pass when min requirement is met", async () => {
        const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
          ndjsonContent =
            '{"type": "create_issue", "title": "First Issue", "body": "First body"}\n{"type": "create_issue", "title": "Second Issue", "body": "Second body"}\n{"type": "create_issue", "title": "Third Issue", "body": "Third body"}';
        (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
        const __config = '{"create_issue": {"min": 2, "max": 5}}',
          configPath = "/opt/gh-aw/safeoutputs/config.json";
        (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
        const setOutputCalls = mockCore.setOutput.mock.calls,
          outputCall = setOutputCalls.find(call => "output" === call[0]);
        expect(outputCall).toBeDefined();
        const parsedOutput = JSON.parse(outputCall[1]);
        (expect(parsedOutput.items).toHaveLength(3), expect(parsedOutput.errors).toHaveLength(0));
      }),
        it("should fail when min requirement is not met", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Only Issue", "body": "Only body"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": {"min": 3, "max": 5}}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.errors).toHaveLength(1), expect(parsedOutput.errors[0]).toContain("Too few items of type 'create_issue'. Minimum required: 3, found: 1."));
        }),
        it("should handle multiple types with different min requirements", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Issue 1", "body": "Body 1"}\n{"type": "create_issue", "title": "Issue 2", "body": "Body 2"}\n{"type": "add_comment", "body": "Comment 1"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": {"min": 1, "max": 5}, "add_comment": {"min": 2, "max": 5}}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(3), expect(parsedOutput.errors).toHaveLength(1), expect(parsedOutput.errors[0]).toContain("Too few items of type 'add_comment'. Minimum required: 2, found: 1."));
        }),
        it("should ignore min when set to 0", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Issue", "body": "Body"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": {"min": 0, "max": 5}}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should work when no min is specified (defaults to 0)", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "create_issue", "title": "Issue", "body": "Body"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": {"max": 5}}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should validate min even when no items are present", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = "";
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"create_issue": {"min": 1, "max": 5}}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors).toHaveLength(1), expect(parsedOutput.errors[0]).toContain("Too few items of type 'create_issue'. Minimum required: 1, found: 0."));
        }),
        it("should work with different safe output types", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "add_comment", "body": "Comment"}\n{"type": "create_discussion", "title": "Discussion", "body": "Discussion body"}\n{"type": "create_discussion", "title": "Discussion 2", "body": "Discussion body 2"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"add_comment": {"min": 2, "max": 5}, "create_discussion": {"min": 1, "max": 5}}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(3), expect(parsedOutput.errors).toHaveLength(1), expect(parsedOutput.errors[0]).toContain("Too few items of type 'add_comment'. Minimum required: 2, found: 1."));
        }));
    }),
    describe("noop output validation", () => {
      (it("should validate noop with message", async () => {
        const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
          ndjsonContent = '{"type": "noop", "message": "No issues found in this review"}';
        (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
        const config = '{"noop": true}',
          configPath = "/opt/gh-aw/safeoutputs/config.json";
        (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, config), await eval(`(async () => { ${collectScript}; await main(); })()`));
        const setOutputCalls = mockCore.setOutput.mock.calls,
          outputCall = setOutputCalls.find(call => "output" === call[0]);
        expect(outputCall).toBeDefined();
        const parsedOutput = JSON.parse(outputCall[1]);
        (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("noop"), expect(parsedOutput.items[0].message).toBe("No issues found in this review"), expect(parsedOutput.errors).toHaveLength(0));
      }),
        it("should reject noop without message", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "noop"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const config = '{"noop": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors.length).toBeGreaterThan(0), expect(parsedOutput.errors.some(e => e.includes("noop requires a 'message' field (string)"))).toBe(!0));
        }),
        it("should reject noop with non-string message", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "noop", "message": 123}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const config = '{"noop": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors.length).toBeGreaterThan(0), expect(parsedOutput.errors.some(e => e.includes("noop requires a 'message' field (string)"))).toBe(!0));
        }),
        it("should sanitize noop message content", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "noop", "message": "Test @mention and fixes #123"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const config = '{"noop": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].message).toContain("`@mention`"), expect(parsedOutput.items[0].message).toContain("`fixes #123`"));
        }),
        it("should handle multiple noop messages", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "noop", "message": "First message"}\n{"type": "noop", "message": "Second message"}\n{"type": "noop", "message": "Third message"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const config = '{"noop": {"max": 3}}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(3),
            expect(parsedOutput.items[0].message).toBe("First message"),
            expect(parsedOutput.items[1].message).toBe("Second message"),
            expect(parsedOutput.items[2].message).toBe("Third message"),
            expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should validate assign_milestone with required fields", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "assign_milestone", "issue_number": 42, "milestone_number": 5}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"assign_milestone": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].issue_number).toBe(42), expect(parsedOutput.items[0].milestone_number).toBe(5), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should validate assign_to_agent with required fields", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "assign_to_agent", "issue_number": 42}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"assign_to_agent": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].issue_number).toBe(42), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should validate assign_to_agent with temporary_id issue_number", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "assign_to_agent", "issue_number": "aw_abc123def456"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"assign_to_agent": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].issue_number).toBe("aw_abc123def456"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should validate assign_to_agent with optional fields", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "assign_to_agent", "issue_number": 42, "agent": "my-agent"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"assign_to_agent": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, __config), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].issue_number).toBe(42), expect(parsedOutput.items[0].agent).toBe("my-agent"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should reject assign_to_agent with missing issue_number", async () => {
          const testFile = "/tmp/gh-aw/test-ndjson-output.txt",
            ndjsonContent = '{"type": "assign_to_agent"}';
          (fs.writeFileSync(testFile, ndjsonContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile));
          const __config = '{"assign_to_agent": true}',
            configPath = "/opt/gh-aw/safeoutputs/config.json";
          (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }),
            fs.writeFileSync(configPath, __config),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors.length).toBeGreaterThan(0), expect(parsedOutput.errors.some(e => e.includes("assign_to_agent requires at least one of"))).toBe(!0));
        }));
    }),
    describe("link_sub_issue temporary ID validation", () => {
      const configPath = "/opt/gh-aw/safeoutputs/config.json";
      (beforeEach(() => {
        (fs.mkdirSync("/opt/gh-aw/safeoutputs", { recursive: !0 }), fs.writeFileSync(configPath, JSON.stringify({ link_sub_issue: {} })));
      }),
        it("should accept valid positive integer for parent_issue_number", async () => {
          const testInput = JSON.stringify({ type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 50 }),
            outputPath = "/tmp/gh-aw/test-link-sub-issue-integers.txt";
          (fs.writeFileSync(outputPath, testInput), (process.env.GH_AW_SAFE_OUTPUTS = outputPath), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const failedCalls = mockCore.setFailed.mock.calls;
          expect(failedCalls.length).toBe(0);
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("link_sub_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should accept temporary ID (aw_ prefix) for parent_issue_number", async () => {
          const testInput = JSON.stringify({ type: "link_sub_issue", parent_issue_number: "aw_abc123def456", sub_issue_number: 50 }),
            outputPath = "/tmp/gh-aw/test-link-sub-issue-temp-id.txt";
          (fs.writeFileSync(outputPath, testInput), (process.env.GH_AW_SAFE_OUTPUTS = outputPath), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const failedCalls = mockCore.setFailed.mock.calls;
          expect(failedCalls.length).toBe(0);
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("link_sub_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should accept temporary ID (aw_ prefix) for sub_issue_number", async () => {
          const testInput = JSON.stringify({ type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: "aw_123456abcdef" }),
            outputPath = "/tmp/gh-aw/test-link-sub-issue-temp-id-sub.txt";
          (fs.writeFileSync(outputPath, testInput), (process.env.GH_AW_SAFE_OUTPUTS = outputPath), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const failedCalls = mockCore.setFailed.mock.calls;
          expect(failedCalls.length).toBe(0);
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("link_sub_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should accept temporary IDs for both parent and sub issue numbers", async () => {
          const testInput = JSON.stringify({ type: "link_sub_issue", parent_issue_number: "aw_abc123def456", sub_issue_number: "aw_fedcba654321" }),
            outputPath = "/tmp/gh-aw/test-link-sub-issue-both-temp-ids.txt";
          (fs.writeFileSync(outputPath, testInput), (process.env.GH_AW_SAFE_OUTPUTS = outputPath), await eval(`(async () => { ${collectScript}; await main(); })()`));
          const failedCalls = mockCore.setFailed.mock.calls;
          expect(failedCalls.length).toBe(0);
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(1), expect(parsedOutput.items[0].type).toBe("link_sub_issue"), expect(parsedOutput.errors).toHaveLength(0));
        }),
        it("should reject invalid temporary ID format (wrong length)", async () => {
          const testInput = JSON.stringify({ type: "link_sub_issue", parent_issue_number: "aw_short", sub_issue_number: 50 }),
            outputPath = "/tmp/gh-aw/test-link-sub-issue-invalid-temp-id.txt";
          (fs.writeFileSync(outputPath, testInput),
            (process.env.GH_AW_SAFE_OUTPUTS = outputPath),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors.length).toBeGreaterThan(0), expect(parsedOutput.errors.some(e => e.includes("must be a positive integer or temporary ID"))).toBe(!0));
        }),
        it("should reject same temporary ID for parent and sub", async () => {
          const sameId = "aw_abc123def456",
            testInput = JSON.stringify({ type: "link_sub_issue", parent_issue_number: sameId, sub_issue_number: sameId }),
            outputPath = "/tmp/gh-aw/test-link-sub-issue-same-temp-ids.txt";
          (fs.writeFileSync(outputPath, testInput),
            (process.env.GH_AW_SAFE_OUTPUTS = outputPath),
            await eval(`(async () => { ${collectScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
          const setOutputCalls = mockCore.setOutput.mock.calls,
            outputCall = setOutputCalls.find(call => "output" === call[0]);
          expect(outputCall).toBeDefined();
          const parsedOutput = JSON.parse(outputCall[1]);
          (expect(parsedOutput.items).toHaveLength(0), expect(parsedOutput.errors.length).toBeGreaterThan(0), expect(parsedOutput.errors.some(e => e.includes("must be different"))).toBe(!0));
        }));
    }));
});
