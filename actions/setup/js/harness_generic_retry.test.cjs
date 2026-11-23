// @ts-check

"use strict";

const { classifyGenericHarnessFailure } = require("./harness_generic_retry.cjs");

describe("classifyGenericHarnessFailure", () => {
  it.each([
    ["HTTP/1.1 429 Too Many Requests\nRetry-After: 257", "rate_limit_error"],
    ['{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}', "rate_limit_error"],
    ["Error: rate limit exceeded for model", "rate_limit_error"],
    ["502 Bad Gateway", "transient_error"],
    ["request failed: socket hang up", "transient_error"],
    ['{"error":{"status":"UNAVAILABLE"}}', "transient_error"],
    ["Authentication failed (Request ID: abc) 429 Too Many Requests", "non_retryable"],
    ["src/server.ts line 429: unexpected token", null],
    ["tests failed: rate limiter spec", null],
    ["", null],
  ])("classifies %j as %s", (output, expected) => {
    expect(classifyGenericHarnessFailure(output)).toBe(expected);
  });
});
