import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noMathMinMaxArraySpreadRule } from "./no-math-minmax-array-spread";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

describe("no-math-minmax-array-spread", () => {
  it("uses the correct docs URL", () => {
    expect(noMathMinMaxArraySpreadRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-math-minmax-array-spread");
  });

  it("valid: fixed-argument Math.min / Math.max calls are accepted", () => {
    cjsRuleTester.run("no-math-minmax-array-spread", noMathMinMaxArraySpreadRule, {
      valid: [`const largest = Math.max(a, b);`, `const smallest = Math.min(0, count);`, `const clamped = Math.min(Math.max(value, 0), 100);`],
      invalid: [],
    });
  });

  it("invalid: mixed spread forms with fixed arguments are flagged", () => {
    cjsRuleTester.run("no-math-minmax-array-spread", noMathMinMaxArraySpreadRule, {
      valid: [],
      invalid: [
        {
          code: `const largest = Math.max(0, ...values);`,
          errors: [
            {
              message:
                "Avoid Math.max(0, ...values) — spreading an array of unknown size can throw `RangeError: Maximum call stack size exceeded`. Use `values.reduce((a, b) => Math.max(a, b), 0)` instead — the `0` initializer preserves the same result as `Math.max(0, ...values)` on an empty array.",
            },
          ],
        },
        {
          code: `const smallest = Math.min(a, b, ...values);`,
          errors: [
            {
              message:
                "Avoid Math.min(a, b, ...values) — spreading an array of unknown size can throw `RangeError: Maximum call stack size exceeded`. Use `values.reduce((a, b) => Math.min(a, b), Math.min(a, b))` instead — the `Math.min(a, b)` initializer preserves the same result as `Math.min(a, b, ...values)` on an empty array.",
            },
          ],
        },
      ],
    });
  });

  it("valid: spreading an inline array literal is statically bounded, even with fixed arguments", () => {
    cjsRuleTester.run("no-math-minmax-array-spread", noMathMinMaxArraySpreadRule, {
      valid: [`const largest = Math.max(...[1, 2, 3]);`, `const smallest = Math.min(...[a, b]);`, `const clamped = Math.max(...[1, 2, 3], 0);`],
      invalid: [],
    });
  });

  it("valid: const arrays bounded by literals, filter, or slice are accepted", () => {
    cjsRuleTester.run("no-math-minmax-array-spread", noMathMinMaxArraySpreadRule, {
      valid: [
        `const times = [reset, retryAt].filter(value => Number.isFinite(value)); Math.max(...times);`,
        `const times = [a, b]; Math.min(...times);`,
        `const times = [a, b, c].slice(1).filter(Boolean); Math.max(...times);`,
        `const times = [a, b]; if (times.length) Math.max(...times);`,
      ],
      invalid: [],
    });
  });

  it("invalid: unknown or mutable array bindings remain unbounded", () => {
    cjsRuleTester.run("no-math-minmax-array-spread", noMathMinMaxArraySpreadRule, {
      valid: [],
      invalid: [
        { code: `const times = runs.map(run => run.duration); Math.max(...times);`, errors: [{ messageId: "noMathMinMaxArraySpread" }] },
        { code: `let times = [a, b]; times = runs; Math.max(...times);`, errors: [{ messageId: "noMathMinMaxArraySpread" }] },
        { code: `const values = [a, b]; values.push(...runtimeValues); Math.max(...values);`, errors: [{ messageId: "noMathMinMaxArraySpread" }] },
        { code: `const values = [a, b]; values.length = count; Math.max(...values);`, errors: [{ messageId: "noMathMinMaxArraySpread" }] },
        { code: `const values = [a, b]; consume(values); Math.max(...values);`, errors: [{ messageId: "noMathMinMaxArraySpread" }] },
        { code: `const times = [...runs, a]; Math.min(...times);`, errors: [{ messageId: "noMathMinMaxArraySpread" }] },
        { code: `function f(times) { return Math.min(...times); }`, errors: [{ messageId: "noMathMinMaxArraySpread" }] },
      ],
    });
  });

  it("valid: a shadowed Math binding is ignored", () => {
    cjsRuleTester.run("no-math-minmax-array-spread", noMathMinMaxArraySpreadRule, {
      valid: [
        `const Math = { max: values => values[0] };
const largest = Math.max(...values);`,
      ],
      invalid: [],
    });
  });

  it("valid: reduce-based alternatives and unrelated spread calls are accepted", () => {
    cjsRuleTester.run("no-math-minmax-array-spread", noMathMinMaxArraySpreadRule, {
      valid: [`const largest = values.reduce((a, b) => Math.max(a, b), -Infinity);`, `const joined = fn(...values);`, `const rounded = Math.round(...values);`],
      invalid: [],
    });
  });

  it("invalid: spreading an identifier into Math.min / Math.max is flagged", () => {
    cjsRuleTester.run("no-math-minmax-array-spread", noMathMinMaxArraySpreadRule, {
      valid: [],
      invalid: [
        {
          code: `const smallest = Math.min(...values);`,
          errors: [{ messageId: "noMathMinMaxArraySpread" }],
        },
        {
          code: `const largest = Math.max(...values);`,
          errors: [{ messageId: "noMathMinMaxArraySpread" }],
        },
      ],
    });
  });

  it("invalid: spreading a member expression or call expression is flagged", () => {
    cjsRuleTester.run("no-math-minmax-array-spread", noMathMinMaxArraySpreadRule, {
      valid: [],
      invalid: [
        {
          code: `const largest = Math.max(...stats.durations);`,
          errors: [{ messageId: "noMathMinMaxArraySpread" }],
        },
        {
          code: `const smallest = Math.min(...runs.map(run => run.duration).filter(Boolean));`,
          errors: [{ messageId: "noMathMinMaxArraySpread" }],
        },
      ],
    });
  });

  it("invalid: computed Math access is flagged and the message names the reduce fix", () => {
    cjsRuleTester.run("no-math-minmax-array-spread", noMathMinMaxArraySpreadRule, {
      valid: [],
      invalid: [
        {
          code: `const largest = Math["max"](...values);`,
          errors: [
            {
              message:
                "Avoid Math.max(...values) — spreading an array of unknown size can throw `RangeError: Maximum call stack size exceeded`. Use `values.reduce((a, b) => Math.max(a, b), -Infinity)` instead — the `-Infinity` initializer preserves the same result as `Math.max(...values)` on an empty array.",
            },
          ],
        },
      ],
    });
  });
});
