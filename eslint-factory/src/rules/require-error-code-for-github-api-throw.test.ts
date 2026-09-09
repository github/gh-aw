import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireErrorCodeForGithubApiThrowRule } from "./require-error-code-for-github-api-throw";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

describe("require-error-code-for-github-api-throw", () => {
  it("valid: files without error_codes.cjs import are ignored", () => {
    cjsRuleTester.run("require-error-code-for-github-api-throw", requireErrorCodeForGithubApiThrowRule, {
      valid: [`async function f(githubClient) { await githubClient.rest.pulls.get({}); throw new Error("failed"); }`],
      invalid: [],
    });
  });

  it("valid: throws that include standardized code are allowed", () => {
    cjsRuleTester.run("require-error-code-for-github-api-throw", requireErrorCodeForGithubApiThrowRule, {
      valid: [
        `const { ERR_API, SAFE_OUTPUT_E099 } = require("./error_codes.cjs"); async function f(githubClient) { await githubClient.rest.pulls.get({}); throw new Error(\`\${ERR_API}: failed\`); }`,
        `const { SAFE_OUTPUT_E099 } = require("./error_codes.cjs"); async function f(githubClient) { await githubClient.graphql("query {}"); const msg = \`\${SAFE_OUTPUT_E099}: failed\`; throw new Error(msg); }`,
      ],
      invalid: [],
    });
  });

  it("invalid: throw after githubClient.rest call without code is flagged", () => {
    cjsRuleTester.run("require-error-code-for-github-api-throw", requireErrorCodeForGithubApiThrowRule, {
      valid: [],
      invalid: [
        {
          code: `const { ERR_API } = require("./error_codes.cjs"); async function f(githubClient) { await githubClient.rest.pulls.get({}); throw new Error("failed to fetch pull request"); }`,
          errors: [{ messageId: "missingErrorCode" }],
        },
      ],
    });
  });

  it("invalid: throw after retry-wrapped githubClient.rest call without code is flagged", () => {
    cjsRuleTester.run("require-error-code-for-github-api-throw", requireErrorCodeForGithubApiThrowRule, {
      valid: [],
      invalid: [
        {
          code: `const { ERR_API } = require("./error_codes.cjs"); async function withRetry(fn) { return fn(); } const retryConfig = {}; async function f(githubClient) { try { await withRetry(() => githubClient.rest.issues.create({}), retryConfig, "create issue"); } catch (error) { throw new Error("failed to create issue"); } }`,
          errors: [{ messageId: "missingErrorCode" }],
        },
      ],
    });
  });

  it("valid: unrelated throw after non-awaited callback with github api call is not flagged", () => {
    cjsRuleTester.run("require-error-code-for-github-api-throw", requireErrorCodeForGithubApiThrowRule, {
      valid: [`const { ERR_API } = require("./error_codes.cjs"); async function f(githubClient) { try { setTimeout(() => githubClient.rest.issues.get({}), 0); } catch (error) {} throw new Error("unrelated"); }`],
      invalid: [],
    });
  });

  it("invalid: throw after retry-wrapped githubClient.rest call inside a conditional rethrow is flagged", () => {
    cjsRuleTester.run("require-error-code-for-github-api-throw", requireErrorCodeForGithubApiThrowRule, {
      valid: [],
      invalid: [
        {
          code: `const { ERR_API } = require("./error_codes.cjs"); async function withRetry(fn) { return fn(); } const retryConfig = {}; async function f(githubClient, shouldRethrow) { try { await withRetry(() => githubClient.rest.issues.create({}), retryConfig, "create issue"); } catch (error) { if (shouldRethrow) { throw error; } } throw new Error("failed to create issue"); }`,
          errors: [{ messageId: "missingErrorCode" }],
        },
      ],
    });
  });

  it("valid: throw after outer catch is not flagged when inner catch swallows the retry-wrapped call", () => {
    cjsRuleTester.run("require-error-code-for-github-api-throw", requireErrorCodeForGithubApiThrowRule, {
      valid: [
        `const { ERR_API } = require("./error_codes.cjs"); async function withRetry(fn) { return fn(); } const retryConfig = {}; async function outer(githubClient) { try { try { await withRetry(() => githubClient.rest.issues.create({}), retryConfig, "create issue"); } catch (innerError) { console.warn(innerError); } } catch (outerError) { throw new Error("outer catch unrelated to api call"); } }`,
      ],
      invalid: [],
    });
  });

  it("valid: throw before github api call in same function is not flagged", () => {
    cjsRuleTester.run("require-error-code-for-github-api-throw", requireErrorCodeForGithubApiThrowRule, {
      valid: [`const { ERR_API } = require("./error_codes.cjs"); async function f(githubClient) { if (!githubClient) throw new Error("missing client"); await githubClient.paginate("GET /repos/{owner}/{repo}/pulls"); }`],
      invalid: [],
    });
  });

  it("valid: github api call in another function is not considered", () => {
    cjsRuleTester.run("require-error-code-for-github-api-throw", requireErrorCodeForGithubApiThrowRule, {
      valid: [
        `const { ERR_API } = require("./error_codes.cjs"); async function fetchPulls(githubClient) { await githubClient.rest.pulls.get({}); } function fail() { throw new Error("failed without api call in this function"); }`,
        `const { ERR_API } = require("./error_codes.cjs"); async function withRetry(fn) { return fn(); } const retryConfig = {}; async function fetchPulls(githubClient) { try { await withRetry(() => githubClient.rest.pulls.get({}), retryConfig, "fetch pulls"); } catch (error) { return null; } } function fail() { throw new Error("failed without api call in this function"); }`,
        `const { ERR_API } = require("./error_codes.cjs"); async function withRetry(fn) { return fn(); } const retryConfig = {}; async function outer(githubClient) { async function fetchPulls() { try { await withRetry(() => githubClient.rest.pulls.get({}), retryConfig, "fetch pulls"); } catch (error) { return null; } } await fetchPulls(); throw new Error("outer failure unrelated to api call"); }`,
      ],
      invalid: [],
    });
  });
});
