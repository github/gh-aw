import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import safeOutputsMCPServerHTTP from "./safe_outputs_mcp_server_http.cjs";
import { normalizeSafeOutputToolArguments } from "./safe_outputs_mcp_arguments.cjs";

const { createMCPServer } = safeOutputsMCPServerHTTP;

describe("safe_outputs_mcp wrapped tool arguments", () => {
  let tempDir;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "safe-outputs-mcp-wrapped-"));
  });

  afterEach(() => {
    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
    delete process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
    delete process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
    delete process.env.GH_AW_SAFE_OUTPUTS;
  });

  it("logs when wrapped arguments are unwrapped", () => {
    const debug = vi.fn();
    const payloadKeys = ["create_discussion"];

    const normalized = normalizeSafeOutputToolArguments(
      "create_discussion",
      {
        create_discussion: {
          title: "Wrapped title",
          body: "Wrapped body",
        },
      },
      { debug }
    );

    expect(normalized).toEqual({
      title: "Wrapped title",
      body: "Wrapped body",
    });

    expect(debug).toHaveBeenCalledWith(expect.stringContaining("Recovered wrapped safe-output tool arguments for 'create_discussion'"));
    expect(debug).toHaveBeenCalledWith(expect.stringContaining("unwrapping key 'create_discussion'"));
    expect(debug).toHaveBeenCalledWith(expect.stringContaining(JSON.stringify(payloadKeys)));
  });

  it.each(["", " \t\n"])("omits blank optional fields before MCP validation and NDJSON append (%j)", async blank => {
    const configPath = path.join(tempDir, "config.json");
    const toolsPath = path.join(tempDir, "tools.json");
    const outputPath = path.join(tempDir, "output.jsonl");
    const tools = JSON.parse(fs.readFileSync(path.join(process.cwd(), "safe_outputs_tools.json"), "utf8"));
    fs.writeFileSync(configPath, JSON.stringify({ add_comment: { enabled: true, target: "42" } }));
    fs.writeFileSync(toolsPath, JSON.stringify(tools.filter(tool => tool.name === "add_comment")));
    process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = configPath;
    process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = toolsPath;
    process.env.GH_AW_SAFE_OUTPUTS = outputPath;

    const { server } = createMCPServer();
    const request = body => ({
      jsonrpc: "2.0",
      id: 1,
      method: "tools/call",
      params: { name: "add_comment", arguments: { body, item_number: blank, comment_id: blank, temporary_id: blank } },
    });
    const rejected = await server.handleRequest(request(""));
    expect(rejected.error || rejected.result?.isError).toBeTruthy();
    expect(fs.existsSync(outputPath)).toBe(false);

    const response = await server.handleRequest(request("A useful comment"));
    expect(response.error).toBeUndefined();
    expect(response.result.isError).toBe(false);
    const written = JSON.parse(fs.readFileSync(outputPath, "utf8").trim());
    expect(written).toMatchObject({ type: "add_comment", body: "A useful comment" });
    for (const field of ["item_number", "comment_id"]) {
      expect(written).not.toHaveProperty(field);
    }
    expect(written.temporary_id).toMatch(/^aw_[A-Za-z0-9_]{3,12}$/);
  });

  it("maps configured parameter synonyms to canonical field names", () => {
    const debug = vi.fn();
    const normalized = normalizeSafeOutputToolArguments(
      "create_code_scanning_alert",
      {
        file: "README.md",
        line: 1,
        level: "warning",
        message: "test",
      },
      { debug },
      {
        type: "object",
        properties: {
          file: { type: "string" },
          line: { type: ["number", "string"] },
          severity: { type: "string", "x-synonyms": ["level"] },
          message: { type: "string" },
        },
      }
    );

    expect(normalized).toEqual({
      file: "README.md",
      line: 1,
      severity: "warning",
      message: "test",
    });
    expect(debug).toHaveBeenCalledWith(expect.stringContaining("Recovered safe-output parameter synonyms"));
  });

  it("maps likely LLM camelCase parameter mistakes", () => {
    const normalized = normalizeSafeOutputToolArguments(
      "close_issue",
      {
        issueNumber: 42,
        body: "done",
      },
      undefined,
      {
        type: "object",
        properties: {
          issue_number: { type: "number", "x-synonyms": ["issueNumber"] },
          body: { type: "string" },
        },
      }
    );

    expect(normalized).toEqual({
      issue_number: 42,
      body: "done",
    });
  });

  it("unwraps child arguments that match the tool name", async () => {
    const configPath = path.join(tempDir, "config.json");
    const toolsPath = path.join(tempDir, "tools.json");
    const outputPath = path.join(tempDir, "output.jsonl");

    fs.writeFileSync(configPath, JSON.stringify({ create_discussion: { enabled: true } }));
    fs.writeFileSync(
      toolsPath,
      JSON.stringify([
        {
          name: "create_discussion",
          description: "Create a discussion",
          inputSchema: {
            type: "object",
            properties: {
              title: { type: "string" },
              body: { type: "string" },
            },
            required: ["title", "body"],
          },
        },
      ])
    );

    process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = configPath;
    process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = toolsPath;
    process.env.GH_AW_SAFE_OUTPUTS = outputPath;

    const { server } = createMCPServer();
    const response = await server.handleRequest({
      jsonrpc: "2.0",
      id: 1,
      method: "tools/call",
      params: {
        name: "create_discussion",
        arguments: {
          create_discussion: {
            title: "Wrapped title",
            body: "Wrapped body",
          },
        },
      },
    });

    expect(response.error).toBeUndefined();
    expect(response.result.isError).toBe(false);

    const written = fs.readFileSync(outputPath, "utf8");
    expect(JSON.parse(written.trim())).toMatchObject({
      type: "create_discussion",
      title: "Wrapped title",
      body: "Wrapped body",
    });
  });

  it("keeps synonym metadata internal while still accepting synonym arguments", async () => {
    const configPath = path.join(tempDir, "config.json");
    const toolsPath = path.join(tempDir, "tools.json");
    const outputPath = path.join(tempDir, "output.jsonl");

    fs.writeFileSync(configPath, JSON.stringify({ create_code_scanning_alert: { enabled: true } }));
    fs.writeFileSync(
      toolsPath,
      JSON.stringify([
        {
          name: "create_code_scanning_alert",
          description: "Create a code scanning alert",
          inputSchema: {
            type: "object",
            properties: {
              severity: { type: "string", "x-synonyms": ["level"] },
              message: { type: "string" },
            },
            required: ["severity", "message"],
            additionalProperties: false,
          },
        },
      ])
    );

    process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = configPath;
    process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = toolsPath;
    process.env.GH_AW_SAFE_OUTPUTS = outputPath;

    const { server } = createMCPServer();
    const listResponse = await server.handleRequest({
      jsonrpc: "2.0",
      id: 1,
      method: "tools/list",
      params: {},
    });

    const listedTool = listResponse?.result?.tools?.find(t => t.name === "create_code_scanning_alert");
    expect(listedTool).toBeTruthy();
    expect(listedTool.inputSchema.properties.severity["x-synonyms"]).toBeUndefined();

    const callResponse = await server.handleRequest({
      jsonrpc: "2.0",
      id: 2,
      method: "tools/call",
      params: {
        name: "create_code_scanning_alert",
        arguments: {
          level: "warning",
          message: "test",
        },
      },
    });

    expect(callResponse.error).toBeUndefined();
    expect(callResponse.result.isError).toBe(false);

    const written = fs.readFileSync(outputPath, "utf8");
    expect(JSON.parse(written.trim())).toMatchObject({
      type: "create_code_scanning_alert",
      severity: "warning",
      message: "test",
    });
  });
});
