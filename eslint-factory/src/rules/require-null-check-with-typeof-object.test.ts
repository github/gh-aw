import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { requireNullCheckWithTypeofObjectRule } from "./require-null-check-with-typeof-object";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

const messageFor = (subject: string, operator = "===") =>
  `\`typeof ${subject} ${operator} "object"\` is also satisfied by \`null\`. Guard \`${subject}\` in the same conjunction or exit early for null before this check; otherwise a null value is treated as an object and later property access throws.`;

describe("require-null-check-with-typeof-object", () => {
  it("uses the correct docs URL", () => {
    expect(requireNullCheckWithTypeofObjectRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#require-null-check-with-typeof-object");
  });

  it("invalid: typeof object checks with no null guard are flagged", () => {
    cjsRuleTester.run("require-null-check-with-typeof-object", requireNullCheckWithTypeofObjectRule, {
      valid: [],
      invalid: [
        {
          code: `function f(headers) { if (typeof headers === "object") { return Object.values(headers); } }`,
          errors: [{ message: messageFor("headers") }],
        },
        {
          code: `const states = typeof result === "object" ? Object.values(result.claims || {}) : [];`,
          errors: [{ message: messageFor("result") }],
        },
        {
          code: `const ok = !!parsed && typeof parsed.counts === "object";`,
          errors: [{ message: messageFor("parsed.counts") }],
        },
        {
          code: `if (value !== undefined && typeof value === "object") Object.keys(value);`,
          errors: [{ message: messageFor("value") }],
        },
        {
          code: `const isObj = "object" === typeof value;`,
          errors: [{ message: messageFor("value") }],
        },
        {
          code: `function f(value) { if (value !== undefined) return; return typeof value === "object" ? Object.keys(value) : []; }`,
          errors: [{ message: messageFor("value") }],
        },
        {
          code: `function f(a, b) { if (b !== null) {} return typeof a === "object"; }`,
          errors: [{ message: messageFor("a") }],
        },
        {
          code: `function f(a) { return typeof a === "object"; } function g(a) { if (a) {} }`,
          errors: [{ message: messageFor("a") }],
        },
        {
          code: `function f(value) { function helper(value) { if (value) return; } if (typeof value === "object") return Object.keys(value); }`,
          errors: [{ message: messageFor("value") }],
        },
        {
          code: `function f(value, condition) { const result = condition && value; return typeof value === "object" ? Object.keys(value) : result; }`,
          errors: [{ message: messageFor("value") }],
        },
        {
          code: `function f(value) { if (value) log("set"); return typeof value === "object" ? Object.keys(value) : []; }`,
          errors: [{ message: messageFor("value") }],
        },
        {
          code: `if (value == null || typeof value === "object") Object.keys(value);`,
          errors: [{ message: messageFor("value") }],
        },
        {
          code: `function helper(x) { if (x) return; } const ok = typeof x === "object";`,
          errors: [{ message: messageFor("x") }],
        },
      ],
    });
  });

  it("valid: null or truthiness guards, and unrelated typeof checks, are accepted", () => {
    cjsRuleTester.run("require-null-check-with-typeof-object", requireNullCheckWithTypeofObjectRule, {
      valid: [
        { code: `const ok = value && typeof value === "object";` },
        { code: `const ok = value !== null && typeof value === "object";` },
        { code: `const ok = typeof value === "object" && value !== null;` },
        { code: `const ok = value != null && typeof value === "object";` },
        { code: `const ok = typeof value === "object" && value !== undefined && value !== null;` },
        { code: `const ok = value != undefined && typeof value === "object";` },
        { code: `const ok = value !== null && typeof value === "object" && !Array.isArray(value);` },
        { code: `const ok = Boolean(value) && typeof value === "object";` },
        { code: `const ok = !value || typeof value !== "object";` },
        { code: `const ok = value == null || typeof value !== "object";` },
        { code: `const notObj = typeof value != "object";` },
        { code: `const notObj = typeof value !== "object";` },
        { code: `const ok = result?.target && typeof result.target === "object" ? result.target : undefined;` },
        { code: `function f(headers) { if (headers == null) { return false; } if (typeof headers === "object") { return true; } return false; }` },
        { code: `function f(value) { if (value === null) return 1; return typeof value === "object" ? 2 : 3; }` },
        { code: `function f(value) { if (!value) return 1; if (typeof value === "object") return 2; return 3; }` },
        { code: `const isString = typeof value === "string";` },
        { code: `const isFn = typeof value === "function";` },
        { code: `const t = typeof value === kind;` },
        { code: `const t = typeof fn() === "object";` },
        { code: `const t = typeof value[key] === "object";` },
      ],
      invalid: [],
    });
  });
});
