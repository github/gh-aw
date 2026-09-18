import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireFsStatAccessTryCatchRule } from "./require-fs-stat-access-try-catch";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

function expectedWrapInTryCatchSuggestion(method: string, statement: string, prefix = "") {
  return {
    messageId: "wrapInTryCatch",
    output:
      `${prefix}try {\n` +
      `  ${statement}\n` +
      `} catch (err) {\n` +
      `  // TODO: handle I/O failure for this fs.${method} call.\n` +
      `  throw new Error(\n` +
      `    "fs.${method} failed: " + (err instanceof Error ? err.message : String(err)),\n` +
      `    { cause: err },\n` +
      `  );\n` +
      `}`,
  };
}

describe("require-fs-stat-access-try-catch", () => {
  it("valid: fs.lstatSync/accessSync/readlinkSync inside try block pass", () => {
    cjsRuleTester.run("require-fs-stat-access-try-catch", requireFsStatAccessTryCatchRule, {
      valid: [
        `try { const s = fs.lstatSync(path); } catch (e) {}`,
        `try { fs.accessSync(path, fs.constants.F_OK); } catch (e) {}`,
        `try { const target = fs.readlinkSync(path); } catch (e) {}`,
      ],
      invalid: [],
    });
  });

  it("valid: other fs methods not in scope are ignored", () => {
    cjsRuleTester.run("require-fs-stat-access-try-catch", requireFsStatAccessTryCatchRule, {
      valid: [`fs.existsSync(path);`, `fs.statSync(path);`, `fs.realpathSync(path);`, `mockFs.lstatSync(path);`, `storage.accessSync(path);`],
      invalid: [],
    });
  });

  it("invalid: fs.lstatSync outside try/catch is flagged", () => {
    cjsRuleTester.run("require-fs-stat-access-try-catch", requireFsStatAccessTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const stat = fs.lstatSync(current);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "lstatSync", arg: "current" } }],
        },
      ],
    });
  });

  it("invalid: fs.accessSync outside try/catch is flagged", () => {
    cjsRuleTester.run("require-fs-stat-access-try-catch", requireFsStatAccessTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `fs.accessSync(filePath, fs.constants.F_OK);`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { method: "accessSync", arg: "filePath" },
              suggestions: [expectedWrapInTryCatchSuggestion("accessSync", "fs.accessSync(filePath, fs.constants.F_OK);")],
            },
          ],
        },
      ],
    });
  });

  it("invalid: fs.readlinkSync outside try/catch is flagged", () => {
    cjsRuleTester.run("require-fs-stat-access-try-catch", requireFsStatAccessTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `hash.update(fs.readlinkSync(fullPath));`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { method: "readlinkSync", arg: "fullPath" },
              suggestions: [expectedWrapInTryCatchSuggestion("readlinkSync", "hash.update(fs.readlinkSync(fullPath));")],
            },
          ],
        },
      ],
    });
  });

  it("valid: node:fs destructured inside try block passes", () => {
    cjsRuleTester.run("require-fs-stat-access-try-catch", requireFsStatAccessTryCatchRule, {
      valid: [`const { lstatSync } = require("node:fs"); try { lstatSync(path); } catch (e) {}`, `const { accessSync } = require("fs"); try { accessSync(path); } catch (e) {}`],
      invalid: [],
    });
  });

  it("invalid: destructured fs methods outside try/catch are flagged", () => {
    cjsRuleTester.run("require-fs-stat-access-try-catch", requireFsStatAccessTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const { lstatSync } = require("node:fs"); lstatSync(path);`,
          errors: [{ messageId: "requireTryCatch", suggestions: [expectedWrapInTryCatchSuggestion("lstatSync", "lstatSync(path);", `const { lstatSync } = require("node:fs"); `)] }],
        },
      ],
    });
  });
});
