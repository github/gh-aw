import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";

const OUTPUT_DIR = "/tmp/gh-aw";
const OUTPUT_PATH = "/tmp/gh-aw/evals.jsonl";
const STATUS_PATH = "/tmp/gh-aw/evals_status.json";
const MCP_CONFIG_DIR = `${process.env.RUNNER_TEMP || "/tmp"}/gh-aw/mcp-config`;
const GATEWAY_OUTPUT_PATH = `${MCP_CONFIG_DIR}/gateway-output.json`;

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  setFailed: vi.fn(),
};

global.core = mockCore;

describe("redact_evals_results.cjs", () => {
  let module;

  beforeEach(async () => {
    vi.clearAllMocks();
    fs.mkdirSync(OUTPUT_DIR, { recursive: true });
    if (fs.existsSync(OUTPUT_PATH)) {
      fs.unlinkSync(OUTPUT_PATH);
    }
    delete process.env.GH_AW_SECRET_NAMES;
    delete process.env.SECRET_EVALS_SECRET;
    for (const variable of ["OTEL_EXPORTER_OTLP_HEADERS", "GH_AW_OTLP_ALL_HEADERS", "GH_AW_OTLP_ENDPOINTS"]) vi.stubEnv(variable, "");
    module = await import("./redact_evals_results.cjs");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    fs.rmSync(STATUS_PATH, { force: true });
    if (fs.existsSync(OUTPUT_PATH)) {
      fs.unlinkSync(OUTPUT_PATH);
    }
    if (fs.existsSync(GATEWAY_OUTPUT_PATH)) {
      fs.unlinkSync(GATEWAY_OUTPUT_PATH);
    }
  });

  it("collects eval secret values from the workflow env", () => {
    process.env.GH_AW_SECRET_NAMES = "EVALS_SECRET, EMPTY_SECRET";
    process.env.SECRET_EVALS_SECRET = "secret-value";
    process.env.SECRET_EMPTY_SECRET = "   ";

    expect(module.getSecretValues()).toContain("secret-value");
    expect(module.getSecretValues()).not.toContain("");
  });

  it("collects MCP gateway tokens from the canonical config path", () => {
    const tokenPart = "gateway-token-abc123";
    const bearerToken = ["Bearer", tokenPart].join(" ");
    fs.mkdirSync(MCP_CONFIG_DIR, { recursive: true });
    fs.writeFileSync(
      GATEWAY_OUTPUT_PATH,
      JSON.stringify({
        mcpServers: {
          github: {
            headers: { Authorization: bearerToken },
          },
        },
      }),
      "utf8"
    );

    expect(module.getSecretValues()).toContain(bearerToken);
    expect(module.getSecretValues()).toContain(tokenPart);
  });

  it("redacts eval output and leaves no lingering secrets", async () => {
    process.env.GH_AW_SECRET_NAMES = "EVALS_SECRET";
    process.env.SECRET_EVALS_SECRET = "super-secret-value";
    fs.writeFileSync(OUTPUT_PATH, `{"question":"Contains super-secret-value"}\n`, "utf8");

    await module.main();

    expect(fs.readFileSync(OUTPUT_PATH, "utf8")).toContain("***REDACTED***");
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("redacts and verifies the not-evaluated marker", async () => {
    process.env.GH_AW_SECRET_NAMES = "EVALS_SECRET";
    process.env.SECRET_EVALS_SECRET = "super-secret-value";
    const marker = { status: "not_evaluated", reason_code: "judge_failed", reason: "Contains super-secret-value" };
    fs.writeFileSync(STATUS_PATH, JSON.stringify(marker) + "\n", "utf8");

    module.verifyRedaction();
    expect(mockCore.setFailed).toHaveBeenCalledWith(`Secret redaction verification failed for ${STATUS_PATH}: unredacted values remain`);

    mockCore.setFailed.mockClear();
    await module.main();
    expect(fs.readFileSync(STATUS_PATH, "utf8")).not.toContain("super-secret-value");
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("fails closed when verification still detects secrets", () => {
    process.env.GH_AW_SECRET_NAMES = "EVALS_SECRET";
    process.env.SECRET_EVALS_SECRET = "super-secret-value";
    fs.writeFileSync(OUTPUT_PATH, `{"question":"Contains super-secret-value"}\n`, "utf8");

    module.verifyRedaction();

    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Secret redaction verification failed"));
  });

  it("detects decoded and JSON-escaped OTLP credentials, then verifies the sanitized eval output", async () => {
    const password = 'synthetic-otlp-"password\\tail';
    const encoded = Buffer.from(`123456:${password}`).toString("base64");
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", JSON.stringify([{ url: "https://traces.example.com", headers: `Authorization=Basic ${encoded}` }]));
    fs.writeFileSync(OUTPUT_PATH, JSON.stringify({ question: password }) + "\n", "utf8");

    module.verifyRedaction();

    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Secret redaction verification failed"));
    mockCore.setFailed.mockClear();
    await module.main();
    expect(JSON.parse(fs.readFileSync(OUTPUT_PATH, "utf8")).question).toBe("***");
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("reports multiple lingering OTLP credentials without claiming an inaccurate count", async () => {
    const passwords = ["synthetic-otlp-password-one", "synthetic-otlp-password-two", "12345678", "9007199254740993"];
    const endpoints = passwords.map(password => ({
      url: "https://traces.example.com",
      headers: `Authorization=Basic ${Buffer.from(`123456:${password}`).toString("base64")}`,
    }));
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", JSON.stringify(endpoints));
    fs.writeFileSync(OUTPUT_PATH, JSON.stringify({ questions: passwords.slice(0, 2) }) + '\n12345678\n{"nested":{"ids":[12345678,{"token":9007199254740993},42]}}\n', "utf8");

    module.verifyRedaction();

    expect(mockCore.setFailed).toHaveBeenCalledExactlyOnceWith(`Secret redaction verification failed for ${OUTPUT_PATH}: unredacted values remain`);
    mockCore.setFailed.mockClear();
    await module.main();
    const content = fs.readFileSync(OUTPUT_PATH, "utf8");
    for (const password of passwords) expect(content).not.toContain(password);
    expect(JSON.parse(content.trim().split("\n")[1])).toBe("***");
    expect(JSON.parse(content.trim().split("\n")[2])).toEqual({ nested: { ids: ["***", { token: "***" }, 42] } });
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it.each(["12345678", "9007199254740993"])("detects a lone nested numeric OTLP credential %s and verifies it after redaction", async password => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_HEADERS", `Authorization=Basic ${Buffer.from(`123456:${password}`).toString("base64")}`);
    fs.writeFileSync(OUTPUT_PATH, `{"nested":{"ids":[${password},42]}}\n`, "utf8");

    module.verifyRedaction();

    expect(mockCore.setFailed).toHaveBeenCalledExactlyOnceWith(`Secret redaction verification failed for ${OUTPUT_PATH}: unredacted values remain`);
    mockCore.setFailed.mockClear();
    await module.main();
    expect(JSON.parse(fs.readFileSync(OUTPUT_PATH, "utf8"))).toEqual({ nested: { ids: ["***", 42] } });
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });
});
