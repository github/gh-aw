// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

import { rewriteUrl, normalizeGatewayEntry, filterAndTransformServers, writeSecureOutput, loadGatewayContext, logCLIFilters, logServerStats, runGatewayConversion } from "./convert_gateway_config_shared.cjs";

describe("rewriteUrl", () => {
  it("replaces hostname and port with the provided url prefix", () => {
    expect(rewriteUrl("http://old.example:81/mcp/github", "http://host.docker.internal:80")).toBe("http://host.docker.internal:80/mcp/github");
  });

  it("preserves path segments after /mcp/", () => {
    expect(rewriteUrl("http://localhost:9090/mcp/some/deep/path", "http://myhost:1234")).toBe("http://myhost:1234/mcp/some/deep/path");
  });

  it("handles url with no path beyond /mcp/", () => {
    expect(rewriteUrl("http://localhost:80/mcp/", "http://newhost:80")).toBe("http://newhost:80/mcp/");
  });

  it("does not rewrite non-mcp URLs", () => {
    const url = "http://localhost/other/path";
    expect(rewriteUrl(url, "http://newhost:80")).toBe(url);
  });
});

describe("normalizeGatewayEntry", () => {
  it("rewrites url and applies mutation", () => {
    const entry = { type: "ignored", url: "http://old/mcp/github" };
    const result = normalizeGatewayEntry(entry, "http://host:80", t => {
      t.type = "http";
    });
    expect(result.type).toBe("http");
    expect(result.url).toBe("http://host:80/mcp/github");
  });

  it("does not mutate the original entry", () => {
    const entry = { url: "http://old/mcp/github", extra: "value" };
    const result = normalizeGatewayEntry(entry, "http://host:80");
    expect(entry.url).toBe("http://old/mcp/github");
    expect(result).not.toBe(entry);
  });

  it("works without a mutate callback", () => {
    const entry = { url: "http://old/mcp/github" };
    const result = normalizeGatewayEntry(entry, "http://host:80");
    expect(result.url).toBe("http://host:80/mcp/github");
  });

  it("skips url rewrite when url field is missing", () => {
    const entry = { type: "http" };
    const result = normalizeGatewayEntry(entry, "http://host:80");
    expect(result.type).toBe("http");
    expect(result.url).toBeUndefined();
  });
});

describe("filterAndTransformServers", () => {
  it("excludes CLI-mounted servers", () => {
    const servers = {
      github: { url: "http://old/mcp/github" },
      playwright: { url: "http://old/mcp/playwright" },
    };
    const result = filterAndTransformServers(servers, new Set(["playwright"]), (_n, e) => e);
    expect(Object.keys(result)).toEqual(["github"]);
  });

  it("applies the transform to each server", () => {
    const servers = { github: { url: "http://old/mcp/github" } };
    const result = filterAndTransformServers(servers, new Set(), (_n, e) => {
      e.type = "http";
      return e;
    });
    expect(result.github.type).toBe("http");
  });

  it("returns empty object when all servers are CLI-mounted", () => {
    const servers = { github: { url: "http://old/mcp/github" } };
    const result = filterAndTransformServers(servers, new Set(["github"]), (_n, e) => e);
    expect(result).toEqual({});
  });
});

describe("writeSecureOutput", () => {
  /** @type {string} */
  let dir;

  beforeEach(() => {
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "gateway-test-"));
  });

  afterEach(() => {
    fs.rmSync(dir, { recursive: true, force: true });
  });

  it("writes file with mode 0o600", () => {
    const outputPath = path.join(dir, "output.json");
    writeSecureOutput(outputPath, '{"key":"value"}');
    expect(fs.readFileSync(outputPath, "utf8")).toBe('{"key":"value"}');
    expect(fs.statSync(outputPath).mode & 0o777).toBe(0o600);
  });

  it("creates nested directories as needed", () => {
    const outputPath = path.join(dir, "deep/nested/output.json");
    writeSecureOutput(outputPath, "{}");
    expect(fs.existsSync(outputPath)).toBe(true);
  });

  it("overwrites existing file and resets permissions to 0o600", () => {
    const outputPath = path.join(dir, "output.json");
    fs.writeFileSync(outputPath, "old");
    fs.chmodSync(outputPath, 0o644);
    writeSecureOutput(outputPath, "new");
    expect(fs.readFileSync(outputPath, "utf8")).toBe("new");
    expect(fs.statSync(outputPath).mode & 0o777).toBe(0o600);
  });

  it("throws on write failure", () => {
    const spy = vi.spyOn(fs, "mkdirSync").mockImplementationOnce(() => {
      throw new Error("EACCES: permission denied, mkdir");
    });
    try {
      expect(() => writeSecureOutput(path.join(dir, "output.json"), "{}")).toThrow();
    } finally {
      spy.mockRestore();
    }
  });
});

describe("loadGatewayContext", () => {
  /** @type {NodeJS.ProcessEnv} */
  let savedEnv;

  beforeEach(() => {
    savedEnv = { ...process.env };
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in savedEnv)) delete process.env[key];
    }
    Object.assign(process.env, savedEnv);
  });

  it("throws when MCP_GATEWAY_OUTPUT is missing", () => {
    delete process.env.MCP_GATEWAY_OUTPUT;
    expect(() => loadGatewayContext()).toThrow("MCP_GATEWAY_OUTPUT");
  });

  it("throws when gateway output file does not exist", () => {
    process.env.MCP_GATEWAY_OUTPUT = "/nonexistent/gateway.json";
    expect(() => loadGatewayContext()).toThrow("Gateway output file not found");
  });

  it("throws when MCP_GATEWAY_DOMAIN is missing", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gateway-test-"));
    const gatewayFile = path.join(dir, "gateway.json");
    fs.writeFileSync(gatewayFile, JSON.stringify({ mcpServers: {} }));
    process.env.MCP_GATEWAY_OUTPUT = gatewayFile;
    delete process.env.MCP_GATEWAY_DOMAIN;
    try {
      expect(() => loadGatewayContext()).toThrow("MCP_GATEWAY_DOMAIN");
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  it("parses gateway output and returns structured context", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gateway-test-"));
    const gatewayFile = path.join(dir, "gateway.json");
    const servers = { github: { url: "http://old/mcp/github", type: "http" } };
    fs.writeFileSync(gatewayFile, JSON.stringify({ mcpServers: servers }));
    process.env.MCP_GATEWAY_OUTPUT = gatewayFile;
    process.env.MCP_GATEWAY_DOMAIN = "host.docker.internal";
    process.env.MCP_GATEWAY_PORT = "80";
    process.env.GH_AW_MCP_CLI_SERVERS = "[]";

    try {
      const ctx = loadGatewayContext();
      expect(ctx.domain).toBe("host.docker.internal");
      expect(ctx.port).toBe("80");
      expect(ctx.urlPrefix).toBe("http://host.docker.internal:80");
      expect(ctx.cliServers).toBeInstanceOf(Set);
      expect(Object.keys(ctx.servers)).toContain("github");
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  it("collects extraRequiredEnv values into extraEnv", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gateway-test-"));
    const gatewayFile = path.join(dir, "gateway.json");
    fs.writeFileSync(gatewayFile, JSON.stringify({ mcpServers: {} }));
    process.env.MCP_GATEWAY_OUTPUT = gatewayFile;
    process.env.MCP_GATEWAY_DOMAIN = "localhost";
    process.env.MCP_GATEWAY_PORT = "9090";
    process.env.GH_AW_MCP_CLI_SERVERS = "[]";
    process.env.MY_CUSTOM_VAR = "custom-value";

    try {
      const ctx = loadGatewayContext({ extraRequiredEnv: ["MY_CUSTOM_VAR"] });
      expect(ctx.extraEnv.MY_CUSTOM_VAR).toBe("custom-value");
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});

describe("logCLIFilters and logServerStats", () => {
  /** @type {{ info: ReturnType<typeof vi.fn>, warning: ReturnType<typeof vi.fn> }} */
  let mockCore;
  /** @type {unknown} */
  let originalCore;

  beforeEach(() => {
    originalCore = global.core;
    mockCore = { info: vi.fn(), warning: vi.fn() };
    // @ts-ignore
    global.core = mockCore;
  });

  afterEach(() => {
    // @ts-ignore
    global.core = originalCore;
  });

  it("logCLIFilters calls core.info when servers are present", () => {
    logCLIFilters(new Set(["github", "playwright"]));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("github"));
  });

  it("logCLIFilters does nothing when no CLI servers", () => {
    logCLIFilters(new Set());
    expect(mockCore.info).not.toHaveBeenCalled();
  });

  it("logServerStats reports included and filtered counts", () => {
    const servers = { github: {}, playwright: {}, bash: {} };
    logServerStats(servers, 2);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringMatching(/2 included/));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringMatching(/1 filtered/));
  });
});

describe("runGatewayConversion", () => {
  /** @type {string} */
  let dir;
  /** @type {NodeJS.ProcessEnv} */
  let savedEnv;
  /** @type {unknown} */
  let originalCore;
  /** @type {{ info: ReturnType<typeof vi.fn> }} */
  let mockCore;

  beforeEach(() => {
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "gateway-test-"));
    savedEnv = { ...process.env };
    originalCore = global.core;
    mockCore = { info: vi.fn() };
    // @ts-ignore
    global.core = mockCore;

    const gatewayFile = path.join(dir, "gateway.json");
    fs.writeFileSync(
      gatewayFile,
      JSON.stringify({
        mcpServers: {
          github: { url: "http://old/mcp/github" },
          cli: { url: "http://old/mcp/cli" },
        },
      })
    );
    process.env.MCP_GATEWAY_OUTPUT = gatewayFile;
    process.env.MCP_GATEWAY_DOMAIN = "gateway.internal";
    process.env.MCP_GATEWAY_PORT = "80";
    process.env.GH_AW_MCP_CLI_SERVERS = '["cli"]';
  });

  afterEach(() => {
    fs.rmSync(dir, { recursive: true, force: true });
    for (const key of Object.keys(process.env)) {
      if (!(key in savedEnv)) delete process.env[key];
    }
    Object.assign(process.env, savedEnv);
    // @ts-ignore
    global.core = originalCore;
  });

  it.each(["Copilot", "Claude"])("injects compat after HTTP conversion for %s", engine => {
    process.env.GH_AW_COMPAT_WEB_FETCH = "1";
    const transformServer = vi.fn((_name, entry) => ({ ...entry, type: "http" }));
    const serialize = vi.fn(servers => JSON.stringify({ mcpServers: servers }));
    const output = runGatewayConversion({ format: engine, engine, outputPath: path.join(dir, "local.json"), transformServer, serialize });
    const config = JSON.parse(output);
    expect(transformServer).toHaveBeenCalledTimes(1);
    expect(config.mcpServers.compat).toEqual({
      type: engine === "Copilot" ? "local" : "stdio",
      command: "node",
      args: [expect.stringMatching(/compat_mcp_server\.cjs$/)],
      env: { NODE_PATH: expect.stringMatching(/compat\/node_modules$/) },
      ...(engine === "Copilot" ? { tools: ["web_fetch"] } : {}),
    });
    expect(config.mcpServers.compat.url).toBeUndefined();
    expect(serialize.mock.calls[0][0].compat).toEqual(config.mcpServers.compat);
  });

  it("rejects collisions even when the gateway server is filtered", () => {
    process.env.GH_AW_COMPAT_WEB_FETCH = "1";
    process.env.GH_AW_MCP_CLI_SERVERS = '["compat"]';
    fs.writeFileSync(process.env.MCP_GATEWAY_OUTPUT, JSON.stringify({ mcpServers: { compat: {} } }));
    expect(() => runGatewayConversion({ format: "Copilot", engine: "Copilot", outputPath: path.join(dir, "collision.json"), transformServer: (_name, entry) => entry, serialize: JSON.stringify })).toThrow("reserved");
  });

  it("rejects converters without local stdio support", () => {
    process.env.GH_AW_COMPAT_WEB_FETCH = "1";
    expect(() => runGatewayConversion({ format: "Pi", engine: "Pi", outputPath: path.join(dir, "unsupported.json"), transformServer: (_name, entry) => entry, serialize: JSON.stringify })).toThrow("not supported");
  });

  it("loads, filters, serializes, securely writes, and reports a converted configuration", () => {
    const sentinel = "sentinel-authorization";
    const outputPath = path.join(dir, "output/config.json");
    const output = runGatewayConversion({
      format: "Test",
      engine: "Test",
      outputPath,
      getTargetDomain: () => "target.internal",
      getUrlPrefix: () => "http://target.internal:80",
      transformServer: (_name, entry, urlPrefix) => normalizeGatewayEntry({ ...entry, headers: { Authorization: sentinel } }, urlPrefix),
      serialize: servers => JSON.stringify({ mcpServers: servers }),
    });

    expect(JSON.parse(output)).toEqual({
      mcpServers: { github: { url: "http://target.internal:80/mcp/github", headers: { Authorization: sentinel } } },
    });
    expect(fs.readFileSync(outputPath, "utf8")).toBe(output);
    expect(fs.statSync(outputPath).mode & 0o777).toBe(0o600);
    expect(mockCore.info).toHaveBeenCalledWith("Converting gateway configuration to Test format...");
    expect(mockCore.info).toHaveBeenCalledWith("Target domain: target.internal:80");
    expect(mockCore.info).toHaveBeenCalledWith("Servers: 1 included, 1 filtered (CLI-mounted)");
    expect(mockCore.info).toHaveBeenCalledWith(`Test configuration written to ${outputPath}`);
    expect(mockCore.info).toHaveBeenCalledWith("Converted servers: github");
    expect(mockCore.info).not.toHaveBeenCalledWith(output);
    expect(JSON.stringify(mockCore.info.mock.calls)).not.toContain(sentinel);
  });

  it("accepts outputPath as a function and calls it with the gateway context", () => {
    const output = runGatewayConversion({
      format: "Test",
      engine: "Test",
      outputPath: context => path.join(dir, `output-${context.port}.json`),
      transformServer: (_name, entry, urlPrefix) => normalizeGatewayEntry(entry, urlPrefix),
      serialize: servers => JSON.stringify({ mcpServers: servers }),
    });

    const expectedPath = path.join(dir, "output-80.json");
    expect(fs.existsSync(expectedPath)).toBe(true);
    expect(JSON.parse(fs.readFileSync(expectedPath, "utf8"))).toEqual({
      mcpServers: { github: { url: "http://gateway.internal:80/mcp/github" } },
    });
    expect(output).toBe(fs.readFileSync(expectedPath, "utf8"));
  });

  it("uses context.urlPrefix when getUrlPrefix is not provided", () => {
    const outputPath = path.join(dir, "output/config-default.json");
    const output = runGatewayConversion({
      format: "Test",
      engine: "Test",
      outputPath,
      transformServer: (_name, entry, urlPrefix) => normalizeGatewayEntry(entry, urlPrefix),
      serialize: servers => JSON.stringify({ mcpServers: servers }),
    });

    // context.urlPrefix = http://gateway.internal:80 (from MCP_GATEWAY_DOMAIN + MCP_GATEWAY_PORT)
    expect(JSON.parse(output)).toEqual({
      mcpServers: { github: { url: "http://gateway.internal:80/mcp/github" } },
    });
    expect(mockCore.info).toHaveBeenCalledWith("Target domain: gateway.internal:80");
  });
});
