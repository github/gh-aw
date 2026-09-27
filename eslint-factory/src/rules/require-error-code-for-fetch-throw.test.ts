import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireErrorCodeForFetchThrowRule } from "./require-error-code-for-fetch-throw";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

describe("require-error-code-for-fetch-throw", () => {
  it("valid: throws that include a standardized code are allowed", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [
        `const { ERR_API } = require("./error_codes.cjs"); async function f() { await fetch("https://example.com"); throw new Error(\`\${ERR_API}: failed to fetch\`); }`,
        `async function f() { await fetch("https://example.com"); throw new Error("E007: failed to fetch"); }`,
      ],
      invalid: [],
    });
  });

  it("invalid: throw after bare fetch() without code is flagged even without error_codes.cjs import", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [],
      invalid: [
        {
          code: `async function f() { const response = await fetch("https://example.com"); if (!response.ok) throw new Error("Azure DevOps request could not be sent"); }`,
          errors: [{ messageId: "missingErrorCode" }],
        },
      ],
    });
  });

  it("valid: throw with no preceding fetch call in the function is not flagged", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [`function f() { throw new Error("unrelated failure"); }`],
      invalid: [],
    });
  });

  it("valid: throw before the fetch call is not flagged", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [`async function f(condition) { if (condition) throw new Error("bad input"); await fetch("https://example.com"); }`],
      invalid: [],
    });
  });

  it("valid: member-expression fetch calls (e.g. transport.fetch) are not flagged", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [`async function f(transport) { await transport.fetch("https://example.com"); throw new Error("failed"); }`],
      invalid: [],
    });
  });

  it("valid: fetch call in a sibling function does not affect an unrelated function's throw", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [`async function a() { await fetch("https://example.com"); } function b() { throw new Error("unrelated"); }`],
      invalid: [],
    });
  });
});
