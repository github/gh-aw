import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireDynamicRegexpConstructorTryCatchRule } from "./require-dynamic-regexp-constructor-try-catch";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

const esmRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "module",
  },
});

describe("require-dynamic-regexp-constructor-try-catch", () => {
  it("valid: new RegExp with a static string pattern is always safe (CommonJS)", () => {
    cjsRuleTester.run("require-dynamic-regexp-constructor-try-catch", requireDynamicRegexpConstructorTryCatchRule, {
      valid: [
        `const r = new RegExp("^abc$");`,
        `const r = new RegExp(\`^abc$\`);`,
        `const r = new RegExp("^" + "abc" + "$", "i");`,
        `const r = new RegExp("^abc$", "gi");`,
      ],
      invalid: [],
    });
  });

  it("valid: new RegExp built from a template literal with interpolated variables is not flagged (CommonJS)", () => {
    cjsRuleTester.run("require-dynamic-regexp-constructor-try-catch", requireDynamicRegexpConstructorTryCatchRule, {
      valid: [`const r = new RegExp(\`^\${escapedKey}:\\\\s*\`, "i");`, `const r = new RegExp("^" + escapedKey + "$");`],
      invalid: [],
    });
  });

  it("valid: new RegExp built from another regex's .source is not flagged (CommonJS)", () => {
    cjsRuleTester.run("require-dynamic-regexp-constructor-try-catch", requireDynamicRegexpConstructorTryCatchRule, {
      valid: [
        `const r = new RegExp(FOO_RE.source, "gi");`,
        `const r = new RegExp([A_RE.source, B_RE.source].join("|"), "i");`,
      ],
      invalid: [],
    });
  });

  it("valid: new RegExp with a dynamic pattern inside try block passes (CommonJS)", () => {
    cjsRuleTester.run("require-dynamic-regexp-constructor-try-catch", requireDynamicRegexpConstructorTryCatchRule, {
      valid: [`try { const r = new RegExp(pattern); } catch (e) {}`, `function f() { try { return new RegExp(validation.pattern); } catch (e) {} }`],
      invalid: [],
    });
  });

  it("valid: RegExp shadowed by a local binding is not the global constructor (CommonJS)", () => {
    cjsRuleTester.run("require-dynamic-regexp-constructor-try-catch", requireDynamicRegexpConstructorTryCatchRule, {
      valid: [`function parse(RegExp, value) { return new RegExp(value); }`, `const RegExp = require("./my-regexp"); const r = new RegExp(variable);`],
      invalid: [],
    });
  });

  it("invalid: new RegExp with a dynamic member expression pattern is flagged (CommonJS)", () => {
    cjsRuleTester.run("require-dynamic-regexp-constructor-try-catch", requireDynamicRegexpConstructorTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const regex = new RegExp(validation.pattern);`,
          errors: [{ messageId: "requireTryCatch" }],
        },
        {
          code: `function f() { const r = new RegExp(userInput, "i"); return r; }`,
          errors: [{ messageId: "requireTryCatch" }],
        },
      ],
    });
  });

  it("invalid: new RegExp with a dynamic pattern is flagged (ES module)", () => {
    esmRuleTester.run("require-dynamic-regexp-constructor-try-catch", requireDynamicRegexpConstructorTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const regex = new RegExp(validation.pattern);`,
          errors: [{ messageId: "requireTryCatch" }],
        },
      ],
    });
  });
});
