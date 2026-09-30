import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noAsyncArrayPredicateCallbackRule } from "./no-async-array-predicate-callback";

const ruleTester = new RuleTester({ languageOptions: { ecmaVersion: 2022, sourceType: "commonjs" } });

describe("no-async-array-predicate-callback", () => {
  it("uses the correct docs URL", () => {
    expect(noAsyncArrayPredicateCallbackRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-async-array-predicate-callback");
  });

  it("valid and invalid cases", () => {
    ruleTester.run("no-async-array-predicate-callback", noAsyncArrayPredicateCallbackRule, {
      valid: [
        `items.forEach(item => run(item));`,
        `Promise.all(items.map(async item => run(item)));`,
        `items.filter(function (x) { return x; });`,
        `items.filter(async);`,
        `items["forEach"](async x => x);`,
        `items.reduce(async (acc, x) => acc, null);`,
      ],
      invalid: [
        { code: `items.forEach(async item => { await run(item); });`, errors: [{ messageId: "asyncCallback", data: { method: "forEach" } }] },
        { code: `items.filter(async function (x) { return await ok(x); });`, errors: [{ messageId: "asyncCallback", data: { method: "filter" } }] },
        { code: `items.some(async x => ok(x));`, errors: [{ messageId: "asyncCallback", data: { method: "some" } }] },
        { code: `items.find(async x => ok(x));`, errors: [{ messageId: "asyncCallback", data: { method: "find" } }] },
      ],
    });
  });
});
