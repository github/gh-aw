import { ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

function isFunctionDefinition(def: TSESLint.Scope.Definition): boolean {
  if (def.type === "FunctionName") {
    return true;
  }
  if (def.type === "Variable") {
    const init = (def.node as TSESTree.VariableDeclarator).init;
    return !!init && (init.type === "ArrowFunctionExpression" || init.type === "FunctionExpression");
  }
  return false;
}

export const noDynamicStringReplacementRule = createRule({
  name: "no-dynamic-string-replacement",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description: "Disallow passing a dynamic string as the replacement argument of String.prototype.replace()/replaceAll(), because `$&`, `$1`, `$$` and similar sequences in the value are interpreted as replacement patterns.",
    },
    schema: [],
    messages: {
      dynamicReplacement: "The replacement passed to {{method}}() is a dynamic value; any `$&`, `$1`, `$$`, `` $` `` or `$'` it contains is expanded as a replacement pattern. Pass a replacer function (`() => value`) instead.",
      useReplacerFunction: "Wrap the replacement in a replacer function.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;

    function isKnownFunctionIdentifier(node: TSESTree.Identifier): boolean {
      let scope: TSESLint.Scope.Scope | null = sourceCode.getScope(node);
      while (scope) {
        const variable = scope.set.get(node.name);
        if (variable) {
          return variable.defs.some(isFunctionDefinition);
        }
        scope = scope.upper;
      }
      return false;
    }

    function isDynamic(node: TSESTree.Node): boolean {
      switch (node.type) {
        case "Identifier":
          return node.name !== "undefined" && !isKnownFunctionIdentifier(node);
        case "MemberExpression":
          return true;
        case "TemplateLiteral":
          return node.expressions.length > 0;
        case "CallExpression":
          return true;
        default:
          return false;
      }
    }

    return {
      CallExpression(node: TSESTree.CallExpression) {
        const callee = node.callee;
        if (callee.type !== "MemberExpression" || callee.computed || callee.property.type !== "Identifier") {
          return;
        }
        const method = callee.property.name;
        if (method !== "replace" && method !== "replaceAll") {
          return;
        }
        if (node.arguments.length !== 2) {
          return;
        }
        const [pattern, replacement] = node.arguments;
        if (pattern.type === "SpreadElement" || replacement.type === "SpreadElement") {
          return;
        }
        // A string/RegExp pattern is required for this to be String#replace rather than an unrelated replace() API.
        const isRegexLiteral = pattern.type === "Literal" && "regex" in pattern;
        const isStringPattern = pattern.type === "Literal" && typeof pattern.value === "string";
        if (!isRegexLiteral && !isStringPattern) {
          return;
        }
        if (!isDynamic(replacement)) {
          return;
        }
        context.report({
          node: replacement,
          messageId: "dynamicReplacement",
          data: { method },
          suggest: [
            {
              messageId: "useReplacerFunction",
              fix(fixer: TSESLint.RuleFixer) {
                return fixer.replaceText(replacement, `() => ${sourceCode.getText(replacement)}`);
              },
            },
          ],
        });
      },
    };
  },
});
