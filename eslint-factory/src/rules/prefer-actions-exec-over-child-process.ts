import { AST_NODE_TYPES, ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";
import { isChildProcessImportBinding, isChildProcessObjectBinding, isRequireChildProcess } from "./try-catch-rule-utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

type SourceCodeScope = ReturnType<TSESLint.SourceCode["getScope"]>;

/**
 * `child_process` methods that run a command to completion and return/capture its output —
 * the exact use case covered by `@actions/exec`'s `exec()` / `getExecOutput()`.
 *
 * `spawn` / `spawnSync` are intentionally excluded: they cover long-running, detached, or
 * interactively-streamed child processes (background servers, sidecars, and similar) for which
 * `@actions/exec` has no equivalent, since `exec()` / `getExecOutput()` always wait for the
 * command to finish before resolving.
 */
const OUTPUT_CAPTURING_METHODS = new Set(["exec", "execSync", "execFile", "execFileSync"]);

/**
 * Asynchronous `child_process` methods that return a `ChildProcess` handle. When the handle is
 * retained (assigned, returned, or member-accessed) the caller can stream stdin/stdout or manage
 * the process lifecycle — capabilities `@actions/exec` does not expose — so those calls are not
 * flagged. Only calls whose result is discarded (pure callback style) are reported.
 */
const HANDLE_RETURNING_METHODS = new Set(["exec", "execFile"]);

/**
 * Marker that identifies modules executed as `actions/github-script` steps (loaded via `require()`
 * and executed with `core`, `github`, `context`, `exec`, `io`, and `getOctokit` already in scope —
 * see `generateGitHubScriptWithRequire` in `pkg/workflow/compiler_github_actions_steps.go`). Only
 * those modules are guaranteed to have the `@actions/exec` toolkit available as the `exec` global;
 * standalone Node entry points (such as the mcp-scripts MCP server and the modules it loads) do
 * not, so this rule stays silent in files without the marker.
 */
const GITHUB_SCRIPT_REFERENCE_PATTERN = /<reference\s+types=["']@actions\/github-script["']\s*\/>/;

function isGitHubScriptModule(sourceCode: TSESLint.SourceCode): boolean {
  return sourceCode.getAllComments().some(comment => GITHUB_SCRIPT_REFERENCE_PATTERN.test(comment.value));
}

/**
 * True when the returned `ChildProcess` handle is retained or directly exposed for streaming or
 * lifecycle control.
 *
 * The check walks outward through value-preserving wrappers — `await`, array literals, object
 * literals, and spread elements — so a handle nested inside one of those (`const child = await
 * exec(...)`, `const [child] = [exec(...)]`, `const holder = { child: exec(...) }`) is still
 * recognized as retained once its enclosing container reaches a retained shape. A bare `await`,
 * `void`, or logical expression used as a statement is never itself a retained shape, so those
 * remain flagged.
 */
function retainsCallResult(node: TSESTree.Node): boolean {
  const parent = node.parent;
  if (!parent) return false;

  switch (parent.type) {
    case AST_NODE_TYPES.VariableDeclarator:
      return parent.init === node;
    case AST_NODE_TYPES.AssignmentExpression:
      return parent.right === node;
    case AST_NODE_TYPES.ReturnStatement:
      return parent.argument === node;
    case AST_NODE_TYPES.ArrowFunctionExpression:
      return parent.body === node;
    case AST_NODE_TYPES.MemberExpression:
      return parent.object === node;
    case AST_NODE_TYPES.CallExpression:
      return parent.arguments.some(argument => argument === node);
    // Value-preserving wrappers/containers: the handle is still reachable through them, so
    // whether it is retained depends on how the wrapper/container itself is used.
    case AST_NODE_TYPES.AwaitExpression:
    case AST_NODE_TYPES.SpreadElement:
      return retainsCallResult(parent);
    case AST_NODE_TYPES.ArrayExpression:
      return parent.elements.some(element => element === node) && retainsCallResult(parent);
    case AST_NODE_TYPES.Property: {
      // `parent` is the `Property`; its own parent is the enclosing `ObjectExpression`, which is
      // the container whose retention actually matters.
      const objectExpression = parent.parent;
      return parent.value === node && retainsCallResult(objectExpression);
    }
    default:
      return false;
  }
}

/**
 * Walks outward from `node` to find the nearest enclosing function (declaration, expression, or
 * arrow function). Returns `null` when the call sits at the top level of the module (no enclosing
 * function), in which case there is no caller chain that would need to become `async`.
 */
function findEnclosingFunction(node: TSESTree.Node): TSESTree.FunctionDeclaration | TSESTree.FunctionExpression | TSESTree.ArrowFunctionExpression | null {
  let current = node.parent;
  while (current) {
    if (current.type === AST_NODE_TYPES.FunctionDeclaration || current.type === AST_NODE_TYPES.FunctionExpression || current.type === AST_NODE_TYPES.ArrowFunctionExpression) {
      return current;
    }
    current = current.parent;
  }
  return null;
}

/**
 * True when migrating this call to `@actions/exec` would require converting its enclosing
 * function — and transitively every caller up the chain — to `async`/`await`.
 *
 * `@actions/exec`'s API is Promise-only, so this cascading conversion is only necessary when
 * both of the following hold:
 *  - the call lives inside a non-`async` function (a bare module-level statement has no caller
 *    chain to convert), and
 *  - the call's return value is consumed non-trivially (assigned, returned, or otherwise used —
 *    see `retainsCallResult`), rather than invoked as a bare statement purely for its side effect.
 */
function requiresAsyncConversion(node: TSESTree.CallExpression): boolean {
  const enclosingFunction = findEnclosingFunction(node);
  return enclosingFunction !== null && !enclosingFunction.async && retainsCallResult(node);
}

function getImportSpecifierName(node: TSESTree.ImportSpecifier): string | null {
  if (node.imported.type === AST_NODE_TYPES.Identifier) return node.imported.name;
  if (node.imported.type === AST_NODE_TYPES.Literal && typeof node.imported.value === "string") return node.imported.value;
  return null;
}

/**
 * True when `identifierName` refers to the whole `child_process` module: a `require("child_process")`
 * binding, an ESM namespace import (`import * as cp from "child_process"`), or an ESM default import
 * (`import childProcess from "child_process"`).
 */
function isChildProcessModuleBinding(identifierName: string, scopeNode: TSESTree.Node, sourceCode: TSESLint.SourceCode): boolean {
  if (isChildProcessObjectBinding(identifierName, scopeNode, sourceCode)) return true;

  let scope: SourceCodeScope | null = sourceCode.getScope(scopeNode);
  while (scope) {
    const variable = scope.set.get(identifierName);
    if (variable && variable.defs.length > 0) {
      return variable.defs.some(def => isChildProcessImportBinding(def) && def.node.type === AST_NODE_TYPES.ImportDefaultSpecifier);
    }
    scope = scope.upper;
  }
  return false;
}

/**
 * A `child_process` output-capturing method reachable from a call site, along with whether it is
 * reached through a `promisify()` wrapper. Promisified bindings resolve to the command's output
 * instead of a `ChildProcess` handle, so the handle-retention exemption does not apply to them.
 */
type ResolvedChildProcessMethod = { method: string; promisified: boolean };

/**
 * True when `node` is a `promisify(<something>)` call — either the bare `promisify(...)` binding
 * (`const { promisify } = require("util")` / `import { promisify } from "util"`) or a member call
 * such as `util.promisify(...)` / `require("util").promisify(...)`.
 */
function isPromisifyCall(node: TSESTree.Node | null | undefined): node is TSESTree.CallExpression {
  if (!node || node.type !== AST_NODE_TYPES.CallExpression || node.arguments.length !== 1) return false;
  const callee = node.callee;
  if (callee.type === AST_NODE_TYPES.Identifier) return callee.name === "promisify";
  return callee.type === AST_NODE_TYPES.MemberExpression && !callee.computed && callee.property.type === AST_NODE_TYPES.Identifier && callee.property.name === "promisify";
}

/**
 * Resolves `childProcess.execSync` / `cp.exec` / `require("child_process").exec` member expressions
 * to the referenced `OUTPUT_CAPTURING_METHODS` name.
 */
function resolveChildProcessMemberMethod(node: TSESTree.MemberExpression, sourceCode: TSESLint.SourceCode): string | null {
  if (node.computed || node.property.type !== AST_NODE_TYPES.Identifier || !OUTPUT_CAPTURING_METHODS.has(node.property.name)) return null;
  const isDirectChildProcessRequire = node.object.type === AST_NODE_TYPES.CallExpression && isRequireChildProcess(node.object);
  const isChildProcessNamespace = node.object.type === AST_NODE_TYPES.Identifier && isChildProcessModuleBinding(node.object.name, node.object, sourceCode);
  return isDirectChildProcessRequire || isChildProcessNamespace ? node.property.name : null;
}

/**
 * Resolves whether `identifierName` is bound (directly or via destructuring/require/`promisify()`)
 * to one of `OUTPUT_CAPTURING_METHODS` from the `child_process` module.
 */
function resolveChildProcessOutputMethodBinding(identifierName: string, scopeNode: TSESTree.Node, sourceCode: TSESLint.SourceCode, visited: Set<string> = new Set()): ResolvedChildProcessMethod | null {
  if (visited.has(identifierName)) return null;
  visited.add(identifierName);

  let scope: SourceCodeScope | null = sourceCode.getScope(scopeNode);
  while (scope) {
    const variable = scope.set.get(identifierName);
    if (variable && variable.defs.length > 0) {
      for (const def of variable.defs) {
        // ESM: import { execSync } from "child_process"
        if (isChildProcessImportBinding(def) && def.node.type === AST_NODE_TYPES.ImportSpecifier) {
          const importedName = getImportSpecifierName(def.node);
          if (importedName && OUTPUT_CAPTURING_METHODS.has(importedName)) return { method: importedName, promisified: false };
        }

        if (def.type !== "Variable") continue;
        const declarator = def.node as TSESTree.VariableDeclarator;

        // CJS: const { execSync } = require("child_process")
        if (declarator.id.type === AST_NODE_TYPES.ObjectPattern && isRequireChildProcess(declarator.init)) {
          for (const prop of declarator.id.properties) {
            if (prop.type !== AST_NODE_TYPES.Property || prop.computed) continue;
            if (prop.key.type !== AST_NODE_TYPES.Identifier || !OUTPUT_CAPTURING_METHODS.has(prop.key.name)) continue;
            const boundName = prop.value.type === AST_NODE_TYPES.Identifier ? prop.value.name : null;
            if (boundName === identifierName) return { method: prop.key.name, promisified: false };
          }
        }

        if (declarator.id.type !== AST_NODE_TYPES.Identifier) continue;

        // const execSync = childProcess.execSync (or cp.execSync, or require("child_process").execSync)
        if (declarator.init?.type === AST_NODE_TYPES.MemberExpression) {
          const method = resolveChildProcessMemberMethod(declarator.init, sourceCode);
          if (method) return { method, promisified: false };
        }

        // const execAsync = promisify(exec) (or promisify(require("child_process").exec))
        if (isPromisifyCall(declarator.init)) {
          const wrapped = declarator.init.arguments[0];
          if (wrapped.type === AST_NODE_TYPES.Identifier) {
            const resolved = resolveChildProcessOutputMethodBinding(wrapped.name, wrapped, sourceCode, visited);
            if (resolved) return { method: resolved.method, promisified: true };
          }
          if (wrapped.type === AST_NODE_TYPES.MemberExpression) {
            const method = resolveChildProcessMemberMethod(wrapped, sourceCode);
            if (method) return { method, promisified: true };
          }
        }
      }
      return null;
    }
    scope = scope.upper;
  }
  return null;
}

/**
 * Returns the resolved `child_process` output-capturing method for a `CallExpression`, or `null`
 * if the call isn't one of `OUTPUT_CAPTURING_METHODS` sourced from `child_process`.
 */
function resolveChildProcessOutputMethod(node: TSESTree.CallExpression, sourceCode: TSESLint.SourceCode): ResolvedChildProcessMethod | null {
  const callee = node.callee;

  // execSync(...) / exec(...) / execFile(...) / execFileSync(...) — destructured, aliased, or promisified
  if (callee.type === AST_NODE_TYPES.Identifier) {
    return resolveChildProcessOutputMethodBinding(callee.name, callee, sourceCode);
  }

  if (callee.type !== AST_NODE_TYPES.MemberExpression) return null;
  const method = resolveChildProcessMemberMethod(callee, sourceCode);
  return method ? { method, promisified: false } : null;
}

export const preferActionsExecOverChildProcessRule = createRule({
  name: "prefer-actions-exec-over-child-process",
  meta: {
    type: "suggestion",
    docs: {
      description:
        "Prefer @actions/exec's exec()/getExecOutput() over child_process's exec()/execSync()/execFile()/execFileSync() to spawn processes. " +
        'Only applies to modules marked with the `/// <reference types="@actions/github-script" />` triple-slash reference, which run as ' +
        "actions/github-script steps with the @actions/exec toolkit already available as `exec`; standalone Node entry points (and the modules they load) " +
        "have no such global and are left alone. spawn()/spawnSync() are never flagged, and exec()/execFile() calls whose returned ChildProcess handle is " +
        "retained (for stdin/stdout streaming or lifecycle management) are exempt, since @actions/exec has no equivalent for those. Bindings created through " +
        "promisify() are resolved to the underlying child_process method.",
    },
    schema: [],
    messages: {
      preferActionsExec:
        "Prefer @actions/exec's exec()/getExecOutput() over child_process.{{method}}() to spawn processes in actions/github-script scripts. child_process.{{method}}() duplicates functionality already provided by the @actions/exec toolkit available in this context.",
      preferActionsExecSyncContext:
        "Prefer @actions/exec's exec()/getExecOutput() over child_process.{{method}}() to spawn processes in actions/github-script scripts. child_process.{{method}}() duplicates functionality already provided by the @actions/exec toolkit available in this context. @actions/exec's API is Promise-only, so migrating this call requires converting the enclosing (currently non-async) function — and every one of its callers up the chain — to async/await.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    if (!isGitHubScriptModule(sourceCode)) return {};

    return {
      CallExpression(node) {
        const resolved = resolveChildProcessOutputMethod(node, sourceCode);
        if (!resolved) return;
        // A promisified binding resolves to captured output, never to a ChildProcess handle.
        if (!resolved.promisified && HANDLE_RETURNING_METHODS.has(resolved.method) && retainsCallResult(node)) return;

        context.report({
          node,
          messageId: requiresAsyncConversion(node) ? "preferActionsExecSyncContext" : "preferActionsExec",
          data: { method: resolved.method },
        });
      },
    };
  },
});
