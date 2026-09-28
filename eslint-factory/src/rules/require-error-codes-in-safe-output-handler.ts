import { AST_NODE_TYPES, ESLintUtils, TSESTree } from "@typescript-eslint/utils";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

// Matches the same conventions used across actions/setup/js: numeric codes
// like E003/SAFE_OUTPUT_E007, and named ERR_*/ERROR_* constants.
const ERROR_CODE_PATTERN = /(?<![A-Za-z0-9])(E[0-9]{3}|ERR_[A-Z0-9_]+|ERROR_[A-Z0-9_]+)\b/;

// Markers that indicate a file behaves like a GitHub safe-output handler:
// it talks to the GitHub API via octokit/@actions/github-script, or it
// reads/writes the safe-output NDJSON protocol.
const SAFE_OUTPUT_HANDLER_MARKER = /\boctokit\.|safe_output|safeOutput|NDJSON/;

// A handler is only flagged once it throws or fails enough times that a
// single missed error code would be surprising; this keeps the rule from
// firing on small utility files that happen to throw once for an edge case
// unrelated to the safe-output error-code convention (e.g. an fs read
// failure surfaced verbatim).
const MINIMUM_UNCODED_FAILURE_COUNT = 3;

function isThrowNewErrorStatement(node: TSESTree.Node): boolean {
  return node.type === AST_NODE_TYPES.ThrowStatement && node.argument !== null && node.argument.type === AST_NODE_TYPES.NewExpression;
}

function isCoreSetFailedCall(node: TSESTree.CallExpression): boolean {
  if (node.callee.type !== AST_NODE_TYPES.MemberExpression) return false;
  const { object, property } = node.callee;
  return object.type === AST_NODE_TYPES.Identifier && object.name === "core" && property.type === AST_NODE_TYPES.Identifier && property.name === "setFailed";
}

export const requireErrorCodesInSafeOutputHandlerRule = createRule({
  name: "require-error-codes-in-safe-output-handler",
  meta: {
    type: "suggestion",
    docs: {
      description:
        "Require safe-output handler files (those that talk to the GitHub API via octokit or read/write the safe-output NDJSON protocol) to use standardized ERR_*/E### error codes once they accumulate several thrown/failed errors, so logs and dashboards can filter failures reliably.",
    },
    schema: [],
    messages: {
      missingErrorCodes:
        "This file looks like a safe-output handler ({{marker}}) and has {{count}} thrown Error/core.setFailed call(s), but none references a standardized error code (e.g. ERR_NOT_FOUND, E004). Import codes from ./error_codes.cjs and prefix thrown messages with one, as done in other safe-output handlers, so failures can be tracked consistently.",
    },
  },
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    const fullText = sourceCode.getText();

    const markerMatch = SAFE_OUTPUT_HANDLER_MARKER.exec(fullText);
    if (!markerMatch) return {};
    if (ERROR_CODE_PATTERN.test(fullText)) return {};

    const failureNodes: TSESTree.Node[] = [];

    return {
      ThrowStatement(node: TSESTree.ThrowStatement) {
        if (isThrowNewErrorStatement(node)) failureNodes.push(node);
      },
      CallExpression(node: TSESTree.CallExpression) {
        if (isCoreSetFailedCall(node)) failureNodes.push(node);
      },
      "Program:exit"(program: TSESTree.Program) {
        if (failureNodes.length < MINIMUM_UNCODED_FAILURE_COUNT) return;
        context.report({
          node: program,
          messageId: "missingErrorCodes",
          data: {
            marker: markerMatch[0],
            count: String(failureNodes.length),
          },
        });
      },
    };
  },
});
