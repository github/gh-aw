import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noUnhandledAsyncTimerCallbackRule } from "./no-unhandled-async-timer-callback";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("no-unhandled-async-timer-callback", () => {
  it("uses the correct docs URL", () => {
    expect(noUnhandledAsyncTimerCallbackRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-unhandled-async-timer-callback");
  });

  it("valid and invalid cases", () => {
    ruleTester.run("no-unhandled-async-timer-callback", noUnhandledAsyncTimerCallbackRule, {
      valid: [
        `setInterval(async () => { try { await poll(); } catch { x = 1; } }, 100);`,
        `setTimeout(async () => { try { await poll(); } catch (e) {} finally { done(); } }, 100);`,
        `setTimeout(() => { run(); }, 100);`,
        `setTimeout(async () => { try { await a(); } catch {} const f = async () => { await b(); }; }, 1);`,
        `async function f() { await g(); }`,
        `arr.forEach(async x => { await x(); });`,
      ],
      invalid: [
        { code: `setInterval(async () => { await poll(); }, 100);`, errors: [{ messageId: "unhandledAwait", data: { timer: "setInterval" } }] },
        { code: `setTimeout(async function () { try { x(); } finally { await y(); } }, 100);`, errors: [{ messageId: "unhandledAwait" }] },
        { code: `setTimeout(async () => { try { await a(); } catch { await cleanup(); } }, 100);`, errors: [{ messageId: "unhandledAwait" }] },
        { code: `globalThis.setTimeout(async () => { await poll(); }, 5);`, errors: [{ messageId: "unhandledAwait", data: { timer: "setTimeout" } }] },
      ],
    });
  });
});
