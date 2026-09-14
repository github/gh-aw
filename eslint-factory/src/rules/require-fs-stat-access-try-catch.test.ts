import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireFsStatAccessTryCatchRule } from "./require-fs-stat-access-try-catch";

const cjsRuleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });
const esmRuleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "module" } });

describe("require-fs-stat-access-try-catch", () => {
  it("allows calls inside try/catch and ignores non-fs receivers", () => {
    cjsRuleTester.run("require-fs-stat-access-try-catch", requireFsStatAccessTryCatchRule, {
      valid: [
        `const fs = require("fs"); try { fs.lstatSync(path); } catch (e) {}`,
        `const fs = require("fs"); try { fs.accessSync(path, fs.constants.X_OK); } catch (e) {}`,
        `const { readlinkSync } = require("node:fs"); try { readlinkSync(path); } catch (e) {}`,
        `mockFs.lstatSync(path);`,
        `const fs = require("mock-fs"); fs.lstatSync(path);`,
      ],
      invalid: [],
    });
  });

  it("flags CommonJS calls and offers an autofix", () => {
    cjsRuleTester.run("require-fs-stat-access-try-catch", requireFsStatAccessTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `fs.lstatSync(filePath);`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { method: "lstatSync", arg: "filePath" },
              suggestions: [
                {
                  messageId: "wrapInTryCatch",
                  output: `try {\n  fs.lstatSync(filePath);\n} catch (err) {\n  // TODO: handle filesystem failure for this fs.lstatSync call.\n  throw new Error(\n    "fs.lstatSync failed: " + (err instanceof Error ? err.message : String(err)),\n    { cause: err },\n  );\n}`,
                },
              ],
            },
          ],
        },
        {
          code: `const fs = require("fs"); fs.accessSync(resolvedPath, fs.constants.X_OK);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "accessSync", arg: "resolvedPath" }, suggestions: 1 }],
        },
        {
          code: `const { readlinkSync } = require("fs"); readlinkSync(fullPath);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "readlinkSync", arg: "fullPath" }, suggestions: 1 }],
        },
      ],
    });
  });

  it("handles ESM namespace and named imports", () => {
    esmRuleTester.run("require-fs-stat-access-try-catch", requireFsStatAccessTryCatchRule, {
      valid: [`import * as fs from "fs"; try { fs.lstatSync(path); } catch (e) {}`],
      invalid: [
        {
          code: `import * as fs from "node:fs"; fs.lstatSync(path);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "lstatSync", arg: "path" }, suggestions: 1 }],
        },
        {
          code: `import { accessSync } from "node:fs"; accessSync(path);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "accessSync", arg: "path" }, suggestions: 1 }],
        },
      ],
    });
  });
});
