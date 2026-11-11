// @ts-check

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createShellHandler } from "./mcp_handler_shell.cjs";
import fs from "fs";
import path from "path";
import os from "os";

describe("createShellHandler", () => {
  let mockServer;
  let tempDir;
  let testScriptPath;

  beforeEach(() => {
    // Create mock server with debug logging
    mockServer = {
      debug: () => {},
      debugError: () => {},
    };

    // Create temporary directory for test scripts
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "shell-handler-test-"));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    // Clean up temporary directory
    if (fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("should execute a simple shell script", async () => {
    testScriptPath = path.join(tempDir, "test.sh");
    const shCode = `#!/bin/bash
echo "result=hello" >> "$GITHUB_OUTPUT"
`;
    fs.writeFileSync(testScriptPath, shCode);
    fs.chmodSync(testScriptPath, "755");

    const handler = createShellHandler(mockServer, "test-tool", testScriptPath, 10);
    const result = await handler({});

    expect(result).toBeDefined();
    expect(result.content).toBeDefined();
    expect(result.content.length).toBe(1);
    expect(result.content[0].type).toBe("text");

    const output = JSON.parse(result.content[0].text);
    expect(output.outputs).toBeDefined();
    expect(output.outputs.result).toBe("hello");
  });

  it("should pass inputs as environment variables", async () => {
    testScriptPath = path.join(tempDir, "env-test.sh");
    const shCode = `#!/bin/bash
echo "name=$INPUT_NAME" >> "$GITHUB_OUTPUT"
echo "count=$INPUT_COUNT" >> "$GITHUB_OUTPUT"
`;
    fs.writeFileSync(testScriptPath, shCode);
    fs.chmodSync(testScriptPath, "755");

    const handler = createShellHandler(mockServer, "env-tool", testScriptPath, 10);
    const result = await handler({ name: "World", count: "42" });

    const output = JSON.parse(result.content[0].text);
    expect(output.outputs.name).toBe("World");
    expect(output.outputs.count).toBe("42");
  });

  it("should convert hyphenated input keys to INPUT_NAME format", async () => {
    testScriptPath = path.join(tempDir, "hyphen-test.sh");
    const shCode = `#!/bin/bash
echo "val=$INPUT_MY_KEY" >> "$GITHUB_OUTPUT"
`;
    fs.writeFileSync(testScriptPath, shCode);
    fs.chmodSync(testScriptPath, "755");

    const handler = createShellHandler(mockServer, "hyphen-tool", testScriptPath, 10);
    const result = await handler({ "my-key": "test-value" });

    const output = JSON.parse(result.content[0].text);
    expect(output.outputs.val).toBe("test-value");
  });

  it("should capture stdout and stderr", async () => {
    testScriptPath = path.join(tempDir, "output-test.sh");
    const shCode = `#!/bin/bash
echo "standard output"
echo "standard error" >&2
`;
    fs.writeFileSync(testScriptPath, shCode);
    fs.chmodSync(testScriptPath, "755");

    const handler = createShellHandler(mockServer, "output-tool", testScriptPath, 10);
    const result = await handler({});

    const output = JSON.parse(result.content[0].text);
    expect(output.stdout).toContain("standard output");
    expect(output.stderr).toContain("standard error");
  });

  it("should return empty outputs when GITHUB_OUTPUT is empty", async () => {
    testScriptPath = path.join(tempDir, "no-output.sh");
    const shCode = `#!/bin/bash
echo "no outputs here"
`;
    fs.writeFileSync(testScriptPath, shCode);
    fs.chmodSync(testScriptPath, "755");

    const handler = createShellHandler(mockServer, "no-output-tool", testScriptPath, 10);
    const result = await handler({});

    const output = JSON.parse(result.content[0].text);
    expect(output.outputs).toEqual({});
  });

  it("should handle shell script errors with enhanced error message", async () => {
    testScriptPath = path.join(tempDir, "error.sh");
    const shCode = `#!/bin/bash
echo "error details" >&2
exit 1
`;
    fs.writeFileSync(testScriptPath, shCode);
    fs.chmodSync(testScriptPath, "755");

    const handler = createShellHandler(mockServer, "error-tool", testScriptPath, 10);

    let caughtError;
    try {
      await handler({});
    } catch (e) {
      caughtError = e;
    }
    expect(caughtError).toBeDefined();
    expect(caughtError.message).toContain("error details");
  });

  it("should clean up output file after successful execution", async () => {
    testScriptPath = path.join(tempDir, "cleanup-test.sh");
    const shCode = `#!/bin/bash
echo "key=value" >> "$GITHUB_OUTPUT"
`;
    fs.writeFileSync(testScriptPath, shCode);
    fs.chmodSync(testScriptPath, "755");

    // Track temporary files created in os.tmpdir()
    const tmpDir = os.tmpdir();
    const filesBefore = fs.readdirSync(tmpDir).filter(f => f.startsWith("mcp-shell-output-"));

    const handler = createShellHandler(mockServer, "cleanup-tool", testScriptPath, 10);
    await handler({});

    const filesAfter = fs.readdirSync(tmpDir).filter(f => f.startsWith("mcp-shell-output-"));
    // No new temporary output files should remain
    expect(filesAfter.length).toBe(filesBefore.length);
  });

  it("should clean up output file after script error", async () => {
    testScriptPath = path.join(tempDir, "error-cleanup.sh");
    const shCode = `#!/bin/bash
exit 1
`;
    fs.writeFileSync(testScriptPath, shCode);
    fs.chmodSync(testScriptPath, "755");

    const tmpDir = os.tmpdir();
    const filesBefore = fs.readdirSync(tmpDir).filter(f => f.startsWith("mcp-shell-output-"));

    const handler = createShellHandler(mockServer, "error-cleanup-tool", testScriptPath, 10);
    await expect(handler({})).rejects.toThrow();

    const filesAfter = fs.readdirSync(tmpDir).filter(f => f.startsWith("mcp-shell-output-"));
    expect(filesAfter.length).toBe(filesBefore.length);
  });

  it("should respect timeout setting", async () => {
    testScriptPath = path.join(tempDir, "slow.sh");
    const shCode = `#!/bin/bash
sleep 30
`;
    fs.writeFileSync(testScriptPath, shCode);
    fs.chmodSync(testScriptPath, "755");

    const outputPaths = [];
    mockServer.debug = message => {
      if (message.includes("Output file: ")) outputPaths.push(message.split("Output file: ")[1]);
    };
    const handler = createShellHandler(mockServer, "slow-tool", testScriptPath, 1);

    await expect(handler({})).rejects.toThrow();
    expect(outputPaths).toHaveLength(1);
    expect(fs.existsSync(path.dirname(outputPaths[0]))).toBe(false);
  }, 15000);

  it("uses a private directory and ignores planted legacy output symlinks", async () => {
    vi.spyOn(os, "tmpdir").mockReturnValue(tempDir);
    vi.spyOn(Date, "now").mockReturnValue(123);
    vi.spyOn(Math, "random").mockReturnValue(0.5);
    const sentinel = path.join(tempDir, "sentinel");
    fs.writeFileSync(sentinel, "external=unchanged");
    const legacyPath = path.join(tempDir, `mcp-shell-output-123-${(0.5).toString(36).substring(2)}.txt`);
    fs.symlinkSync(sentinel, legacyPath);
    testScriptPath = path.join(tempDir, "private-output.sh");
    fs.writeFileSync(testScriptPath, '#!/bin/bash\nprintf "key=value\\n" >> "$GITHUB_OUTPUT"\nprintf "%s" "$GITHUB_OUTPUT"\n');
    fs.chmodSync(testScriptPath, "755");

    let outputPath;
    mockServer.debug = message => {
      if (message.includes("Output file: ")) {
        outputPath = message.split("Output file: ")[1];
        expect(fs.statSync(path.dirname(outputPath)).mode & 0o777).toBe(0o700);
      }
    };
    const result = await createShellHandler(mockServer, "private-output", testScriptPath)({});
    const output = JSON.parse(result.content[0].text);
    expect(output.outputs).toEqual({ key: "value" });
    expect(output.stdout).toBe(outputPath);
    expect(fs.existsSync(path.dirname(outputPath))).toBe(false);
    expect(fs.readFileSync(sentinel, "utf8")).toBe("external=unchanged");
    expect(fs.lstatSync(legacyPath).isSymbolicLink()).toBe(true);
  });

  it("reads the original output handle when the script replaces its path with a symlink", async () => {
    const sentinel = path.join(tempDir, "sentinel");
    fs.writeFileSync(sentinel, "external=secret\n");
    testScriptPath = path.join(tempDir, "replace-output.sh");
    fs.writeFileSync(testScriptPath, '#!/bin/bash\nprintf "key=value\\n" >> "$GITHUB_OUTPUT"\nrm "$GITHUB_OUTPUT"\nln -s "$INPUT_SENTINEL" "$GITHUB_OUTPUT"\nprintf "%s" "$GITHUB_OUTPUT"\n');
    fs.chmodSync(testScriptPath, "755");
    const result = await createShellHandler(mockServer, "replace-output", testScriptPath)({ sentinel });
    const output = JSON.parse(result.content[0].text);
    expect(output.outputs).toEqual({ key: "value" });
    expect(fs.readFileSync(sentinel, "utf8")).toBe("external=secret\n");
    expect(fs.existsSync(path.dirname(output.stdout))).toBe(false);
  });

  it("cleans up the private directory if exclusive output creation fails", async () => {
    vi.spyOn(os, "tmpdir").mockReturnValue(tempDir);
    vi.spyOn(fs, "openSync").mockImplementation(() => {
      throw new Error("output allocation failed");
    });
    await expect(createShellHandler(mockServer, "allocation-failure", "/unused")({})).rejects.toThrow("output allocation failed");
    expect(fs.readdirSync(tempDir)).toEqual([]);
  });

  it("reports directory allocation failures with the original cause", async () => {
    const cause = new Error("temporary parent is not writable");
    vi.spyOn(fs, "mkdtempSync").mockImplementation(() => {
      throw cause;
    });
    await expect(createShellHandler(mockServer, "allocation-failure", "/unused")({})).rejects.toMatchObject({
      message: expect.stringContaining("Failed to create shell output directory for allocation-failure"),
      cause,
    });
  });

  it("should execute script from GITHUB_WORKSPACE directory", async () => {
    const originalWorkspace = process.env.GITHUB_WORKSPACE;
    process.env.GITHUB_WORKSPACE = tempDir;

    try {
      testScriptPath = path.join(tempDir, "test-cwd.sh");
      const shCode = `#!/bin/bash
echo "cwd=$(pwd)" >> "$GITHUB_OUTPUT"
`;
      fs.writeFileSync(testScriptPath, shCode);
      fs.chmodSync(testScriptPath, "755");

      const handler = createShellHandler(mockServer, "cwd-tool", testScriptPath, 10);
      const result = await handler({});

      const output = JSON.parse(result.content[0].text);
      expect(output.outputs.cwd).toBe(tempDir);
    } finally {
      if (originalWorkspace === undefined) {
        delete process.env.GITHUB_WORKSPACE;
      } else {
        process.env.GITHUB_WORKSPACE = originalWorkspace;
      }
    }
  });

  it("should handle multiple outputs", async () => {
    testScriptPath = path.join(tempDir, "multi-output.sh");
    const shCode = `#!/bin/bash
echo "first=one" >> "$GITHUB_OUTPUT"
echo "second=two" >> "$GITHUB_OUTPUT"
echo "third=three" >> "$GITHUB_OUTPUT"
`;
    fs.writeFileSync(testScriptPath, shCode);
    fs.chmodSync(testScriptPath, "755");

    const handler = createShellHandler(mockServer, "multi-tool", testScriptPath, 10);
    const result = await handler({});

    const output = JSON.parse(result.content[0].text);
    expect(output.outputs).toEqual({ first: "one", second: "two", third: "three" });
  });
});
