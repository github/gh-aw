// @ts-check

import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createServer, loadToolHandlers } from "./mcp_server_core.cjs";
import { createToolEnvironment } from "./mcp_handler_process.cjs";

const tempDirs = [];
afterEach(() => {
  vi.unstubAllEnvs();
  for (const dir of tempDirs.splice(0)) fs.rmSync(dir, { recursive: true, force: true });
});

function binding(tool, name) {
  return `GH_AW_MCP_SCRIPTS_ENV_${Buffer.from(tool).toString("hex")}_${Buffer.from(name).toString("hex")}`;
}

const probes = {
  cjs: `const e = process.env; console.log(JSON.stringify({key: e.API_KEY || "", alpha: e.ALPHA_ONLY || "", beta: e.BETA_ONLY || "", auth: e.GH_AW_MCP_SCRIPTS_API_KEY || "", job: e.JOB_SECRET || "", transport: Object.keys(e).some(k => k.startsWith("GH_AW_MCP_SCRIPTS_ENV_")), path: Boolean(e.PATH), home: Boolean(e.HOME)}));`,
  sh: `#!/bin/bash
set -eu
printf '{"key":"%s","alpha":"%s","beta":"%s","auth":"%s","job":"%s","transport":%s,"path":%s,"home":%s}\\n' "\${API_KEY-}" "\${ALPHA_ONLY-}" "\${BETA_ONLY-}" "\${GH_AW_MCP_SCRIPTS_API_KEY-}" "\${JOB_SECRET-}" "$(env | grep -q '^GH_AW_MCP_SCRIPTS_ENV_' && echo true || echo false)" "$([ -n "\${PATH-}" ] && echo true || echo false)" "$([ -n "\${HOME-}" ] && echo true || echo false)"
echo "input=$INPUT_MESSAGE" >> "$GITHUB_OUTPUT"
`,
  py: `import os, json
e = os.environ
print(json.dumps(dict(key=e.get("API_KEY", ""), alpha=e.get("ALPHA_ONLY", ""), beta=e.get("BETA_ONLY", ""), auth=e.get("GH_AW_MCP_SCRIPTS_API_KEY", ""), job=e.get("JOB_SECRET", ""), transport=any(k.startswith("GH_AW_MCP_SCRIPTS_ENV_") for k in e), path=bool(e.get("PATH")), home=bool(e.get("HOME")))))
`,
  go: `package main
import ("os"; "encoding/json"; "strings")
func main() {
transport := false
for _, e := range os.Environ() { if strings.HasPrefix(e, "GH_AW_MCP_SCRIPTS_ENV_") { transport = true } }
json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"key":os.Getenv("API_KEY"), "alpha":os.Getenv("ALPHA_ONLY"), "beta":os.Getenv("BETA_ONLY"), "auth":os.Getenv("GH_AW_MCP_SCRIPTS_API_KEY"), "job":os.Getenv("JOB_SECRET"), "transport":transport, "path":os.Getenv("PATH") != "", "home":os.Getenv("HOME") != ""})
}
`,
};

describe("MCP scripts per-tool environment isolation", () => {
  for (const [extension, probe] of Object.entries(probes)) {
    it(`${extension}: preserves overlapping bindings and denies sibling/server secrets`, async () => {
      const dir = fs.mkdtempSync(path.join(process.cwd(), ".mcp-env-isolation-"));
      tempDirs.push(dir);
      vi.stubEnv("TMPDIR", dir);
      vi.stubEnv("GH_AW_MCP_SCRIPTS_API_KEY", "server-credential");
      vi.stubEnv("JOB_SECRET", "undeclared-job-credential");
      vi.stubEnv("API_KEY", "unscoped-credential");
      vi.stubEnv("ALPHA_ONLY", "unscoped-alpha-credential");
      vi.stubEnv("BETA_ONLY", "unscoped-beta-credential");
      vi.stubEnv("NODE_OPTIONS", "--require=/missing-server-only-hook.cjs");
      vi.stubEnv(binding("alpha", "API_KEY"), "alpha-key");
      vi.stubEnv(binding("beta", "API_KEY"), "beta-key");
      vi.stubEnv(binding("alpha", "ALPHA_ONLY"), "alpha-private");
      vi.stubEnv(binding("beta", "BETA_ONLY"), "beta-private");
      const tools = ["alpha", "beta", "public"].map(name => {
        const handler = `${name}.${extension}`;
        fs.writeFileSync(path.join(dir, handler), probe, { mode: 0o700 });
        return {
          name,
          handler,
          env:
            name === "public"
              ? {}
              : {
                  API_KEY: binding(name, "API_KEY"),
                  [name === "alpha" ? "ALPHA_ONLY" : "BETA_ONLY"]: binding(name, name === "alpha" ? "ALPHA_ONLY" : "BETA_ONLY"),
                },
        };
      });
      const server = createServer({ name: "isolation-test", version: "1.0.0" });
      const loaded = loadToolHandlers(server, tools, dir);
      const results = await Promise.all(
        loaded.map(async tool => {
          expect(typeof tool.handler).toBe("function");
          const response = await tool.handler({ message: "preserved" });
          const result = JSON.parse(response.content[0].text);
          if (extension === "sh") expect(result.outputs.input).toBe("preserved");
          return extension === "sh" ? JSON.parse(result.stdout) : result;
        })
      );
      expect(results).toEqual([
        { key: "alpha-key", alpha: "alpha-private", beta: "", auth: "", job: "", transport: false, path: true, home: true },
        { key: "beta-key", alpha: "", beta: "beta-private", auth: "", job: "", transport: false, path: true, home: true },
        { key: "", alpha: "", beta: "", auth: "", job: "", transport: false, path: true, home: true },
      ]);
    }, 120000);
  }

  it("fails explicitly on missing bindings, preserves empty values and explicit legacy references", () => {
    vi.stubEnv("DECLARED_EMPTY", "");
    vi.stubEnv("LEGACY_TOKEN", "declared-value");
    expect(createToolEnvironment({ EMPTY: "DECLARED_EMPTY", TOKEN: "LEGACY_TOKEN" })).toMatchObject({ EMPTY: "", TOKEN: "declared-value" });
    expect(() => createToolEnvironment({ TOKEN: "MCP_ISOLATION_MISSING_BINDING" })).toThrow("Missing MCP script environment binding for 'TOKEN'");
  });

  it("requires explicit bindings for ambient network, TLS, registry and GitHub configuration", () => {
    const ambient = {
      HTTPS_PROXY: "https://proxy.example.com:8443",
      NODE_EXTRA_CA_CERTS: "/etc/company-ca.pem",
      SSL_CERT_FILE: "/etc/company-ca.pem",
      REQUESTS_CA_BUNDLE: "/etc/company-ca.pem",
      PYTHONPATH: "/opt/company-python",
      PIP_INDEX_URL: "https://packages.example.com/simple",
      NPM_CONFIG_REGISTRY: "https://packages.example.com/npm",
      GOPROXY: "https://packages.example.com/go,direct",
      GITHUB_REPOSITORY: "owner/repo",
      GITHUB_SERVER_URL: "https://github.example.com",
      GITHUB_API_URL: "https://github.example.com/api/v3",
      GITHUB_RUN_ID: "1234",
      GITHUB_TOKEN: "ambient-token",
    };
    for (const [name, value] of Object.entries(ambient)) vi.stubEnv(name, value);
    const env = createToolEnvironment();
    expect(env.PATH).toBe(process.env.PATH);
    for (const name of Object.keys(ambient)) expect(env).not.toHaveProperty(name);
    expect(createToolEnvironment({ HTTPS_PROXY: "HTTPS_PROXY", NODE_EXTRA_CA_CERTS: "NODE_EXTRA_CA_CERTS" })).toMatchObject({
      HTTPS_PROXY: ambient.HTTPS_PROXY,
      NODE_EXTRA_CA_CERTS: ambient.NODE_EXTRA_CA_CERTS,
    });
    expect(() => createToolEnvironment({ TOKEN: "MCP_ISOLATION_MISSING_BINDING" })).toThrow("Missing MCP script environment binding for 'TOKEN'");
  });
});
