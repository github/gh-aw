import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noDynamicStringReplacementRule } from "./no-dynamic-string-replacement";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("no-dynamic-string-replacement", () => {
  it("uses the correct docs URL", () => {
    expect(noDynamicStringReplacementRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-dynamic-string-replacement");
  });

  it("valid and invalid cases", () => {
    ruleTester.run("no-dynamic-string-replacement", noDynamicStringReplacementRule, {
      valid: [
        `s.replace(/x/g, "literal");`,
        "s.replace(/x/g, `static`);",
        `s.replace(/x/g, () => value);`,
        `s.replace(/x/g, function () { return value; });`,
        `function escape(m) { return m; } s.replace(/x/g, escape);`,
        `const esc = m => m; s.replace(/x/g, esc);`,
        `s.replace(pat, value);`,
        `s.replace(/x/g);`,
        `s.split(/x/g, value);`,
        `s["replace"](/x/g, value);`,
      ],
      invalid: [
        {
          code: `s.replace(/{NAME}/g, name);`,
          errors: [{ messageId: "dynamicReplacement", suggestions: [{ messageId: "useReplacerFunction", output: `s.replace(/{NAME}/g, () => name);` }] }],
        },
        {
          code: `s.replaceAll("{A}", info.text);`,
          errors: [{ messageId: "dynamicReplacement", suggestions: [{ messageId: "useReplacerFunction", output: `s.replaceAll("{A}", () => info.text);` }] }],
        },
        {
          code: "s.replace(/x/, `a ${v}`);",
          errors: [{ messageId: "dynamicReplacement", suggestions: [{ messageId: "useReplacerFunction", output: "s.replace(/x/, () => `a ${v}`);" }] }],
        },
        {
          code: `s.replace(/x/g, process.env.NAME || "d");`.replace(`process.env.NAME || "d"`, `get()`),
          errors: [{ messageId: "dynamicReplacement", suggestions: [{ messageId: "useReplacerFunction", output: `s.replace(/x/g, () => get());` }] }],
        },
      ],
    });
  });
});
