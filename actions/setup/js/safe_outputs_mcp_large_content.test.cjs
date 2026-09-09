import { describe, it, expect, beforeEach, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import { spawn } from "child_process";
import crypto from "crypto";
describe("safe_outputs_mcp_server.cjs large content handling", () => {
  let originalEnv, tempOutputDir, tempConfigFile, tempOutputFile;
  (beforeEach(() => {
    ((originalEnv = { ...process.env }),
      (tempOutputDir = path.join("/tmp", `test_large_content_${Date.now()}`)),
      fs.mkdirSync(tempOutputDir, { recursive: !0 }),
      (tempConfigFile = path.join(tempOutputDir, "config.json")),
      (tempOutputFile = path.join(tempOutputDir, "outputs.jsonl")),
      // Provide a target repo so create_issue handler's resolveAndValidateRepo succeeds
      // without depending on the ambient CI env (GITHUB_REPOSITORY).
      (process.env.GH_AW_TARGET_REPO_SLUG = "test-owner/test-repo"),
      fs.writeFileSync(tempConfigFile, JSON.stringify({ "create-issue": {} })));
    const toolsJsonPath = path.join(tempOutputDir, "tools.json"),
      tools = JSON.parse(fs.readFileSync(path.join(__dirname, "safe_outputs_tools.json"), "utf8")),
      createIssueTool = tools.find(tool => tool.name === "create_issue");
    if (createIssueTool?.inputSchema?.properties?.body) {
      createIssueTool.inputSchema.properties.body.maxLength = 2e6;
    }
    fs.writeFileSync(toolsJsonPath, JSON.stringify(tools));
  }),
    afterEach(() => {
      (Object.keys(process.env).forEach(k => {
        if (!(k in originalEnv)) delete process.env[k];
      }),
        Object.assign(process.env, originalEnv),
        fs.existsSync(tempOutputDir) && fs.rmSync(tempOutputDir, { recursive: !0, force: !0 }));
    }),
    it("should write large content to file when exceeding 16000 tokens", async () => {
      ((process.env.GH_AW_SAFE_OUTPUTS = tempOutputFile), (process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = tempConfigFile), (process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = path.join(tempOutputDir, "tools.json")));
      const serverPath = path.join(__dirname, "safe_outputs_mcp_server.cjs");
      return new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            (child.kill(), reject(new Error("Test timeout")));
          }, 1e4),
          child = spawn("node", [serverPath], { stdio: ["pipe", "pipe", "pipe"], env: { ...process.env } });
        let stderr = "",
          stdout = "";
        (child.stderr.on("data", data => {
          stderr += data.toString();
        }),
          child.stdout.on("data", data => {
            stdout += data.toString();
          }),
          child.on("error", error => {
            (clearTimeout(timeout), reject(error));
          }));
        const initMessage = JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2024-11-05", capabilities: {}, clientInfo: { name: "test-client", version: "1.0.0" } } }) + "\n";
        (child.stdin.write(initMessage),
          setTimeout(() => {
            const largeBody = "A".repeat(7e4),
              toolCallMessage = JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "create_issue", arguments: { title: "Test Issue", body: largeBody } } }) + "\n";
            (child.stdin.write(toolCallMessage),
              setTimeout(() => {
                (child.kill(), clearTimeout(timeout));
                const toolCallResponse = stdout
                  .trim()
                  .split("\n")
                  .find(line => {
                    try {
                      return 2 === JSON.parse(line).id;
                    } catch {
                      return !1;
                    }
                  });
                expect(toolCallResponse).toBeDefined();
                const parsed = JSON.parse(toolCallResponse);
                (expect(parsed.result).toBeDefined(), expect(parsed.result.content).toBeDefined(), expect(parsed.result.content.length).toBeGreaterThan(0));
                const responseText = parsed.result.content[0].text,
                  responseObj = JSON.parse(responseText);
                (expect(responseObj.filename).toBeDefined(), expect(responseObj.description).toBeDefined(), expect(responseObj.description).not.toBe("generated content large!"));
                const expectedFilePath = path.join("/tmp/gh-aw/safeoutputs", responseObj.filename);
                expect(fs.existsSync(expectedFilePath)).toBe(!0);
                const fileContent = fs.readFileSync(expectedFilePath, "utf8");
                expect(fileContent).toBe(largeBody);
                const hash = crypto.createHash("sha256").update(largeBody).digest("hex");
                expect(responseObj.filename).toBe(`${hash}.json`);
                const outputLines = fs.readFileSync(tempOutputFile, "utf8").trim().split("\n"),
                  lastOutput = JSON.parse(outputLines[outputLines.length - 1]);
                (expect(lastOutput.type).toBe("create_issue"),
                  expect(lastOutput.body).toContain("Content too large, saved to file:"),
                  expect(lastOutput.body).toContain(responseObj.filename),
                  fs.existsSync(expectedFilePath) && fs.unlinkSync(expectedFilePath),
                  resolve());
              }, 1e3));
          }, 1e3));
      });
    }),
    it("should handle normal content without writing to file", async () => {
      ((process.env.GH_AW_SAFE_OUTPUTS = tempOutputFile), (process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = tempConfigFile), (process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = path.join(tempOutputDir, "tools.json")));
      const serverPath = path.join(__dirname, "safe_outputs_mcp_server.cjs");
      return new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            (child.kill(), reject(new Error("Test timeout")));
          }, 1e4),
          child = spawn("node", [serverPath], { stdio: ["pipe", "pipe", "pipe"], env: { ...process.env } });
        let stderr = "",
          stdout = "";
        (child.stderr.on("data", data => {
          stderr += data.toString();
        }),
          child.stdout.on("data", data => {
            stdout += data.toString();
          }),
          child.on("error", error => {
            (clearTimeout(timeout), reject(error));
          }));
        const initMessage = JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2024-11-05", capabilities: {}, clientInfo: { name: "test-client", version: "1.0.0" } } }) + "\n";
        (child.stdin.write(initMessage),
          setTimeout(() => {
            const toolCallMessage = JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "create_issue", arguments: { title: "Test Issue", body: "This is a normal issue body." } } }) + "\n";
            (child.stdin.write(toolCallMessage),
              setTimeout(() => {
                (child.kill(), clearTimeout(timeout));
                const toolCallResponse = stdout
                  .trim()
                  .split("\n")
                  .find(line => {
                    try {
                      return 2 === JSON.parse(line).id;
                    } catch {
                      return !1;
                    }
                  });
                expect(toolCallResponse).toBeDefined();
                const parsed = JSON.parse(toolCallResponse);
                (expect(parsed.result).toBeDefined(), expect(parsed.result.content).toBeDefined(), expect(parsed.result.content.length).toBeGreaterThan(0));
                const responseText = parsed.result.content[0].text,
                  responseObj = JSON.parse(responseText);
                expect(responseObj.result).toBe("success");
                const outputLines = fs.readFileSync(tempOutputFile, "utf8").trim().split("\n"),
                  lastOutput = JSON.parse(outputLines[outputLines.length - 1]);
                (expect(lastOutput.type).toBe("create_issue"), expect(lastOutput.body).toBe("This is a normal issue body."), resolve());
              }, 1e3));
          }, 1e3));
      });
    }),
    it("should detect JSON content and use .json extension", async () => {
      ((process.env.GH_AW_SAFE_OUTPUTS = tempOutputFile), (process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = tempConfigFile), (process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = path.join(tempOutputDir, "tools.json")));
      const serverPath = path.join(__dirname, "safe_outputs_mcp_server.cjs");
      return new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            (child.kill(), reject(new Error("Test timeout")));
          }, 1e4),
          child = spawn("node", [serverPath], { stdio: ["pipe", "pipe", "pipe"], env: { ...process.env } });
        let stderr = "",
          stdout = "";
        (child.stderr.on("data", data => {
          stderr += data.toString();
        }),
          child.stdout.on("data", data => {
            stdout += data.toString();
          }),
          child.on("error", error => {
            (clearTimeout(timeout), reject(error));
          }));
        const initMessage = JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2024-11-05", capabilities: {}, clientInfo: { name: "test-client", version: "1.0.0" } } }) + "\n";
        (child.stdin.write(initMessage),
          setTimeout(() => {
            const largeArray = Array(2e3)
                .fill(null)
                .map((_, i) => ({ id: i, name: `Item ${i}`, data: "X".repeat(30) })),
              largeBody = JSON.stringify(largeArray, null, 2),
              toolCallMessage = JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "create_issue", arguments: { title: "Test Issue", body: largeBody } } }) + "\n";
            (child.stdin.write(toolCallMessage),
              setTimeout(() => {
                (child.kill(), clearTimeout(timeout));
                const toolCallResponse = stdout
                  .trim()
                  .split("\n")
                  .find(line => {
                    try {
                      return 2 === JSON.parse(line).id;
                    } catch {
                      return !1;
                    }
                  });
                expect(toolCallResponse).toBeDefined();
                const responseText = JSON.parse(toolCallResponse).result.content[0].text,
                  responseObj = JSON.parse(responseText);
                (expect(responseObj.filename).toMatch(/\.json$/), expect(responseObj.description).toBeDefined(), expect(responseObj.description).toContain("items"), expect(responseObj.description).toContain("id, name, data"));
                const expectedFilePath = path.join("/tmp/gh-aw/safeoutputs", responseObj.filename);
                expect(fs.existsSync(expectedFilePath)).toBe(!0);
                const fileContent = fs.readFileSync(expectedFilePath, "utf8");
                (expect(() => JSON.parse(fileContent)).not.toThrow(), fs.existsSync(expectedFilePath) && fs.unlinkSync(expectedFilePath), resolve());
              }, 1e3));
          }, 1e3));
      });
    }),
    it("should always use .json extension even for non-JSON content", async () => {
      ((process.env.GH_AW_SAFE_OUTPUTS = tempOutputFile), (process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = tempConfigFile), (process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = path.join(tempOutputDir, "tools.json")));
      const serverPath = path.join(__dirname, "safe_outputs_mcp_server.cjs");
      return new Promise((resolve, reject) => {
        const child = spawn("node", [serverPath], { stdio: ["pipe", "pipe", "pipe"], env: { ...process.env } }),
          timeout = setTimeout(() => {
            (child.kill(), reject(new Error("Test timeout")));
          }, 1e4);
        let stderr = "",
          stdout = "";
        (child.stderr.on("data", data => {
          stderr += data.toString();
        }),
          child.stdout.on("data", data => {
            stdout += data.toString();
          }),
          child.on("error", error => {
            (clearTimeout(timeout), reject(error));
          }));
        const initMessage = JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2024-11-05", capabilities: {}, clientInfo: { name: "test-client", version: "1.0.0" } } }) + "\n";
        (child.stdin.write(initMessage),
          setTimeout(() => {
            let largeBody = "# Large Markdown Document\n\n";
            for (let i = 0; i < 1e3; i++)
              ((largeBody += `## Section ${i}\n\n`),
                (largeBody += "Lorem ipsum dolor sit amet, consectetur adipiscing elit. ".repeat(20)),
                (largeBody += "\n\n"),
                (largeBody += "```javascript\n"),
                (largeBody += "const example = 'code block';\n"),
                (largeBody += "console.log(example);\n"),
                (largeBody += "```\n\n"));
            const toolCallMessage = JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "create_issue", arguments: { title: "Test Issue", body: largeBody } } }) + "\n";
            (child.stdin.write(toolCallMessage),
              setTimeout(() => {
                (child.kill(), clearTimeout(timeout));
                const toolCallResponse = stdout
                  .trim()
                  .split("\n")
                  .find(line => {
                    try {
                      return 2 === JSON.parse(line).id;
                    } catch {
                      return !1;
                    }
                  });
                expect(toolCallResponse).toBeDefined();
                const responseText = JSON.parse(toolCallResponse).result.content[0].text,
                  responseObj = JSON.parse(responseText);
                (expect(responseObj.filename).toMatch(/\.json$/), expect(responseObj.description).toBe("text content"));
                const expectedFilePath = path.join("/tmp/gh-aw/safeoutputs", responseObj.filename);
                expect(fs.existsSync(expectedFilePath)).toBe(!0);
                const fileContent = fs.readFileSync(expectedFilePath, "utf8");
                (expect(fileContent).toContain("# Large Markdown Document"), expect(fileContent).toContain("```javascript"), fs.existsSync(expectedFilePath) && fs.unlinkSync(expectedFilePath), resolve());
              }, 1e3));
          }, 1e3));
      });
    }));
});
