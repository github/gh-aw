import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noSingleCharStringReplaceRule } from "./no-single-char-string-replace";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("no-single-char-string-replace", () => {
  it("uses the correct docs URL", () => {
    expect(noSingleCharStringReplaceRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-single-char-string-replace");
  });

  it("valid and invalid cases", () => {
    ruleTester.run("no-single-char-string-replace", noSingleCharStringReplaceRule, {
      valid: [`s.replaceAll("_", "-");`, `s.replace(/_/g, "-");`, `s.replace("/tmp/gh-aw/", "");`, `s.replace("", "x");`, `s.replace("_");`, `s["replace"]("_", "-");`, `s.replace(sep, "-");`],
      invalid: [
        {
          code: `name.replace("_", "-");`,
          errors: [{ messageId: "singleCharReplace", suggestions: [{ messageId: "useReplaceAll", output: `name.replaceAll("_", "-");` }] }],
        },
        {
          code: `a.replace(".", "\\\\.").trim();`,
          errors: [{ messageId: "singleCharReplace", suggestions: [{ messageId: "useReplaceAll", output: `a.replaceAll(".", "\\\\.").trim();` }] }],
        },
      ],
    });
  });
});
