import { AST_NODE_TYPES, ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const SERIALIZERS = new Set(["toISOString", "toJSON"]);
const GUARD_NAMES = new Set(["isNaN", "isFinite"]);

function isDateNowCall(node: TSESTree.Node): boolean {
  return (
    node.type === AST_NODE_TYPES.CallExpression &&
    node.callee.type === AST_NODE_TYPES.MemberExpression &&
    !node.callee.computed &&
    node.callee.object.type === AST_NODE_TYPES.Identifier &&
    node.callee.object.name === "Date" &&
    node.callee.property.type === AST_NODE_TYPES.Identifier &&
    node.callee.property.name === "now"
  );
}

/** True for expressions that cannot yield an invalid time value: literals, Date.now(), and arithmetic over those and UPPER_CASE constants. */
function isTriviallyValidTime(node: TSESTree.Node): boolean {
  if (node.type === AST_NODE_TYPES.Literal) return typeof node.value === "number";
  if (isDateNowCall(node)) return true;
  if (node.type === AST_NODE_TYPES.Identifier) return /^[A-Z][A-Z0-9_]*$/.test(node.name);
  if (node.type === AST_NODE_TYPES.BinaryExpression && ["+", "-", "*", "/"].includes(node.operator)) {
    return isTriviallyValidTime(node.left) && isTriviallyValidTime(node.right);
  }
  return false;
}

function collectIdentifierNames(node: TSESTree.Node, names: Set<string>): void {
  if (node.type === AST_NODE_TYPES.Identifier) names.add(node.name);
  else if (node.type === AST_NODE_TYPES.MemberExpression) {
    collectIdentifierNames(node.object, names);
  } else if (node.type === AST_NODE_TYPES.BinaryExpression || node.type === AST_NODE_TYPES.LogicalExpression) {
    collectIdentifierNames(node.left, names);
    collectIdentifierNames(node.right, names);
  } else if (node.type === AST_NODE_TYPES.CallExpression) {
    node.arguments.forEach(arg => collectIdentifierNames(arg, names));
  } else if (node.type === AST_NODE_TYPES.ChainExpression) {
    collectIdentifierNames(node.expression, names);
  }
}

function isGuardCall(node: TSESTree.CallExpression): boolean {
  const { callee } = node;
  if (callee.type === AST_NODE_TYPES.Identifier) return GUARD_NAMES.has(callee.name);
  return (
    callee.type === AST_NODE_TYPES.MemberExpression &&
    !callee.computed &&
    callee.object.type === AST_NODE_TYPES.Identifier &&
    callee.object.name === "Number" &&
    callee.property.type === AST_NODE_TYPES.Identifier &&
    GUARD_NAMES.has(callee.property.name)
  );
}

export const requireFiniteCheckBeforeDateToIsoStringRule = createRule({
  name: "require-finite-check-before-date-toisostring",
  meta: {
    type: "problem",
    docs: {
      description: "Require dynamic values passed to new Date(...) to be validated before calling toISOString()/toJSON(), which throw RangeError for invalid dates.",
    },
    schema: [],
    messages: {
      requireFiniteCheck: "new Date({{arg}}).{{method}}() throws RangeError when the value is NaN, out of range, or an unparseable string. Validate the input with Number.isFinite()/Number.isNaN() or wrap the call in try/catch.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;

    function isInsideTryBlock(node: TSESTree.Node): boolean {
      const ancestors = sourceCode.getAncestors(node);
      for (let i = ancestors.length - 1; i >= 0; i--) {
        const a = ancestors[i];
        if (a.type === AST_NODE_TYPES.FunctionDeclaration || a.type === AST_NODE_TYPES.FunctionExpression || a.type === AST_NODE_TYPES.ArrowFunctionExpression) return false;
        if (a.type === AST_NODE_TYPES.TryStatement && a.handler != null) {
          const child = ancestors[i + 1] ?? node;
          if (child === a.block) return true;
        }
      }
      return false;
    }

    function enclosingScopeNode(node: TSESTree.Node): TSESTree.Node {
      const ancestors = sourceCode.getAncestors(node);
      for (let i = ancestors.length - 1; i >= 0; i--) {
        const a = ancestors[i];
        if (a.type === AST_NODE_TYPES.FunctionDeclaration || a.type === AST_NODE_TYPES.FunctionExpression || a.type === AST_NODE_TYPES.ArrowFunctionExpression) return a;
      }
      return ancestors[0];
    }

    /** True when the enclosing function contains a finite/NaN check involving any identifier used in the date argument. */
    function hasGuard(node: TSESTree.Node, arg: TSESTree.Node): boolean {
      const argNames = new Set<string>();
      collectIdentifierNames(arg, argNames);
      if (argNames.size === 0) return false;
      let found = false;
      const visit = (n: TSESTree.Node): void => {
        if (found) return;
        if (n.type === AST_NODE_TYPES.CallExpression && isGuardCall(n)) {
          const names = new Set<string>();
          n.arguments.forEach(a => collectIdentifierNames(a, names));
          for (const name of names) {
            if (argNames.has(name)) {
              found = true;
              return;
            }
          }
        }
        for (const key of Object.keys(n) as Array<keyof typeof n>) {
          if (key === "parent") continue;
          const value = n[key] as unknown;
          if (Array.isArray(value)) value.forEach(v => v && typeof (v as TSESTree.Node).type === "string" && visit(v as TSESTree.Node));
          else if (value && typeof (value as TSESTree.Node).type === "string") visit(value as TSESTree.Node);
        }
      };
      visit(enclosingScopeNode(node));
      return found;
    }

    return {
      CallExpression(node: TSESTree.CallExpression) {
        const callee = node.callee;
        if (callee.type !== AST_NODE_TYPES.MemberExpression || callee.computed || callee.property.type !== AST_NODE_TYPES.Identifier || !SERIALIZERS.has(callee.property.name)) return;
        const receiver = callee.object;
        if (receiver.type !== AST_NODE_TYPES.NewExpression || receiver.callee.type !== AST_NODE_TYPES.Identifier || receiver.callee.name !== "Date") return;
        const arg = receiver.arguments[0];
        if (!arg || arg.type === AST_NODE_TYPES.SpreadElement || isTriviallyValidTime(arg)) return;
        if (isInsideTryBlock(node) || hasGuard(node, arg)) return;
        context.report({
          node: receiver,
          messageId: "requireFiniteCheck",
          data: { arg: sourceCode.getText(arg), method: callee.property.name },
        });
      },
    };
  },
});
