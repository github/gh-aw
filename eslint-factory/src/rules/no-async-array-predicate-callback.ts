import { ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const CALLBACK_METHODS = new Set(["forEach", "filter", "some", "every", "find", "findIndex", "findLast", "findLastIndex"]);

export const noAsyncArrayPredicateCallbackRule = createRule({
  name: "no-async-array-predicate-callback",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow async callbacks in Array forEach/filter/some/every/find, where the returned Promise is ignored or always truthy.",
    },
    schema: [],
    messages: {
      asyncCallback: "Async callback passed to .{{method}}() is not awaited (forEach) or always truthy (predicate methods). Use a for...of loop, or resolve with Promise.all() before filtering.",
    },
  },
  defaultOptions: [],
  create(context) {
    return {
      CallExpression(node: TSESTree.CallExpression) {
        const callee = node.callee;
        if (callee.type !== "MemberExpression" || callee.computed || callee.property.type !== "Identifier") {
          return;
        }
        const method = callee.property.name;
        if (!CALLBACK_METHODS.has(method)) {
          return;
        }
        const callback = node.arguments[0];
        if (!callback || (callback.type !== "ArrowFunctionExpression" && callback.type !== "FunctionExpression") || !callback.async || callback.generator) {
          return;
        }
        context.report({ node: callback, messageId: "asyncCallback", data: { method } });
      },
    };
  },
});
