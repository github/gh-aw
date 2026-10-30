import { data, Lexer, Parser } from "@actions/expressions";
import { Binary, ContextAccess, FunctionCall, Grouping, IndexAccess, Literal, Logical, Star, Unary } from "@actions/expressions/ast";
import { parseWorkflow, NoOperationTraceWriter } from "@actions/workflow-parser";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

function tree(expr) {
  if (!expr) return null;
  if (expr instanceof Literal) {
    if (expr.literal.kind === data.Kind.Null) return ["literal", "null"];
    const value = expr.literal.value;
    return ["literal", typeof value, typeof value === "number" && !Number.isFinite(value) ? String(value) : value];
  }
  if (expr instanceof ContextAccess) return ["context", expr.name.lexeme];
  if (expr instanceof FunctionCall) return ["call", expr.functionName.lexeme, ...expr.args.map(tree)];
  if (expr instanceof Grouping) return ["group", tree(expr.group)];
  if (expr instanceof IndexAccess) return ["index", tree(expr.expr), tree(expr.index)];
  if (expr instanceof Unary) return ["unary", expr.operator.lexeme, tree(expr.expr)];
  if (expr instanceof Binary) return ["binary", expr.operator.lexeme, tree(expr.left), tree(expr.right)];
  if (expr instanceof Logical) return ["logical", expr.operator.lexeme, ...expr.args.map(tree)];
  if (expr instanceof Star) return ["star"];
  throw new Error(`Unknown official expression node: ${expr.constructor.name}`);
}

export function oracle(input) {
  if (input.kind === "expression") {
    const contexts = input.contexts.filter(c => !c.includes("("));
    const functions = input.contexts
      .filter(c => c.includes("("))
      .map(c => {
        const declaration = c.match(/^(\w+)\((\d+),(\d+|MAX)\)$/);
        if (!declaration) throw new Error(`Invalid oracle function context: ${c}`);
        const [, name, min, max] = declaration;
        return {
          name,
          minArgs: Number(min),
          maxArgs: max === "MAX" ? Number.MAX_SAFE_INTEGER : Number(max),
        };
      });
    let expression;
    try {
      const { tokens } = new Lexer(input.source).lex();
      expression = new Parser(tokens, contexts, functions).parse();
    } catch {
      // Only rejection by the lexer/parser is a negative conformance result.
      // Oracle setup and AST normalization failures must fail the harness.
      return { accepted: false };
    }
    return { accepted: true, tree: tree(expression) };
  }
  if (input.kind !== "workflow") throw new Error("Unknown conformance kind");
  const result = parseWorkflow({ name: input.name, content: input.source }, new NoOperationTraceWriter());
  return {
    accepted: result.context.errors.count === 0,
    diagnostics: result.context.errors.getErrors().map(error => error.message),
  };
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  const inputs = JSON.parse(readFileSync(0, "utf8"));
  process.stdout.write(JSON.stringify(inputs.map(oracle)));
}
