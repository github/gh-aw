import { afterEach, describe, expect, it, vi } from "vitest";

const { getProviderErrorDetails, sanitizeProviderErrorMessage } = await import("./pi_provider_error.cjs");

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("Pi provider error helpers", () => {
  it("redacts configured provider secrets", () => {
    vi.stubEnv("PI_PROVIDER_TOKEN", "provider-secret-value");
    const message = sanitizeProviderErrorMessage("Authorization: provider-secret-value");
    expect(message).toContain("[REDACTED]");
    expect(message).not.toContain("provider-secret-value");
  });

  it("removes control characters and bounds long messages", () => {
    expect(sanitizeProviderErrorMessage("line one\nline two")).toBe("line one line two");
    expect(sanitizeProviderErrorMessage("x".repeat(2000))).toHaveLength(1001);
  });

  it("parses an HTTP status prefix when no response status is available", () => {
    expect(getProviderErrorDetails("429: rate limited", undefined)).toEqual({
      status: 429,
      message: "rate limited",
    });
  });
});
