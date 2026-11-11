import { AST_NODE_TYPES, ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const EQUALITY_OPERATORS = new Set(["===", "!==", "==", "!="]);

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
  if (!EQUALITY_OPERATORS.has(node.operator)) return null;
  const isObjectLiteral = (n: TSESTree.Node) => n.type === AST_NODE_TYPES.Literal && n.value === "object";
  const typeofSide = isObjectLiteral(node.right) ? node.left : isObjectLiteral(node.left) ? node.right : null;
  if (!typeofSide || typeofSide.type !== AST_NODE_TYPES.UnaryExpression || typeofSide.operator !== "typeof") return null;
  const text = subjectText(typeofSide.argument);
  return text === null ? null : { text, operator: node.operator };
}

/** True when `node` is an expression that tests `text` for null/undefined or truthiness. */
function isNullGuard(node: TSESTree.Node, text: string): boolean {
  if (node.type === AST_NODE_TYPES.BinaryExpression && EQUALITY_OPERATORS.has(node.operator)) {
    const other = isNullish(node.right) ? node.left : isNullish(node.left) ? node.right : null;
    return other !== null && subjectText(other) === text;
  }
  if (subjectText(node) !== text) return false;
  const parent = node.parent;
  if (!parent) return false;
  if (parent.type === AST_NODE_TYPES.LogicalExpression) return true;
  if (parent.type === AST_NODE_TYPES.UnaryExpression) return parent.operator === "!";
  if (parent.type === AST_NODE_TYPES.CallExpression) return parent.callee.type === AST_NODE_TYPES.Identifier && parent.callee.name === "Boolean" && parent.arguments[0] === node;
  if (parent.type === AST_NODE_TYPES.IfStatement || parent.type === AST_NODE_TYPES.ConditionalExpression || parent.type === AST_NODE_TYPES.WhileStatement) return parent.test === node;
  return false;
}

function outermostLogicalChain(node: TSESTree.Node): TSESTree.Node {
  let current = node;
  while (current.parent && (current.parent.type === AST_NODE_TYPES.LogicalExpression || (current.parent.type === AST_NODE_TYPES.UnaryExpression && current.parent.operator === "!"))) {
    current = current.parent;
  }
  return current;
}

function enclosingFunctionOrProgram(node: TSESTree.Node): TSESTree.Node {
  let current: TSESTree.Node | undefined = node.parent;
  while (current) {
    if (current.type === AST_NODE_TYPES.FunctionDeclaration || current.type === AST_NODE_TYPES.FunctionExpression || current.type === AST_NODE_TYPES.ArrowFunctionExpression || current.type === AST_NODE_TYPES.Program) {
      return current;
    }
    current = current.parent;
  }
  return node;
}

/** Depth-first walk of `root`, skipping `skip`, returning true as soon as `visit` matches a node whose range ends at or before `endLimit`. */
function someNode(root: TSESTree.Node, skip: TSESTree.Node, endLimit: number, visit: (node: TSESTree.Node) => boolean): boolean {
  const stack: TSESTree.Node[] = [root];
  while (stack.length > 0) {
    const node = stack.pop()!;
    if (node === skip) continue;
    if (node.range[1] <= endLimit && visit(node)) return true;
    for (const key of Object.keys(node)) {
      if (key === "parent") continue;
      const value = (node as unknown as Record<string, unknown>)[key];
      const children = Array.isArray(value) ? value : [value];
      for (const child of children) {
        if (child && typeof (child as TSESTree.Node).type === "string") stack.push(child as TSESTree.Node);
      }
    }
  }
  return false;
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
        '`typeof {{subject}} {{operator}} "object"` is also satisfied by `null`. Check `{{subject}}` for null (or truthiness) in the same condition or earlier in the function; otherwise a null value is treated as an object and later property access throws.',
    },
  },
  defaultOptions: [],
  create(context: Readonly<TSESLint.RuleContext<"missingNullCheck", []>>) {
    return {
      BinaryExpression(node: TSESTree.BinaryExpression) {
        const match = matchTypeofObject(node);
        if (!match) return;

        const guard = (candidate: TSESTree.Node) => isNullGuard(candidate, match.text);
        if (someNode(outermostLogicalChain(node), node, Infinity, guard)) return;
        if (someNode(enclosingFunctionOrProgram(node), node, node.range[0], guard)) return;

        context.report({
          node,
          messageId: "missingNullCheck",
          data: { subject: match.text, operator: match.operator },
        });
      },
    };
  },
});
