import { RuleTester } from "eslint";
import { describe, it } from "vitest";
import { requireFsChmodTryCatchRule } from "./require-fs-chmod-try-catch";

const cjsRuleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

function expectedWrapInTryCatchSuggestion(method: string, statement: string, prefix = "") {
  return {
    messageId: "wrapInTryCatch",
    output: `${prefix}try {\n` + `  ${statement}\n` + `} catch (err) {\n` + `  throw new Error(\n` + `    "fs.${method} failed: " + (err instanceof Error ? err.message : String(err)),\n` + `    { cause: err },\n` + `  );\n` + `}`,
  };
}

describe("require-fs-chmod-try-catch", () => {
  it("valid: fs.chmodSync and fs.fchmodSync inside try block pass", () => {
    cjsRuleTester.run("require-fs-chmod-try-catch", requireFsChmodTryCatchRule, {
      valid: [`try { fs.chmodSync(path, 0o600); } catch (e) {}`, `try { fs.fchmodSync(fd, 0o600); } catch (e) {}`],
      invalid: [],
    });
  });

  it("valid: other fs methods and non-fs identifiers are ignored", () => {
    cjsRuleTester.run("require-fs-chmod-try-catch", requireFsChmodTryCatchRule, {
      valid: [`fs.existsSync(path);`, `fs.readFileSync(path, "utf8");`, `fs.statSync(path);`, `mockFs.chmodSync(path, 0o600);`, `storage.fchmodSync(fd, 0o600);`],
      invalid: [],
    });
  });

  it("invalid: fs.chmodSync outside try/catch is flagged", () => {
    cjsRuleTester.run("require-fs-chmod-try-catch", requireFsChmodTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `fs.chmodSync(path, 0o600);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "chmodSync", arg: "path" }, suggestions: [expectedWrapInTryCatchSuggestion("chmodSync", "fs.chmodSync(path, 0o600);")] }],
        },
      ],
    });
  });

  it("invalid: fs.fchmodSync outside try/catch is flagged", () => {
    cjsRuleTester.run("require-fs-chmod-try-catch", requireFsChmodTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `fs.fchmodSync(fd, 0o600);`,
          errors: [{ messageId: "requireTryCatch", data: { method: "fchmodSync", arg: "fd" }, suggestions: [expectedWrapInTryCatchSuggestion("fchmodSync", "fs.fchmodSync(fd, 0o600);")] }],
        },
      ],
    });
  });

  it("invalid: fs.chmodSync after an unrelated try block is still flagged", () => {
    cjsRuleTester.run("require-fs-chmod-try-catch", requireFsChmodTryCatchRule, {
      valid: [],
      invalid: [
        {
          code: `try { doSomethingElse(); } catch (e) {}\nfs.chmodSync(dir, 0o700);`,
          errors: [
            {
              messageId: "requireTryCatch",
              data: { method: "chmodSync", arg: "dir" },
              suggestions: [expectedWrapInTryCatchSuggestion("chmodSync", "fs.chmodSync(dir, 0o700);", "try { doSomethingElse(); } catch (e) {}\n")],
            },
          ],
        },
      ],
    });
  });
});
