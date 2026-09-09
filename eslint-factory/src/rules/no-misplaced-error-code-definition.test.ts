import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noMisplacedErrorCodeDefinitionRule } from "./no-misplaced-error-code-definition";

const ruleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

describe("no-misplaced-error-code-definition", () => {
  it("uses the correct docs URL", () => {
    expect(noMisplacedErrorCodeDefinitionRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-misplaced-error-code-definition");
  });

  it("accepts centralized and local-only code definitions", () => {
    ruleTester.run("no-misplaced-error-code-definition", noMisplacedErrorCodeDefinitionRule, {
      valid: [
        {
          filename: "actions/setup/js/error_codes.cjs",
          code: `const POLICY_FILE_PROTECTION_DENIED_REASON_CODE = "POLICY_FILE_PROTECTION_DENIED"; module.exports = { POLICY_FILE_PROTECTION_DENIED_REASON_CODE };`,
        },
        {
          filename: "actions/setup/js/safe_output_summary.cjs",
          code: `const UNCLASSIFIED_ERROR_CODE = "UNCLASSIFIED"; function summarize() { return UNCLASSIFIED_ERROR_CODE; } module.exports = { summarize };`,
        },
        {
          filename: "actions/setup/js/helper.cjs",
          code: `const STATUS_CODE = "STATUS"; module.exports = { STATUS_CODE };`,
        },
      ],
      invalid: [],
    });
  });

  it("reports exported code constants outside error_codes.cjs", () => {
    ruleTester.run("no-misplaced-error-code-definition", noMisplacedErrorCodeDefinitionRule, {
      valid: [],
      invalid: [
        {
          filename: "actions/setup/js/manifest_file_helpers.cjs",
          code: `const POLICY_FILE_PROTECTION_DENIED_REASON_CODE = "POLICY_FILE_PROTECTION_DENIED"; module.exports = { POLICY_FILE_PROTECTION_DENIED_REASON_CODE };`,
          errors: [{ messageId: "misplacedErrorCode", data: { name: "POLICY_FILE_PROTECTION_DENIED_REASON_CODE" } }],
        },
        {
          filename: "actions/setup/js/helper.cjs",
          code: `const LOCAL_ERROR_CODE = "LOCAL"; module.exports = { code: LOCAL_ERROR_CODE };`,
          errors: [{ messageId: "misplacedErrorCode", data: { name: "LOCAL_ERROR_CODE" } }],
        },
        {
          filename: "actions/setup/js/helper.cjs",
          code: `const LOCAL_REASON_CODE = "LOCAL"; exports.LOCAL_REASON_CODE = LOCAL_REASON_CODE;`,
          errors: [{ messageId: "misplacedErrorCode", data: { name: "LOCAL_REASON_CODE" } }],
        },
        {
          filename: "actions/setup/js/helper.cjs",
          code: `module.exports.INLINE_ERROR_CODE = "INLINE";`,
          errors: [{ messageId: "misplacedErrorCode", data: { name: "INLINE_ERROR_CODE" } }],
        },
        {
          filename: "actions/setup/js/helper.cjs",
          code: `module.exports = { INLINE_ERROR_CODE: "INLINE" };`,
          errors: [{ messageId: "misplacedErrorCode", data: { name: "INLINE_ERROR_CODE" } }],
        },
      ],
    });
  });
});
