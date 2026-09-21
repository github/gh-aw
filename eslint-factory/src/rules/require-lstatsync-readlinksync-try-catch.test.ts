import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireLstatSyncReadlinkSyncTryCatchRule } from "./require-lstatsync-readlinksync-try-catch";

const cjsRuleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });
const esmRuleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "module" } });

describe("require-lstatsync-readlinksync-try-catch", () => {
  it("allows calls inside try/catch and ignores non-fs receivers", () => {
    cjsRuleTester.run("require-lstatsync-readlinksync-try-catch", requireLstatSyncReadlinkSyncTryCatchRule, {
      valid: [
        `const fs = require("fs"); try { fs.lstatSync(path); } catch (e) {}`,
        `const fs = require("fs"); try { fs.readlinkSync(path); } catch (e) {}`,
        `const { lstatSync, readlinkSync } = require("node:fs"); try { lstatSync(path); readlinkSync(path); } catch (e) {}`,
        `mockFs.lstatSync(path);`,
        `const fs = require("mock-fs"); fs.lstatSync(path);`,
      ],
      invalid: [],
    });
  });

  it("flags CommonJS calls and offers an autofix", () => {
    cjsRuleTester.run("require-lstatsync-readlinksync-try-catch", requireLstatSyncReadlinkSyncTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `fs.lstatSync(current);`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { method: "lstatSync", arg: "current" },
              suggestions: [
                {
                  messageId: "wrapInTryCatch",
                  output: `try {\n  fs.lstatSync(current);\n} catch (err) {\n  // TODO: handle filesystem failure for this fs.lstatSync call.\n  throw new Error(\n    "fs.lstatSync failed: " + (err instanceof Error ? err.message : String(err)),\n    { cause: err },\n  );\n}`,
                },
              ],
            },
          ],
        },
        {
          code: `fs.readlinkSync(fullPath);`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { method: "readlinkSync", arg: "fullPath" },
              suggestions: [
                {
                  messageId: "wrapInTryCatch",
                  output: `try {\n  fs.readlinkSync(fullPath);\n} catch (err) {\n  // TODO: handle filesystem failure for this fs.readlinkSync call.\n  throw new Error(\n    "fs.readlinkSync failed: " + (err instanceof Error ? err.message : String(err)),\n    { cause: err },\n  );\n}`,
                },
              ],
            },
          ],
        },
        {
          code: `const fs = require("fs"); fs["lstatSync"](filePath);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "lstatSync", arg: "filePath" }, suggestions: 1 }],
        },
        {
          code: `const { lstatSync } = require("fs"); lstatSync(filePath);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "lstatSync", arg: "filePath" }, suggestions: 1 }],
        },
        {
          code: `const { readlinkSync } = require("fs"); readlinkSync(filePath);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "readlinkSync", arg: "filePath" }, suggestions: 1 }],
        },
      ],
    });
  });

  it("handles ESM namespace and named imports", () => {
    esmRuleTester.run("require-lstatsync-readlinksync-try-catch", requireLstatSyncReadlinkSyncTryCatchRule, {
      valid: [`import * as fs from "fs"; try { fs.lstatSync(path); } catch (e) {}`],
      invalid: [
        {
          code: `import * as fs from "node:fs"; fs.lstatSync(path);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "lstatSync", arg: "path" }, suggestions: 1 }],
        },
        {
          code: `import { readlinkSync } from "fs"; readlinkSync(path);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "readlinkSync", arg: "path" }, suggestions: 1 }],
        },
      ],
    });
  });

  it("flags calls in async functions and try/finally without catch", () => {
    cjsRuleTester.run("require-lstatsync-readlinksync-try-catch", requireLstatSyncReadlinkSyncTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `async function run() { fs.lstatSync(path); }`,
          errors: [{ messageId: "requireTryCatch", suggestions: 1 }],
        },
        {
          code: `try { fs.lstatSync(path); } finally { cleanup(); }`,
          errors: [{ messageId: "requireTryCatch", suggestions: 1 }],
        },
      ],
    });
  });
});
