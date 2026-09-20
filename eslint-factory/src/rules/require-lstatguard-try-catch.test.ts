import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireLstatGuardTryCatchRule } from "./require-lstatguard-try-catch";

const cjsRuleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });
const esmRuleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "module" } });

describe("require-lstatguard-try-catch", () => {
  it("allows calls inside try/catch and ignores unrelated bindings", () => {
    cjsRuleTester.run("require-lstatguard-try-catch", requireLstatGuardTryCatchRule, {
      valid: [
        `const { lstatGuard } = require("./symlink_guard.cjs"); try { lstatGuard(filePath); } catch (e) {}`,
        // Not imported from symlink_guard.cjs — a same-named local helper should not be flagged.
        `function lstatGuard(p) { return null; } lstatGuard(filePath);`,
        // Imported from an unrelated module with the same name.
        `const { lstatGuard } = require("./other-module.cjs"); lstatGuard(filePath);`,
        // No local binding at all (undeclared global) should not be flagged.
        `lstatGuard(filePath);`,
      ],
      invalid: [],
    });
  });

  it("flags CommonJS destructured calls and offers an autofix", () => {
    cjsRuleTester.run("require-lstatguard-try-catch", requireLstatGuardTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const { lstatGuard } = require("./symlink_guard.cjs"); lstatGuard(filePath);`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { arg: "filePath" },
              suggestions: [
                {
                  messageId: "wrapInTryCatch",
                  output: `const { lstatGuard } = require("./symlink_guard.cjs"); try {\n  lstatGuard(filePath);\n} catch (err) {\n  // TODO: handle filesystem failure for this lstatGuard(...) call.\n  throw new Error(\n    "lstatGuard failed: " + (err instanceof Error ? err.message : String(err)),\n    { cause: err },\n  );\n}`,
                },
              ],
            },
          ],
        },
        {
          code: `const { lstatGuard } = require("./symlink_guard.cjs");\nfunction check(entryPath) {\n  const stats = lstatGuard(entryPath);\n  return stats;\n}`,
          errors: [{ messageId: "requireTryCatch", data: { arg: "entryPath" }, suggestions: 0 }],
        },
      ],
    });
  });

  it("handles ESM named imports", () => {
    esmRuleTester.run("require-lstatguard-try-catch", requireLstatGuardTryCatchRule, {
      valid: [`import { lstatGuard } from "./symlink_guard.cjs"; try { lstatGuard(p); } catch (e) {}`],
      invalid: [
        {
          code: `import { lstatGuard } from "./symlink_guard.cjs"; lstatGuard(p);`,
          errors: [{ messageId: "requireTryCatch", data: { arg: "p" }, suggestions: 1 }],
        },
      ],
    });
  });

  it("flags calls in async functions and try/finally without catch", () => {
    cjsRuleTester.run("require-lstatguard-try-catch", requireLstatGuardTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const { lstatGuard } = require("./symlink_guard.cjs"); async function run() { lstatGuard(path); }`,
          errors: [{ messageId: "requireTryCatch", suggestions: 1 }],
        },
        {
          code: `const { lstatGuard } = require("./symlink_guard.cjs"); try { lstatGuard(path); } finally { cleanup(); }`,
          errors: [{ messageId: "requireTryCatch", suggestions: 1 }],
        },
      ],
    });
  });
});
