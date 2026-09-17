import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { requireSpawnSyncErrorCheckRule } from "./require-spawnsync-error-check";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

describe("require-spawnsync-error-check", () => {
  it("uses the correct docs URL", () => {
    expect(requireSpawnSyncErrorCheckRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#require-spawnsync-error-check");
  });

  it("valid: result.error is checked alongside result.status", () => {
    cjsRuleTester.run("require-spawnsync-error-check", requireSpawnSyncErrorCheckRule, {
      valid: [
        // bare spawnSync, checks result.error
        `const result = spawnSync("git", ["status"]); if (result.error) throw result.error; if (result.status !== 0) throw new Error("failed");`,
        // namespaced childProcess.spawnSync, checks result.error
        `const childProcess = require("child_process"); const result = childProcess.spawnSync("git", ["status"]); if (result.error) throw result.error;`,
        // child_process.spawnSync, checks result.error
        `const child_process = require("child_process"); const result = child_process.spawnSync("curl", ["-v"]); if (result.error) { throw result.error; } if (result.status !== 0) throw new Error("x");`,
        // arbitrary namespace alias (e.g. const cp = require("child_process")) resolved via scope binding, checks result.error
        `const cp = require("child_process"); const result = cp.spawnSync("git", ["status"]); if (result.error) throw result.error;`,
        // logging is fine when there is also a guard
        `const result = spawnSync("git", ["status"]); if (result.error) throw result.error; core.info(String(result.error));`,
        // logging before a later guard is still valid
        `const result = spawnSync("git", ["status"]); core.debug(String(result.error)); if (result.error) throw result.error;`,
        // destructured binding includes error and guards on it
        `const { status, error } = spawnSync("zip", ["-v"]); if (error) throw error; if (status !== 0) throw new Error("x");`,
        // renamed destructuring bindings are supported
        `const { error: spawnError } = spawnSync("zip", ["-v"]); if (spawnError !== undefined) throw spawnError;`,
        // string-literal keys are supported when they bind error
        `const { "error": spawnError } = spawnSync("zip", ["-v"]); if (spawnError) throw spawnError;`,
        // comparison-based guards should count
        `const result = spawnSync("git", ["status"]); if (result.error !== undefined) throw result.error;`,
        // result.error on the right side of || can still guard when the full expression is the test
        `const result = spawnSync("git", ["status"]); if (result.status !== 0 || result.error) throw result.error;`,
        // single-assignment alias: result.error stored in a const, then guarded
        `const result = spawnSync("git", ["status"]); const e = result.error; if (e) throw e;`,
        // single-assignment alias with destructuring: error binding aliased, then guarded
        `const { status, error } = spawnSync("zip", ["-v"]); const e = error; if (e) throw e;`,
      ],
      invalid: [],
    });
  });

  it("invalid: only result.status checked, result.error never read", () => {
    cjsRuleTester.run("require-spawnsync-error-check", requireSpawnSyncErrorCheckRule, {
      valid: [],
      invalid: [
        {
          code: `const result = spawnSync("git", ["status"]); if (result.status !== 0) throw new Error("failed");`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          code: `const childProcess = require("child_process"); const result = childProcess.spawnSync("zip", ["-v"], { stdio: "ignore" }); if (result.status !== 0) throw new Error("zip not found");`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          code: `const child_process = require("child_process"); const result = child_process.spawnSync("curl", ["--version"]); return result.stdout;`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          // the common `cp` alias (const cp = require("child_process")) must be resolved via scope binding, not name allowlist
          code: `const cp = require("child_process"); const result = cp.spawnSync("zip", ["-v"]); if (result.status !== 0) throw new Error("zip not found");`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          code: `const result = spawnSync("git", ["status"]); core.info(String(result.error)); if (result.status !== 0) throw new Error("failed");`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          code: `const { status } = spawnSync("git", ["status"]); if (status !== 0) throw new Error("failed");`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          code: `const { status, error } = spawnSync("git", ["status"]); core.info(String(error)); if (status !== 0) throw new Error("failed");`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          code: `const result = spawnSync("git", ["status"]); const cached = result.error && result.error.message; return cached;`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          code: `const result = spawnSync("git", ["status"]); if (result.status !== 0 && result.error) core.info(String(result.error));`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          code: `const result = spawnSync("git", ["status"]); const fallback = result.error || new Error("fallback"); throw fallback;`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          code: `const result = spawnSync("git", ["status"]); const maybeError = result.error ?? null; return maybeError;`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          // alias is assigned but only used in a non-guarding position — not a valid error check
          code: `const result = spawnSync("git", ["status"]); const e = result.error; core.info(String(e));`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
        {
          // mutable alias: error is stored then overwritten before the guard — not a valid error check
          code: `const result = spawnSync("git", ["status"]); let e = result.error; e = undefined; if (e) throw e;`,
          errors: [{ messageId: "missingErrorCheck" }],
        },
      ],
    });
  });

  it("valid: non-spawnSync call is ignored", () => {
    cjsRuleTester.run("require-spawnsync-error-check", requireSpawnSyncErrorCheckRule, {
      valid: [
        `const result = execSync("git status"); if (!result) throw new Error("failed");`,
        `const result = spawnSync; result.toString();`,
        // an identifier merely named "childProcess" that is not bound to require("child_process")
        // must not be treated as the child_process module namespace
        `const childProcess = getFakeApi(); const result = childProcess.spawnSync("git", ["status"]); if (result.status !== 0) throw new Error("failed");`,
      ],
      invalid: [],
    });
  });
});
