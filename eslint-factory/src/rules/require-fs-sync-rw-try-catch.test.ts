import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireFsSyncRwTryCatchRule } from "./require-fs-sync-rw-try-catch";

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

describe("require-fs-sync-rw-try-catch", () => {
  it("valid: fs.writeSync/fs.readSync inside try block passes", () => {
    cjsRuleTester.run("require-fs-sync-rw-try-catch", requireFsSyncRwTryCatchRule, {
      valid: [
        `try { fs.writeSync(fd, data); } catch (e) {}`,
        `try { const n = fs.readSync(fd, buf, 0, len, pos); } catch (e) {}`,
        `try { fs.writeSync(1, bytes); } catch (e) {} finally { fs.closeSync(1); }`,
      ],
      invalid: [],
    });
  });

  it("valid: other fs methods and non-fs receivers not in scope are ignored", () => {
    cjsRuleTester.run("require-fs-sync-rw-try-catch", requireFsSyncRwTryCatchRule, {
      valid: [`fs.existsSync(path);`, `fs.readFileSync(path, "utf8");`, `fs.writeFileSync(path, data);`, `mockFs.writeSync(fd, data);`, `stream.readSync(fd, buf, 0, len, pos);`],
      invalid: [],
    });
  });

  it("invalid: fs.writeSync outside try/catch is flagged", () => {
    cjsRuleTester.run("require-fs-sync-rw-try-catch", requireFsSyncRwTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `fs.writeSync(1, bytes);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "writeSync", arg: "1" }, suggestions: [expectedWrapInTryCatchSuggestion("writeSync", "fs.writeSync(1, bytes);")] }],
        },
      ],
    });
  });

  it("invalid: fs.readSync outside try/catch is flagged", () => {
    cjsRuleTester.run("require-fs-sync-rw-try-catch", requireFsSyncRwTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const n = fs.readSync(fd, buf, 0, bufSize, null);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "readSync", arg: "fd" } }],
        },
      ],
    });
  });

  it("invalid: fs.writeSync inside try/finally without catch is still flagged", () => {
    cjsRuleTester.run("require-fs-sync-rw-try-catch", requireFsSyncRwTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `try { fs.writeSync(fd, data); } finally { fs.closeSync(fd); }`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { method: "writeSync", arg: "fd" },
              suggestions: [
                {
                  messageId: "wrapInTryCatch",
                  output:
                    `try { try {\n` +
                    `  fs.writeSync(fd, data);\n` +
                    `} catch (err) {\n` +
                    `  // TODO: handle I/O failure for this fs.writeSync call.\n` +
                    `  throw new Error(\n` +
                    `    "fs.writeSync failed: " + (err instanceof Error ? err.message : String(err)),\n` +
                    `    { cause: err },\n` +
                    `  );\n` +
                    `} } finally { fs.closeSync(fd); }`,
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("valid: node:fs destructured inside try block passes", () => {
    cjsRuleTester.run("require-fs-sync-rw-try-catch", requireFsSyncRwTryCatchRule, {
      valid: [`const { writeSync } = require("node:fs"); try { writeSync(1, bytes); } catch (e) {}`, `const { readSync } = require("fs"); try { readSync(fd, buf, 0, len, pos); } catch (e) {}`],
      invalid: [],
    });
  });

  it("invalid: destructured fs methods outside try/catch are flagged", () => {
    cjsRuleTester.run("require-fs-sync-rw-try-catch", requireFsSyncRwTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `const { writeSync } = require("node:fs"); writeSync(1, bytes);`,
          errors: [{ messageId: "requireTryCatch", suggestions: [expectedWrapInTryCatchSuggestion("writeSync", "writeSync(1, bytes);", `const { writeSync } = require("node:fs"); `)] }],
        },
      ],
    });
  });

  it("valid: fs import inside try block passes (ESM)", () => {
    esmRuleTester.run("require-fs-sync-rw-try-catch", requireFsSyncRwTryCatchRule, {
      valid: [`import fs from "node:fs"; try { fs.writeSync(1, bytes); } catch (e) {}`],
      invalid: [],
    });
  });

  it("invalid: fs import outside try/catch is flagged (ESM)", () => {
    esmRuleTester.run("require-fs-sync-rw-try-catch", requireFsSyncRwTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `import fs from "node:fs"; fs.writeSync(1, bytes);`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { method: "writeSync", arg: "1" },
              suggestions: [expectedWrapInTryCatchSuggestion("writeSync", "fs.writeSync(1, bytes);", `import fs from "node:fs"; `)],
            },
          ],
        },
      ],
    });
  });
});
