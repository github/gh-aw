import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noUnguardedAsyncTimerCallbackRule } from "./no-unguarded-async-timer-callback";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("no-unguarded-async-timer-callback", () => {
  it("uses the correct docs URL", () => {
    expect(noUnguardedAsyncTimerCallbackRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-unguarded-async-timer-callback");
  });

  it("valid and invalid cases", () => {
    ruleTester.run("no-unguarded-async-timer-callback", noUnguardedAsyncTimerCallbackRule, {
      valid: [
        `setTimeout(() => { run(); }, 10);`,
        `setInterval(async () => { try { await poll(); } catch (e) { log(e); } }, 10);`,
        `setTimeout(handler, 10);`,
        `setTimeout(async function () { try { await x(); } catch {} }, 10);`,
        `obj.setTimeout(async () => { await x(); }, 10);`,
      ],
      invalid: [
        { code: `setInterval(async () => { await poll(); }, 10);`, errors: [{ messageId: "unguardedAsyncTimer" }] },
        { code: `setTimeout(async function () { await x(); }, 10);`, errors: [{ messageId: "unguardedAsyncTimer" }] },
        { code: `setImmediate(async () => { try { await x(); } finally { done(); } });`, errors: [{ messageId: "unguardedAsyncTimer" }] },
        { code: `setTimeout(async () => { try { await x(); } catch (e) {} next(); }, 1);`, errors: [{ messageId: "unguardedAsyncTimer" }] },
        { code: `globalThis.setTimeout(async () => fn(), 1);`, errors: [{ messageId: "unguardedAsyncTimer" }] },
      ],
    });
  });
});
