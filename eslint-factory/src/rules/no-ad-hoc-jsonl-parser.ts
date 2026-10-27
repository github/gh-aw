import { ESLintUtils, TSESLint, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);
const helpers = /(?:^|\/)jsonl_helpers\.cjs$/;
const rawLineReaders = new Set(["iterateLines", "readLines", "streamLines"]);
const jsonlReaders = new Set(["iterateJsonl", "iterateJsonlEvents", "parseJsonlContent", "JsonlParser"]);
const lineMethods = new Set(["trim", "trimStart", "trimEnd", "slice", "substring", "replace", "replaceAll"]);
const arrayCallbacks = new Set(["map", "flatMap", "forEach", "filter", "find", "some", "every"]);

function propertyName(node: TSESTree.MemberExpression | TSESTree.Property): string | undefined {
  const key = node.type === "MemberExpression" ? node.property : node.key;
  if (!node.computed && key.type === "Identifier") return key.name;
  if (key.type === "Literal" && typeof key.value === "string") return key.value;
}

export const noAdHocJsonlParserRule = createRule({
  name: "no-ad-hoc-jsonl-parser",
  meta: {
    type: "problem",
    docs: { description: "Use one shared JSONL parser per input instead of JSON.parse on manually framed lines or restarting a parser per record." },
    schema: [],
    messages: {
      adHoc: "Do not JSON.parse manually framed JSONL records. Consume iterateJsonl/iterateJsonlEvents, readJsonlFile, or streamJsonl once per input; use one persistent JsonlParser for callbacks.",
      restart: "Do not restart a JSONL parser for each line. Pass the complete input to one iterator, or keep one JsonlParser instance for the stream.",
    },
  },
  defaultOptions: [],
  create(context) {
    if (/(?:^|[/\\])jsonl_helpers\.cjs$/.test(context.filename)) return {};
    const sourceCode = context.sourceCode;
    const lineBindings = new Set<TSESLint.Scope.Variable>();
    const perLineContexts = new Set<TSESTree.Node>();
    const calls: Array<TSESTree.CallExpression | TSESTree.NewExpression> = [];

    function binding(node: TSESTree.Identifier): TSESLint.Scope.Variable | undefined {
      let scope: TSESLint.Scope.Scope | null = sourceCode.getScope(node);
      while (scope) {
        const variable = scope.set.get(node.name);
        if (variable) return variable;
        scope = scope.upper;
      }
    }

    function initializer(node: TSESTree.Identifier): TSESTree.Expression | undefined {
      const variable = binding(node);
      if (variable?.references.some(ref => ref.isWrite() && !ref.init)) return;
      const def = variable?.defs[0];
      if (def?.node.type === "VariableDeclarator" && def.node.id.type === "Identifier") return def.node.init ?? undefined;
    }

    function importedName(node: TSESTree.Node, module: RegExp, seen = new Set<TSESTree.Node>()): string | undefined {
      if (seen.has(node)) return;
      seen.add(node);
      if (node.type === "MemberExpression") {
        return importedName(node.object, module, seen) === "*" ? propertyName(node) : undefined;
      }
      if (node.type === "CallExpression" && node.callee.type === "Identifier" && node.callee.name === "require" && !binding(node.callee)?.defs.length) {
        const source = node.arguments[0];
        if (source?.type === "Literal" && typeof source.value === "string" && module.test(source.value)) return "*";
      }
      if (node.type !== "Identifier") return;
      const variable = binding(node);
      if (variable?.references.some(ref => ref.isWrite() && !ref.init)) return;
      const def = variable?.defs[0];
      if (def?.type === "ImportBinding" && def.parent.type === "ImportDeclaration" && module.test(def.parent.source.value)) {
        return def.node.type === "ImportSpecifier" ? (def.node.imported.type === "Identifier" ? def.node.imported.name : String(def.node.imported.value)) : "*";
      }
      if (def?.node.type !== "VariableDeclarator" || !def.node.init) return;
      if (def.node.id.type === "Identifier") return importedName(def.node.init, module, seen);
      if (def.node.id.type === "ObjectPattern" && importedName(def.node.init, module, seen) === "*") {
        const property = def.node.id.properties.find(p => p.type === "Property" && p.value.type === "Identifier" && p.value.name === node.name);
        if (property?.type === "Property") return propertyName(property);
      }
    }

    function isLineSource(node: TSESTree.Node, seen = new Set<TSESTree.Node>()): boolean {
      if (seen.has(node)) return false;
      seen.add(node);
      if (node.type === "Identifier") {
        const init = initializer(node);
        return !!init && isLineSource(init, seen);
      }
      if (node.type === "ConditionalExpression") return isLineSource(node.consequent, seen) || isLineSource(node.alternate, seen);
      if (node.type !== "CallExpression") return false;
      if (rawLineReaders.has(importedName(node.callee, helpers) ?? "")) return true;
      if (importedName(node.callee, /^(?:node:)?readline(?:\/promises)?$/) === "createInterface") return true;
      if (node.callee.type !== "MemberExpression") return false;
      const method = propertyName(node.callee);
      if (method === "split") {
        const separator = node.arguments[0];
        return separator?.type === "Literal" && (typeof separator.value === "string" ? /[\r\n]/.test(separator.value) : "regex" in separator && /(?:^|[^\\])(?:\\\\)*\\(?:[rn]|x0[ad]|u000[ad])/i.test(separator.regex.pattern));
      }
      if (method === "filter" || method === "slice" || method === "reverse" || method === "toReversed") return isLineSource(node.callee.object, seen);
      if ((method === "push" || method === "finish") && node.callee.object.type === "Identifier") {
        const init = initializer(node.callee.object);
        return init?.type === "NewExpression" && importedName(init.callee, helpers) === "LineFramer";
      }
      return false;
    }

    function markLine(node: TSESTree.Node | undefined) {
      if (node?.type !== "Identifier") return;
      const variable = binding(node);
      if (variable) lineBindings.add(variable);
    }

    function callback(node: TSESTree.Node | undefined): TSESTree.ArrowFunctionExpression | TSESTree.FunctionExpression | TSESTree.FunctionDeclaration | undefined {
      if (node?.type === "ArrowFunctionExpression" || node?.type === "FunctionExpression") return node;
      if (node?.type !== "Identifier") return;
      const def = binding(node)?.defs[0];
      if (def?.node.type === "FunctionDeclaration") return def.node;
      const init = initializer(node);
      if (init?.type === "ArrowFunctionExpression" || init?.type === "FunctionExpression") return init;
    }

    function isJsonParse(node: TSESTree.Node, seen = new Set<TSESTree.Node>()): boolean {
      if (seen.has(node)) return false;
      seen.add(node);
      if (node.type === "Identifier") {
        const init = initializer(node);
        return !!init && isJsonParse(init, seen);
      }
      return node.type === "MemberExpression" && propertyName(node) === "parse" && node.object.type === "Identifier" && node.object.name === "JSON" && !binding(node.object)?.defs.length;
    }

    function fromLine(node: TSESTree.Node | undefined, seen = new Set<TSESTree.Node>()): boolean {
      if (!node || seen.has(node)) return false;
      seen.add(node);
      if (node.type === "Identifier") {
        const variable = binding(node);
        return (!!variable && lineBindings.has(variable)) || fromLine(initializer(node), seen);
      }
      if (node.type === "ConditionalExpression") return fromLine(node.consequent, seen) || fromLine(node.alternate, seen);
      if (node.type === "ArrayExpression") return node.elements.some(element => fromLine(element ?? undefined, seen));
      if (node.type === "MemberExpression" && node.computed) return isLineSource(node.object);
      if (node.type === "CallExpression" && node.callee.type === "MemberExpression" && propertyName(node.callee) === "shift") return isLineSource(node.callee.object);
      if (node.type === "CallExpression" && node.callee.type === "MemberExpression" && lineMethods.has(propertyName(node.callee) ?? "")) return fromLine(node.callee.object, seen);
      return false;
    }

    return {
      ForOfStatement(node) {
        if (!isLineSource(node.right)) return;
        perLineContexts.add(node.body);
        markLine(node.left.type === "VariableDeclaration" ? node.left.declarations[0]?.id : node.left);
      },
      NewExpression(node) {
        calls.push(node);
      },
      AssignmentExpression(node) {
        if (node.operator === "=" && fromLine(node.right)) markLine(node.left);
      },
      CallExpression(node) {
        calls.push(node);
        if (node.callee.type !== "MemberExpression") return;
        const method = propertyName(node.callee);
        const lineArray = arrayCallbacks.has(method ?? "") && isLineSource(node.callee.object);
        const lineEvent = (method === "on" || method === "once" || method === "addListener") && node.arguments[0]?.type === "Literal" && node.arguments[0].value === "line";
        if (!lineArray && !lineEvent) return;
        const handler = node.arguments[lineEvent ? 1 : 0];
        if (handler && isJsonParse(handler)) context.report({ node: handler, messageId: "adHoc" });
        const fn = callback(handler);
        if (fn) {
          perLineContexts.add(fn);
          markLine(fn.params[0]);
        }
      },
      "Program:exit"() {
        let previousSize;
        do {
          previousSize = lineBindings.size;
          for (const node of calls) {
            if (node.type !== "CallExpression") continue;
            const fn = callback(node.callee);
            if (!fn) continue;
            node.arguments.forEach((argument, index) => {
              if (fromLine(argument)) {
                markLine(fn.params[index]);
                perLineContexts.add(fn);
              }
            });
          }
        } while (lineBindings.size !== previousSize);
        for (const node of calls) {
          if (node.type === "CallExpression" && isJsonParse(node.callee) && fromLine(node.arguments[0])) {
            context.report({ node, messageId: "adHoc" });
          } else if (jsonlReaders.has(importedName(node.callee, helpers) ?? "")) {
            let inLineContext = false;
            for (const ancestor of sourceCode.getAncestors(node).reverse()) {
              if (perLineContexts.has(ancestor)) {
                inLineContext = true;
                break;
              }
              if (ancestor.type === "FunctionDeclaration" || ancestor.type === "FunctionExpression" || ancestor.type === "ArrowFunctionExpression") break;
            }
            const parserConstruction = node.type === "NewExpression" && importedName(node.callee, helpers) === "JsonlParser";
            if ((parserConstruction && inLineContext) || fromLine(node.arguments[0])) context.report({ node, messageId: "restart" });
          }
        }
      },
    };
  },
});
