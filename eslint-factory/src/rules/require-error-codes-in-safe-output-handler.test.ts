import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireErrorCodesInSafeOutputHandlerRule } from "./require-error-codes-in-safe-output-handler";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

describe("require-error-codes-in-safe-output-handler", () => {
  it("valid: files without a safe-output/octokit/NDJSON marker are not flagged", () => {
    cjsRuleTester.run("require-error-codes-in-safe-output-handler", requireErrorCodesInSafeOutputHandlerRule, {
      valid: [
        `throw new Error("one"); throw new Error("two"); throw new Error("three");`,
        `function f() { core.setFailed("a"); core.setFailed("b"); core.setFailed("c"); }`,
      ],
      invalid: [],
    });
  });

  it("valid: safe-output handlers already using error codes anywhere are not flagged", () => {
    cjsRuleTester.run("require-error-codes-in-safe-output-handler", requireErrorCodesInSafeOutputHandlerRule, {
      valid: [
        `const { ERR_NOT_FOUND } = require("./error_codes.cjs");
         async function run() { await octokit.rest.issues.get({}); throw new Error("one"); throw new Error("two"); throw new Error("three"); }`,
        `async function run() { await octokit.rest.issues.get({}); throw new Error("E004: one"); throw new Error("two"); throw new Error("three"); }`,
        `function writeSafeOutput() { safeOutput.append("x"); throw new Error("one"); throw new Error("two"); throw new Error("ERROR_BAD: three"); }`,
      ],
      invalid: [],
    });
  });

  it("valid: safe-output handlers with fewer than 3 uncoded failures are not flagged", () => {
    cjsRuleTester.run("require-error-codes-in-safe-output-handler", requireErrorCodesInSafeOutputHandlerRule, {
      valid: [
        `async function run() { await octokit.rest.issues.get({}); throw new Error("one"); throw new Error("two"); }`,
        `function run() { safeOutput.write("x"); core.setFailed("a"); }`,
      ],
      invalid: [],
    });
  });

  it("invalid: octokit-based handlers with 3+ uncoded throws and no error codes are flagged", () => {
    cjsRuleTester.run("require-error-codes-in-safe-output-handler", requireErrorCodesInSafeOutputHandlerRule, {
      valid: [],
      invalid: [
        {
          code: `async function run() {
             await octokit.rest.issues.get({});
             if (!a) throw new Error("missing a");
             if (!b) throw new Error("missing b");
             if (!c) throw new Error("missing c");
           }`,
          errors: [{ messageId: "missingErrorCodes" }],
        },
      ],
    });
  });

  it("invalid: NDJSON-based handlers mixing throw and core.setFailed without codes are flagged", () => {
    cjsRuleTester.run("require-error-codes-in-safe-output-handler", requireErrorCodesInSafeOutputHandlerRule, {
      valid: [],
      invalid: [
        {
          code: `function run() {
             writeNDJSON(entry);
             if (!a) throw new Error("missing a");
             if (!b) throw new Error("missing b");
             if (!c) core.setFailed("missing c");
           }`,
          errors: [{ messageId: "missingErrorCodes" }],
        },
      ],
    });
  });

  it("invalid: safe_output-based handlers are flagged", () => {
    cjsRuleTester.run("require-error-codes-in-safe-output-handler", requireErrorCodesInSafeOutputHandlerRule, {
      valid: [],
      invalid: [
        {
          code: `function run() {
             fs.appendFileSync(safeOutputPath, entry);
             throw new Error("missing a");
             throw new Error("missing b");
             throw new Error("missing c");
           }`,
          errors: [{ messageId: "missingErrorCodes" }],
        },
      ],
    });
  });
});
