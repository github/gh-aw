// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import { tmpdir } from "os";
import { join } from "path";
import { writeFileSync, readFileSync, mkdtempSync, rmSync, mkdirSync } from "fs";

const req = createRequire(import.meta.url);
const { main } = req("./update_network_allowed.cjs");

const ECOSYSTEM_MAP = {
  npm: ["registry.npmjs.org", "nodejs.org"],
  python: ["pypi.org", "files.pythonhosted.org"],
};

describe("update_network_allowed.cjs", () => {
  /** @type {string} */
  let tempDir;
  /** @type {string} */
  let configPath;
  /** @type {Record<string, string | undefined>} */
  let savedEnv;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "update-network-allowed-test-"));
    const ghAwDir = join(tempDir, "gh-aw");
    mkdirSync(ghAwDir);
    configPath = join(ghAwDir, "awf-config.json");

    savedEnv = {
      RUNNER_TEMP: process.env.RUNNER_TEMP,
      GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED: process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED,
      GH_AW_ECOSYSTEM_MAP_JSON: process.env.GH_AW_ECOSYSTEM_MAP_JSON,
      GH_AW_OTLP_ENDPOINTS: process.env.GH_AW_OTLP_ENDPOINTS,
      GH_AW_OTLP_IF_MISSING: process.env.GH_AW_OTLP_IF_MISSING,
      OTEL_EXPORTER_OTLP_ENDPOINT: process.env.OTEL_EXPORTER_OTLP_ENDPOINT,
    };

    process.env.RUNNER_TEMP = tempDir;
    process.env.GH_AW_ECOSYSTEM_MAP_JSON = JSON.stringify(ECOSYSTEM_MAP);
    delete process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED;
    delete process.env.GH_AW_OTLP_ENDPOINTS;
    delete process.env.GH_AW_OTLP_IF_MISSING;
    delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });

    for (const [key, value] of Object.entries(savedEnv)) {
      if (value === undefined) {
        delete process.env[key];
      } else {
        process.env[key] = value;
      }
    }
  });

  it("leaves allowDomains unchanged when no tokens are set", async () => {
    const initial = { network: { allowDomains: ["example.com"] } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED = "";
    await main();

    const result = JSON.parse(readFileSync(configPath, "utf8"));
    expect(result.network.allowDomains).toEqual(["example.com"]);
  });

  it("expands an ecosystem token to its domains", async () => {
    const initial = { network: { allowDomains: [] } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED = "npm";
    await main();

    const result = JSON.parse(readFileSync(configPath, "utf8"));
    expect(result.network.allowDomains).toContain("registry.npmjs.org");
    expect(result.network.allowDomains).toContain("nodejs.org");
  });

  it("expands multiple ecosystem tokens", async () => {
    const initial = { network: { allowDomains: [] } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED = "npm,python";
    await main();

    const result = JSON.parse(readFileSync(configPath, "utf8"));
    expect(result.network.allowDomains).toContain("registry.npmjs.org");
    expect(result.network.allowDomains).toContain("pypi.org");
  });

  it("treats unknown tokens as raw domain names", async () => {
    const initial = { network: { allowDomains: [] } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED = "custom.example.com";
    await main();

    const result = JSON.parse(readFileSync(configPath, "utf8"));
    expect(result.network.allowDomains).toContain("custom.example.com");
  });

  it("does not add duplicate domains", async () => {
    const initial = { network: { allowDomains: ["registry.npmjs.org"] } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED = "npm";
    await main();

    const result = JSON.parse(readFileSync(configPath, "utf8"));
    const count = result.network.allowDomains.filter((/** @type {string} */ d) => d === "registry.npmjs.org").length;
    expect(count).toBe(1);
  });

  it("allows all configured OTLP endpoint hosts at runtime", async () => {
    const initial = { network: { allowDomains: ["existing.example.com"] } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://observabilityci.ingest.us-east-2.gcp.elasticcloud.com:443/v1/traces" }, { url: "https://secondary.example.com:4318/v1/traces" }]);
    process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "https://observabilityci.ingest.us-east-2.gcp.elasticcloud.com:443/v1/traces";
    await main();

    const result = JSON.parse(readFileSync(configPath, "utf8"));
    expect(result.network.allowDomains).toEqual(["existing.example.com", "observabilityci.ingest.us-east-2.gcp.elasticcloud.com", "secondary.example.com"]);
  });

  it("ignores malformed and non-HTTP OTLP endpoint values", async () => {
    const initial = { network: { allowDomains: [] } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "not a URL" }, { url: "file:///etc/passwd" }]);
    process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "invalid";
    await main();

    const result = JSON.parse(readFileSync(configPath, "utf8"));
    expect(result.network.allowDomains).toEqual([]);
  });

  it("allows IPv6 and gRPC collector hosts in firewall format", async () => {
    writeFileSync(configPath, JSON.stringify({ network: { allowDomains: ["::1"] } }) + "\n");
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://[::1]:4318" }, { url: "grpc://traces.example.com:4317" }]);

    await main();

    expect(JSON.parse(readFileSync(configPath, "utf8")).network.allowDomains).toEqual(["::1", "traces.example.com"]);
  });

  it("does not allow enterprise-default collectors without credentials", async () => {
    writeFileSync(configPath, JSON.stringify({ network: { allowDomains: [] } }) + "\n");
    process.env.GH_AW_OTLP_IF_MISSING = "ignore";
    process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "https://unconfigured.example.com";
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://unconfigured.example.com" }, { url: "https://configured.example.com", headers: "Authorization=******" }]);

    await main();

    expect(JSON.parse(readFileSync(configPath, "utf8")).network.allowDomains).toEqual(["configured.example.com"]);
  });

  it("initialises network.allowDomains when not present", async () => {
    const initial = { apiProxy: { enabled: true } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED = "npm";
    await main();

    const result = JSON.parse(readFileSync(configPath, "utf8"));
    expect(Array.isArray(result.network.allowDomains)).toBe(true);
    expect(result.network.allowDomains).toContain("registry.npmjs.org");
  });

  it("trims whitespace around tokens", async () => {
    const initial = { network: { allowDomains: [] } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED = " npm , python ";
    await main();

    const result = JSON.parse(readFileSync(configPath, "utf8"));
    expect(result.network.allowDomains).toContain("registry.npmjs.org");
    expect(result.network.allowDomains).toContain("pypi.org");
  });

  it("writes compact JSON with a trailing newline", async () => {
    const initial = { network: { allowDomains: [] } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED = "npm";
    await main();

    const raw = readFileSync(configPath, "utf8");
    expect(raw.endsWith("\n")).toBe(true);
    // Compact JSON: no spaces after : or ,
    expect(raw).not.toMatch(/: /);
    expect(raw).not.toMatch(/, /);
  });

  it("exits 1 when RUNNER_TEMP is not set", async () => {
    delete process.env.RUNNER_TEMP;
    process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED = "npm";

    const exitSpy = vi.spyOn(process, "exit").mockImplementation(_code => {
      throw new Error("process.exit called");
    });
    try {
      await expect(main()).rejects.toThrow();
      expect(exitSpy).toHaveBeenCalledWith(1);
    } finally {
      exitSpy.mockRestore();
    }
  });

  it("exits 1 when GH_AW_ECOSYSTEM_MAP_JSON is invalid JSON", async () => {
    const initial = { network: { allowDomains: [] } };
    writeFileSync(configPath, JSON.stringify(initial) + "\n");

    process.env.GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED = "npm";
    process.env.GH_AW_ECOSYSTEM_MAP_JSON = "{not valid json";

    const exitSpy = vi.spyOn(process, "exit").mockImplementation(_code => {
      throw new Error("process.exit called");
    });
    try {
      await expect(main()).rejects.toThrow();
      expect(exitSpy).toHaveBeenCalledWith(1);
    } finally {
      exitSpy.mockRestore();
    }
  });
});
