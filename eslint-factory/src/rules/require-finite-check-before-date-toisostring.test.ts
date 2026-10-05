import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { requireFiniteCheckBeforeDateToIsoStringRule } from "./require-finite-check-before-date-toisostring";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("require-finite-check-before-date-toisostring", () => {
  it("uses the correct docs URL", () => {
    expect(requireFiniteCheckBeforeDateToIsoStringRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#require-finite-check-before-date-toisostring");
  });

  it("valid and invalid cases", () => {
    ruleTester.run("require-finite-check-before-date-toisostring", requireFiniteCheckBeforeDateToIsoStringRule, {
      valid: [
        `new Date().toISOString();`,
        `new Date(Date.now()).toISOString();`,
        `new Date(Date.now() - WINDOW_MS).toISOString();`,
        `new Date(0).toISOString();`,
        `function f(ms) { if (!Number.isFinite(ms)) return ""; return new Date(ms).toISOString(); }`,
        `function f(data) { if (Number.isNaN(data.reset)) return null; return new Date(data.reset * 1000).toISOString(); }`,
        `function f(ms) { try { return new Date(ms).toISOString(); } catch { return ""; } }`,
        `const d = new Date(ms); d.toISOString();`,
        `new Date(ms).getTime();`,
      ],
      invalid: [
        { code: `function f(ms) { return new Date(ms).toISOString(); }`, errors: [{ messageId: "requireFiniteCheck" }] },
        { code: `const s = new Date(data.reset * 1000).toISOString();`, errors: [{ messageId: "requireFiniteCheck" }] },
        { code: `function f(ms) { try { x(); } catch { return new Date(ms).toJSON(); } }`, errors: [{ messageId: "requireFiniteCheck" }] },
        { code: `function f(a, b) { if (Number.isFinite(b)) {} return new Date(a).toISOString(); }`, errors: [{ messageId: "requireFiniteCheck" }] },
      ],
    });
  });
});
