import { ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

export const noAsyncForEachCallbackRule = createRule({
  name: "no-async-foreach-callback",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow async callbacks passed to Array.prototype.forEach(); the returned promises are discarded, so awaits are not sequenced and rejections go unhandled.",
    },
    schema: [],
    messages: {
      asyncForEach: "forEach() ignores the promise returned by an async callback, so work is not awaited and rejections are unhandled. Use `for (const x of items) { await ... }` or `await Promise.all(items.map(async ...))`.",
    },
  },
  defaultOptions: [],
  create(context) {
    return {
      CallExpression(node: TSESTree.CallExpression) {
        const callee = node.callee;
        if (callee.type !== "MemberExpression" || callee.computed || callee.property.type !== "Identifier" || callee.property.name !== "forEach") {
          return;
        }
        const callback = node.arguments[0];
        if (!callback || (callback.type !== "ArrowFunctionExpression" && callback.type !== "FunctionExpression") || !callback.async) {
          return;
        }
        context.report({ node: callback, messageId: "asyncForEach" });
      },
    };
  },
});
