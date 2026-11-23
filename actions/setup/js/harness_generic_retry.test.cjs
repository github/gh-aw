// @ts-check

"use strict";

const { GENERIC_HARNESS_RETRY_SIGNAL_TAIL_CHARS, classifyGenericHarnessFailure } = require("./harness_generic_retry.cjs");

describe("classifyGenericHarnessFailure", () => {
  it.each([
    ["HTTP/1.1 429 Too Many Requests\nRetry-After: 257", "rate_limit_error"],
    ["Retry-After: 257", null],
    ['{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}', "rate_limit_error"],
    ["Error: rate limit exceeded for model", "rate_limit_error"],
    ["502 Bad Gateway", "transient_error"],
    ["request failed: socket hang up", "transient_error"],
    ['{"error":{"status":"UNAVAILABLE"}}', "transient_error"],
    ["Authentication failed (Request ID: abc) 429 Too Many Requests", "non_retryable"],
    ["Authentication failed: HTTP 403\n<!DOCTYPE html>\n<title>Forbidden</title>", "transient_error"],
    ["Authentication failed\n<!DOCTYPE html>", "non_retryable"],
    ["Authentication failed\n<!doctype html><html>Login</html>", "non_retryable"],
    ["Authentication failed: HTTP 403\nContent-Type: text/html\n\ncf-chl-token", "transient_error"],
    ["API Error: 403 Forbidden: /cdn-cgi/challenge-platform", "transient_error"],
    ["API Error: 403 Forbidden: cf-chl-token", "transient_error"],
    ['{"error":{"status":403,"message":"<html>Forbidden</html>"}}', "transient_error"],
    ["HTTP 403 Forbidden", null],
    ["Authentication failed: API Error: Just a moment...\nAuthentication failed (Request ID: abc)", "non_retryable"],
    [JSON.stringify({ type: "assistant", message: "API Error: Just a moment..." }), null],
    [JSON.stringify({ type: "result", is_error: false, result: "API Error: <html>Sample</html>" }), null],
    ["src/server.ts line 429: unexpected token", null],
    ["tests failed: rate limiter spec", null],
    ["", null],
  ])("classifies %j as %s", (output, expected) => {
    expect(classifyGenericHarnessFailure(output)).toBe(expected);
  });

  it("ignores retryable signals before the output tail", () => {
    const filler = "x".repeat(GENERIC_HARNESS_RETRY_SIGNAL_TAIL_CHARS);
    expect(classifyGenericHarnessFailure(`429 Too Many Requests\nRetry-After: 257\n${filler}`)).toBeNull();
    expect(classifyGenericHarnessFailure(`${filler}\n429 Too Many Requests\nRetry-After: 257`)).toBe("rate_limit_error");
    expect(classifyGenericHarnessFailure(`API Error: <html>\n${filler}`)).toBeNull();
  });

  it("checks non-retryable guards across the whole output", () => {
    const filler = "x".repeat(GENERIC_HARNESS_RETRY_SIGNAL_TAIL_CHARS);
    expect(classifyGenericHarnessFailure(`Authentication failed (Request ID: abc)\n${filler}\n429 Too Many Requests`)).toBe("non_retryable");
  });

  it.each([
    body => `Authentication failed: HTTP 403\nContent-Type: text/html\n\n${body}`,
    body => `Authentication failed: HTTP 403\n${body.replace(/\n/g, "")}`,
    body => JSON.stringify({ event: "result", result: { status: "ERROR", error: `Authentication failed: HTTP 403\n${body}` } }),
  ])("retains terminal HTML response attribution when its body exceeds the retry-signal tail", envelope => {
    const body = "<!DOCTYPE html><html>\n" + "challenge body ".repeat(GENERIC_HARNESS_RETRY_SIGNAL_TAIL_CHARS) + "\n</html>";
    const output = envelope(body);
    expect(classifyGenericHarnessFailure(output)).toBe("transient_error");
    expect(classifyGenericHarnessFailure(output + "\nAuthentication failed (Request ID: abc)")).toBe("non_retryable");
    expect(classifyGenericHarnessFailure(output + "\nunrelated transcript ".repeat(GENERIC_HARNESS_RETRY_SIGNAL_TAIL_CHARS))).toBeNull();
  });
});
