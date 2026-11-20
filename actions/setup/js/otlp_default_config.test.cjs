// @ts-check
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { resolveOTLPDefaultConfig } = require("./otlp_default_config.cjs");
const { parseOTLPEndpoints } = require("./send_otlp_span.cjs");
const keys = ["GH_AW_DEFAULT_OTLP_CONFIG", "GH_AW_DEFAULT_OTLP_ENDPOINT", "GH_AW_DEFAULT_OTLP_HEADERS", "GH_AW_OTLP_ENDPOINTS", "GH_AW_OTLP_IF_MISSING", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_HEADERS", "GH_AW_OTLP_ALL_HEADERS"];
let savedEnv;
let savedCore;
let masks;
let warnings;

beforeEach(() => {
  savedEnv = Object.fromEntries(keys.map(key => [key, process.env[key]]));
  for (const key of keys) delete process.env[key];
  savedCore = global.core;
  masks = [];
  warnings = [];
  global.core = { setSecret: value => masks.push(value), warning: value => warnings.push(value) };
  process.env.GH_AW_DEFAULT_OTLP_ENDPOINT = "https://legacy.example.com";
  process.env.GH_AW_DEFAULT_OTLP_HEADERS = "Authorization=Bearer legacy-credential";
  process.env.GH_AW_OTLP_IF_MISSING = "ignore";
});

afterEach(() => {
  global.core = savedCore;
  for (const [key, value] of Object.entries(savedEnv)) {
    if (value === undefined) delete process.env[key];
    else process.env[key] = value;
  }
});

describe("atomic OTLP runtime defaults", () => {
  it("masks the pair and decoded credentials before exporting any derived environment values", () => {
    const endpoint = "https://collector.example.com/private-endpoint";
    const password = "json-only-private-password";
    const credential = Buffer.from(`username:${password}`).toString("base64");
    const headers = `Authorization=Basic ${credential}`;
    process.env.GH_AW_DEFAULT_OTLP_CONFIG = JSON.stringify({ endpoint, headers });
    const exports = {};
    resolveOTLPDefaultConfig((key, value) => {
      expect(masks).toEqual(expect.arrayContaining([endpoint, headers, credential, password]));
      expect(masks).not.toContain("legacy-credential");
      exports[key] = value;
    });
    expect(exports).toEqual({
      OTEL_EXPORTER_OTLP_ENDPOINT: endpoint,
      OTEL_EXPORTER_OTLP_HEADERS: headers,
      GH_AW_OTLP_ENDPOINTS: JSON.stringify([{ url: endpoint, headers }]),
    });
    expect(parseOTLPEndpoints()).toEqual([{ url: endpoint, headers }]);
  });

  it.each([{ endpoint: "https://atomic.example.com" }, { headers: "Authorization=Bearer atomic-only-credential" }, {}])("never fills an incomplete atomic pair from legacy fields: %j", config => {
    process.env.GH_AW_DEFAULT_OTLP_CONFIG = JSON.stringify(config);
    resolveOTLPDefaultConfig();
    expect(parseOTLPEndpoints()).toEqual([]);
    expect(process.env.OTEL_EXPORTER_OTLP_ENDPOINT).toBe(config.endpoint || "");
    expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toBe(config.headers || "");
    expect(JSON.stringify(masks)).not.toContain("legacy-credential");
  });

  it("disables malformed JSON without exposing parser diagnostics or borrowing legacy credentials", () => {
    process.env.GH_AW_DEFAULT_OTLP_CONFIG = '{"headers":"private-secret-fragment",';
    resolveOTLPDefaultConfig();
    expect(parseOTLPEndpoints()).toEqual([]);
    expect(warnings).toEqual(["Invalid GH_AW_DEFAULT_OTLP_CONFIG JSON; OTLP export is disabled"]);
    expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toBe("");
  });

  it("preserves the legacy secret/variable fallback when the atomic secret is absent", () => {
    process.env.GH_AW_DEFAULT_OTLP_CONFIG = "";
    expect(parseOTLPEndpoints()).toEqual([{ url: "https://legacy.example.com", headers: "Authorization=Bearer legacy-credential" }]);
    expect(masks).toContain("legacy-credential");
  });

  it("resolves and masks raw defaults for conclusion/report exporters without a setup invocation", () => {
    process.env.GH_AW_DEFAULT_OTLP_CONFIG = JSON.stringify({
      endpoint: "https://conclusion.example.com",
      headers: "Authorization=Bearer conclusion-private-token",
    });
    expect(parseOTLPEndpoints()).toEqual([{ url: "https://conclusion.example.com", headers: "Authorization=Bearer conclusion-private-token" }]);
    expect(masks).toContain("conclusion-private-token");
  });

  it("masks the serialized endpoint list before exporting JSON-escaped credentials", () => {
    const config = { endpoint: "https://collector.example.com", headers: 'x-key=private-"quoted"-credential' };
    process.env.GH_AW_DEFAULT_OTLP_CONFIG = JSON.stringify(config);
    resolveOTLPDefaultConfig((key, value) => {
      if (key === "GH_AW_OTLP_ENDPOINTS") expect(masks).toContain(value);
    });
    expect(parseOTLPEndpoints()).toEqual([{ url: config.endpoint, headers: config.headers }]);
  });

  it("preserves explicit workflow endpoint/header settings over the default pair", () => {
    process.env.GH_AW_DEFAULT_OTLP_CONFIG = JSON.stringify({
      endpoint: "https://default.example.com",
      headers: "Authorization=Bearer default-private-token",
    });
    process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "https://explicit.example.com";
    process.env.OTEL_EXPORTER_OTLP_HEADERS = "Authorization=Bearer explicit-private-token";
    expect(parseOTLPEndpoints()).toEqual([{ url: "https://explicit.example.com", headers: "Authorization=Bearer explicit-private-token" }]);
    expect(masks).not.toContain("default-private-token");
  });

  it("never sends default credentials to an explicitly overridden endpoint", () => {
    process.env.GH_AW_DEFAULT_OTLP_CONFIG = JSON.stringify({
      endpoint: "https://default.example.com",
      headers: "Authorization=Bearer default-private-token",
    });
    process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "https://explicit.example.com";
    expect(parseOTLPEndpoints()).toEqual([]);
    expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toBe("");
  });

  it("does not override explicit endpoint arrays or their opt-out", () => {
    process.env.GH_AW_DEFAULT_OTLP_CONFIG = '{"endpoint":"https://default.example.com","headers":"x-key=private-default"}';
    process.env.GH_AW_OTLP_ENDPOINTS = "[]";
    const exportEnv = vi.fn();
    resolveOTLPDefaultConfig(exportEnv);
    expect(parseOTLPEndpoints()).toEqual([]);
    expect(exportEnv).not.toHaveBeenCalled();
    expect(process.env.OTEL_EXPORTER_OTLP_ENDPOINT).toBeUndefined();
  });

  it.each(["\r", "\n"])("masks but never exports file-command injection containing %j", lineBreak => {
    process.env.GH_AW_DEFAULT_OTLP_CONFIG = JSON.stringify({
      endpoint: "https://collector.example.com",
      headers: `x-key=private-secret${lineBreak}INJECTED_VAR=value`,
    });
    const exports = {};
    resolveOTLPDefaultConfig((key, value) => {
      expect(value).not.toMatch(/[\r\n]/);
      exports[key] = value;
    });
    expect(exports).toEqual({
      OTEL_EXPORTER_OTLP_ENDPOINT: "",
      OTEL_EXPORTER_OTLP_HEADERS: "",
      GH_AW_OTLP_ENDPOINTS: "[]",
    });
    expect(warnings.join(" ")).not.toContain("private-secret");
  });
});
