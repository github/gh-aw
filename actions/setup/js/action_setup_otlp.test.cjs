// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import { tmpdir } from "os";
import { join } from "path";
import { writeFileSync, readFileSync, mkdtempSync, rmSync } from "fs";

const req = createRequire(import.meta.url);

// Set up global.core mock before loading the module under test so shim.cjs
// sees it already present and does not overwrite it with its stderr-based shim.
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  setSecret: vi.fn(),
};
global.core = mockCore;

// Load send_otlp_span.cjs first so we can patch its exports before run() calls require()
// inside action_setup_otlp.cjs. Both share the same CJS module cache, so patching the
// exports object here is reflected when run() destructures from require(...send_otlp_span.cjs).
const sendOtlpModule = req("./send_otlp_span.cjs");
const originalSendJobSetupSpan = sendOtlpModule.sendJobSetupSpan;
const originalIsValidTraceId = sendOtlpModule.isValidTraceId;
const originalIsValidSpanId = sendOtlpModule.isValidSpanId;

// Load the module under test after patching is possible
const { run, writeEnvLine } = req("./action_setup_otlp.cjs");

const mockSendJobSetupSpan = vi.fn();

/** 32 lowercase hex chars — valid OTLP trace ID */
const VALID_TRACE_ID = "0102030405060708090a0b0c0d0e0f10";
/** 16 lowercase hex chars — valid OTLP span ID */
const VALID_SPAN_ID = "0102030405060708";
/** 16 lowercase hex chars — valid OTLP parent span ID */
const VALID_PARENT_SPAN_ID = "1112131415161718";

describe("action_setup_otlp.cjs", () => {
  /** @type {string} */
  let outputFile;
  /** @type {string} */
  let envFile;
  /** @type {string} */
  let tempDir;
  /** @type {Record<string, string | undefined>} */
  let savedEnv;

  beforeEach(() => {
    vi.clearAllMocks();

    // Patch the shared CJS module exports — run() re-destructures on every call
    sendOtlpModule.sendJobSetupSpan = mockSendJobSetupSpan;
    mockSendJobSetupSpan.mockResolvedValue({ traceId: VALID_TRACE_ID, spanId: VALID_SPAN_ID, parentSpanId: VALID_PARENT_SPAN_ID });

    // Provide fresh temp files so GITHUB_OUTPUT and GITHUB_ENV writes are isolated
    tempDir = mkdtempSync(join(tmpdir(), "action-setup-otlp-test-"));
    outputFile = join(tempDir, "test_github_output");
    envFile = join(tempDir, "test_github_env");
    writeFileSync(outputFile, "");
    writeFileSync(envFile, "");

    savedEnv = {
      OTEL_EXPORTER_OTLP_ENDPOINT: process.env.OTEL_EXPORTER_OTLP_ENDPOINT,
      SETUP_START_MS: process.env.SETUP_START_MS,
      GITHUB_OUTPUT: process.env.GITHUB_OUTPUT,
      GITHUB_ENV: process.env.GITHUB_ENV,
      INPUT_TRACE_ID: process.env.INPUT_TRACE_ID,
      "INPUT_TRACE-ID": process.env["INPUT_TRACE-ID"],
      INPUT_JOB_NAME: process.env.INPUT_JOB_NAME,
      "INPUT_JOB-NAME": process.env["INPUT_JOB-NAME"],
      INPUT_PARENT_SPAN_ID: process.env.INPUT_PARENT_SPAN_ID,
      "INPUT_PARENT-SPAN-ID": process.env["INPUT_PARENT-SPAN-ID"],
      INPUT_OTLP_OIDC_TOKEN: process.env.INPUT_OTLP_OIDC_TOKEN,
      GH_AW_OTLP_ENDPOINTS: process.env.GH_AW_OTLP_ENDPOINTS,
      OTEL_EXPORTER_OTLP_HEADERS: process.env.OTEL_EXPORTER_OTLP_HEADERS,
      GH_AW_OTLP_ALL_HEADERS: process.env.GH_AW_OTLP_ALL_HEADERS,
    };

    delete process.env.GH_AW_OTLP_ENDPOINTS;
    delete process.env.SETUP_START_MS;
    delete process.env.INPUT_TRACE_ID;
    delete process.env["INPUT_TRACE-ID"];
    delete process.env.INPUT_JOB_NAME;
    delete process.env["INPUT_JOB-NAME"];
    delete process.env.INPUT_PARENT_SPAN_ID;
    delete process.env["INPUT_PARENT-SPAN-ID"];
    delete process.env.INPUT_OTLP_OIDC_TOKEN;
    delete process.env["INPUT_OTLP_OIDC_TOKEN"];
    delete process.env.OTEL_EXPORTER_OTLP_HEADERS;
    delete process.env.GH_AW_OTLP_ALL_HEADERS;
    process.env.GITHUB_OUTPUT = outputFile;
    process.env.GITHUB_ENV = envFile;
  });

  afterEach(() => {
    vi.restoreAllMocks();
    sendOtlpModule.sendJobSetupSpan = originalSendJobSetupSpan;
    sendOtlpModule.isValidTraceId = originalIsValidTraceId;
    sendOtlpModule.isValidSpanId = originalIsValidSpanId;

    rmSync(tempDir, { recursive: true, force: true });

    for (const [key, val] of Object.entries(savedEnv)) {
      if (val !== undefined) process.env[key] = val;
      else delete process.env[key];
    }
  });

  it("should export run as a function", () => {
    expect(typeof run).toBe("function");
  });

  describe("writeEnvLine", () => {
    it.each(["\n", "\r", "\r\n"])("rejects line breaks %j in keys and values without logging them", lineBreak => {
      const malicious = `synthetic-secret${lineBreak}INJECTED_VAR=malicious-value`;
      writeFileSync(envFile, "EXISTING_VAR=preserved\n");

      writeEnvLine(envFile, malicious, "safe", malicious, "GITHUB_ENV");
      writeEnvLine(envFile, "SAFE_KEY", malicious, malicious, "GITHUB_ENV");

      expect(readFileSync(envFile, "utf8")).toBe("EXISTING_VAR=preserved\n");
      expect(mockCore.warning).toHaveBeenCalledTimes(2);
      expect(mockCore.warning).toHaveBeenCalledWith("[otlp] refusing to write a key or value containing line breaks");
      expect(mockCore.info).not.toHaveBeenCalled();
      expect(mockCore.warning.mock.calls.flat().join("\n")).not.toContain("synthetic-secret");
      expect(mockCore.warning.mock.calls.flat().join("\n")).not.toContain("INJECTED_VAR");
    });

    it("preserves valid single-line values", () => {
      writeEnvLine(envFile, "SAFE_KEY", "value=with,delimiters", "SAFE_KEY", "GITHUB_ENV");

      expect(readFileSync(envFile, "utf8")).toBe("SAFE_KEY=value=with,delimiters\n");
      expect(mockCore.warning).not.toHaveBeenCalled();
    });
  });

  it.each(["OTEL_EXPORTER_OTLP_HEADERS", "GH_AW_OTLP_ALL_HEADERS", "GH_AW_OTLP_ENDPOINTS"])("does not register plain placeholders from %s as standalone runner masks", async variable => {
    const headers = [
      "X-No-Auth=true",
      "X-False=false",
      "X-Null=null",
      "X-Short=abcd",
      "X-Seven=abcdefg",
      "X-Digits=12345678",
      "X-Encoded-Boolean=%74%72%75%65",
      "X-Encoded-Short=%61%62%63%64",
      "X-Encoded-Digits=%31%32%33%34%35%36%37%38",
      "X-Key=abcdefgh",
      "X-Encoded-Key=%61bcdefgh",
    ].join(",");
    process.env[variable] = variable === "GH_AW_OTLP_ENDPOINTS" ? JSON.stringify([{ url: "", headers }]) : headers;
    mockSendJobSetupSpan.mockImplementationOnce(async () => {
      expect(mockCore.setSecret.mock.calls.map(([value]) => value).sort()).toEqual([headers, "abcdefgh", "%61bcdefgh"].sort());
      return { traceId: VALID_TRACE_ID, spanId: VALID_SPAN_ID };
    });

    await run();

    expect(mockCore.setSecret.mock.calls.map(([value]) => value).sort()).toEqual([headers, "abcdefgh", "%61bcdefgh"].sort());
  });

  it.each(["Bearer abcd", "bearer 1234", "Bearer true", "Bearer abc", "Basic dTp0cnVl", "basic dToxMjM0"])("preserves short auth credential runner masks for %s", async authorization => {
    const encoded = encodeURIComponent(authorization);
    const headers = `Authorization=${encoded}`;
    process.env.OTEL_EXPORTER_OTLP_HEADERS = headers;
    const credential = authorization.split(" ")[1];
    const expected = new Set([headers, encoded, authorization]);
    if (credential.length >= 4) expected.add(credential);
    if (authorization.toLowerCase().startsWith("basic ")) {
      const decoded = Buffer.from(credential, "base64").toString("utf8");
      expected.add(decoded);
      expected.add(decoded.split(":")[1]);
    }

    await run();

    expect(mockCore.setSecret.mock.calls.map(([value]) => value).sort()).toEqual([...expected].sort());
  });

  it("masks decoded OTLP credentials before sending spans without changing configured headers", async () => {
    const password = "synthetic-grafana-cloud-token";
    const basic = `Basic ${Buffer.from(`123456:${password}`).toString("base64")}`;
    const headers = `Authorization=${basic}`;
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://grafana.example.com", headers }]);
    process.env.OTEL_EXPORTER_OTLP_HEADERS = "Authorization=Bearer synthetic-primary-token";
    process.env.GH_AW_OTLP_ALL_HEADERS = "x-sentry-auth=synthetic-consolidated-token";
    mockSendJobSetupSpan.mockImplementationOnce(async () => {
      expect(mockCore.setSecret).toHaveBeenCalledWith(password);
      expect(mockCore.setSecret).toHaveBeenCalledWith("synthetic-primary-token");
      expect(mockCore.setSecret).toHaveBeenCalledWith("synthetic-consolidated-token");
      return { traceId: VALID_TRACE_ID, spanId: VALID_SPAN_ID };
    });

    await run();

    expect(JSON.parse(process.env.GH_AW_OTLP_ENDPOINTS || "[]")[0].headers).toBe(headers);
    expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toBe("Authorization=Bearer synthetic-primary-token");
    expect(mockCore.info.mock.calls.flat().join("\n")).not.toContain(password);
  });

  it.each(["GH_AW_OTLP_ALL_HEADERS", "OTEL_EXPORTER_OTLP_HEADERS"])("masks repeated Authorization headers in %s even when the middle endpoint URL is empty", async headerVariable => {
    const passwords = ["synthetic-first-password", "synthetic-middle-password", "synthetic-last-password"];
    const headers = passwords.map(password => `Authorization=${encodeURIComponent(`Basic ${Buffer.from(`123456:${password}`).toString("base64")}`)}`);
    const endpoints = [
      { url: "https://first.example.com", headers: headers[0] },
      { url: "", headers: headers[1] },
      { url: "https://last.example.com", headers: headers[2] },
    ];
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify(endpoints);
    process.env[headerVariable] = headers.join(",Authorization=%invalid,");
    mockSendJobSetupSpan.mockImplementationOnce(async () => {
      for (const password of passwords) {
        expect(mockCore.setSecret).toHaveBeenCalledWith(password);
      }
      return { traceId: VALID_TRACE_ID, spanId: VALID_SPAN_ID };
    });

    await run();

    expect(JSON.parse(process.env.GH_AW_OTLP_ENDPOINTS || "[]")).toEqual(endpoints);
    expect(process.env[headerVariable]).toBe(headers.join(",Authorization=%invalid,"));
  });

  it("registers escaped passwords and credentials from disabled endpoints before eval output can stream", async () => {
    const password = 'synthetic-otlp-"password\\tail';
    const decoded = `123456:${password}`;
    const encoded = Buffer.from(decoded).toString("base64");
    const headers = `Authorization=${encodeURIComponent(`Basic ${encoded}`)}`;
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "", headers }]);
    mockSendJobSetupSpan.mockImplementationOnce(async () => {
      for (const value of [password, decoded, encoded, JSON.stringify(password).slice(1, -1), encodeURIComponent(`Basic ${encoded}`)]) {
        expect(mockCore.setSecret).toHaveBeenCalledWith(value);
      }
      return { traceId: VALID_TRACE_ID, spanId: VALID_SPAN_ID };
    });

    await run();

    expect(mockCore.info.mock.calls.flat().join("\n")).not.toContain(password);
    expect(JSON.parse(process.env.GH_AW_OTLP_ENDPOINTS || "[]")).toEqual([{ url: "", headers }]);
  });

  describe("when GH_AW_OTLP_ENDPOINTS is not set", () => {
    it("should log that OTLP export is being skipped", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith("[otlp] GH_AW_OTLP_ENDPOINTS not set, skipping setup span");
    });

    it("should still call sendJobSetupSpan for JSONL mirror", async () => {
      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledOnce();
    });

    it("should not log 'sending setup span to' when endpoint is absent", async () => {
      await run();

      const logged = vi.mocked(mockCore.info).mock.calls.flat();
      expect(logged.some(msg => typeof msg === "string" && msg.includes("sending setup span to"))).toBe(false);
    });
  });

  describe("when GH_AW_OTLP_ENDPOINTS is set", () => {
    beforeEach(() => {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:4318" }]);
    });

    it("should log sending the setup span to the configured endpoint", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith("[otlp] sending setup span to configured endpoints");
    });

    it("should call sendJobSetupSpan exactly once", async () => {
      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledOnce();
    });

    it("should log setup span sent with traceId and spanId", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining(`traceId=${VALID_TRACE_ID}`));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining(`spanId=${VALID_SPAN_ID}`));
    });

    it("should log the resolved trace ID", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining(`trace-id=${VALID_TRACE_ID}`));
    });

    it("disables OTLP for later steps when the Authorization secret is empty", async () => {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com", headers: "Authorization=" }]);
      process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "https://traces.example.com";
      process.env.OTEL_EXPORTER_OTLP_HEADERS = "Authorization=";

      await run();

      expect(process.env.OTEL_EXPORTER_OTLP_ENDPOINT).toBe("");
      expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toBe("");
      expect(readFileSync(envFile, "utf8")).toContain("OTEL_EXPORTER_OTLP_ENDPOINT=\n");
      expect(readFileSync(envFile, "utf8")).toContain("OTEL_EXPORTER_OTLP_HEADERS=\n");
      expect(mockCore.info).toHaveBeenCalledWith("[otlp] no OTLP endpoints have usable credentials, skipping setup span");
      expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("[otlp] setup span sent"));
    });

    it("promotes the next configured endpoint when the first Authorization secret is empty", async () => {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([
        { url: "https://sentry.example.com", headers: "x-sentry-auth=" },
        { url: "https://grafana.example.com", headers: "Authorization=******" },
      ]);
      process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "https://sentry.example.com";
      process.env.OTEL_EXPORTER_OTLP_HEADERS = "x-sentry-auth=";

      await run();

      expect(process.env.OTEL_EXPORTER_OTLP_ENDPOINT).toBe("https://grafana.example.com");
      expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toBe("Authorization=******");
    });

    it("preserves explicit exporter overrides when the primary endpoint is unavailable", async () => {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([
        { url: "https://sentry.example.com", headers: "x-sentry-auth=" },
        { url: "https://grafana.example.com", headers: "Authorization=******" },
      ]);
      process.env.OTEL_EXPORTER_OTLP_ENDPOINT = "https://custom.example.com";
      process.env.OTEL_EXPORTER_OTLP_HEADERS = "Authorization=custom";

      await run();

      expect(process.env.OTEL_EXPORTER_OTLP_ENDPOINT).toBe("https://custom.example.com");
      expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toBe("Authorization=custom");
    });
  });

  describe("OTLP OIDC token header injection", () => {
    it.each(["\n", "\r", "\r\n"])("rejects tokens containing %j without injecting environment variables", async lineBreak => {
      const token = `synthetic-oidc-token${lineBreak}INJECTED_VAR=malicious-value`;
      const headers = "X-Tenant=acme";
      const endpoints = JSON.stringify([{ url: "https://otlp.example.com", headers }]);
      process.env.INPUT_OTLP_OIDC_TOKEN = token;
      process.env.OTEL_EXPORTER_OTLP_HEADERS = headers;
      process.env.GH_AW_OTLP_ENDPOINTS = endpoints;

      await run();

      expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toBe(headers);
      expect(process.env.GH_AW_OTLP_ENDPOINTS).toBe(endpoints);
      expect(readFileSync(envFile, "utf8")).toMatch(new RegExp(`^GITHUB_AW_OTEL_TRACE_ID=${VALID_TRACE_ID}\nGITHUB_AW_OTEL_PARENT_SPAN_ID=${VALID_SPAN_ID}\nGITHUB_AW_OTEL_JOB_START_MS=\\d+\n$`));
      expect(mockCore.warning).toHaveBeenCalledWith("[otlp] ignoring OTLP OIDC token containing line breaks");
      const logs = [...mockCore.info.mock.calls.flat(), ...mockCore.warning.mock.calls.flat()].join("\n");
      expect(logs).not.toContain("synthetic-oidc-token");
      expect(logs).not.toContain("INJECTED_VAR");
    });

    it.each(["\n", "\r", "\r\n"])("does not export merged headers containing %j", async lineBreak => {
      process.env.INPUT_OTLP_OIDC_TOKEN = "synthetic-oidc-token";
      process.env.OTEL_EXPORTER_OTLP_HEADERS = `X-Tenant=acme${lineBreak}INJECTED_VAR=malicious-value`;

      await run();

      const contents = readFileSync(envFile, "utf8");
      expect(contents).not.toContain("OTEL_EXPORTER_OTLP_HEADERS=");
      expect(contents).not.toContain("INJECTED_VAR");
      expect(mockCore.warning).toHaveBeenCalledWith("[otlp] refusing to write a key or value containing line breaks");
      expect(mockCore.warning.mock.calls.flat().join("\n")).not.toContain("synthetic-oidc-token");
    });

    it("injects Authorization header and exports it to GITHUB_ENV when INPUT_OTLP_OIDC_TOKEN is set", async () => {
      const minted = "oidc" + "-" + "token" + "-" + "value";
      process.env.INPUT_OTLP_OIDC_TOKEN = minted;

      await run();

      expect(mockCore.setSecret).toHaveBeenCalledWith(minted);
      expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toContain("Authorization=Bearer ");
      expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toContain(minted);
      expect(readFileSync(envFile, "utf8")).toContain("OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer ");
    });

    it("does not override existing Authorization header", async () => {
      process.env.INPUT_OTLP_OIDC_TOKEN = "oidc" + "-second" + "-value";
      process.env.OTEL_EXPORTER_OTLP_HEADERS = "Authorization=******";

      await run();

      expect(process.env.OTEL_EXPORTER_OTLP_HEADERS).toBe("Authorization=******");
    });

    it("merges Authorization into each GH_AW_OTLP_ENDPOINTS endpoint and exports it", async () => {
      const minted = "oidc" + "-endpoint" + "-value";
      process.env.INPUT_OTLP_OIDC_TOKEN = minted;
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://otlp-a.example.com", headers: "X-Tenant=acme" }, { url: "https://otlp-b.example.com" }, { url: "https://otlp-c.example.com", headers: "Authorization=******" }]);

      await run();

      const endpoints = JSON.parse(process.env.GH_AW_OTLP_ENDPOINTS || "[]");
      expect(endpoints[0].headers).toContain("X-Tenant=acme");
      expect(endpoints[0].headers).toContain("Authorization=");
      expect(endpoints[0].headers).toContain(minted);
      expect(endpoints[1].headers).toContain("Authorization=");
      expect(endpoints[1].headers).toContain(minted);
      expect(endpoints[2].headers).toBe("Authorization=******");
      expect(readFileSync(envFile, "utf8")).toContain("GH_AW_OTLP_ENDPOINTS=");
      expect(readFileSync(envFile, "utf8")).toContain("Authorization=");
      expect(readFileSync(envFile, "utf8")).toContain(minted);
    });
  });

  describe("SETUP_START_MS propagation", () => {
    it("should pass startMs=0 when SETUP_START_MS is not set", async () => {
      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ startMs: 0 }));
    });

    it("should pass the parsed integer startMs when SETUP_START_MS is a valid number", async () => {
      const jobStartMs = Date.now() - 60_000;
      process.env.SETUP_START_MS = String(jobStartMs);

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ startMs: jobStartMs }));
    });

    it("should pass startMs=0 when SETUP_START_MS is not a number", async () => {
      process.env.SETUP_START_MS = "not-a-number";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ startMs: 0 }));
    });

    it("should pass startMs=0 when SETUP_START_MS is a partial number string like '123abc'", async () => {
      process.env.SETUP_START_MS = "123abc";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ startMs: 0 }));
    });

    it("should pass startMs=0 when SETUP_START_MS is scientific notation like '1e3'", async () => {
      process.env.SETUP_START_MS = "1e3";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ startMs: 0 }));
    });

    it("should pass startMs=0 when SETUP_START_MS is hex notation like '0x10'", async () => {
      process.env.SETUP_START_MS = "0x10";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ startMs: 0 }));
    });
  });

  describe("INPUT_TRACE_ID handling", () => {
    it("should pass traceId=undefined when INPUT_TRACE_ID is not set", async () => {
      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ traceId: undefined }));
    });

    it("should pass the trace ID as-is (already lowercase) when INPUT_TRACE_ID is lowercase", async () => {
      process.env.INPUT_TRACE_ID = "abcdef0123456789abcdef0123456789";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ traceId: "abcdef0123456789abcdef0123456789" }));
    });

    it("should normalize INPUT_TRACE_ID to lowercase", async () => {
      process.env.INPUT_TRACE_ID = "ABCDEF0123456789ABCDEF0123456789";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ traceId: "abcdef0123456789abcdef0123456789" }));
    });

    it("should log the INPUT_TRACE_ID value when set", async () => {
      process.env.INPUT_TRACE_ID = "abcdef0123456789abcdef0123456789";

      await run();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("INPUT_TRACE_ID=abcdef0123456789abcdef0123456789"));
    });

    it("should log that INPUT_TRACE_ID is not set when absent", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith("[otlp] INPUT_TRACE_ID not set, a new trace ID will be generated");
    });

    it("should accept the hyphen form INPUT_TRACE-ID as a fallback", async () => {
      process.env["INPUT_TRACE-ID"] = "abcdef0123456789abcdef0123456789";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ traceId: "abcdef0123456789abcdef0123456789" }));
    });

    it("should prefer INPUT_TRACE_ID over the hyphen form when both are set", async () => {
      process.env.INPUT_TRACE_ID = "1111111111111111111111111111111a";
      process.env["INPUT_TRACE-ID"] = "2222222222222222222222222222222b";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ traceId: "1111111111111111111111111111111a" }));
    });
  });

  describe("INPUT_PARENT_SPAN_ID handling", () => {
    it("should pass parentSpanId=undefined when INPUT_PARENT_SPAN_ID is not set", async () => {
      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ parentSpanId: undefined }));
    });

    it("should pass the parent span ID to sendJobSetupSpan when INPUT_PARENT_SPAN_ID is set", async () => {
      process.env.INPUT_PARENT_SPAN_ID = "aabbccddeeff0011";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ parentSpanId: "aabbccddeeff0011" }));
    });

    it("should normalize INPUT_PARENT_SPAN_ID to lowercase", async () => {
      process.env.INPUT_PARENT_SPAN_ID = "AABBCCDDEEFF0011";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ parentSpanId: "aabbccddeeff0011" }));
    });

    it("should log the INPUT_PARENT_SPAN_ID value when set", async () => {
      process.env.INPUT_PARENT_SPAN_ID = "aabbccddeeff0011";

      await run();

      expect(mockCore.info).toHaveBeenCalledWith("[otlp] INPUT_PARENT_SPAN_ID=aabbccddeeff0011 (will parent setup span)");
    });

    it("should accept the hyphen form INPUT_PARENT-SPAN-ID as a fallback", async () => {
      process.env["INPUT_PARENT-SPAN-ID"] = "aabbccddeeff0011";

      await run();

      expect(mockSendJobSetupSpan).toHaveBeenCalledWith(expect.objectContaining({ parentSpanId: "aabbccddeeff0011" }));
    });

    it("should set INPUT_PARENT_SPAN_ID env var from the hyphen form when only hyphen form is present", async () => {
      delete process.env.INPUT_PARENT_SPAN_ID;
      process.env["INPUT_PARENT-SPAN-ID"] = "aabbccddeeff0011";

      await run();

      expect(process.env.INPUT_PARENT_SPAN_ID).toBe("aabbccddeeff0011");
    });
  });

  describe("INPUT_JOB_NAME normalization", () => {
    it("should set INPUT_JOB_NAME env var from the hyphen form when only hyphen form is present", async () => {
      delete process.env.INPUT_JOB_NAME;
      process.env["INPUT_JOB-NAME"] = "agent";

      await run();

      expect(process.env.INPUT_JOB_NAME).toBe("agent");
    });

    it("should set INPUT_JOB_NAME env var when INPUT_JOB_NAME is already in underscore form", async () => {
      process.env.INPUT_JOB_NAME = "setup";

      await run();

      expect(process.env.INPUT_JOB_NAME).toBe("setup");
    });
  });

  describe("GITHUB_OUTPUT file writing", () => {
    it("should write trace-id to GITHUB_OUTPUT when trace ID is valid", async () => {
      await run();

      const content = readFileSync(outputFile, "utf8");
      expect(content).toContain(`trace-id=${VALID_TRACE_ID}\n`);
    });

    it("should log that trace-id was written to GITHUB_OUTPUT", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith(`[otlp] trace-id=${VALID_TRACE_ID} written to GITHUB_OUTPUT`);
    });

    it("should write parent-span-id to GITHUB_OUTPUT when parent span ID is valid", async () => {
      await run();

      const content = readFileSync(outputFile, "utf8");
      expect(content).toContain(`parent-span-id=${VALID_PARENT_SPAN_ID}\n`);
    });

    it("should not write to GITHUB_OUTPUT when GITHUB_OUTPUT is not set", async () => {
      delete process.env.GITHUB_OUTPUT;

      // Should not throw
      await expect(run()).resolves.toBeUndefined();
    });

    it("should not write trace-id to GITHUB_OUTPUT when returned trace ID is invalid", async () => {
      mockSendJobSetupSpan.mockResolvedValue({ traceId: "not-valid", spanId: VALID_SPAN_ID });

      await run();

      const content = readFileSync(outputFile, "utf8");
      expect(content).not.toContain("trace-id=");
    });
  });

  describe("GITHUB_ENV file writing", () => {
    it("should write GITHUB_AW_OTEL_TRACE_ID to GITHUB_ENV when trace ID is valid", async () => {
      await run();

      const content = readFileSync(envFile, "utf8");
      expect(content).toContain(`GITHUB_AW_OTEL_TRACE_ID=${VALID_TRACE_ID}\n`);
    });

    it("should write GITHUB_AW_OTEL_PARENT_SPAN_ID to GITHUB_ENV when span ID is valid", async () => {
      await run();

      const content = readFileSync(envFile, "utf8");
      expect(content).toContain(`GITHUB_AW_OTEL_PARENT_SPAN_ID=${VALID_SPAN_ID}\n`);
    });

    it("should write GITHUB_AW_OTEL_JOB_START_MS with a positive integer timestamp", async () => {
      await run();

      const content = readFileSync(envFile, "utf8");
      const match = content.match(/GITHUB_AW_OTEL_JOB_START_MS=(\d+)\n/);
      expect(match).not.toBeNull();
      const ts = parseInt(match?.[1] ?? "0", 10);
      expect(ts).toBeGreaterThan(0);
    });

    it("should always write GITHUB_AW_OTEL_JOB_START_MS even when trace ID is invalid", async () => {
      mockSendJobSetupSpan.mockResolvedValue({ traceId: "bad", spanId: "bad" });

      await run();

      const content = readFileSync(envFile, "utf8");
      expect(content).toContain("GITHUB_AW_OTEL_JOB_START_MS=");
    });

    it("should not write to GITHUB_ENV when GITHUB_ENV is not set", async () => {
      delete process.env.GITHUB_ENV;

      // Should not throw
      await expect(run()).resolves.toBeUndefined();
    });

    it("should not write GITHUB_AW_OTEL_TRACE_ID when returned trace ID is invalid", async () => {
      mockSendJobSetupSpan.mockResolvedValue({ traceId: "not-valid", spanId: VALID_SPAN_ID });

      await run();

      const content = readFileSync(envFile, "utf8");
      expect(content).not.toContain("GITHUB_AW_OTEL_TRACE_ID=");
    });

    it("should not write GITHUB_AW_OTEL_PARENT_SPAN_ID when returned span ID is invalid", async () => {
      mockSendJobSetupSpan.mockResolvedValue({ traceId: VALID_TRACE_ID, spanId: "not-valid" });

      await run();

      const content = readFileSync(envFile, "utf8");
      expect(content).not.toContain("GITHUB_AW_OTEL_PARENT_SPAN_ID=");
    });

    it("should log the GITHUB_AW_OTEL_TRACE_ID write", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith("[otlp] GITHUB_AW_OTEL_TRACE_ID written to GITHUB_ENV");
    });

    it("should log the GITHUB_AW_OTEL_PARENT_SPAN_ID write", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith("[otlp] GITHUB_AW_OTEL_PARENT_SPAN_ID written to GITHUB_ENV");
    });

    it("should log the GITHUB_AW_OTEL_JOB_START_MS write", async () => {
      await run();

      expect(mockCore.info).toHaveBeenCalledWith("[otlp] GITHUB_AW_OTEL_JOB_START_MS written to GITHUB_ENV");
    });
  });

  describe("error handling", () => {
    it("should propagate errors from sendJobSetupSpan to the caller", async () => {
      mockSendJobSetupSpan.mockRejectedValueOnce(new Error("OTLP connection refused"));

      await expect(run()).rejects.toThrow("OTLP connection refused");
    });
  });
});
