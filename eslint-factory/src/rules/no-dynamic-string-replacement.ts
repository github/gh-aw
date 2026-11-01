import { ASTUtils, ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

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

    function unwrapExpression(node: TSESTree.Expression): TSESTree.Expression {
      switch (node.type) {
        case "ChainExpression":
        case "TSAsExpression":
        case "TSNonNullExpression":
        case "TSTypeAssertion":
        case "TSInstantiationExpression":
          return unwrapExpression(node.expression);
        default:
          return node;
      }
    }

    function resolveVariable(node: TSESTree.Identifier): TSESLint.Scope.Variable | null {
      return ASTUtils.findVariable(sourceCode.getScope(node), node);
    }

    function hasNonInitializingWrite(variable: TSESLint.Scope.Variable): boolean {
      return variable.references.some(reference => reference.isWrite() && !reference.init);
    }

    function getVariableInitializer(variable: TSESLint.Scope.Variable): TSESTree.Expression | null {
      if (variable.defs.length !== 1 || variable.defs[0].type !== "Variable") {
        return null;
      }
      const declaration = variable.defs[0].node as TSESTree.VariableDeclarator;
      return declaration.init ?? null;
    }

    function getStaticMemberName(node: TSESTree.MemberExpression): string | null {
      return getStaticPropertyName(node.property, node.computed);
    }

    function getStaticPropertyName(node: TSESTree.Expression | TSESTree.PrivateIdentifier, computed: boolean): string | null {
      if (node.type === "PrivateIdentifier") {
        return null;
      }
      if (node.type === "Identifier") {
        return computed ? null : node.name;
      }
      return node.type === "Literal" && typeof node.value === "string" ? node.value : null;
    }

    function isOnlyReadThroughMember(variable: TSESLint.Scope.Variable, propertyName: string): boolean {
      return variable.references.every(reference => {
        if (reference.isWrite() && !reference.init) {
          return false;
        }
        if (reference.init) {
          return true;
        }
        const ancestors = sourceCode.getAncestors(reference.identifier);
        const member = ancestors.at(-1);
        const parent = ancestors.at(-2);
        return member?.type === "MemberExpression" && member.object === reference.identifier && getStaticMemberName(member) === propertyName && !(parent?.type === "AssignmentExpression" && parent.left === member);
      });
    }

    function isKnownFunction(node: TSESTree.Expression, resolving: Set<TSESLint.Scope.Variable> = new Set()): boolean {
      const expression = unwrapExpression(node);
      switch (expression.type) {
        case "ArrowFunctionExpression":
        case "FunctionExpression":
          return true;
        case "Identifier": {
          const variable = resolveVariable(expression);
          if (!variable || variable.defs.length !== 1 || resolving.has(variable) || hasNonInitializingWrite(variable)) {
            return false;
          }
          const definition = variable.defs[0];
          if (definition?.type === "FunctionName") {
            return true;
          }
          const initializer = getVariableInitializer(variable);
          if (!initializer) {
            return false;
          }
          resolving.add(variable);
          const result = isKnownFunction(initializer, resolving);
          resolving.delete(variable);
          return result;
        }
        case "MemberExpression": {
          const propertyName = getStaticMemberName(expression);
          if (!propertyName || expression.object.type !== "Identifier") {
            return false;
          }
          const variable = resolveVariable(expression.object);
          if (!variable || variable.defs.length !== 1 || hasNonInitializingWrite(variable) || !isOnlyReadThroughMember(variable, propertyName)) {
            return false;
          }
          const initializer = getVariableInitializer(variable);
          if (!initializer || initializer.type !== "ObjectExpression") {
            return false;
          }
          const properties = initializer.properties.filter(property => property.type === "Property" && getStaticPropertyName(property.key, property.computed) === propertyName);
          if (properties.length !== 1 || properties[0].type !== "Property" || properties[0].kind !== "init") {
            return false;
          }
          if (properties[0].method) {
            return true;
          }
          const value = properties[0].value;
          return value.type !== "AssignmentPattern" && value.type !== "ArrayPattern" && value.type !== "ObjectPattern" && isKnownFunction(value as TSESTree.Expression, resolving);
        }
        case "LogicalExpression":
          return isKnownFunction(expression.left, resolving) && isKnownFunction(expression.right, resolving);
        case "ConditionalExpression":
          return isKnownFunction(expression.consequent, resolving) && isKnownFunction(expression.alternate, resolving);
        default:
          return false;
      }
    }

    function isDynamic(node: TSESTree.Expression): boolean {
      const expression = unwrapExpression(node);
      if (isKnownFunction(expression)) {
        return false;
      }
      switch (expression.type) {
        case "Identifier":
          return expression.name !== "undefined";
        case "MemberExpression":
          return true;
        case "TemplateLiteral":
          return expression.expressions.length > 0;
        case "CallExpression":
          return true;
        case "BinaryExpression":
          return isDynamicOperand(expression.left) || isDynamicOperand(expression.right);
        case "LogicalExpression":
          return isDynamic(expression.left) || isDynamic(expression.right);
        case "ConditionalExpression":
          return isDynamic(expression.consequent) || isDynamic(expression.alternate);
        case "SequenceExpression":
          return expression.expressions.length > 0 && isDynamic(expression.expressions[expression.expressions.length - 1]);
        case "UnaryExpression":
          return isDynamic(expression.argument);
        default:
          return false;
      }
    }

    function isDefinitelyNonFunction(node: TSESTree.Expression, resolving: Set<TSESLint.Scope.Variable> = new Set()): boolean {
      const expression = unwrapExpression(node);
      switch (expression.type) {
        case "Literal":
        case "TemplateLiteral":
        case "BinaryExpression":
        case "UnaryExpression":
          return true;
        case "Identifier": {
          const variable = resolveVariable(expression);
          if (!variable || resolving.has(variable) || hasNonInitializingWrite(variable)) {
            return false;
          }
          const initializer = getVariableInitializer(variable);
          if (!initializer) {
            return false;
          }
          resolving.add(variable);
          const result = isDefinitelyNonFunction(initializer, resolving);
          resolving.delete(variable);
          return result;
        }
        case "LogicalExpression":
          return isDefinitelyNonFunction(expression.left, resolving) && isDefinitelyNonFunction(expression.right, resolving);
        case "ConditionalExpression":
          return isDefinitelyNonFunction(expression.consequent, resolving) && isDefinitelyNonFunction(expression.alternate, resolving);
        case "SequenceExpression":
          return expression.expressions.length > 0 && isDefinitelyNonFunction(expression.expressions[expression.expressions.length - 1], resolving);
        default:
          return false;
      }
    }

    function hasStaticReplacementToken(node: TSESTree.Expression): boolean {
      const expression = unwrapExpression(node);
      switch (expression.type) {
        case "Literal":
          return typeof expression.value === "string" && expression.value.includes("$");
        case "TemplateLiteral":
          return expression.quasis.some(quasi => quasi.value.raw.includes("$"));
        case "BinaryExpression":
          return hasStaticReplacementTokenOperand(expression.left) || hasStaticReplacementTokenOperand(expression.right);
        case "LogicalExpression":
          return hasStaticReplacementToken(expression.left) || hasStaticReplacementToken(expression.right);
        case "ConditionalExpression":
          return hasStaticReplacementToken(expression.consequent) || hasStaticReplacementToken(expression.alternate);
        case "SequenceExpression":
          return expression.expressions.length > 0 && hasStaticReplacementToken(expression.expressions[expression.expressions.length - 1]);
        default:
          return false;
      }
    }

    function isDynamicOperand(node: TSESTree.Expression | TSESTree.PrivateIdentifier): boolean {
      return node.type !== "PrivateIdentifier" && isDynamic(node);
    }

    function hasStaticReplacementTokenOperand(node: TSESTree.Expression | TSESTree.PrivateIdentifier): boolean {
      return node.type !== "PrivateIdentifier" && hasStaticReplacementToken(node);
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
        const canSuggest = isDefinitelyNonFunction(replacement) && !hasStaticReplacementToken(replacement);
        context.report({
          node: replacement,
          messageId: "dynamicReplacement",
          data: { method },
          suggest: canSuggest
            ? [
                {
                  messageId: "useReplacerFunction",
                  fix(fixer: TSESLint.RuleFixer) {
                    return fixer.replaceText(replacement, `((_replacement) => () => _replacement)(${sourceCode.getText(replacement)})`);
                  },
                },
              ]
            : undefined,
        });
      },
    };
  },
});
