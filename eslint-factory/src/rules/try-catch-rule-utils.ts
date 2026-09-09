import { AST_NODE_TYPES, TSESLint, TSESTree } from "@typescript-eslint/utils";

/** Callback sinks that are safe to recognize as either bare function calls or member calls. */
const DEFERRED_SINK_NAMES = new Set(["then", "catch", "finally", "on", "once", "addEventListener", "setTimeout", "setInterval", "setImmediate", "queueMicrotask"]);

/**
 * Callback sinks that must only be recognized as member calls.
 * This avoids false negatives for user-defined synchronous helpers like `nextTick(fn)`.
 */
const MEMBER_ONLY_DEFERRED_SINK_NAMES = new Set(["nextTick"]);

export const SAFE_WRAPPABLE_STATEMENT_TYPES = new Set<AST_NODE_TYPES>([AST_NODE_TYPES.ExpressionStatement, AST_NODE_TYPES.ReturnStatement]);
const FS_MODULE_SPECIFIERS = new Set(["fs", "node:fs"]);

type SourceCodeScope = ReturnType<TSESLint.SourceCode["getScope"]>;
type FsBindingDefinition = { type: string; node: TSESTree.Node; parent?: TSESTree.Node | null };
type ChildProcessBindingDefinition = { type: string; node: TSESTree.Node; parent?: TSESTree.Node | null };
const CHILD_PROCESS_SPECIFIERS = new Set(["child_process", "node:child_process"]);

function escapeRegex(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function getCommonContinuationIndent(lines: string[]): string {
  const indents = lines.filter(line => line.trim().length > 0).map(line => line.match(/^(\s*)/)?.[1] ?? "");

  if (indents.length === 0) return "";

  let commonIndent = indents[0];
  for (const indent of indents.slice(1)) {
    let shared = 0;
    while (shared < commonIndent.length && shared < indent.length && commonIndent[shared] === indent[shared]) {
      shared++;
    }
    commonIndent = commonIndent.slice(0, shared);
  }

  return commonIndent;
}

function isFunctionExpressionLike(node: TSESTree.Node): node is TSESTree.ArrowFunctionExpression | TSESTree.FunctionExpression {
  return node.type === AST_NODE_TYPES.ArrowFunctionExpression || node.type === AST_NODE_TYPES.FunctionExpression;
}

/** Returns true when funcNode is passed to a callback sink not protected by the outer try. */
export function isDeferredCallback(funcNode: TSESTree.Node): boolean {
  if (!isFunctionExpressionLike(funcNode)) return false;

  const parent = funcNode.parent;
  if (!parent) return false;

  const isCallLikeParent = parent.type === AST_NODE_TYPES.NewExpression || parent.type === AST_NODE_TYPES.CallExpression;
  const args = isCallLikeParent ? parent.arguments : undefined;
  const isArgument = args?.includes(funcNode) ?? false;
  const isPromiseConstructor = parent.type === AST_NODE_TYPES.NewExpression && parent.callee.type === AST_NODE_TYPES.Identifier && parent.callee.name === "Promise";
  if (isPromiseConstructor && isArgument) {
    return true;
  }

  if (parent.type === AST_NODE_TYPES.CallExpression && isArgument) {
    const callee = parent.callee;
    if (callee.type === AST_NODE_TYPES.Identifier && DEFERRED_SINK_NAMES.has(callee.name)) {
      return true;
    }
    if (callee.type === AST_NODE_TYPES.MemberExpression && !callee.computed && callee.property.type === AST_NODE_TYPES.Identifier) {
      return DEFERRED_SINK_NAMES.has(callee.property.name) || MEMBER_ONLY_DEFERRED_SINK_NAMES.has(callee.property.name);
    }
  }

  return false;
}

export function isInsideTryBlock(sourceCode: TSESLint.SourceCode, node: TSESTree.Node): boolean {
  const ancestors = sourceCode.getAncestors(node);
  let crossedDeferredBoundary = false;

  for (let i = ancestors.length - 1; i >= 0; i--) {
    const ancestor = ancestors[i];

    if (isDeferredCallback(ancestor)) {
      crossedDeferredBoundary = true;
    }

    if (ancestor.type === "TryStatement" && !crossedDeferredBoundary && ancestor.handler != null) {
      const block = ancestor.block;
      if (node.range != null && block.range != null && node.range[0] >= block.range[0] && node.range[1] <= block.range[1]) {
        return true;
      }
    }
  }

  return false;
}

export function findEnclosingStatement(sourceCode: TSESLint.SourceCode, node: TSESTree.Node): TSESTree.Statement | null {
  const ancestors = sourceCode.getAncestors(node);
  for (let i = ancestors.length - 1; i >= 0; i--) {
    const ancestor = ancestors[i];
    if (SAFE_WRAPPABLE_STATEMENT_TYPES.has(ancestor.type)) {
      return ancestor as TSESTree.Statement;
    }
  }
  return null;
}

function isRequireFsCall(node: TSESTree.Node | null | undefined): boolean {
  if (!node) return false;
  const firstArg = node.type === AST_NODE_TYPES.CallExpression ? node.arguments[0] : null;
  const firstArgValue = firstArg?.type === AST_NODE_TYPES.Literal ? firstArg.value : null;
  return (
    node.type === AST_NODE_TYPES.CallExpression &&
    node.callee.type === AST_NODE_TYPES.Identifier &&
    node.callee.name === "require" &&
    node.arguments.length >= 1 &&
    node.arguments[0].type === AST_NODE_TYPES.Literal &&
    typeof firstArgValue === "string" &&
    FS_MODULE_SPECIFIERS.has(firstArgValue)
  );
}

export function isRequireChildProcess(node: TSESTree.Node | null | undefined): boolean {
  if (!node) return false;
  return (
    node.type === AST_NODE_TYPES.CallExpression &&
    node.callee.type === AST_NODE_TYPES.Identifier &&
    node.callee.name === "require" &&
    node.arguments.length >= 1 &&
    node.arguments[0].type === AST_NODE_TYPES.Literal &&
    typeof (node.arguments[0] as TSESTree.Literal).value === "string" &&
    CHILD_PROCESS_SPECIFIERS.has((node.arguments[0] as TSESTree.Literal).value as string)
  );
}

export function isChildProcessImportBinding(definition: ChildProcessBindingDefinition): boolean {
  if (definition.type !== "ImportBinding") return false;
  if (!definition.parent || definition.parent.type !== AST_NODE_TYPES.ImportDeclaration) return false;
  if (definition.parent.source.type !== AST_NODE_TYPES.Literal) return false;
  return typeof definition.parent.source.value === "string" && CHILD_PROCESS_SPECIFIERS.has(definition.parent.source.value);
}

export function isChildProcessObjectBinding(name: string, scopeNode: TSESTree.Node, sourceCode: TSESLint.SourceCode): boolean {
  let scope: SourceCodeScope | null = sourceCode.getScope(scopeNode);
  while (scope) {
    const variable = scope.set.get(name);
    if (variable && variable.defs.length > 0) {
      for (const def of variable.defs) {
        if (def.type === "Variable") {
          const declarator = def.node as TSESTree.VariableDeclarator;
          if (declarator.id.type === AST_NODE_TYPES.Identifier && isRequireChildProcess(declarator.init)) {
            return true;
          }
        }
        if (isChildProcessImportBinding(def) && def.node.type === AST_NODE_TYPES.ImportNamespaceSpecifier) {
          return true;
        }
      }
      return false;
    }
    scope = scope.upper;
  }
  return false;
}

function isFsImportBinding(definition: FsBindingDefinition): boolean {
  if (definition.type !== "ImportBinding") return false;
  if (!definition.parent || definition.parent.type !== AST_NODE_TYPES.ImportDeclaration) return false;
  if (definition.parent.source.type !== AST_NODE_TYPES.Literal) return false;
  return typeof definition.parent.source.value === "string" && FS_MODULE_SPECIFIERS.has(definition.parent.source.value);
}

function getFsMethodFromImportBinding(definition: FsBindingDefinition, fsSyncMethods: ReadonlySet<string>): string | null {
  if (!isFsImportBinding(definition)) return null;
  if (definition.node.type !== AST_NODE_TYPES.ImportSpecifier) return null;
  if (definition.node.imported.type !== AST_NODE_TYPES.Identifier) return null;
  return fsSyncMethods.has(definition.node.imported.name) ? definition.node.imported.name : null;
}

type FsSyncMethodResolverOptions = {
  allowUnboundFsIdentifier?: boolean;
};

export function createFsSyncMethodResolver(sourceCode: TSESLint.SourceCode, fsSyncMethods: ReadonlySet<string>, options: FsSyncMethodResolverOptions = {}): (node: TSESTree.CallExpression) => string | null {
  function getFsSyncMethodFromProperty(memberExpr: TSESTree.MemberExpression): string | null {
    const property = memberExpr.property;
    if (!memberExpr.computed && property.type === AST_NODE_TYPES.Identifier && fsSyncMethods.has(property.name)) {
      return property.name;
    }
    if (memberExpr.computed && property.type === AST_NODE_TYPES.Literal && typeof property.value === "string" && fsSyncMethods.has(property.value)) {
      return property.value;
    }
    return null;
  }

  function isIdentifierBoundToFsModule(identifierName: string, scopeNode: TSESTree.Node): boolean {
    let scope: SourceCodeScope | null = sourceCode.getScope(scopeNode);
    while (scope) {
      const variable = scope.set.get(identifierName);
      if (variable && variable.defs.length > 0) {
        for (const def of variable.defs) {
          if (isFsImportBinding(def)) {
            return true;
          }
          if (def.type !== "Variable") continue;
          const declarator = def.node as TSESTree.VariableDeclarator;
          if (declarator.id.type === AST_NODE_TYPES.Identifier && isRequireFsCall(declarator.init)) {
            return true;
          }
        }
        return false;
      }
      scope = scope.upper;
    }
    return false;
  }

  function hasAnyBinding(identifierName: string, scopeNode: TSESTree.Node): boolean {
    let scope: SourceCodeScope | null = sourceCode.getScope(scopeNode);
    while (scope) {
      const variable = scope.set.get(identifierName);
      if (variable && variable.defs.length > 0) {
        return true;
      }
      scope = scope.upper;
    }
    return false;
  }

  function resolveFsSyncMethodFromIdentifier(node: TSESTree.CallExpression): string | null {
    const callee = node.callee;
    if (callee.type !== AST_NODE_TYPES.Identifier) return null;

    let scope: SourceCodeScope | null = sourceCode.getScope(node);
    while (scope) {
      const variable = scope.set.get(callee.name);
      if (variable && variable.defs.length > 0) {
        for (const def of variable.defs) {
          const importedMethod = getFsMethodFromImportBinding(def, fsSyncMethods);
          if (importedMethod !== null) {
            return importedMethod;
          }
          if (isFsImportBinding(def)) {
            continue;
          }
          if (def.type !== "Variable") continue;
          const declarator = def.node as TSESTree.VariableDeclarator;

          if (declarator.id.type === AST_NODE_TYPES.ObjectPattern && isRequireFsCall(declarator.init)) {
            for (const prop of declarator.id.properties) {
              if (prop.type !== AST_NODE_TYPES.Property) continue;
              if (prop.key.type !== AST_NODE_TYPES.Identifier) continue;
              if (!fsSyncMethods.has(prop.key.name)) continue;
              const boundName = prop.value.type === AST_NODE_TYPES.Identifier ? prop.value.name : null;
              if (boundName === callee.name) {
                return prop.key.name;
              }
            }
          }

          if (declarator.id.type === AST_NODE_TYPES.Identifier && declarator.init?.type === AST_NODE_TYPES.MemberExpression) {
            const init = declarator.init;
            if (init.object.type === AST_NODE_TYPES.Identifier && isIdentifierBoundToFsModule(init.object.name, init.object)) {
              const methodName = getFsSyncMethodFromProperty(init);
              if (methodName !== null) return methodName;
            }
          }
        }
        return null;
      }
      scope = scope.upper;
    }
    return null;
  }

  return function resolveFsSyncMethod(node: TSESTree.CallExpression): string | null {
    const callee = node.callee;

    if (callee.type === AST_NODE_TYPES.MemberExpression) {
      if (callee.object.type !== AST_NODE_TYPES.Identifier) return null;
      const canUseUnboundFsIdentifier = options.allowUnboundFsIdentifier === true && callee.object.name === "fs" && !hasAnyBinding(callee.object.name, callee.object);
      if (!canUseUnboundFsIdentifier && !isIdentifierBoundToFsModule(callee.object.name, callee.object)) return null;
      return getFsSyncMethodFromProperty(callee);
    }

    if (callee.type === AST_NODE_TYPES.Identifier) {
      return resolveFsSyncMethodFromIdentifier(node);
    }

    return null;
  };
}

type TryCatchSuggestionOptions = {
  indent: string;
  todoComment?: string;
  errorPrefix: string;
};

export function buildTryCatchSuggestion(stmtText: string, options: TryCatchSuggestionOptions): string {
  const { indent, todoComment, errorPrefix } = options;
  const lines = stmtText.split("\n");
  const continuationIndent = getCommonContinuationIndent(lines.slice(1));
  const continuationIndentPattern = continuationIndent.length > 0 ? new RegExp(`^${escapeRegex(continuationIndent)}`) : null;
  const indentedStatement = lines
    .map((line, index) => {
      const normalizedLine = index === 0 ? line.trimStart() : continuationIndentPattern ? line.replace(continuationIndentPattern, "") : line;
      return `${indent}  ${normalizedLine}`;
    })
    .join("\n");

  return [
    "try {",
    indentedStatement,
    `${indent}} catch (err) {`,
    ...(todoComment ? [`${indent}  // ${todoComment}`] : []),
    `${indent}  throw new Error(`,
    `${indent}    "${errorPrefix}" + (err instanceof Error ? err.message : String(err)),`,
    `${indent}    { cause: err },`,
    `${indent}  );`,
    `${indent}}`,
  ].join("\n");
}
