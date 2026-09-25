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
  it("invalid: throw after fetch() call without a code is flagged even without error_codes.cjs", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [],
      invalid: [
        {
          code: `async function adoRequest(url) { let response; try { response = await fetch(url); } catch (error) { throw new Error("Azure DevOps request could not be sent", { cause: error }); } if (!response.ok) { throw new Error(\`Azure DevOps request failed with HTTP \${response.status}\`); } }`,
          errors: [{ messageId: "missingErrorCode" }, { messageId: "missingErrorCode" }],
        },
      ],
    });
  });

  it("valid: throws that already include a standardized code are allowed", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [
        `const { ERR_API } = require("./error_codes.cjs"); async function f() { const response = await fetch("https://example.com"); if (!response.ok) { throw new Error(\`\${ERR_API}: failed\`); } }`,
        `async function f() { const response = await fetch("https://example.com"); if (!response.ok) { throw new Error("ERR_API: failed"); } }`,
      ],
      invalid: [],
    });
  });

  it("valid: throw with no preceding fetch() call in the same function is not flagged", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [`function validateInput(value) { if (!value) throw new Error("value is required"); }`],
      invalid: [],
    });
  });

  it("valid: throw before the fetch() call in the same function is not flagged", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [`async function f(url) { if (!url) throw new Error("url is required"); await fetch(url); }`],
      invalid: [],
    });
  });

  it("valid: member-expression fetch calls (for example octokit.request or transport.fetch) are not flagged", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [`async function f(transport, url) { const response = await transport.fetch(url); if (!response.ok) { throw new Error("failed"); } }`],
      invalid: [],
    });
  });

  it("valid: fetch() call in another function is not considered", () => {
    cjsRuleTester.run("require-error-code-for-fetch-throw", requireErrorCodeForFetchThrowRule, {
      valid: [`async function request(url) { return fetch(url); } function fail() { throw new Error("failed without fetch in this function"); }`],
      invalid: [],
    });
  });
});
