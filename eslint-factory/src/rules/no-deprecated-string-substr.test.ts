import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noDeprecatedStringSubstrRule } from "./no-deprecated-string-substr";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("no-deprecated-string-substr", () => {
  it("uses the correct docs URL", () => {
    expect(noDeprecatedStringSubstrRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-deprecated-string-substr");
  });

  it("valid and invalid cases", () => {
    ruleTester.run("no-deprecated-string-substr", noDeprecatedStringSubstrRule, {
      valid: [`s.slice(1, 3);`, `s.substring(1, 3);`, `s.substr();`, `s.substr(1, 2, 3);`, `substr(1, 2);`, `s.substring(1);`],
      invalid: [
        { code: `s.substr(1, 3);`, errors: [{ messageId: "noSubstr" }] },
        { code: `s.substr(2);`, errors: [{ messageId: "noSubstr" }] },
        { code: `s["substr"](0, n);`, errors: [{ messageId: "noSubstr" }] },
        { code: `(a + b).substr(-1);`, errors: [{ messageId: "noSubstr" }] },
      ],
    });
  });
});
