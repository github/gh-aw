import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  applyOTLPIgnoreIfMissing,
  clearGatewayStartupMarker,
  createGatewayStderrPath,
  getGatewayStartupMarkerPath,
  redactGatewayDiagnostics,
  writeGatewayStartupMarker,
  customGatewayEnvNamesVar,
  customGatewayReservedEnvPrefix,
  customGatewayEnvTransportPrefix,
  detectEngineType,
  extractOptionalServerNames,
  getJSONParseErrorContext,
  getOTLPIfMissingMode,
  hasNonEmptyOTLPHeaders,
  injectCustomGatewayEnvArgs,
  computeHealthRetryBudgetMs,
  MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_DEFAULT_MS,
  MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS,
  MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT,
  MCP_GATEWAY_HEALTH_RETRY_DEFAULTS,
  MCP_GATEWAY_HEALTH_RETRY_ENV,
  resolveGatewayHealthRetryConfig,
  normalizeSinkVisibilityEncoding,
  resolveCopilotConfigPaths,
  validateGatewayAgentIdentifiers,
} from "./start_mcp_gateway.cjs";

describe("start_mcp_gateway logging", () => {
  it("does not create the legacy MCP gateway stderr log", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).not.toContain("/tmp/gh-aw/mcp-logs/stderr.log");
  });

  it("does not create the MCP gateway startup log", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).not.toContain("/tmp/gh-aw/mcp-logs/start-gateway.log");
  });

  it("captures the gateway child process stderr outside the artifact directory", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).toContain(`stdio: ["pipe", outputFd, stderrFd]`);
    expect(source).toContain(`path.join(os.tmpdir(), "gh-aw-mcp-gateway-")`);
  });

  it("exports the gateway agent ID as the AWF API key handoff", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).toContain("`gateway-api-key=${agentId}`");
  });
});

describe("start_mcp_gateway health timeout", () => {
  it("allows cleanup and registration time after the backend startup timeout", () => {
    const config = resolveGatewayHealthRetryConfig({});

    expect(config).toMatchObject(MCP_GATEWAY_HEALTH_RETRY_DEFAULTS);
    expect(config.retryBudgetMs).toBe(computeHealthRetryBudgetMs(MCP_GATEWAY_HEALTH_RETRY_DEFAULTS));
    expect(config.retryBudgetMs).toBeGreaterThanOrEqual(MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_DEFAULT_MS + MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS);
  });

  it("applies environment overrides for every retry parameter", () => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxTotalAttempts]: "10",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs]: "100",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxDelayMs]: "400",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier]: "1.5",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backendStartupTimeoutMs]: "1000",
    });

    expect(config).toMatchObject({ maxTotalAttempts: 10, initialDelayMs: 100, maxDelayMs: 400, backoffMultiplier: 1.5, backendStartupTimeoutMs: 1000 });
    expect(config.retryBudgetMs).toBe(computeHealthRetryBudgetMs(config));
  });

  it("falls back to defaults for invalid overrides", () => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxTotalAttempts]: "not-a-number",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs]: "-5",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxDelayMs]: "",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier]: "0",
    });

    expect(config).toMatchObject(MCP_GATEWAY_HEALTH_RETRY_DEFAULTS);
  });

  it("raises the delay cap when it is below the initial delay", () => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs]: "2000",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxDelayMs]: "500",
    });

    expect(config.maxDelayMs).toBe(2000);
  });

  it("clamps a shrinking backoff multiplier to a constant delay", () => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier]: "0.5",
    });

    expect(config.backoffMultiplier).toBe(1);
  });

  it("caps an excessive attempt count", () => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxTotalAttempts]: "1000000000",
    });

    expect(config.maxTotalAttempts).toBe(MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT);
  });

  it("computes the cumulative retry delay withRetry will sleep", () => {
    // withRetry multiplies before its first sleep: 500ms, then 1000ms capped.
    expect(computeHealthRetryBudgetMs({ maxTotalAttempts: 3, initialDelayMs: 250, maxDelayMs: 1000, backoffMultiplier: 2 })).toBe(1500);
  });
});

describe("start_mcp_gateway startup diagnostics", () => {
  it("creates the stderr capture file in an owner-only directory outside /tmp/gh-aw", () => {
    const stderrPath = createGatewayStderrPath();
    const dir = path.dirname(stderrPath);
    expect(dir.startsWith("/tmp/gh-aw/")).toBe(false);
    expect(fs.statSync(dir).mode & 0o777).toBe(0o700);
    fs.rmSync(dir, { recursive: true, force: true });
  });

  it("redacts bearer headers and structured credential keys", () => {
    const sample = ["Authorization: ******", '{"agentId":"redact-me"}', "token=redact-me"].join("\n");
    const redacted = redactGatewayDiagnostics(sample);
    expect(redacted).not.toContain("redact-me");
    expect(redacted.match(/\[REDACTED\]/g)).toHaveLength(3);
  });
});

describe("start_mcp_gateway startup marker", () => {
  it("defaults to the path read by print_firewall_logs.sh", () => {
    expect(getGatewayStartupMarkerPath({})).toBe("/tmp/gh-aw/mcp-gateway-started");
  });

  it("honors the MCP_GATEWAY_STARTUP_MARKER override", () => {
    expect(getGatewayStartupMarkerPath({ MCP_GATEWAY_STARTUP_MARKER: "/tmp/custom-marker" })).toBe("/tmp/custom-marker");
  });

  it("clears a stale marker and recreates it with owner-only permissions", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-marker-"));
    const markerPath = path.join(dir, "nested", "mcp-gateway-started");
    fs.mkdirSync(path.dirname(markerPath), { recursive: true });
    fs.writeFileSync(markerPath, "stale");

    clearGatewayStartupMarker(markerPath);
    expect(fs.existsSync(markerPath)).toBe(false);

    writeGatewayStartupMarker(markerPath);
    expect(fs.existsSync(markerPath)).toBe(true);
    expect(fs.statSync(markerPath).mode & 0o777).toBe(0o600);

    fs.rmSync(dir, { recursive: true, force: true });
  });
});

describe("start_mcp_gateway custom environment arguments", () => {
  const marker = "__GH_AW_MCP_GATEWAY_CUSTOM_ENV__";

  it("passes hostile values as one atomic Docker argument", () => {
    const hostileValue = `x" --privileged -v /workspace/evil:/evil --entrypoint /evil -e X="x`;
    const args = injectCustomGatewayEnvArgs(["run", "--rm", marker, "gateway-image"], {
      [customGatewayEnvNamesVar]: '["BASH_ENV"]',
      [`${customGatewayEnvTransportPrefix}0`]: hostileValue,
    });

    expect(args).toEqual(["run", "--rm", "-e", `BASH_ENV=${hostileValue}`, "gateway-image"]);
  });

  it("preserves sorted multi-value index mapping and empty values", () => {
    const args = injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
      [customGatewayEnvNamesVar]: '["ALPHA","EMPTY","OMEGA"]',
      [`${customGatewayEnvTransportPrefix}0`]: "first",
      [`${customGatewayEnvTransportPrefix}1`]: "",
      [`${customGatewayEnvTransportPrefix}2`]: "last\nline",
    });

    expect(args).toEqual(["run", "-e", "ALPHA=first", "-e", "EMPTY=", "-e", "OMEGA=last\nline", "gateway-image"]);
  });

  it("uses an empty value when transport metadata is missing", () => {
    const args = injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
      [customGatewayEnvNamesVar]: '["PRESENT","MISSING"]',
      [`${customGatewayEnvTransportPrefix}0`]: "value",
    });

    expect(args).toEqual(["run", "-e", "PRESENT=value", "-e", "MISSING=", "gateway-image"]);
  });

  it("leaves commands without the marker unchanged", () => {
    const args = ["run", "--rm", "gateway-image"];
    expect(injectCustomGatewayEnvArgs(args, { [customGatewayEnvNamesVar]: "not-json" })).toBe(args);
  });

  it("rejects malformed JSON metadata", () => {
    expect(() =>
      injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
        [customGatewayEnvNamesVar]: "not-json",
      })
    ).toThrow(/must be valid JSON/);
  });

  it("rejects malformed or unsafe environment variable names", () => {
    expect(() =>
      injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
        [customGatewayEnvNamesVar]: '["BAD-NAME"]',
      })
    ).toThrow(/valid environment variable names/);
  });

  it.each([[`${customGatewayEnvTransportPrefix}0`], [customGatewayEnvNamesVar], [`${customGatewayReservedEnvPrefix}FOO`]])("rejects the reserved name %s", reservedName => {
    expect(() =>
      injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
        [customGatewayEnvNamesVar]: JSON.stringify([reservedName]),
      })
    ).toThrow(/reserved/);
  });

  it("rejects duplicate environment variable names", () => {
    expect(() =>
      injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
        [customGatewayEnvNamesVar]: '["API_TOKEN","API_TOKEN"]',
      })
    ).toThrow(/duplicate/);
  });
});

describe("start_mcp_gateway OTLP if-missing helpers", () => {
  let originalWarning;

  beforeEach(() => {
    originalWarning = global.core.warning;
    global.core.warning = vi.fn();
  });

  afterEach(() => {
    delete process.env.GH_AW_OTLP_IF_MISSING;
    global.core.warning = originalWarning;
  });

  it("normalizes if-missing mode", () => {
    expect(getOTLPIfMissingMode(undefined)).toBe("error");
    expect(getOTLPIfMissingMode(" warn ")).toBe("warn");
    expect(getOTLPIfMissingMode("ignore")).toBe("ignore");
    expect(getOTLPIfMissingMode("invalid")).toBe("error");
  });

  it("detects non-empty OTLP headers for string/map/array forms", () => {
    expect(hasNonEmptyOTLPHeaders("")).toBe(false);
    expect(hasNonEmptyOTLPHeaders("Authorization=Bearer token")).toBe(true);
    expect(hasNonEmptyOTLPHeaders({ Authorization: "" })).toBe(false);
    expect(hasNonEmptyOTLPHeaders({ Authorization: "Bearer token" })).toBe(true);
    expect(hasNonEmptyOTLPHeaders(["", "  "])).toBe(false);
    expect(hasNonEmptyOTLPHeaders(["", "token"])).toBe(true);
  });

  it("is a no-op when if-missing mode is unset/error", () => {
    const config = {
      gateway: {
        opentelemetry: {
          endpoint: "   ",
          headers: "",
        },
      },
    };
    applyOTLPIgnoreIfMissing(config);
    expect(config.gateway.opentelemetry).toEqual({
      endpoint: "   ",
      headers: "",
    });
  });

  it("removes opentelemetry when endpoint is empty for warn mode and emits a warning", () => {
    const warningSpy = vi.fn();
    global.core.warning = warningSpy;
    process.env.GH_AW_OTLP_IF_MISSING = "warn";

    const config = {
      gateway: {
        opentelemetry: {
          endpoint: "   ",
          headers: { Authorization: "" },
        },
      },
    };

    applyOTLPIgnoreIfMissing(config);

    expect(config.gateway.opentelemetry).toBeUndefined();
    expect(warningSpy).toHaveBeenCalledOnce();
    expect(warningSpy).toHaveBeenCalledWith(expect.stringContaining("OTLP endpoint is missing/empty"));
  });

  it("removes empty headers object for warn mode and emits a warning", () => {
    const warningSpy = vi.fn();
    global.core.warning = warningSpy;
    process.env.GH_AW_OTLP_IF_MISSING = "warn";

    const config = {
      gateway: {
        opentelemetry: {
          endpoint: "https://collector.example/v1/traces",
          headers: { Authorization: "", "X-Tenant": "   " },
        },
      },
    };

    applyOTLPIgnoreIfMissing(config);

    expect(config.gateway.opentelemetry.headers).toBeUndefined();
    expect(warningSpy).toHaveBeenCalledOnce();
    expect(warningSpy).toHaveBeenCalledWith(expect.stringContaining("OTLP headers are missing/empty"));
  });

  it("removes empty headers object for ignore mode without warning", () => {
    const warningSpy = vi.fn();
    global.core.warning = warningSpy;
    process.env.GH_AW_OTLP_IF_MISSING = "ignore";

    const config = {
      gateway: {
        opentelemetry: {
          endpoint: "https://collector.example/v1/traces",
          headers: { Authorization: "" },
        },
      },
    };

    applyOTLPIgnoreIfMissing(config);

    expect(config.gateway.opentelemetry.headers).toBeUndefined();
    expect(warningSpy).not.toHaveBeenCalled();
  });
});

// -----------------------------------------------------------------------------
// resolveCopilotConfigPaths — guards against the regression where /home/runner
// was hard-coded and broke self-hosted runners with HOME != /home/runner.
// -----------------------------------------------------------------------------
describe("start_mcp_gateway resolveCopilotConfigPaths", () => {
  let originalHome;

  beforeEach(() => {
    originalHome = process.env.HOME;
  });

  afterEach(() => {
    if (originalHome === undefined) {
      delete process.env.HOME;
    } else {
      process.env.HOME = originalHome;
    }
  });

  it("resolves the Copilot config dir under the runtime $HOME", () => {
    process.env.HOME = "/home/runner";
    expect(resolveCopilotConfigPaths()).toEqual({
      dir: "/home/runner/.copilot",
      file: "/home/runner/.copilot/mcp-config.json",
    });
  });

  it("respects a self-hosted runner HOME (not /home/runner)", () => {
    process.env.HOME = "/home/actions";
    expect(resolveCopilotConfigPaths()).toEqual({
      dir: "/home/actions/.copilot",
      file: "/home/actions/.copilot/mcp-config.json",
    });
  });

  it("respects a containerized HOME (/root)", () => {
    process.env.HOME = "/root";
    expect(resolveCopilotConfigPaths()).toEqual({
      dir: "/root/.copilot",
      file: "/root/.copilot/mcp-config.json",
    });
  });

  it("handles HOME with spaces and special characters via path.join", () => {
    process.env.HOME = "/var/lib/actions runner";
    expect(resolveCopilotConfigPaths()).toEqual({
      dir: "/var/lib/actions runner/.copilot",
      file: "/var/lib/actions runner/.copilot/mcp-config.json",
    });
  });

  it("throws (not exits) when HOME is unset so tests can exercise the branch", () => {
    delete process.env.HOME;
    expect(() => resolveCopilotConfigPaths()).toThrow(/HOME environment variable is not set/);
  });

  it("throws when HOME is empty string", () => {
    process.env.HOME = "";
    expect(() => resolveCopilotConfigPaths()).toThrow(/HOME environment variable is not set/);
  });

  it("never returns a path containing the literal /home/runner when HOME is different", () => {
    process.env.HOME = "/opt/actions/home";
    const { dir, file } = resolveCopilotConfigPaths();
    expect(dir).not.toContain("/home/runner");
    expect(file).not.toContain("/home/runner");
  });
});

describe("start_mcp_gateway detectEngineType", () => {
  const configDir = "/tmp/gh-aw/mcp-config";

  it("does not require HOME for an explicit non-copilot engine", () => {
    expect(detectEngineType(configDir, { GH_AW_ENGINE: "codex" }, () => false)).toBe("codex");
  });

  it("does not require HOME when auto-detecting codex", () => {
    const existsSync = vi.fn(p => p === `${configDir}/config.toml`);
    expect(detectEngineType(configDir, {}, existsSync)).toBe("codex");
    expect(existsSync).not.toHaveBeenCalledWith("/.copilot");
  });

  it("auto-detects copilot from the HOME-scoped config directory", () => {
    const env = { HOME: "/var/lib/actions runner" };
    const existsSync = vi.fn(p => p === "/var/lib/actions runner/.copilot");
    expect(detectEngineType(configDir, env, existsSync)).toBe("copilot");
  });
});

describe("start_mcp_gateway getJSONParseErrorContext", () => {
  it("extracts line/column and key for invalid escape values", () => {
    const invalidConfig = `{
  "mcpServers": {
    "github": {
      "env": {
        "GITHUB_HOST": "\\https://github.com"
      }
    }
  }
}`;
    let parseErrorMessage = "";
    try {
      JSON.parse(invalidConfig);
    } catch (err) {
      parseErrorMessage = /** @type {Error} */ err.message;
    }
    const context = getJSONParseErrorContext(invalidConfig, parseErrorMessage);
    expect(context).toBeTruthy();
    expect(context?.key).toBe("GITHUB_HOST");
    expect(context?.lineText).toContain(`"GITHUB_HOST"`);
  });
});

describe("start_mcp_gateway normalizeSinkVisibilityEncoding", () => {
  it("normalizes double-encoded sink visibility values", () => {
    const invalidConfig = `{
  "guard-policies": {
    "write-sink": {
      "sink-visibility": ""public""
    }
  }
}`;
    expect(normalizeSinkVisibilityEncoding(invalidConfig)).toContain(`"sink-visibility": "public"`);
  });

  it("leaves correctly encoded sink visibility values unchanged", () => {
    const validConfig = `{
  "guard-policies": {
    "write-sink": {
      "sink-visibility": "public"
    }
  }
}`;
    expect(normalizeSinkVisibilityEncoding(validConfig)).toBe(validConfig);
  });
});

describe("start_mcp_gateway extractOptionalServerNames", () => {
  it("collects servers declared with required: false and strips the flag from every server", () => {
    const configObj = {
      mcpServers: {
        datadog: { type: "http", url: "https://example.com/mcp", required: false },
        grafana: { type: "http", url: "https://example.com/grafana" },
        sentry: { type: "http", url: "https://example.com/sentry", required: true },
      },
    };

    expect(extractOptionalServerNames(configObj)).toEqual(["datadog"]);
    // The gateway configuration specification has no `required` field, so it is
    // removed for every server regardless of its value.
    expect(configObj.mcpServers.datadog).not.toHaveProperty("required");
    expect(configObj.mcpServers.sentry).not.toHaveProperty("required");
    expect(configObj.mcpServers.grafana).not.toHaveProperty("required");
  });

  it("returns an empty list when no servers are configured", () => {
    expect(extractOptionalServerNames({})).toEqual([]);
    expect(extractOptionalServerNames({ mcpServers: null })).toEqual([]);
  });
});

describe("start_mcp_gateway gateway agent identifier validation", () => {
  it.each([
    ["accepts a singular agent ID", { agentId: "agent-1" }, null],
    ["accepts one plural agent ID", { agentIds: ["agent-1"] }, null],
    ["accepts multiple plural agent IDs", { agentIds: ["agent-1", "agent-2"] }, null],
    ["rejects an empty plural agent ID list", { agentIds: [] }, "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"],
    ["rejects a non-array plural agent ID", { agentIds: "agent-1" }, "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"],
    ["rejects an empty ID in the plural list", { agentIds: ["agent-1", ""] }, "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"],
    ["rejects a non-string ID in the plural list", { agentIds: ["agent-1", 2] }, "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"],
    ["rejects both identifier forms", { agentId: "agent-1", agentIds: ["agent-1"] }, "ERROR: Gateway configuration must specify exactly one of 'agentId' or 'agentIds'"],
    ["rejects neither identifier form", {}, "ERROR: Gateway configuration must specify exactly one of 'agentId' or 'agentIds'"],
  ])("%s", (_name, gateway, expectedError) => {
    expect(validateGatewayAgentIdentifiers(gateway)).toBe(expectedError);
  });

  it("rejects an empty or malformed singular agent ID", () => {
    expect(validateGatewayAgentIdentifiers({ agentId: "" })).toBe("ERROR: Gateway 'agentId' must be a non-empty string");
    expect(validateGatewayAgentIdentifiers({ agentId: ["agent-1"] })).toBe("ERROR: Gateway 'agentId' must be a non-empty string");
  });
});
