// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import http from "node:http";
import net from "node:net";
import { spawn } from "node:child_process";

const require = createRequire(import.meta.url);
const { bindCompatEnvironment, prepareCompatMCPConfig } = require("./compat_mcp_config.cjs");

function config() {
  return {
    mcpServers: {
      compat: { type: "local", command: "node", args: [path.join(import.meta.dirname, "compat_mcp_server.cjs")], env: { NODE_PATH: "/compiler/compat/node_modules" }, tools: ["web_fetch"] },
      github: { type: "http", url: "http://gateway/mcp/github" },
    },
  };
}

afterEach(() => vi.unstubAllEnvs());

describe("compat runtime environment binding", () => {
  it("binds only runtime proxies and CA settings, without ambient secrets or NO_PROXY", () => {
    const source = config();
    const result = bindCompatEnvironment(source, { HTTPS_PROXY: "http://awf:3128", NODE_EXTRA_CA_CERTS: "/awf/ca.pem", SSL_CERT_FILE: "/awf/ca.pem", GITHUB_TOKEN: "not-forwarded", NO_PROXY: "*" });
    expect(result.mcpServers.compat.env).toEqual({ NODE_PATH: "/compiler/compat/node_modules", HTTPS_PROXY: "http://awf:3128", NODE_EXTRA_CA_CERTS: "/awf/ca.pem", SSL_CERT_FILE: "/awf/ca.pem" });
    expect(source.mcpServers.compat.env).toEqual({ NODE_PATH: "/compiler/compat/node_modules" });
    expect(result.mcpServers.github).toEqual(source.mcpServers.github);
    expect(result.mcpServers.compat.command).toBe(process.execPath);
  });

  it("fails closed without runtime proxy settings or without a managed compat entry", () => {
    expect(() => bindCompatEnvironment(config(), {})).toThrow("requires an AWF proxy at agent runtime");
    expect(() => bindCompatEnvironment({ mcpServers: {} }, { HTTP_PROXY: "http://awf" })).toThrow("Missing compiler-managed");
  });

  it("writes a private runtime copy and can update the writable Copilot config in place", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "compat-config-"));
    try {
      const source = path.join(dir, "source.json");
      const destination = path.join(dir, "runtime.json");
      fs.writeFileSync(source, JSON.stringify(config()));
      vi.stubEnv("HTTPS_PROXY", "http://awf-runtime:3128");
      prepareCompatMCPConfig(source, destination);
      expect(JSON.parse(fs.readFileSync(destination, "utf8")).mcpServers.compat.env.HTTPS_PROXY).toBe("http://awf-runtime:3128");
      expect(fs.statSync(destination).mode & 0o777).toBe(0o600);
      expect(JSON.parse(fs.readFileSync(source, "utf8")).mcpServers.compat.env.HTTPS_PROXY).toBeUndefined();
      prepareCompatMCPConfig(source, source);
      expect(JSON.parse(fs.readFileSync(source, "utf8")).mcpServers.compat.env.HTTPS_PROXY).toBe("http://awf-runtime:3128");
      const link = path.join(dir, "link.json");
      fs.symlinkSync(source, link);
      expect(() => prepareCompatMCPConfig(link, destination)).toThrow();
      expect(() => prepareCompatMCPConfig(source, link)).toThrow();
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  it("fetches through the runtime proxy with a sanitized MCP child environment", async () => {
    const origin = http.createServer((_req, res) => res.end("inside firewall"));
    const proxy = http.createServer();
    const targets = [];
    proxy.on("connect", (req, client, head) => {
      targets.push(req.url);
      const [hostname, port] = req.url.split(":");
      const upstream = net.connect(Number(port), hostname, () => {
        client.write("HTTP/1.1 200 Connection Established\r\n\r\n");
        upstream.write(head);
        upstream.pipe(client);
        client.pipe(upstream);
      });
      upstream.on("error", () => client.destroy());
      client.on("error", () => upstream.destroy());
    });
    await new Promise(resolve => origin.listen(0, "127.0.0.1", resolve));
    await new Promise(resolve => proxy.listen(0, "127.0.0.1", resolve));
    const originPort = /** @type {net.AddressInfo} */ origin.address().port;
    const proxyPort = /** @type {net.AddressInfo} */ proxy.address().port;
    const bound = bindCompatEnvironment(config(), { HTTP_PROXY: `http://127.0.0.1:${proxyPort}`, NO_PROXY: "*" });
    const child = spawn(bound.mcpServers.compat.command, bound.mcpServers.compat.args, { env: bound.mcpServers.compat.env, stdio: ["pipe", "pipe", "pipe"] });
    let output = "";
    let diagnostics = "";
    child.stderr.on("data", chunk => (diagnostics += chunk.toString()));
    try {
      const reply = new Promise((resolve, reject) => {
        child.on("error", reject);
        child.on("exit", code => reject(new Error(`compat exited: ${code}; ${diagnostics}`)));
        child.stdout.on("data", chunk => {
          output += chunk.toString();
          for (const line of output.split("\n").filter(Boolean)) {
            const message = JSON.parse(line);
            if (message.id === 1) resolve(message);
          }
        });
      });
      child.stdin.write(JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "web_fetch", arguments: { url: `http://127.0.0.1:${originPort}/`, prompt: "Read this page" } } }) + "\n");
      const message = await reply;
      expect(message.result.isError).toBe(false);
      expect(JSON.parse(message.result.content[0].text).content).toBe("inside firewall");
      expect(targets).toEqual([`127.0.0.1:${originPort}`]);
    } finally {
      child.kill();
      await Promise.all([new Promise(resolve => origin.close(resolve)), new Promise(resolve => proxy.close(resolve))]);
    }
  });
});
