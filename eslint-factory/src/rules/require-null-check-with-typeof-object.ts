import { AST_NODE_TYPES, ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const EQUALITY_OPERATORS = new Set(["===", "!==", "==", "!="]);
const TYPEOF_OBJECT_OPERATORS = new Set(["===", "=="]);

/** Source-level name of an identifier or non-computed member chain (optional chaining ignored); null for any other expression. */
function subjectText(node: TSESTree.Node): string | null {
  let current: TSESTree.Node = node;
  while (current.type === AST_NODE_TYPES.ChainExpression) current = current.expression;
  if (current.type === AST_NODE_TYPES.Identifier) return current.name;
  if (current.type === AST_NODE_TYPES.ThisExpression) return "this";
  if (current.type === AST_NODE_TYPES.MemberExpression && !current.computed && current.property.type === AST_NODE_TYPES.Identifier) {
    const objectText = subjectText(current.object);
    return objectText === null ? null : `${objectText}.${current.property.name}`;
  }
  return null;
}

function isNullish(node: TSESTree.Node): boolean {
  return (node.type === AST_NODE_TYPES.Literal && node.value === null && node.raw === "null") || (node.type === AST_NODE_TYPES.Identifier && node.name === "undefined");
}

/** Matches `typeof <subject> <op> "object"` in either operand order. */
function matchTypeofObject(node: TSESTree.BinaryExpression): { text: string; operator: string } | null {
  if (!TYPEOF_OBJECT_OPERATORS.has(node.operator)) return null;
  const isObjectLiteral = (n: TSESTree.Node) => n.type === AST_NODE_TYPES.Literal && n.value === "object";
  const typeofSide = isObjectLiteral(node.right) ? node.left : isObjectLiteral(node.left) ? node.right : null;
  if (!typeofSide || typeofSide.type !== AST_NODE_TYPES.UnaryExpression || typeofSide.operator !== "typeof") return null;
  const text = subjectText(typeofSide.argument);
  return text === null ? null : { text, operator: node.operator };
}

type GuardEffect = "nonNull" | "nullish" | null;

/** Describes whether a true result from `node` proves `text` non-null or nullish. */
function guardEffect(node: TSESTree.Node, text: string): GuardEffect {
  if (node.type === AST_NODE_TYPES.UnaryExpression && node.operator === "!") {
    const effect = guardEffect(node.argument, text);
    return effect === "nonNull" ? "nullish" : effect === "nullish" ? "nonNull" : null;
  }
  if (node.type === AST_NODE_TYPES.CallExpression && node.callee.type === AST_NODE_TYPES.Identifier && node.callee.name === "Boolean" && node.arguments.length === 1) {
    return subjectText(node.arguments[0]) === text ? "nonNull" : null;
  }
  if (node.type === AST_NODE_TYPES.BinaryExpression && EQUALITY_OPERATORS.has(node.operator)) {
    const other = isNullish(node.right) ? node.left : isNullish(node.left) ? node.right : null;
    if (other === null || subjectText(other) !== text) return null;
    const nullLiteral = (node.left.type === AST_NODE_TYPES.Literal && node.left.value === null) || (node.right.type === AST_NODE_TYPES.Literal && node.right.value === null);
    if (nullLiteral) {
      return node.operator === "!=" || node.operator === "!==" ? "nonNull" : "nullish";
    }
    if (node.operator === "==") return "nullish";
    if (node.operator === "!=") return "nonNull";
    return null;
  }
  return subjectText(node) === text ? "nonNull" : null;
}

/** True when a conjunction guards the `typeof` check with a test for `text`. */
function hasSameExpressionGuard(node: TSESTree.Node, text: string): boolean {
  let chain = node;
  while (chain.parent?.type === AST_NODE_TYPES.LogicalExpression && chain.parent.operator === "&&") {
    chain = chain.parent;
  }
  const operands: TSESTree.Node[] = [];
  const flattenAnd = (current: TSESTree.Node) => {
    if (current.type === AST_NODE_TYPES.LogicalExpression && current.operator === "&&") {
      flattenAnd(current.left);
      flattenAnd(current.right);
    } else {
      operands.push(current);
    }
  };
  flattenAnd(chain);
  const nodeIndex = operands.indexOf(node);
  const isGuardSyntax = (candidate: TSESTree.Node) =>
    guardEffect(candidate, text) !== null ||
    (candidate.type === AST_NODE_TYPES.BinaryExpression &&
      EQUALITY_OPERATORS.has(candidate.operator) &&
      ((isNullish(candidate.left) && subjectText(candidate.right) === text) || (isNullish(candidate.right) && subjectText(candidate.left) === text)));
  if (nodeIndex >= 0) {
    for (let index = 0; index < operands.length; index++) {
      if (index === nodeIndex || guardEffect(operands[index], text) !== "nonNull") continue;
      if (index < nodeIndex || operands.slice(nodeIndex + 1, index).every(isGuardSyntax)) return true;
    }
  }

  return false;
}

function enclosingFunction(node: TSESTree.Node): TSESTree.FunctionDeclaration | TSESTree.FunctionExpression | TSESTree.ArrowFunctionExpression | null {
  let current: TSESTree.Node | undefined = node.parent;
  while (current) {
    if (current.type === AST_NODE_TYPES.FunctionDeclaration || current.type === AST_NODE_TYPES.FunctionExpression || current.type === AST_NODE_TYPES.ArrowFunctionExpression) {
      return current;
    }
    current = current.parent;
  }
  return null;
}

function exitsUnconditionally(statement: TSESTree.Statement): boolean {
  if (statement.type === AST_NODE_TYPES.ReturnStatement || statement.type === AST_NODE_TYPES.ThrowStatement) return true;
  return statement.type === AST_NODE_TYPES.BlockStatement && statement.body.length > 0 && exitsUnconditionally(statement.body[statement.body.length - 1]);
}

/** Earlier guards count only when a top-level early exit excludes null before this check. */
function hasEarlierExitGuard(node: TSESTree.Node, text: string): boolean {
  const fn = enclosingFunction(node);
  if (!fn || fn.body.type !== AST_NODE_TYPES.BlockStatement) return false;
  return fn.body.body.some(statement => statement.range[1] < node.range[0] && statement.type === AST_NODE_TYPES.IfStatement && guardEffect(statement.test, text) === "nullish" && exitsUnconditionally(statement.consequent));
}

export const requireNullCheckWithTypeofObjectRule = createRule({
  name: "require-null-check-with-typeof-object",
  meta: {
    type: "problem",
    docs: {
      description: 'Require a null check for the same value alongside `typeof value === "object"`, because `typeof null === "object"` and a null value would otherwise be treated as an object.',
    },
    schema: [],
    messages: {
      missingNullCheck:
        '`typeof {{subject}} {{operator}} "object"` is also satisfied by `null`. Guard `{{subject}}` in the same conjunction or exit early for null before this check; otherwise a null value is treated as an object and later property access throws.',
    },
  },
  defaultOptions: [],
  create(context: Readonly<TSESLint.RuleContext<"missingNullCheck", []>>) {
    return {
      BinaryExpression(node: TSESTree.BinaryExpression) {
        const match = matchTypeofObject(node);
        if (!match) return;

        if (hasSameExpressionGuard(node, match.text) || hasEarlierExitGuard(node, match.text)) return;

        context.report({
          node,
          messageId: "missingNullCheck",
          data: { subject: match.text, operator: match.operator },
        });
      },
    };
  },
});
