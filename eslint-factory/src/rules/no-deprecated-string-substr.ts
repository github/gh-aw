import { ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

export const noDeprecatedStringSubstrRule = createRule({
  name: "no-deprecated-string-substr",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow the deprecated String.prototype.substr(); its (start, length) signature is easily confused with slice/substring.",
    },
    schema: [],
    messages: {
      noSubstr: "String.prototype.substr() is deprecated (Annex B) and takes (start, length). Use .slice(start, start + length) or .substring(...) instead.",
    },
  },
  defaultOptions: [],
  create(context) {
    function isSubstrProperty(node: TSESTree.MemberExpression): boolean {
      const property = node.property;
      if (!node.computed) {
        return property.type === "Identifier" && property.name === "substr";
      }
      return property.type === "Literal" && property.value === "substr";
    }

    return {
      CallExpression(node) {
        const callee = node.callee;
        if (callee.type !== "MemberExpression" || !isSubstrProperty(callee)) {
          return;
        }
        // substr always needs at least a start argument; zero-arg calls are some other API.
        if (node.arguments.length === 0 || node.arguments.length > 2) {
          return;
        }
        context.report({ node: callee.property, messageId: "noSubstr" });
      },
    };
  },
});
