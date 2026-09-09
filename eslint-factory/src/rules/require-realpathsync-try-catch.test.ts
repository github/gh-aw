import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireRealpathSyncTryCatchRule } from "./require-realpathsync-try-catch";

const cjsRuleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });
const esmRuleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "module" } });

describe("require-realpathsync-try-catch", () => {
  it("allows calls inside try/catch and ignores non-fs receivers", () => {
    cjsRuleTester.run("require-realpathsync-try-catch", requireRealpathSyncTryCatchRule, {
      valid: [
        `const fs = require("fs"); try { fs.realpathSync(path); } catch (e) {}`,
        `const { realpathSync } = require("node:fs"); try { realpathSync(path); } catch (e) {}`,
        `mockFs.realpathSync(path);`,
        `const fs = require("mock-fs"); fs.realpathSync(path);`,
      ],
      invalid: [],
    });
  });

  it("flags CommonJS calls and offers an autofix", () => {
    cjsRuleTester.run("require-realpathsync-try-catch", requireRealpathSyncTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `fs.realpathSync(unresolved);`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { arg: "unresolved" },
              suggestions: [
                {
                  messageId: "wrapInTryCatch",
                  output: `try {\n  fs.realpathSync(unresolved);\n} catch (err) {\n  // TODO: handle filesystem failure for this fs.realpathSync call.\n  throw new Error(\n    "fs.realpathSync failed: " + (err instanceof Error ? err.message : String(err)),\n    { cause: err },\n  );\n}`,
                },
              ],
            },
          ],
        },
        {
          code: `const fs = require("fs"); fs["realpathSync"](root);`,
          errors: [{ messageId: "requireTryCatch", data: { arg: "root" }, suggestions: 1 }],
        },
        {
          code: `const { realpathSync } = require("fs"); realpathSync(directoryPath);`,
          errors: [{ messageId: "requireTryCatch", data: { arg: "directoryPath" }, suggestions: 1 }],
        },
      ],
    });
  });

  it("handles ESM namespace and named imports", () => {
    esmRuleTester.run("require-realpathsync-try-catch", requireRealpathSyncTryCatchRule, {
      valid: [`import * as fs from "fs"; try { fs.realpathSync(path); } catch (e) {}`],
      invalid: [
        {
          code: `import * as fs from "node:fs"; fs.realpathSync(path);`,
          errors: [{ messageId: "requireTryCatch", data: { arg: "path" }, suggestions: 1 }],
        },
        {
          code: `import { realpathSync } from "fs"; realpathSync(path);`,
          errors: [{ messageId: "requireTryCatch", data: { arg: "path" }, suggestions: 1 }],
        },
      ],
    });
  });

  it("flags calls in async functions and try/finally without catch", () => {
    cjsRuleTester.run("require-realpathsync-try-catch", requireRealpathSyncTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `async function run() { fs.realpathSync(path); }`,
          errors: [{ messageId: "requireTryCatch", suggestions: 1 }],
        },
        {
          code: `try { fs.realpathSync(path); } finally { cleanup(); }`,
          errors: [{ messageId: "requireTryCatch", suggestions: 1 }],
        },
      ],
    });
  });
});
