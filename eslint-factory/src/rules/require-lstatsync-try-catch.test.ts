import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireLstatSyncTryCatchRule } from "./require-lstatsync-try-catch";

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

describe("require-lstatsync-try-catch", () => {
  it("valid: fs.lstatSync inside try block passes", () => {
    cjsRuleTester.run("require-lstatsync-try-catch", requireLstatSyncTryCatchRule, {
      valid: [`try { const s = fs.lstatSync(path); } catch (e) {}`, `try { const target = fs.readlinkSync(path); } catch (e) {}`],
      invalid: [],
    });
  });

  it("valid: other fs methods not in scope are ignored", () => {
    cjsRuleTester.run("require-lstatsync-try-catch", requireLstatSyncTryCatchRule, {
      valid: [`fs.existsSync(path);`, `fs.statSync(path);`, `fs.readFileSync(path, "utf8");`, `mockFs.lstatSync(path);`, `storage.readlinkSync(path);`],
      invalid: [],
    });
  });

  it("invalid: fs.lstatSync outside try/catch is flagged", () => {
    cjsRuleTester.run("require-lstatsync-try-catch", requireLstatSyncTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `fs.lstatSync(current);`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { method: "lstatSync", arg: "current" },
              suggestions: [expectedWrapInTryCatchSuggestion("lstatSync", "fs.lstatSync(current);")],
            },
          ],
        },
      ],
    });
  });

  it("invalid: fs.readlinkSync outside try/catch is flagged", () => {
    cjsRuleTester.run("require-lstatsync-try-catch", requireLstatSyncTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const target = fs.readlinkSync(fullPath);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "readlinkSync", arg: "fullPath" } }],
        },
      ],
    });
  });

  it("valid: node:fs destructured inside try block passes", () => {
    cjsRuleTester.run("require-lstatsync-try-catch", requireLstatSyncTryCatchRule, {
      valid: [`const { lstatSync } = require("node:fs"); try { lstatSync(path); } catch (e) {}`, `const { readlinkSync } = require("fs"); try { readlinkSync(path); } catch (e) {}`],
      invalid: [],
    });
  });

  it("invalid: destructured fs methods outside try/catch are flagged", () => {
    cjsRuleTester.run("require-lstatsync-try-catch", requireLstatSyncTryCatchRule, {
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
