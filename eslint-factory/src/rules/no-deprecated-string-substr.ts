import { ESLintUtils } from "@typescript-eslint/utils";

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
      noSubstr: "String.prototype.substr() is deprecated (Annex B); migrate while preserving its (start, length) semantics.",
    },
  },
  defaultOptions: [],
  create(context) {
    return {
      CallExpression(node) {
        const callee = node.callee;
        if (callee.type !== "MemberExpression") {
          return;
        }
        const property = callee.property;
        const isSubstrProperty = callee.computed ? property.type === "Literal" && property.value === "substr" : property.type === "Identifier" && property.name === "substr";
        if (!isSubstrProperty) {
          return;
        }
        context.report({ node: callee.property, messageId: "noSubstr" });
      },
    };
  },
});
