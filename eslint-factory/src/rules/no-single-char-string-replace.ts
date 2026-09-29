import { ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

export const noSingleCharStringReplaceRule = createRule({
  name: "no-single-char-string-replace",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description: "Disallow String.prototype.replace() with a single-character string pattern, which silently replaces only the first occurrence.",
    },
    schema: [],
    messages: {
      singleCharReplace: 'replace("{{pattern}}", ...) only replaces the first occurrence. Use replaceAll() (or a global RegExp) to replace every occurrence.',
      useReplaceAll: "Use replaceAll() instead of replace().",
    },
  },
  defaultOptions: [],
  create(context) {
    return {
      CallExpression(node: TSESTree.CallExpression) {
        const callee = node.callee;
        if (callee.type !== "MemberExpression" || callee.computed || callee.property.type !== "Identifier" || callee.property.name !== "replace") {
          return;
        }
        if (node.arguments.length !== 2) {
          return;
        }
        const pattern = node.arguments[0];
        if (pattern.type !== "Literal" || typeof pattern.value !== "string" || pattern.value.length !== 1) {
          return;
        }
        // Chained/known-string receivers only make sense for strings; arrays and other objects have no replace().
        context.report({
          node: callee.property,
          messageId: "singleCharReplace",
          data: { pattern: pattern.value },
          suggest: [
            {
              messageId: "useReplaceAll",
              fix(fixer: TSESLint.RuleFixer) {
                return fixer.replaceText(callee.property, "replaceAll");
              },
            },
          ],
        });
      },
    };
  },
});
