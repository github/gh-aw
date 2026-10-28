import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noSliceToUncheckedIndexOfRule } from "./no-slice-to-unchecked-indexof";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("no-slice-to-unchecked-indexof", () => {
  it("uses the correct docs URL", () => {
    expect(noSliceToUncheckedIndexOfRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-slice-to-unchecked-indexof");
  });

  it("validates slice/indexOf usage", () => {
    ruleTester.run("no-slice-to-unchecked-indexof", noSliceToUncheckedIndexOfRule, {
      valid: [
        `const i = s.indexOf("\\t"); const a = i === -1 ? s : s.slice(0, i);`,
        `args.slice(0, args.indexOf("--") === -1 ? args.length : args.indexOf("--"));`,
        `a.slice(0, b.indexOf("x"));`,
        `s.slice(0, s.indexOf("x") + 1);`,
        `s.slice(1);`,
      ],
      invalid: [
        { code: `entry.slice(0, entry.indexOf("\\t"));`, errors: [{ messageId: "uncheckedIndexOf" }] },
        { code: `line.slice(start, line.lastIndexOf("="));`, errors: [{ messageId: "uncheckedIndexOf" }] },
        { code: `this.buf.slice(0, this.buf.indexOf("\\n"));`, errors: [{ messageId: "uncheckedIndexOf" }] },
      ],
    });
  });
});
