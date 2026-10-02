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
      valid: [`items.filter(x => x.ok);`, `Promise.all(items.map(async x => f(x)));`, `items.filter(function (x) { return x; });`, `items.filter(isOk);`, `items.filter();`, `items["filter"](async x => true);`],
      invalid: [
        { code: `items.filter(async x => (await f(x)).ok);`, errors: [{ messageId: "asyncPredicate", data: { method: "filter" } }] },
        { code: `a.b.some(async function (x) { return await f(x); });`, errors: [{ messageId: "asyncPredicate", data: { method: "some" } }] },
        { code: `items.find(async x => f(x));`, errors: [{ messageId: "asyncPredicate", data: { method: "find" } }] },
      ],
    });
  });
});
