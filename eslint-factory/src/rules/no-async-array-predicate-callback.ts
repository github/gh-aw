import { ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const PREDICATE_METHODS = new Set(["filter", "some", "every", "find", "findIndex", "findLast", "findLastIndex"]);

export const noAsyncArrayPredicateCallbackRule = createRule({
  name: "no-async-array-predicate-callback",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow async callbacks passed to filter/some/every/find/findIndex; the returned Promise is always truthy, so the predicate never filters.",
    },
    schema: [],
    messages: {
      asyncPredicate: "`{{method}}()` receives an async callback, which returns a Promise that is always truthy, so the predicate has no effect. Resolve the conditions first with `await Promise.all(items.map(async ...))`, then filter synchronously.",
    },
  },
  defaultOptions: [],
  create(context) {
    return {
      CallExpression(node: TSESTree.CallExpression) {
        const callee = node.callee;
        if (callee.type !== "MemberExpression" || callee.computed || callee.property.type !== "Identifier" || !PREDICATE_METHODS.has(callee.property.name)) {
          return;
        }
        const callback = node.arguments[0];
        if (!callback || (callback.type !== "ArrowFunctionExpression" && callback.type !== "FunctionExpression") || !callback.async) {
          return;
        }
        context.report({ node: callback, messageId: "asyncPredicate", data: { method: callee.property.name } });
      },
    };
  },
});
