import { AST_NODE_TYPES, ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const UNSAFE_PROPERTIES = new Set(["message", "stack", "code", "status", "cause", "name"]);

interface CatchFrame {
  varName: string;
  param?: TSESTree.Identifier;
  safeCalls: TSESTree.CallExpression[];
  unsafeNodes: Array<{ node: TSESTree.MemberExpression; prop: string; varName: string }>;
}

function isInstanceofExprCheck(node: TSESTree.Expression, varName: string): boolean {
  return node.type === AST_NODE_TYPES.BinaryExpression && node.operator === "instanceof" && node.left.type === AST_NODE_TYPES.Identifier && node.left.name === varName;
}

function isTerminating(stmt: TSESTree.Statement): boolean {
  if (stmt.type === AST_NODE_TYPES.ReturnStatement || stmt.type === AST_NODE_TYPES.ThrowStatement) return true;
  if (stmt.type === AST_NODE_TYPES.BlockStatement) return stmt.body.length > 0 && isTerminating(stmt.body[stmt.body.length - 1]);
  return false;
}

function isTypeofObjectCheck(node: TSESTree.Expression, varName: string): boolean {
  if (node.type !== AST_NODE_TYPES.BinaryExpression || node.operator !== "===") return false;
  const { left, right } = node;
  return (
    (left.type === AST_NODE_TYPES.UnaryExpression && left.operator === "typeof" && left.argument.type === AST_NODE_TYPES.Identifier && left.argument.name === varName && right.type === AST_NODE_TYPES.Literal && right.value === "object") ||
    (right.type === AST_NODE_TYPES.UnaryExpression && right.operator === "typeof" && right.argument.type === AST_NODE_TYPES.Identifier && right.argument.name === varName && left.type === AST_NODE_TYPES.Literal && left.value === "object")
  );
}

function isNonNullGuardCheck(node: TSESTree.Expression, varName: string): boolean {
  if (node.type === AST_NODE_TYPES.Identifier) {
    return node.name === varName;
  }

  if (node.type !== AST_NODE_TYPES.BinaryExpression || (node.operator !== "!==" && node.operator !== "!=")) {
    return false;
  }

  const isVarRef = (value: TSESTree.Expression): value is TSESTree.Identifier => value.type === AST_NODE_TYPES.Identifier && value.name === varName;
  const isNullLiteral = (value: TSESTree.Expression): value is TSESTree.Literal => value.type === AST_NODE_TYPES.Literal && value.value === null;

  return (isVarRef(node.left) && isNullLiteral(node.right)) || (isVarRef(node.right) && isNullLiteral(node.left));
}

function getNearestBlockEntry(sourceCode: Readonly<TSESLint.SourceCode>, node: TSESTree.Node): { block: TSESTree.BlockStatement; entry: TSESTree.Statement } | null {
  const ancestors = sourceCode.getAncestors(node);
  let current: TSESTree.Node = node;
  for (let index = ancestors.length - 1; index >= 0; index--) {
    const ancestor = ancestors[index];
    if (ancestor.type === AST_NODE_TYPES.BlockStatement) {
      const entry = ancestor.body.find(stmt => stmt === current);
      if (entry) return { block: ancestor, entry };
    }
    current = ancestor;
  }
  return null;
}

function hasPriorNonNullReturnGuard(sourceCode: Readonly<TSESLint.SourceCode>, anchorNode: TSESTree.Node, varName: string): boolean {
  const location = getNearestBlockEntry(sourceCode, anchorNode);
  if (!location) return false;

  const entryIndex = location.block.body.indexOf(location.entry);
  for (let index = 0; index < entryIndex; index++) {
    const stmt = location.block.body[index];
    if (
      stmt.type === AST_NODE_TYPES.IfStatement &&
      stmt.test.type === AST_NODE_TYPES.UnaryExpression &&
      stmt.test.operator === "!" &&
      stmt.test.argument.type === AST_NODE_TYPES.Identifier &&
      stmt.test.argument.name === varName &&
      stmt.consequent.type === AST_NODE_TYPES.ReturnStatement
    ) {
      return true;
    }
  }

  return false;
}

// Recognizes early-exit instanceof narrowing patterns that precede the access in the same block:
//   if (!(err instanceof Error)) return/throw;   err.stack;
//   if (err instanceof Error) { } else return/throw;   err.stack;
function hasPriorEarlyExitInstanceofGuard(sourceCode: Readonly<TSESLint.SourceCode>, anchorNode: TSESTree.Node, varName: string): boolean {
  const location = getNearestBlockEntry(sourceCode, anchorNode);
  if (!location) return false;

  const entryIndex = location.block.body.indexOf(location.entry);
  for (let index = 0; index < entryIndex; index++) {
    const stmt = location.block.body[index];
    if (stmt.type !== AST_NODE_TYPES.IfStatement) continue;

    const { test, consequent, alternate } = stmt;

    // if (!(err instanceof Error)) return/throw
    if (test.type === AST_NODE_TYPES.UnaryExpression && test.operator === "!" && isInstanceofExprCheck(test.argument, varName) && alternate === null && isTerminating(consequent)) {
      return true;
    }

    // if (err instanceof Error) { ... } else return/throw
    if (isInstanceofExprCheck(test, varName) && alternate !== null && isTerminating(alternate)) {
      return true;
    }
  }

  return false;
}

function isTruthyBranchGuarded(sourceCode: Readonly<TSESLint.SourceCode>, test: TSESTree.Expression, varName: string, guardNode: TSESTree.Node): boolean {
  const conjuncts: TSESTree.Expression[] = [];
  const collectConjuncts = (expr: TSESTree.Expression): void => {
    if (expr.type === AST_NODE_TYPES.LogicalExpression && expr.operator === "&&") {
      collectConjuncts(expr.left);
      collectConjuncts(expr.right);
      return;
    }
    conjuncts.push(expr);
  };
  collectConjuncts(test);

  const hasInstanceof = conjuncts.some(expr => isInstanceofExprCheck(expr, varName));
  if (hasInstanceof) return true;

  const hasTypeofObject = conjuncts.some(expr => isTypeofObjectCheck(expr, varName));
  const hasNonNullGuard = conjuncts.some(expr => isNonNullGuardCheck(expr, varName));

  if (!hasTypeofObject) return false;
  if (hasNonNullGuard) return true;
  return hasPriorNonNullReturnGuard(sourceCode, guardNode, varName);
}

function isGuardedByAncestorBranch(sourceCode: Readonly<TSESLint.SourceCode>, node: TSESTree.MemberExpression, varName: string): boolean {
  const ancestors = sourceCode.getAncestors(node);
  let current: TSESTree.Node = node;
  for (let index = ancestors.length - 1; index >= 0; index--) {
    const ancestor = ancestors[index];

    if (ancestor.type === AST_NODE_TYPES.IfStatement && ancestor.consequent === current) {
      if (isTruthyBranchGuarded(sourceCode, ancestor.test, varName, ancestor)) return true;
    }

    if (ancestor.type === AST_NODE_TYPES.ConditionalExpression && ancestor.consequent === current) {
      if (isTruthyBranchGuarded(sourceCode, ancestor.test, varName, ancestor)) return true;
    }

    if (ancestor.type === AST_NODE_TYPES.LogicalExpression && ancestor.operator === "&&" && ancestor.right === current) {
      if (isTruthyBranchGuarded(sourceCode, ancestor.left, varName, ancestor)) return true;
    }

    current = ancestor;
  }

  return false;
}

function isCallOrderingGuarded(sourceCode: Readonly<TSESLint.SourceCode>, node: TSESTree.MemberExpression, safeCalls: TSESTree.CallExpression[]): boolean {
  const nodeLocation = getNearestBlockEntry(sourceCode, node);
  if (!nodeLocation) return false;
  const nodeEntryIndex = nodeLocation.block.body.indexOf(nodeLocation.entry);

  for (const callNode of safeCalls) {
    if (callNode.range[1] > node.range[0]) continue;
    const callLocation = getNearestBlockEntry(sourceCode, callNode);
    if (!callLocation || callLocation.block !== nodeLocation.block) continue;

    const callEntryIndex = callLocation.block.body.indexOf(callLocation.entry);
    if (callEntryIndex < nodeEntryIndex) return true;
    if (callEntryIndex === nodeEntryIndex && callNode.range[1] <= node.range[0]) return true;
  }

  return false;
}

export const noUnsafeCatchErrorPropertyRule = createRule({
  name: "no-unsafe-catch-error-property",
  meta: {
    type: "problem",
    hasSuggestions: true,
    docs: {
      description: "Disallow direct access to .message, .stack, .code, .status, .cause, or .name on a caught error variable without a getErrorMessage guard",
    },
    schema: [],
    messages: {
      unsafeProperty: "Direct access to .{{prop}} on caught error '{{errorVar}}' is unsafe — the thrown value may not be an Error instance.",
      useGetErrorMessage: "Replace with getErrorMessage({{errorVar}}) from error_helpers.cjs for safe error message extraction.",
      wrapWithInstanceof: "Wrap with '({{errorVar}} instanceof Error ? {{errorVar}}.{{prop}} : undefined)' to guard against non-Error throws.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    const catchStack: CatchFrame[] = [];

    function resolveVariable(identifier: TSESTree.Identifier): TSESLint.Scope.Variable | undefined {
      let scope: TSESLint.Scope.Scope | null = sourceCode.getScope(identifier);
      while (scope) {
        const variable = scope.set.get(identifier.name);
        if (variable) return variable;
        scope = scope.upper;
      }
      return undefined;
    }

    function caughtName(identifier: TSESTree.Identifier, frame: CatchFrame): string | null {
      if (!frame.param) return null;
      const caught = resolveVariable(frame.param);
      const variable = resolveVariable(identifier);
      if (!caught || !variable) return null;
      if (variable === caught) return identifier.name;
      if (variable.defs.length !== 1 || variable.references.some(ref => ref.isWrite() && !ref.init)) return null;
      const def = variable.defs[0];
      if (def.type !== "Variable" || def.parent.kind !== "const" || def.node.id.type !== AST_NODE_TYPES.Identifier) return null;
      const init = def.node.init;
      return init?.type === AST_NODE_TYPES.Identifier && resolveVariable(init) === caught ? identifier.name : null;
    }

    return {
      CatchClause(node) {
        const param = node.param;

        // Only handle simple identifier bindings; skip bare catch {} and destructuring patterns.
        // Push a sentinel frame so CatchClause:exit always has a matching pop.
        if (!param || param.type !== AST_NODE_TYPES.Identifier) {
          catchStack.push({ varName: "", safeCalls: [], unsafeNodes: [] });
          return;
        }

        catchStack.push({ varName: param.name, param, safeCalls: [], unsafeNodes: [] });
      },

      "CatchClause:exit"() {
        const frame = catchStack.pop();
        if (!frame || !frame.varName) return;

        for (const { node: memberExpr, prop, varName } of frame.unsafeNodes) {
          if (
            isGuardedByAncestorBranch(sourceCode, memberExpr, varName) ||
            hasPriorEarlyExitInstanceofGuard(sourceCode, memberExpr, varName) ||
            (varName !== frame.varName && (isGuardedByAncestorBranch(sourceCode, memberExpr, frame.varName) || hasPriorEarlyExitInstanceofGuard(sourceCode, memberExpr, frame.varName))) ||
            isCallOrderingGuarded(sourceCode, memberExpr, frame.safeCalls)
          ) {
            continue;
          }

          const parent = memberExpr.parent;
          const isChained =
            parent != null &&
            ((parent.type === AST_NODE_TYPES.MemberExpression && (parent as TSESTree.MemberExpression).object === memberExpr) || (parent.type === AST_NODE_TYPES.CallExpression && (parent as TSESTree.CallExpression).callee === memberExpr));
          context.report({
            node: memberExpr,
            messageId: "unsafeProperty",
            data: { prop, errorVar: varName },
            suggest:
              prop === "message"
                ? [
                    {
                      messageId: "useGetErrorMessage" as const,
                      data: { errorVar: varName },
                      fix(fixer) {
                        return fixer.replaceText(memberExpr, `getErrorMessage(${varName})`);
                      },
                    },
                  ]
                : isChained
                  ? []
                  : [
                      {
                        messageId: "wrapWithInstanceof" as const,
                        data: { errorVar: varName, prop },
                        fix(fixer) {
                          return fixer.replaceText(memberExpr, `(${varName} instanceof Error ? ${varName}.${prop} : undefined)`);
                        },
                      },
                    ],
          });
        }
      },

      // Detect getErrorMessage(catchVar) call — accepted safe guard
      CallExpression(node) {
        if (catchStack.length === 0) return;
        const top = catchStack[catchStack.length - 1];
        if (!top || !top.varName) return;

        const firstArg = node.arguments[0];
        if (node.callee.type === AST_NODE_TYPES.Identifier && node.callee.name === "getErrorMessage" && firstArg?.type === AST_NODE_TYPES.Identifier && caughtName(firstArg, top)) {
          top.safeCalls.push(node);
        }
      },

      // Collect catchVar.message / catchVar.stack / catchVar.code / catchVar.status / catchVar.cause / catchVar.name accesses
      // Also detects computed string-literal access: catchVar["message"], catchVar["status"], etc.
      MemberExpression(node) {
        if (catchStack.length === 0) return;
        const top = catchStack[catchStack.length - 1];
        if (!top || !top.varName) return;

        const obj = node.object;
        const prop = node.property;

        if (obj.type !== AST_NODE_TYPES.Identifier || !caughtName(obj, top)) return;

        // Non-computed dot access: err.message / err.stack / err.code / err.status / err.cause / err.name
        if (!node.computed && prop.type === AST_NODE_TYPES.Identifier && UNSAFE_PROPERTIES.has(prop.name)) {
          top.unsafeNodes.push({ node, prop: prop.name, varName: obj.name });
          return;
        }

        // Computed string-literal access: err["message"] / err["stack"] / err["status"] / etc.
        // Dynamic access (err[prop]) is kept out of scope intentionally.
        if (node.computed && prop.type === AST_NODE_TYPES.Literal && typeof prop.value === "string" && UNSAFE_PROPERTIES.has(prop.value)) {
          top.unsafeNodes.push({ node, prop: prop.value, varName: obj.name });
        }
      },
    };
  },
});
