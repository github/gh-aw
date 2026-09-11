import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noSetFailedThenExitZeroRule } from "./no-setfailed-then-exit-zero";

const ruleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: "latest",
    sourceType: "commonjs",
  },
});

describe("no-setfailed-then-exit-zero", () => {
  it("uses the correct docs URL", () => {
    expect(noSetFailedThenExitZeroRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-setfailed-then-exit-zero");
  });

  it("valid and invalid cases", () => {
    ruleTester.run("no-setfailed-then-exit-zero", noSetFailedThenExitZeroRule, {
      valid: [
        // process.exit(0) without a preceding core.setFailed is fine
        `process.exit(0);`,
        // core.setFailed followed by return is the correct pattern
        `function f() { core.setFailed("bad"); return; }`,
        // core.setFailed followed by process.exit(1) is fine — non-zero exit preserves failure
        `core.setFailed("bad"); process.exit(1);`,
        // core.setFailed followed by process.exit(2) is fine
        `function f() { core.setFailed("bad"); process.exit(2); }`,
        // core.setFailed with no next statement is fine
        `function f() { core.setFailed("bad"); }`,
        // Non-core object is not flagged
        `other.setFailed("bad"); process.exit(0);`,
        // core.error (not setFailed) is not flagged by this rule
        `core.error("bad"); process.exit(0);`,
        // return between setFailed and process.exit(0) stops scanning
        `function f() { core.setFailed("bad"); return; process.exit(0); }`,
        // throw between setFailed and process.exit(0) stops scanning
        `function f() { core.setFailed("bad"); throw new Error("x"); process.exit(0); }`,
        // process.exit(variable) — runtime value unknown, not flagged
        `core.setFailed("bad"); process.exit(code);`,
        // process.exit with non-zero string literal — not matched
        `core.setFailed("bad"); process.exit("0");`,
        // Computed process["exit"] with non-zero argument is fine
        `core.setFailed("bad"); process["exit"](1);`,
        // Computed process["exitCode"] with non-zero assignment is fine
        `core.setFailed("bad"); process["exitCode"] = 1;`,
        // Computed alias not matching core
        `const c = other; function f() { c.setFailed("bad"); process.exit(0); }`,
        // Destructured from non-core — not flagged
        `const { setFailed } = other; function f() { setFailed("bad"); process.exit(0); }`,
        // Destructured setFailed with return is the correct pattern
        `const { setFailed } = core; function f() { setFailed("bad"); return; }`,
        // process.exitCode = 0 without a preceding core.setFailed is fine
        `process.exitCode = 0;`,
        // core.setFailed followed by process.exitCode = 1 is fine — non-zero code preserves failure
        `core.setFailed("bad"); process.exitCode = 1;`,
        // core.setFailed followed by process.exitCode = variable — runtime value unknown, not flagged
        `core.setFailed("bad"); process.exitCode = code;`,
        // return between setFailed and process.exitCode = 0 stops scanning
        `function f() { core.setFailed("bad"); return; process.exitCode = 0; }`,
        // process.exitCode = "0" (string, not the number literal) — not matched
        `core.setFailed("bad"); process.exitCode = "0";`,
        // try/finally where setFailed is NOT called — no false positive
        `function f() { try { doWork(); } finally { process.exit(0); } }`,
        // try/catch that does not call setFailed, followed by sibling exit(0) — no false positive
        `function f() { try { doWork(); } catch (err) { core.info("x"); } process.exit(0); }`,
        // setFailed inside try, followed by non-zero exit in finally — fine
        `function f() { try { core.setFailed("bad"); } finally { process.exit(1); } }`,
      ],
      invalid: [
        // Adjacent: core.setFailed immediately followed by process.exit(0)
        {
          code: `core.setFailed("bad"); process.exit(0);`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [{ messageId: "replaceWithReturn", output: `core.setFailed("bad"); return;` }] }],
        },
        // Inside a function
        {
          code: `function f() { core.setFailed("bad"); process.exit(0); }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [{ messageId: "replaceWithReturn", output: `function f() { core.setFailed("bad"); return; }` }] }],
        },
        // process.exit() with no arguments (defaults to 0)
        {
          code: `function f() { core.setFailed("bad"); process.exit(); }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [{ messageId: "replaceWithReturn", output: `function f() { core.setFailed("bad"); return; }` }] }],
        },
        // Computed property: core["setFailed"]
        {
          code: `core["setFailed"]("bad"); process.exit(0);`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [{ messageId: "replaceWithReturn", output: `core["setFailed"]("bad"); return;` }] }],
        },
        // Non-adjacent: intervening log statement does not stop detection
        {
          code: `core.setFailed("bad"); core.info("msg"); process.exit(0);`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [] }],
        },
        // Aliased core object
        {
          code: `const c = core; function f() { c.setFailed("bad"); process.exit(0); }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [{ messageId: "replaceWithReturn", output: `const c = core; function f() { c.setFailed("bad"); return; }` }] }],
        },
        // Inside an if block
        {
          code: `function f() { if (bad) { core.setFailed("x"); process.exit(0); } }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [{ messageId: "replaceWithReturn", output: `function f() { if (bad) { core.setFailed("x"); return; } }` }] }],
        },
        // switch-case
        {
          code: `switch (x) { case 1: core.setFailed("bad"); process.exit(0); break; }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [{ messageId: "replaceWithReturn", output: `switch (x) { case 1: core.setFailed("bad"); return; break; }` }] }],
        },
        // Nested setFailed in if-block, then sibling process.exit(0) in enclosing block
        {
          code: `function f() { if (bad) { core.setFailed("x"); } process.exit(0); }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [] }],
        },
        // Existing same-block detection remains invalid
        {
          code: `function f() { core.setFailed("bad"); core.info("msg"); process.exit(0); }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [] }],
        },
        // Destructured setFailed from core
        {
          code: `const { setFailed } = core; function f() { setFailed("bad"); process.exit(0); }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [{ messageId: "replaceWithReturn", output: `const { setFailed } = core; function f() { setFailed("bad"); return; }` }] }],
        },
        // Renamed destructured binding: const { setFailed: sf } = core
        {
          code: `const { setFailed: sf } = core; function f() { sf("bad"); process.exit(0); }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [{ messageId: "replaceWithReturn", output: `const { setFailed: sf } = core; function f() { sf("bad"); return; }` }] }],
        },
        // Adjacent: core.setFailed immediately followed by process.exitCode = 0
        {
          code: `core.setFailed("bad"); process.exitCode = 0;`,
          errors: [{ messageId: "noSetFailedThenExitCodeZero", suggestions: [{ messageId: "removeExitCodeZero", output: `core.setFailed("bad"); ` }] }],
        },
        // Non-adjacent: intervening log statement does not stop detection for exitCode = 0
        {
          code: `core.setFailed("bad"); core.info("msg"); process.exitCode = 0;`,
          errors: [{ messageId: "noSetFailedThenExitCodeZero", suggestions: [{ messageId: "removeExitCodeZero", output: `core.setFailed("bad"); core.info("msg"); ` }] }],
        },
        // Inside a function
        {
          code: `function f() { core.setFailed("bad"); process.exitCode = 0; }`,
          errors: [{ messageId: "noSetFailedThenExitCodeZero", suggestions: [{ messageId: "removeExitCodeZero", output: `function f() { core.setFailed("bad");  }` }] }],
        },
        // Computed process["exit"](0)
        {
          code: `core.setFailed("bad"); process["exit"](0);`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [{ messageId: "replaceWithReturn", output: `core.setFailed("bad"); return;` }] }],
        },
        // Computed process["exitCode"] = 0
        {
          code: `core.setFailed("bad"); process["exitCode"] = 0;`,
          errors: [{ messageId: "noSetFailedThenExitCodeZero", suggestions: [{ messageId: "removeExitCodeZero", output: `core.setFailed("bad"); ` }] }],
        },
        // try { setFailed } finally { process.exit(0) } — the two live in different
        // BlockStatement bodies, so this exercises the dedicated TryStatement handler.
        {
          code: `function f() { try { core.setFailed("bad"); } finally { process.exit(0); } }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [] }],
        },
        // setFailed inside catch, sibling process.exit(0) after the whole try statement
        {
          code: `function f() { try { doWork(); } catch (err) { core.setFailed(getErrorMessage(err)); } process.exit(0); }`,
          errors: [{ messageId: "noSetFailedThenExitZero", suggestions: [] }],
        },
        // setFailed inside try, process.exitCode = 0 in finally
        {
          code: `function f() { try { core.setFailed("bad"); } finally { process.exitCode = 0; } }`,
          errors: [{ messageId: "noSetFailedThenExitCodeZero", suggestions: [{ messageId: "removeExitCodeZero", output: `function f() { try { core.setFailed("bad"); } finally {  } }` }] }],
        },
      ],
    });
  });
});
