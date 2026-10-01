import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noAsyncForEachCallbackRule } from "./no-async-foreach-callback";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("no-async-foreach-callback", () => {
  it("uses the correct docs URL", () => {
    expect(noAsyncForEachCallbackRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-async-foreach-callback");
  });

  it("valid and invalid cases", () => {
    ruleTester.run("no-async-foreach-callback", noAsyncForEachCallbackRule, {
      valid: [`items.forEach(x => use(x));`, `Promise.all(items.map(async x => fetchIt(x)));`, `items.forEach(function (x) { use(x); });`, `items.forEach(handler);`, `items.forEach();`, `items["forEach"](async x => {});`],
      invalid: [
        { code: `items.forEach(async x => { await f(x); });`, errors: [{ messageId: "asyncForEach" }] },
        { code: `a.b.forEach(async function (x) { await f(x); });`, errors: [{ messageId: "asyncForEach" }] },
      ],
    });
  });
});
