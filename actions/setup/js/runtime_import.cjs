// @ts-check
/// <reference types="@actions/github-script" />

// runtime_import.cjs
// Processes {{#runtime-import filepath}} and {{#runtime-import? filepath}} macros
// at runtime to import markdown file contents dynamically.
// Also processes inline @path and @url references.

const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_API, ERR_CONFIG, ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { isTruthy } = require("./is_truthy.cjs");
const { closeUnterminatedSkillMarkers } = require("./extract_inline_skills.cjs");
const { closeUnterminatedSubAgentMarkers } = require("./extract_inline_sub_agents.cjs");

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");

/**
 * Makes any "## skill:"/"## agent:" block in a runtime-imported chunk of
 * content self-terminating before it is spliced into the larger prompt.
 *
 * Runtime imports are resolved and spliced into the surrounding document
 * before inline skill/sub-agent extraction runs (see interpolate_prompt.cjs).
 * If an imported file relies on implicit closing (next H2 heading or EOF)
 * for a skill/agent block, that block would expand past the imported file's
 * own boundary once spliced in and swallow whatever follows it — content
 * from the importing workflow's main body or from subsequent imports.
 * Making the end marker explicit here keeps every runtime import
 * self-contained regardless of where it ends up in the assembled document.
 *
 * @param {string} content - Resolved content of a single runtime import (file or URL).
 * @returns {string} Content with implicit skill/agent end markers made explicit.
 */
function closeUnterminatedInlineMarkers(content) {
  return closeUnterminatedSubAgentMarkers(closeUnterminatedSkillMarkers(content));
}

/**
 * Checks if a file starts with front matter (---\n)
 * @param {string} content - The file content to check
 * @returns {boolean} - True if content starts with front matter
 */
function hasFrontMatter(content) {
  return content.trimStart().startsWith("---\n") || content.trimStart().startsWith("---\r\n");
}

/**
 * Returns true when runtime-import frontmatter should be preserved for agent/skill files.
 * Agent/skill frontmatter can carry execution metadata (for example model selection)
 * that must not be stripped before the engine reads the imported definition.
 *
 * @param {string} filepath - Resolved runtime import path (relative form).
 * @returns {boolean}
 */
function shouldPreserveFrontMatter(filepath) {
  const normalized = String(filepath || "").replace(/\\/g, "/");
  const segments = normalized.split("/").filter(Boolean);
  // resolveRuntimeImportFilePath() strips ".github/" prefixes, so files imported from
  // ".github/agents/*" arrive here as "agents/*".
  return segments[0] === "agents" || segments[0] === "skills" || (segments[0] === ".agents" && (segments[1] === "agents" || segments[1] === "skills"));
}

/**
 * Removes XML comments from content
 * @param {string} content - The content to process
 * @returns {string} - Content with XML comments removed
 */
function removeXMLComments(content) {
  // Remove XML/HTML comments: <!-- ... -->
  // Apply repeatedly to handle nested/overlapping patterns that could reintroduce comment markers
  let previous;
  do {
    previous = content;
    content = content.replace(/<!--[\s\S]*?-->/g, "");
  } while (content !== previous);
  return content;
}

/**
 * Neutralizes AI-model control tags to prevent prompt injection.
 *
 * `<system>` (and its closing form `</system>`) act as top-level system-prompt
 * delimiters in Anthropic's Claude API. When workflow markdown files are
 * runtime-imported into prompt.txt — which already contains a legitimate
 * `<system>...</system>` security-policy block — a second `<system>` tag
 * anywhere in the imported content creates a duplicate system block and can
 * override the security policy (prompt injection).
 *
 * This function converts those tags to a parenthetical form `(system)` /
 * `(/system)` so they are treated as ordinary text rather than control tokens,
 * consistent with the approach used by `convertXmlTags` in
 * `sanitize_content_core.cjs` for user-provided content.
 *
 * @param {string} content - The content to process
 * @returns {string} - Content with AI-model control tags neutralized
 */
function neutralizeSystemTags(content) {
  // Convert <system>, <system attr="...">, </system> to parenthetical equivalents.
  // The regex matches:
  //   - optional closing slash (</system>)
  //   - tag name "system" (case-insensitive)
  //   - optional attributes (everything up to the closing >)
  return content.replace(/<(\/?\s*system(?:\s[^>]*)?)\s*>/gi, "($1)");
}

/**
 * Safe list of allowed GitHub Actions expressions
 * These are expressions that cannot be tampered with by users
 * and are safe to evaluate at runtime.
 *
 * This list matches pkg/constants/constants.go:AllowedExpressions
 */
const ALLOWED_EXPRESSIONS = [
  "github.event.after",
  "github.event.before",
  "github.event.check_run.id",
  "github.event.check_suite.id",
  "github.event.comment.id",
  "github.event.deployment.id",
  "github.event.deployment_status.id",
  "github.event.deployment_status.state",
  "github.event.head_commit.id",
  "github.event.installation.id",
  "github.event.issue.number",
  "github.event.discussion.number",
  "github.event.pull_request.number",
  "github.event.milestone.number",
  "github.event.check_run.number",
  "github.event.check_suite.number",
  "github.event.workflow_job.run_id",
  "github.event.workflow_run.number",
  "github.event.label.id",
  "github.event.milestone.id",
  "github.event.organization.id",
  "github.event.page.id",
  "github.event.project.id",
  "github.event.project_card.id",
  "github.event.project_column.id",
  "github.event.release.assets[0].id",
  "github.event.release.id",
  "github.event.release.tag_name",
  "github.event.repository.id",
  "github.event.repository.default_branch",
  "github.event.review.id",
  "github.event.review_comment.id",
  "github.event.sender.id",
  "github.event.workflow_run.id",
  "github.event.workflow_run.conclusion",
  "github.event.workflow_run.html_url",
  "github.event.workflow_run.head_sha",
  "github.event.workflow_run.run_number",
  "github.event.workflow_run.event",
  "github.event.workflow_run.status",
  "github.event.issue.state",
  "github.event.issue.title",
  "github.event.pull_request.state",
  "github.event.pull_request.title",
  "github.event.discussion.title",
  "github.event.discussion.category.name",
  "github.event.release.name",
  "github.event.workflow_job.id",
  "github.event.deployment.environment",
  "github.event.pull_request.head.sha",
  "github.event.pull_request.base.sha",
  "github.actor",
  "github.event_name",
  "github.job",
  "github.owner",
  "github.repository",
  "github.repository_owner",
  "github.run_id",
  "github.run_number",
  "github.server_url",
  "github.workflow",
  "github.workspace",
];

/**
 * Checks if an expression is in the safe list
 * @param {string} expr - The expression to check (without ${{ }})
 * @returns {boolean} - True if expression is safe
 */
function isSafeExpression(expr) {
  const trimmed = expr.trim();

  // Expressions containing line terminators are never safe.
  // A newline inside an expression can split the operator regex matching and
  // cause compound expressions like "safe == 'x' &\n 'payload' || 'default'"
  // to appear safe via the comparison extractor even though the full expression
  // is not.  Cover all JavaScript line terminator characters: LF, CR, LS (U+2028),
  // and PS (U+2029).  Check the original `expr` (before trimming) so that
  // leading/trailing line terminators like "\ngithub.repository\n" are also caught.
  if (/[\n\r\u2028\u2029]/.test(expr)) {
    return false;
  }

  // Block dangerous JavaScript built-in property names
  const DANGEROUS_PROPS = [
    "constructor",
    "__proto__",
    "prototype",
    "__defineGetter__",
    "__defineSetter__",
    "__lookupGetter__",
    "__lookupSetter__",
    "hasOwnProperty",
    "isPrototypeOf",
    "propertyIsEnumerable",
    "toString",
    "valueOf",
    "toLocaleString",
  ];

  // Split expression into parts and check each for dangerous properties
  // Handle both dot notation (e.g., "github.event.issue") and bracket notation (e.g., "release.assets[0].id")
  const parts = trimmed.split(/[.\[\]]+/).filter(p => p && !/^\d+$/.test(p));

  for (const part of parts) {
    if (DANGEROUS_PROPS.includes(part)) {
      return false; // Block dangerous property
    }
  }

  // Check exact match in allowed list
  if (ALLOWED_EXPRESSIONS.includes(trimmed)) {
    return true;
  }

  // Check if it matches dynamic patterns:
  // - needs.* and steps.* (job dependencies and step outputs) - max depth 5 levels
  // - github.event.inputs.* (workflow_dispatch inputs)
  // - github.aw.inputs.* (shared workflow inputs)
  // - inputs.* (workflow_call inputs)
  // - env.* (environment variables)
  // Limit nesting depth to max 5 levels to prevent deep traversal attacks
  const dynamicPatterns = [
    /^(needs|steps)\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+){0,2}$/, // Max depth: needs.job.outputs.foo.bar (5 levels)
    /^github\.event\.inputs\.[a-zA-Z0-9_-]+$/,
    /^github\.aw\.inputs\.[a-zA-Z0-9_-]+$/,
    /^inputs\.[a-zA-Z0-9_-]+$/,
    /^env\.[a-zA-Z0-9_-]+$/,
    /^experiments\.[a-zA-Z0-9_-]+$/,
  ];

  for (const pattern of dynamicPatterns) {
    if (pattern.test(trimmed)) {
      return true;
    }
  }

  // Strict string-literal regex: the body must not contain an unescaped copy of the
  // opening quote character.  This prevents compound expressions like
  // `'a' || secrets.TOKEN || 'b'` from being misclassified as a string literal because
  // they happen to start and end with a quote.
  // Pattern: ^(quote)(non-quote-non-backslash | escaped-char)*(same-quote)$
  //   [^'\\] = any char except the single-quote and backslash
  //   \\.    = backslash followed by any character (escape sequence)
  const STRING_LITERAL_RE = /^'(?:[^'\\]|\\.)*'$|^"(?:[^"\\]|\\.)*"$|^`(?:[^`\\]|\\.)*`$/;

  /**
   * Returns true when `expr` is a standalone literal value (string, number, or boolean).
   * Used to refuse literal operands inside && / || compound expressions — a literal in a
   * conjunction or disjunction is semantically incomplete and may hide injection vectors.
   * @param {string} expr - The trimmed expression to test
   * @returns {boolean}
   */
  const isLiteralValue = expr => {
    const t = expr.trim();
    if (STRING_LITERAL_RE.test(t)) return true;
    if (/^-?\d+(\.\d+)?$/.test(t)) return true;
    if (t === "true" || t === "false") return true;
    return false;
  };

  // Allow literal values (string, number, boolean) as *standalone* safe expressions only.
  // A literal is only valid when it is the entire expression, not as a sub-expression inside
  // && or ||.  The checks below enforce this constraint by refusing literal operands there.
  const isStringLiteralStandalone = STRING_LITERAL_RE.test(trimmed);
  if (isStringLiteralStandalone) {
    const contentMatch = trimmed.match(/^(['"`])(.+)\1$/);
    if (contentMatch) {
      const content = contentMatch[2];
      // Reject nested expressions
      if (content.includes("${{") || content.includes("}}")) {
        return false;
      }
      // Reject escape sequences that could hide keywords
      if (/\\[xu][\da-fA-F]/.test(content) || /\\[0-7]{1,3}/.test(content)) {
        return false;
      }
      // Reject zero-width characters
      if (/[\u200B-\u200D\uFEFF]/.test(content)) {
        return false;
      }
    }
    return true;
  }
  if (/^-?\d+(\.\d+)?$/.test(trimmed) || trimmed === "true" || trimmed === "false") {
    return true;
  }

  // Check for OR expressions (e.g., "inputs.repository || 'default'").
  // The RIGHT side may be a literal (fallback default), but the LEFT side must not be a
  // literal — a literal on the left is always truthy and makes the right side dead code.
  // Important: once an OR match is found the decision is final — do NOT fall through to
  // the AND/comparison checks below, because doing so would allow a partially-validated
  // OR expression like "github.actor == 'x' || secrets.TOKEN" to pass via the comparison
  // path even though the right side is unsafe.
  const orMatch = trimmed.match(/^(.+?)\s*\|\|\s*(.+)$/);
  if (orMatch) {
    const leftExpr = orMatch[1].trim();
    const rightExpr = orMatch[2].trim();

    // Refuse a literal on the left side of a disjunction — semantically always-true
    // and a potential source of confusion or injection vectors.
    if (isLiteralValue(leftExpr)) {
      return false;
    }

    // Check if left side is safe
    if (!isSafeExpression(leftExpr)) {
      return false;
    }

    // Check if right side is a literal string (single, double, or backtick quotes).
    // Use the same strict regex that requires no unescaped matching quote in the body.
    const isStringLiteral = STRING_LITERAL_RE.test(rightExpr);
    if (isStringLiteral) {
      // Validate string literal content for security
      const contentMatch = rightExpr.match(/^(['"`])(.+)\1$/);
      if (contentMatch) {
        const content = contentMatch[2];

        // Reject nested expressions
        if (content.includes("${{") || content.includes("}}")) {
          return false;
        }

        // Reject escape sequences that could hide keywords
        if (/\\[xu][\da-fA-F]/.test(content) || /\\[0-7]{1,3}/.test(content)) {
          return false;
        }

        // Reject zero-width characters
        if (/[\u200B-\u200D\uFEFF]/.test(content)) {
          return false;
        }
      }
      return true;
    }

    // Check if right side is a number literal
    const isNumberLiteral = /^-?\d+(\.\d+)?$/.test(rightExpr);
    // Check if right side is a boolean literal
    const isBooleanLiteral = rightExpr === "true" || rightExpr === "false";

    if (isNumberLiteral || isBooleanLiteral) {
      return true;
    }

    // If right side is also a safe expression (e.g., inputs.repo || github.repository)
    if (isSafeExpression(rightExpr)) {
      return true;
    }

    // Right side is neither a safe literal nor a safe expression — reject.
    return false;
  }

  // Check for AND expressions (e.g., "github.actor && github.repository").
  // Both sides must be independently safe property expressions — literal operands are refused
  // because a literal in a conjunction is semantically incomplete (always truthy/falsy constant)
  // and could hide injection vectors.  Operator precedence means && binds tighter than ||, so
  // this check runs after the OR check above.
  // Important: once an AND match is found the decision is final — do NOT fall through to
  // the comparison check, which could otherwise allow "github.actor == 'x' && secrets.TOKEN"
  // to pass because the comparison extracts only "github.actor" as safe.
  const andMatch = trimmed.match(/^(.+?)\s*&&\s*(.+)$/);
  if (andMatch) {
    const leftExpr = andMatch[1].trim();
    const rightExpr = andMatch[2].trim();
    // Refuse literal sub-expressions in a conjunction
    if (isLiteralValue(leftExpr) || isLiteralValue(rightExpr)) {
      return false;
    }
    return isSafeExpression(leftExpr) && isSafeExpression(rightExpr);
  }

  // Check for simple comparison expressions (e.g., "github.event.inputs.enforce_all == 'true'").
  // This check only runs for expressions that have no top-level || or && operators (since those
  // cases are fully handled above), preventing a partially-validated compound expression from
  // sneaking through via the comparison path.
  const comparisonMatch = trimmed.match(/^(.+?)\s*(?:==|!=|<=?|>=?)\s*(.+)$/);
  if (comparisonMatch) {
    const leftExpr = comparisonMatch[1].trim();
    const rightExpr = comparisonMatch[2].trim();
    return leftExpr.length > 0 && rightExpr.length > 0 && isSafeExpression(leftExpr) && isSafeExpression(rightExpr);
  }

  return false;
}

/**
 * Generates the compiler's hash-based environment variable name for a GitHub expression.
 * @param {string} expr - The GitHub expression content without ${{ }}.
 * @returns {string}
 */
function generateHashedExpressionEnvVarName(expr) {
  return "GH_AW_EXPR_" + crypto.createHash("sha256").update(expr).digest("hex").slice(0, 8).toUpperCase();
}

/**
 * Evaluates a safe GitHub Actions expression at runtime
 * @param {string} expr - The expression to evaluate (without ${{ }})
 * @returns {string} - The evaluated value or original expression if cannot evaluate
 */
function evaluateExpression(expr) {
  const trimmed = expr.trim();

  // Check for OR expressions with literals (e.g., "inputs.repository || 'default'")
  const orMatch = trimmed.match(/^(.+?)\s*\|\|\s*(.+)$/);
  if (orMatch) {
    const leftExpr = orMatch[1].trim();
    const rightExpr = orMatch[2].trim();

    // Try to evaluate the left expression
    const leftValue = evaluateExpression(leftExpr);

    // Check if left value is truthy (not empty, not undefined, not null)
    // If it's wrapped in ${{ }}, it means it couldn't be evaluated
    if (!leftValue.startsWith("${{")) {
      return leftValue;
    }

    // Left value is falsy or couldn't be evaluated, use the right side
    // If right side is a literal, extract and return it
    const stringLiteralMatch = rightExpr.match(/^(['"`])(.+)\1$/);
    if (stringLiteralMatch) {
      const content = stringLiteralMatch[2];
      // Neutralize any expression markers
      return content.replace(/\$/g, "\\$").replace(/\{/g, "\\{");
    }

    // If right side is a number or boolean literal, return it
    if (/^-?\d+(\.\d+)?$/.test(rightExpr) || rightExpr === "true" || rightExpr === "false") {
      return rightExpr;
    }

    // Otherwise try to evaluate the right expression
    return evaluateExpression(rightExpr);
  }

  // Check if this is a needs.*, steps.*, or inputs.* expression that should be looked up from environment variables
  // The compiler extracts these expressions and makes them available as GH_AW_* environment variables
  // For example: needs.search_issues.outputs.issue_list → GH_AW_NEEDS_SEARCH_ISSUES_OUTPUTS_ISSUE_LIST
  // For inputs: inputs.errors → GH_AW_INPUTS_ERRORS
  // This is required for workflow_call where inputs are not in context.payload.inputs;
  // for workflow_dispatch, context.payload.inputs is populated but the env var lookup takes precedence.
  if (trimmed.startsWith("needs.") || trimmed.startsWith("steps.") || trimmed.startsWith("inputs.")) {
    // Convert expression to environment variable name
    // e.g., "needs.search_issues.outputs.issue_list" → "GH_AW_NEEDS_SEARCH_ISSUES_OUTPUTS_ISSUE_LIST"
    const envVarName = "GH_AW_" + trimmed.toUpperCase().replace(/\./g, "_");
    const envValue = process.env[envVarName];
    if (envValue !== undefined && envValue !== null) {
      return envValue;
    }

    const hashedEnvVarName = generateHashedExpressionEnvVarName(trimmed);
    const hashedEnvValue = process.env[hashedEnvVarName];
    if (hashedEnvValue !== undefined && hashedEnvValue !== null) {
      return hashedEnvValue;
    }
    // If not found in environment, continue to try other evaluation methods below
  }

  // Access GitHub context through environment variables
  // The context object is available globally when running in github-script
  if (typeof context !== "undefined") {
    try {
      // Build the evaluation context with safe properties
      const evalContext = {
        github: {
          actor: context.actor,
          event_name: context.eventName,
          job: context.job,
          owner: context.repo.owner,
          repository: `${context.repo.owner}/${context.repo.repo}`,
          repository_owner: context.repo.owner,
          run_id: context.runId,
          run_number: context.runNumber,
          server_url: process.env.GITHUB_SERVER_URL || "https://github.com",
          workflow: context.workflow,
          workspace: process.env.GITHUB_WORKSPACE || "",
          event: context.payload || {},
        },
        env: process.env,
        inputs: context.payload?.inputs || {},
      };

      // Freeze the evaluation context to prevent modification
      Object.freeze(evalContext);
      Object.freeze(evalContext.github);

      // Parse property access (e.g., "github.actor" -> ["github", "actor"])
      const parts = trimmed.split(".");
      /** @type {any} */
      let value = evalContext;

      for (const part of parts) {
        // Handle array access like release.assets[0].id
        const arrayMatch = part.match(/^([a-zA-Z0-9_-]+)\[(\d+)\]$/);
        if (arrayMatch) {
          const key = arrayMatch[1];
          const index = parseInt(arrayMatch[2], 10);
          // Use Object.prototype.hasOwnProperty.call() to prevent prototype chain access
          if (value && typeof value === "object" && Object.prototype.hasOwnProperty.call(value, key)) {
            const arrayValue = value[key];
            if (Array.isArray(arrayValue) && index >= 0 && index < arrayValue.length) {
              value = arrayValue[index];
            } else {
              value = undefined;
              break;
            }
          } else {
            value = undefined;
            break;
          }
        } else {
          // Use Object.prototype.hasOwnProperty.call() to prevent prototype chain access
          if (value && typeof value === "object" && Object.prototype.hasOwnProperty.call(value, part)) {
            value = value[part];
          } else {
            value = undefined;
            break;
          }
        }

        if (value === undefined || value === null) {
          break;
        }
      }

      // If we successfully resolved the value, return it as a string
      if (value !== undefined && value !== null) {
        return String(value);
      }

      // If the direct context lookup failed, try resolving via aw_context.
      // Slash-command workflows run as workflow_dispatch events; the triggering
      // issue/discussion/PR number lives in inputs.aw_context, not in the event
      // payload (context.payload.issue is undefined for workflow_dispatch).
      const awCtxStr = context?.payload?.inputs?.aw_context;
      if (awCtxStr && typeof awCtxStr === "string") {
        try {
          /** @type {{ item_type?: string, item_number?: number|string, comment_id?: number|string }} */
          const awCtx = JSON.parse(awCtxStr);
          /** @type {Record<string, () => string | undefined>} */
          const fieldResolvers = {
            "github.event.issue.number": () => (awCtx.item_type === "issue" && awCtx.item_number ? String(awCtx.item_number) : undefined),
            "github.event.discussion.number": () => (awCtx.item_type === "discussion" && awCtx.item_number ? String(awCtx.item_number) : undefined),
            "github.event.pull_request.number": () => (awCtx.item_type === "pull_request" && awCtx.item_number ? String(awCtx.item_number) : undefined),
            "github.event.comment.id": () => (awCtx.comment_id ? String(awCtx.comment_id) : undefined),
          };
          const resolver = fieldResolvers[trimmed];
          if (resolver) {
            const awValue = resolver();
            if (awValue !== undefined) {
              return awValue;
            }
          }
        } catch (_parseError) {
          // aw_context is not valid JSON – ignore and fall through
        }
      }
    } catch (error) {
      // If evaluation fails, log but don't throw
      const errorMessage = getErrorMessage(error);
      core.warning(`Failed to evaluate expression "${trimmed}": ${errorMessage}`);
    }
  }

  // If we can't evaluate, return the original expression wrapped in ${{ }}
  // This allows GitHub Actions to evaluate it later
  return `\${{ ${trimmed} }}`;
}

/**
 * Validates and renders GitHub Actions expressions in content
 * @param {string} content - The content with potential expressions
 * @param {string} source - The source identifier (file path or URL) for error messages
 * @returns {string} - Content with safe expressions rendered
 * @throws {Error} - If unsafe expressions are found
 */
function processExpressions(content, source) {
  // Pattern to match GitHub Actions expressions: ${{ ... }}
  const expressionRegex = /\$\{\{([\s\S]*?)\}\}/g;

  const matches = [...content.matchAll(expressionRegex)];

  // Reject malformed/truncated expressions containing secrets that bypass the
  // expression regex (e.g. "${{ secrets.TOKEN }T" where "}}" is missing or broken).
  // The expression regex only matches well-formed ${{ ... }} blocks, so a partial
  // "${{ secrets." sequence slips through and must be caught here as a security violation.
  // We check whether any captured expression content (m[1]) contains "secrets." to
  // distinguish a well-formed ${{ secrets.X }} (already handled below) from a malformed one.
  const partialSecretsRegex = /\$\{\{[^}]*secrets\./;
  if (partialSecretsRegex.test(content)) {
    const wellFormedSecretsMatched = matches.some(m => /secrets\./.test(m[1]));
    if (!wellFormedSecretsMatched) {
      throw new Error(
        `${ERR_VALIDATION}: ${source} contains unauthorized GitHub Actions expressions:\n` +
          `  - (partial or malformed secrets expression)\n\n` +
          "Only expressions from the safe list can be used in runtime imports.\n" +
          "Safe expressions include:\n" +
          "  - github.actor, github.repository, github.run_id, etc.\n" +
          "  - github.event.issue.number, github.event.pull_request.number, etc.\n" +
          "  - needs.*, steps.*, env.*, inputs.*, experiments.*\n\n" +
          "See documentation for the complete list of allowed expressions."
      );
    }
  }

  if (matches.length === 0) {
    return content;
  }

  core.info(`Found ${matches.length} expression(s) in ${source}`);

  const unsafeExpressions = [];
  const replacements = new Map();

  // First pass: validate all expressions
  for (const match of matches) {
    const fullMatch = match[0];
    const expr = match[1];

    // Skip multiline expressions (security: prevent injection)
    if (expr.includes("\n")) {
      unsafeExpressions.push(expr.trim());
      continue;
    }

    const trimmed = expr.trim();

    // Check if expression is safe
    if (!isSafeExpression(trimmed)) {
      unsafeExpressions.push(trimmed);
      continue;
    }

    // Expression is safe - evaluate it
    const evaluated = evaluateExpression(trimmed);
    replacements.set(fullMatch, evaluated);
  }

  // If any unsafe expressions found, throw error
  if (unsafeExpressions.length > 0) {
    const errorMsg =
      `${ERR_VALIDATION}: ${source} contains unauthorized GitHub Actions expressions:\n` +
      unsafeExpressions.map(e => `  - ${e}`).join("\n") +
      "\n\n" +
      "Only expressions from the safe list can be used in runtime imports.\n" +
      "Safe expressions include:\n" +
      "  - github.actor, github.repository, github.run_id, etc.\n" +
      "  - github.event.issue.number, github.event.pull_request.number, etc.\n" +
      "  - needs.*, steps.*, env.*, inputs.*, experiments.*\n\n" +
      "See documentation for the complete list of allowed expressions.";
    throw new Error(errorMsg);
  }

  // Second pass: replace safe expressions with evaluated values.
  // Build a single regex that matches any of the original expressions so all
  // replacements are done in one pass.  A multi-pass approach would incorrectly
  // re-replace expression syntax that appears inside an already-evaluated value
  // (e.g. an issue title that literally contains "${{ github.actor }}").
  const escapeForRegex = (/** @type {string} */ s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const pattern = new RegExp(Array.from(replacements.keys()).map(escapeForRegex).join("|"), "g");
  const result = content.replace(pattern, match => replacements.get(match) ?? match);

  core.info(`Successfully processed ${replacements.size} safe expression(s) in ${source}`);
  return result;
}

/**
 * Checks if content contains GitHub Actions macros (${{ ... }})
 * @param {string} content - The content to check
 * @returns {boolean} - True if GitHub Actions macros are found
 */
function hasGitHubActionsMacros(content) {
  return /\$\{\{[\s\S]*?\}\}/.test(content);
}

/** @type {number} Timeout in milliseconds for fetching URL content in {@link fetchUrlContent} */
const FETCH_URL_TIMEOUT_MS = 30_000;

/**
 * Fetches content from a URL with caching
 * @param {string} url - The URL to fetch
 * @param {string} cacheDir - Directory to store cached URL content
 * @returns {Promise<string>} - The fetched content
 * @throws {Error} - If URL fetch fails
 */
async function fetchUrlContent(url, cacheDir) {
  // Create cache directory if it doesn't exist
  if (!fs.existsSync(cacheDir)) {
    try {
      fs.mkdirSync(cacheDir, { recursive: true });
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to create directory ${cacheDir}: ${getErrorMessage(err)}`, { cause: err });
    }
  }

  // Generate cache filename from URL (hash it for safety)
  const crypto = require("crypto");
  const urlHash = crypto.createHash("sha256").update(url).digest("hex");
  const cacheFile = path.join(cacheDir, `url-${urlHash}.cache`);

  // Check if cached version exists and is recent (less than 1 hour old)
  if (fs.existsSync(cacheFile)) {
    let stats;
    try {
      stats = fs.statSync(cacheFile);
    } catch (err) {
      core.warning(`Failed to inspect cache file ${cacheFile}: ${getErrorMessage(err)}. Refetching URL content.`);
      stats = null;
    }
    if (stats) {
      const ageInMs = Date.now() - stats.mtimeMs;
      const oneHourInMs = 60 * 60 * 1000;

      if (ageInMs < oneHourInMs) {
        core.info(`Using cached content for URL: ${url}`);
        try {
          return fs.readFileSync(cacheFile, "utf8");
        } catch (err) {
          throw new Error(`${ERR_SYSTEM}: Failed to read file ${cacheFile}: ${getErrorMessage(err)}`, { cause: err });
        }
      }
    }
  }

  // Fetch URL content
  core.info(`Fetching content from URL: ${url}`);

  // Share a single abort signal across both the connection/headers phase and the body-reading
  // phase, since fetch()'s own timeout signal is only honored until the promise resolves.
  const timeoutController = new AbortController();
  const timeoutId = setTimeout(() => timeoutController.abort(new Error(`Timed out after ${FETCH_URL_TIMEOUT_MS}ms`)), FETCH_URL_TIMEOUT_MS);

  let response;
  let data;
  try {
    // Do not follow redirects automatically: a redirect landing on a 200 response would
    // silently broaden the fetched origin and bypass the non-200 status check below.
    response = await fetch(url, { signal: timeoutController.signal, redirect: "manual" });

    if (!response.ok) {
      throw new Error(`${ERR_API}: Failed to fetch URL ${url}: HTTP ${response.status}`);
    }

    data = await response.text();
  } catch (err) {
    if (err instanceof Error && err.message.startsWith(ERR_API)) {
      throw err;
    }
    throw new Error(`${ERR_API}: Failed to fetch URL ${url}: ${getErrorMessage(err)}`, { cause: err });
  } finally {
    clearTimeout(timeoutId);
  }

  // Cache the content
  try {
    fs.writeFileSync(cacheFile, data, "utf8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to write file ${cacheFile}: ${getErrorMessage(err)}`, { cause: err });
  }

  return data;
}

/**
 * Processes a URL import and returns content with sanitization
 * @param {string} url - The URL to fetch
 * @param {boolean} optional - Whether the import is optional
 * @param {number} [startLine] - Optional start line (1-indexed, inclusive)
 * @param {number} [endLine] - Optional end line (1-indexed, inclusive)
 * @returns {Promise<string>} - The processed URL content
 * @throws {Error} - If URL fetch fails or content is invalid
 */
async function processUrlImport(url, optional, startLine, endLine) {
  const cacheDir = "/tmp/gh-aw/url-cache";

  // Fetch URL content (with caching)
  let content;
  try {
    content = await fetchUrlContent(url, cacheDir);
  } catch (error) {
    if (optional) {
      const errorMessage = getErrorMessage(error);
      core.warning(`Optional runtime import URL failed: ${url}: ${errorMessage}`);
      return "";
    }
    throw error;
  }

  // If line range is specified, extract those lines first (before other processing)
  if (startLine !== undefined || endLine !== undefined) {
    const lines = content.split("\n");
    const totalLines = lines.length;

    // Validate line numbers (1-indexed)
    const start = startLine !== undefined ? startLine : 1;
    const end = endLine !== undefined ? endLine : totalLines;

    if (start < 1 || start > totalLines) {
      throw new Error(`${ERR_VALIDATION}: Invalid start line ${start} for URL ${url} (total lines: ${totalLines})`);
    }
    if (end < 1 || end > totalLines) {
      throw new Error(`${ERR_VALIDATION}: Invalid end line ${end} for URL ${url} (total lines: ${totalLines})`);
    }
    if (start > end) {
      throw new Error(`${ERR_VALIDATION}: Start line ${start} cannot be greater than end line ${end} for URL ${url}`);
    }

    // Extract lines (convert to 0-indexed)
    content = lines.slice(start - 1, end).join("\n");
  }

  // Check for front matter and warn
  if (hasFrontMatter(content)) {
    core.debug(`URL ${url} contains front matter which will be ignored in runtime import`);
    // Remove front matter (everything between first --- and second ---)
    const lines = content.split("\n");
    let inFrontMatter = false;
    let frontMatterCount = 0;
    const processedLines = [];

    for (const line of lines) {
      if (line.trim() === "---" || line.trim() === "---\r") {
        frontMatterCount++;
        if (frontMatterCount === 1) {
          inFrontMatter = true;
          continue;
        } else if (frontMatterCount === 2) {
          inFrontMatter = false;
          continue;
        }
      }
      if (!inFrontMatter && frontMatterCount >= 2) {
        processedLines.push(line);
      }
    }
    content = processedLines.join("\n");
  }

  // Remove XML comments
  content = removeXMLComments(content);

  // Process GitHub Actions expressions (validate and render safe ones)
  if (hasGitHubActionsMacros(content)) {
    content = processExpressions(content, `URL ${url}`);
  }

  // Close any unterminated "## skill:"/"## agent:" block so this import
  // cannot swallow content that gets spliced in after it (see
  // closeUnterminatedInlineMarkers above).
  content = closeUnterminatedInlineMarkers(content);

  return content;
}

/**
 * Wraps bare GitHub expressions in template conditionals with ${{ }}
 * Transforms {{#if expression}} to {{#if ${{ expression }} }} if expression looks like a GitHub Actions expression
 * @param {string} content - The markdown content
 * @returns {string} - Content with GitHub expressions wrapped
 */
function wrapExpressionsInTemplateConditionals(content) {
  // Pattern to match {{#if expression}} where expression is not already wrapped in ${{ }}
  const pattern = /\{\{#if\s+((?:\$\{\{[^\}]*\}\}|[^\}])*?)\s*\}\}/g;

  return content.replace(pattern, (match, expr) => {
    const trimmed = expr.trim();

    // If already wrapped in ${{ }}, return as-is
    if (trimmed.startsWith("${{") && trimmed.endsWith("}}")) {
      return match;
    }

    // If it's an environment variable reference (starts with ${), return as-is
    if (trimmed.startsWith("${")) {
      return match;
    }

    // If it's a placeholder reference (starts with __), return as-is
    if (trimmed.startsWith("__")) {
      return match;
    }

    // Boolean/null literals are self-evaluating — the template renderer's isTruthy()
    // handles them directly. Wrapping them would create __GH_AW_TRUE__/__GH_AW_FALSE__/__GH_AW_NULL__
    // placeholders that cannot be resolved at runtime (no corresponding env var is set),
    // causing the placeholder validator to flag them as unsubstituted.
    if (trimmed === "true" || trimmed === "false" || trimmed === "null") {
      return match;
    }

    // Only process expressions whose root matches a known GitHub Actions namespace.
    // Restricting to explicit prefixes prevents non-GH dotted identifiers such as
    // `experiments.foo` (resolved later by interpolate_prompt.cjs via experiment
    // substitution) from being incorrectly collapsed to {{#if }} (falsy) here.
    const looksLikeGitHubExpr = trimmed.startsWith("github.") || trimmed.startsWith("needs.") || trimmed.startsWith("steps.") || trimmed.startsWith("env.") || trimmed.startsWith("inputs.");

    if (!looksLikeGitHubExpr) {
      // Not a GitHub Actions expression, leave as-is
      return match;
    }

    // Evaluate the condition inline so that the template renderer (renderMarkdownTemplate /
    // isTruthy) receives a concrete boolean sentinel rather than a raw value string or an
    // always-truthy __GH_AW__ placeholder.
    //
    // We emit "{{#if true}}" / "{{#if }}" rather than the raw resolved value to prevent
    // template tag injection: if the resolved value contained "}}" it would prematurely
    // close the {{#if ...}} tag and corrupt the rendered output.
    const evaluated = evaluateExpression(trimmed);
    const shouldRenderBlock = !evaluated.startsWith("${{") && isTruthy(evaluated);
    return shouldRenderBlock ? `{{#if true}}` : `{{#if }}`;
  });
}

/**
 * Extracts GitHub expressions from wrapped template conditionals and replaces them with placeholders
 * Transforms {{#if ${{ expression }} }} to {{#if __GH_AW_PLACEHOLDER__ }}
 * @param {string} content - The markdown content with wrapped expressions
 * @returns {string} - Content with expressions replaced by placeholders
 */
function extractAndReplacePlaceholders(content) {
  // Pattern to match {{#if ${{ expression }} }} where expression needs to be extracted
  const pattern = /\{\{#if\s+\$\{\{\s*(.*?)\s*\}\}\s*\}\}/g;

  return content.replace(pattern, (match, expr) => {
    const trimmed = expr.trim();

    // Generate placeholder name from expression
    // Convert dots and special chars to underscores and uppercase
    const placeholder = generatePlaceholderName(trimmed);

    // Return the conditional with placeholder
    return `{{#if __${placeholder}__ }}`;
  });
}

/**
 * Generates a placeholder name from a GitHub expression
 * @param {string} expr - The GitHub expression (e.g., "github.event.issue.number")
 * @returns {string} - The placeholder name (e.g., "GH_AW_GITHUB_EVENT_ISSUE_NUMBER")
 */
function generatePlaceholderName(expr) {
  // Check if it's a simple property access chain (e.g., github.event.issue.number)
  const simplePattern = /^[a-zA-Z][a-zA-Z0-9_.]*$/;

  if (simplePattern.test(expr)) {
    // Convert dots to underscores and uppercase
    // e.g., "github.event.issue.number" -> "GH_AW_GITHUB_EVENT_ISSUE_NUMBER"
    return "GH_AW_" + expr.replace(/\./g, "_").toUpperCase();
  }

  // For boolean literals, use special placeholders
  if (expr === "true") {
    return "GH_AW_TRUE";
  }
  if (expr === "false") {
    return "GH_AW_FALSE";
  }
  if (expr === "null") {
    return "GH_AW_NULL";
  }

  // For complex expressions or unknown variables, create a generic placeholder
  // Replace non-alphanumeric characters with underscores
  const sanitized = expr.replace(/[^a-zA-Z0-9_]/g, "_").toUpperCase();
  return "GH_AW_" + sanitized;
}

/**
 * Resolves a runtime-import file path to its normalized absolute path.
 * @param {string} filepathOrUrl - File path (not URL)
 * @param {string} workspaceDir - The GITHUB_WORKSPACE directory path
 * @returns {{filepath: string, normalizedPath: string, normalizedBaseFolder: string}}
 */
function resolveRuntimeImportFilePath(filepathOrUrl, workspaceDir) {
  if (/^https?:\/\//i.test(filepathOrUrl)) {
    throw new Error(`${ERR_VALIDATION}: Expected file path for runtime import, received URL: ${filepathOrUrl}`);
  }

  let filepath = filepathOrUrl;
  let isAgentsPath = false;

  // Strip leading "/" or "//" (and any number of slashes) for repo-root-absolute paths
  // (e.g. /.agents/skills/..., //.github/agents/...).
  // After stripping, the existing .agents/ and .github/ prefix checks handle resolution correctly.
  // Only strip when the result begins with .agents/ or .github/ to preserve security restrictions.
  if (filepath.startsWith("/")) {
    const stripped = filepath.replace(/^\/+/, "");
    if (stripped.startsWith(".agents/") || stripped.startsWith(".agents\\") || stripped.startsWith(".github/") || stripped.startsWith(".github\\")) {
      filepath = stripped;
    } else {
      throw new Error(`${ERR_VALIDATION}: Security: Path ${filepathOrUrl} must be within .agents/ or .github/ folder`);
    }
  }

  // Check if this is a .agents/ path (top-level folder for skills)
  if (filepath.startsWith(".agents/")) {
    isAgentsPath = true;
    // Keep .agents/ as is - it's a top-level folder at workspace root
  } else if (filepath.startsWith(".agents\\")) {
    isAgentsPath = true;
    // Keep .agents\ as is - it's a top-level folder at workspace root (Windows)
  } else if (filepath.startsWith(".github/")) {
    // Trim .github/ prefix if provided (support both .github/file and file)
    filepath = filepath.substring(8); // Remove ".github/"
  } else if (filepath.startsWith(".github\\")) {
    filepath = filepath.substring(8); // Remove ".github\" (Windows)
  } else {
    // If path doesn't start with .github or .agents, prefix with workflows/
    // This makes imports like "a.md" resolve to ".github/workflows/a.md"
    filepath = path.join("workflows", filepath);
  }

  // Remove leading ./ or ../ if present (only for non-agents paths)
  if (!isAgentsPath) {
    if (filepath.startsWith("./")) {
      filepath = filepath.substring(2);
    } else if (filepath.startsWith(".\\")) {
      filepath = filepath.substring(2);
    }
  }
  // Note: We don't allow ../ paths as they would escape the base folder

  // Construct the absolute path - .agents paths are relative to workspace root, others to .github
  let absolutePath, normalizedPath, baseFolder, normalizedBaseFolder;

  if (isAgentsPath) {
    // .agents/ paths resolve to top-level .agents folder at workspace root
    baseFolder = workspaceDir;
    absolutePath = path.resolve(workspaceDir, filepath);
    normalizedPath = path.normalize(absolutePath);
    normalizedBaseFolder = path.normalize(path.join(workspaceDir, ".agents"));

    // Security check: ensure the resolved path is within the workspace
    const relativePath = path.relative(path.normalize(baseFolder), normalizedPath);
    if (relativePath === ".." || relativePath.startsWith(".." + path.sep) || path.isAbsolute(relativePath)) {
      throw new Error(`${ERR_CONFIG}: Security: Path ${filepathOrUrl} must be within workspace (resolves to: ${relativePath})`);
    }
    // Additional check: ensure path stays within .agents folder
    if (!relativePath.startsWith(".agents" + path.sep) && relativePath !== ".agents") {
      throw new Error(`${ERR_VALIDATION}: Security: Path ${filepathOrUrl} must be within .agents folder`);
    }
  } else {
    // Regular paths resolve within .github folder
    const githubFolder = path.join(workspaceDir, ".github");
    baseFolder = githubFolder;
    absolutePath = path.resolve(githubFolder, filepath);
    normalizedPath = path.normalize(absolutePath);
    normalizedBaseFolder = path.normalize(githubFolder);

    // Security check: ensure the resolved path is within the .github folder
    const relativePath = path.relative(normalizedBaseFolder, normalizedPath);
    if (relativePath.startsWith("..") || path.isAbsolute(relativePath)) {
      throw new Error(`${ERR_VALIDATION}: Security: Path ${filepathOrUrl} must be within .github folder (resolves to: ${relativePath})`);
    }
  }

  return { filepath, normalizedPath, normalizedBaseFolder };
}

/**
 * Verifies the resolved runtime-import target remains inside the allowed base
 * after following symlinks.
 * @param {string} normalizedPath
 * @param {string} normalizedBaseFolder
 * @param {string} filepathOrUrl
 */
function assertRuntimeImportRealPathWithinBase(normalizedPath, normalizedBaseFolder, filepathOrUrl) {
  let realPath;
  let realBase;
  try {
    realPath = fs.realpathSync(normalizedPath);
    realBase = fs.realpathSync(normalizedBaseFolder);
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to resolve runtime import path ${filepathOrUrl}: ${getErrorMessage(err)}`, { cause: err });
  }
  const relativePath = path.relative(realBase, realPath);
  if (relativePath === ".." || relativePath.startsWith(".." + path.sep) || path.isAbsolute(relativePath)) {
    throw new Error(`${ERR_VALIDATION}: Security: Path ${filepathOrUrl} must remain within its allowed folder after resolving symlinks`);
  }
}

/**
 * Reads and processes a file or URL for runtime import
 * @param {string} filepathOrUrl - The path to the file (relative to GITHUB_WORKSPACE) or URL to import
 * @param {boolean} optional - Whether the import is optional (true for {{#runtime-import? filepath}})
 * @param {string} workspaceDir - The GITHUB_WORKSPACE directory path
 * @param {number} [startLine] - Optional start line (1-indexed, inclusive)
 * @param {number} [endLine] - Optional end line (1-indexed, inclusive)
 * @returns {Promise<string>} - The processed file or URL content, or empty string if optional and file not found
 * @throws {Error} - If file/URL is not found and import is not optional, or if GitHub Actions macros are detected
 */
async function processRuntimeImport(filepathOrUrl, optional, workspaceDir, startLine, endLine) {
  // Check if this is a URL
  if (/^https?:\/\//i.test(filepathOrUrl)) {
    return await processUrlImport(filepathOrUrl, optional, startLine, endLine);
  }

  // Otherwise, process as a file
  const { filepath, normalizedPath, normalizedBaseFolder } = resolveRuntimeImportFilePath(filepathOrUrl, workspaceDir);

  // Check if file exists
  if (!fs.existsSync(normalizedPath)) {
    if (optional) {
      core.warning(`Optional runtime import file not found: ${normalizedPath}`);
      return "";
    }
    throw new Error(`${ERR_SYSTEM}: Runtime import file not found: ${normalizedPath}`);
  }
  assertRuntimeImportRealPathWithinBase(normalizedPath, normalizedBaseFolder, filepathOrUrl);

  // Read the file
  let content;
  try {
    content = fs.readFileSync(normalizedPath, "utf8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to read file ${normalizedPath}: ${getErrorMessage(err)}`, { cause: err });
  }

  // If line range is specified, extract those lines first (before other processing)
  if (startLine !== undefined || endLine !== undefined) {
    const lines = content.split("\n");
    const totalLines = lines.length;

    // Validate line numbers (1-indexed)
    const start = startLine !== undefined ? startLine : 1;
    const end = endLine !== undefined ? endLine : totalLines;

    if (start < 1 || start > totalLines) {
      throw new Error(`${ERR_VALIDATION}: Invalid start line ${start} for file ${filepath} (total lines: ${totalLines})`);
    }
    if (end < 1 || end > totalLines) {
      throw new Error(`${ERR_VALIDATION}: Invalid end line ${end} for file ${filepath} (total lines: ${totalLines})`);
    }
    if (start > end) {
      throw new Error(`${ERR_VALIDATION}: Start line ${start} cannot be greater than end line ${end} for file ${filepath}`);
    }

    // Extract lines (convert to 0-indexed)
    content = lines.slice(start - 1, end).join("\n");
  }

  // Check for front matter and warn
  if (hasFrontMatter(content)) {
    if (shouldPreserveFrontMatter(filepath)) {
      core.debug(`File ${filepath} contains front matter which will be preserved for agent runtime import`);
    } else {
      core.debug(`File ${filepath} contains front matter which will be ignored in runtime import`);
      // Remove front matter (everything between first --- and second ---)
      const lines = content.split("\n");
      let inFrontMatter = false;
      let frontMatterCount = 0;
      const processedLines = [];

      for (const line of lines) {
        if (line.trim() === "---" || line.trim() === "---\r") {
          frontMatterCount++;
          if (frontMatterCount === 1) {
            inFrontMatter = true;
            continue;
          } else if (frontMatterCount === 2) {
            inFrontMatter = false;
            continue;
          }
        }
        if (!inFrontMatter && frontMatterCount >= 2) {
          processedLines.push(line);
        }
      }
      content = processedLines.join("\n");
    }
  }

  // Remove XML comments
  content = removeXMLComments(content);

  // Neutralize AI-model control tags to prevent prompt injection via runtime-imported files
  content = neutralizeSystemTags(content);

  // Wrap expressions in template conditionals
  // This handles {{#if expression}} where expression is not already wrapped in ${{ }}
  content = wrapExpressionsInTemplateConditionals(content);

  // Extract and replace GitHub expressions in template conditionals with placeholders
  // This transforms {{#if ${{ expression }} }} to {{#if __GH_AW_PLACEHOLDER__ }}
  content = extractAndReplacePlaceholders(content);

  // Process GitHub Actions expressions (validate and render safe ones)
  if (hasGitHubActionsMacros(content)) {
    content = processExpressions(content, `File ${filepath}`);
  }

  // Close any unterminated "## skill:"/"## agent:" block so this import
  // cannot swallow content that gets spliced in after it (see
  // closeUnterminatedInlineMarkers above).
  content = closeUnterminatedInlineMarkers(content);

  return content;
}

/**
 * Resolves an import path/URL to a canonical key for deduplication checks.
 * @param {string} filepathOrUrl
 * @param {string} workspaceDir
 * @param {number} [startLine]
 * @param {number} [endLine]
 * @returns {string}
 */
function resolveRuntimeImportKey(filepathOrUrl, workspaceDir, startLine, endLine) {
  const rangeSuffix = startLine !== undefined && endLine !== undefined ? `:${startLine}-${endLine}` : "";

  if (/^https?:\/\//i.test(filepathOrUrl)) {
    return `${filepathOrUrl}${rangeSuffix}`;
  }

  const { normalizedPath } = resolveRuntimeImportFilePath(filepathOrUrl, workspaceDir);

  return `${normalizedPath}${rangeSuffix}`;
}

/**
 * @typedef {Object} ImportTreeNode
 * @property {string} macro - The original {{#runtime-import ...}} macro text
 * @property {string} src - The resolved file path or URL
 * @property {boolean} optional - Whether the import was optional ({{#runtime-import?}})
 * @property {number|null} startLine - Start line for partial imports, or null
 * @property {number|null} endLine - End line for partial imports, or null
 * @property {string} rawContent - File content before nested import expansion (or cached content)
 * @property {boolean} [cached] - True when content was served from cache (children were already expanded)
 * @property {ImportTreeNode[]} children - Nested import nodes
 */

/**
 * Processes all runtime-import macros in the content recursively.
 * Also handles body-level {{#import}} directives by normalizing them to
 * {{#runtime-import}} before processing, so that both the frontmatter `imports:`
 * style and the inline `{{#import filepath}}` style resolve correctly at runtime.
 * @param {string} content - The markdown content containing runtime-import macros
 * @param {string} workspaceDir - The GITHUB_WORKSPACE directory path
 * @param {Set<string>} [importedFiles] - Set of already imported files (for recursion tracking)
 * @param {Map<string, string>} [importCache] - Cache of imported file contents (for deduplication)
 * @param {Array<string>} [importStack] - Stack of currently importing files (for circular dependency detection)
 * @param {ImportTreeNode[]|null} [parentTreeChildren] - Array to push import tree nodes into, or null to skip tree building
 * @param {Map<string, string>} [rawImportCache] - Cache of raw (pre-expansion) file contents, used to set rawContent on cached tree nodes
 * @param {Set<string>} [resolvedInParent] - Canonical import keys already resolved in parent/sibling context; skipped during recursion
 * @returns {Promise<string>} - Content with runtime-import macros replaced by file/URL contents
 */
async function processRuntimeImports(content, workspaceDir, importedFiles = new Set(), importCache = new Map(), importStack = [], parentTreeChildren = null, rawImportCache = new Map(), resolvedInParent = new Set()) {
  // Normalize body-level {{#import}} directives to {{#runtime-import}} equivalents.
  // {{#import}} is deprecated — use {{#runtime-import}} or the 'imports:' frontmatter field instead.
  // Both colon and no-colon syntax are supported for backward compatibility:
  //   {{#import filepath}}   {{#import? filepath}}
  //   {{#import: filepath}}  {{#import?: filepath}}
  // Use [^\{\}] to avoid matching across brace boundaries (e.g. nested expressions).
  //
  // To avoid treating documentation examples inside backtick code spans (e.g. `{{#import ...}}`)
  // as real directives, temporarily replace inline code spans with placeholders before matching.
  // Note: only single-line backtick spans are protected (multi-line spans use fences, not backticks).
  const codeSpanPlaceholders = [];
  const contentWithPlaceholders = content.replace(/`[^`\n]+`/g, match => {
    const idx = codeSpanPlaceholders.length;
    codeSpanPlaceholders.push(match);
    // Use a sentinel that cannot appear in normal workflow content.
    return `\u0000GH_AW_CODESPAN_${idx}_GH_AW\u0000`;
  });
  const bodyImportRe = /\{\{#import(\?)?(?:[ \t]+|[ \t]*:[ \t]*)([^\{\}]+?)\}\}/g;
  let bodyImportCount = 0;
  const normalizedContent = contentWithPlaceholders.replace(bodyImportRe, (_, optional, importPath) => {
    bodyImportCount++;
    const trimmedPath = importPath.trim();
    return `{{#runtime-import${optional || ""} ${trimmedPath}}}`;
  });
  // Restore inline code spans after directive normalization
  content = normalizedContent.replace(/\u0000GH_AW_CODESPAN_(\d+)_GH_AW\u0000/g, (_, idx) => codeSpanPlaceholders[parseInt(idx, 10)]);
  if (bodyImportCount > 0) {
    core.warning(`Deprecated: ${bodyImportCount} {{#import}} directive(s) found. ` + `Use {{#runtime-import}} or the 'imports:' frontmatter field instead.`);
  }

  // Pattern to match {{#runtime-import filepath}} or {{#runtime-import? filepath}}
  // Captures: optional flag (?), whitespace, filepath/URL (which may include :startline-endline)
  const pattern = /\{\{#runtime-import(\?)?[ \t]+([^\}]+?)\}\}/g;

  let processedContent = content;
  const matches = [];
  let match;

  // Reset regex state and collect all matches
  pattern.lastIndex = 0;

  while ((match = pattern.exec(content)) !== null) {
    const optional = match[1] === "?";
    const filepathWithRange = match[2].trim();
    const fullMatch = match[0];

    // Parse filepath/URL and optional line range (filepath:startline-endline)
    const rangeMatch = filepathWithRange.match(/^(.+?):(\d+)-(\d+)$/);
    let filepathOrUrl, startLine, endLine;

    if (rangeMatch) {
      filepathOrUrl = rangeMatch[1];
      startLine = parseInt(rangeMatch[2], 10);
      endLine = parseInt(rangeMatch[3], 10);
    } else {
      filepathOrUrl = filepathWithRange;
      startLine = undefined;
      endLine = undefined;
    }

    matches.push({
      fullMatch,
      filepathOrUrl,
      optional,
      startLine,
      endLine,
      filepathWithRange,
    });
  }

  // Process all imports sequentially (to handle async URLs)
  const resolvedInThisCall = new Set();
  for (const matchData of matches) {
    const { fullMatch, filepathOrUrl, optional, startLine, endLine, filepathWithRange } = matchData;
    let importKey;
    try {
      importKey = resolveRuntimeImportKey(filepathOrUrl, workspaceDir, startLine, endLine);
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      throw new Error(`${ERR_API}: Failed to process runtime import for ${filepathWithRange}: ${errorMessage}`);
    }

    // Skip imports already resolved in the parent/sibling context.
    // This avoids duplicate expansion when the workflow file self-imports and
    // recursively encounters imports that were already expanded in the outer pass.
    if (resolvedInParent.has(importKey)) {
      // Intentionally replace with empty string (instead of cached content):
      // this branch exists specifically to prevent duplicate prompt blocks when
      // recursively traversing a self-imported workflow body.
      processedContent = processedContent.replace(fullMatch, "");
      core.info(`Skipping already resolved import for ${filepathWithRange}`);
      continue;
    }

    // Check if this file is already in the import cache
    if (importCache.has(filepathWithRange)) {
      // Reuse cached content
      const cachedContent = importCache.get(filepathWithRange);
      if (cachedContent !== undefined) {
        processedContent = processedContent.replace(fullMatch, () => cachedContent);
        core.info(`Reusing cached content for ${filepathWithRange}`);
        if (parentTreeChildren !== null) {
          // Use the raw (pre-expansion) content from rawImportCache so that
          // rawContent is consistent between first and subsequent occurrences.
          const rawContent = rawImportCache.get(filepathWithRange) ?? cachedContent;
          parentTreeChildren.push({
            macro: fullMatch,
            src: filepathOrUrl,
            optional,
            startLine: startLine ?? null,
            endLine: endLine ?? null,
            rawContent,
            cached: true,
            children: [],
          });
        }
        continue;
      }
    }

    // Check for circular dependencies
    if (importStack.includes(filepathWithRange)) {
      const cycle = [...importStack, filepathWithRange].join(" -> ");
      throw new Error(`${ERR_PARSE}: Circular dependency detected: ${cycle}`);
    }

    // Add to import stack for circular dependency detection
    importStack.push(filepathWithRange);

    try {
      // Import the file content
      let importedContent = await processRuntimeImport(filepathOrUrl, optional, workspaceDir, startLine, endLine);

      // Capture raw content before any nested expansion so tree nodes always
      // record the pre-recursion state (consistent with first-occurrence behaviour).
      const rawContent = importedContent;

      // Build a tree node for this import and append it immediately so that
      // parentTreeChildren.push() is only called when tree building is active.
      // treeNodeChildren is used below to pass into the recursive call.
      /** @type {ImportTreeNode[]} */
      const treeNodeChildren = [];
      if (parentTreeChildren !== null) {
        parentTreeChildren.push({
          macro: fullMatch,
          src: filepathOrUrl,
          optional,
          startLine: startLine ?? null,
          endLine: endLine ?? null,
          rawContent,
          children: treeNodeChildren,
        });
      }

      // Recursively process any runtime-import or body-level {{#import}} macros in the
      // imported content. The recursive call to processRuntimeImports will normalize
      // any {{#import}} directives before processing them.
      if (importedContent && /\{\{#(?:runtime-import|import)/.test(importedContent)) {
        core.info(`Recursively processing imports in ${filepathWithRange}`);
        const inheritedResolved = new Set([...resolvedInParent, ...resolvedInThisCall]);
        importedContent = await processRuntimeImports(importedContent, workspaceDir, importedFiles, importCache, [...importStack], parentTreeChildren !== null ? treeNodeChildren : null, rawImportCache, inheritedResolved);
      }

      // Cache the fully processed content and the raw pre-expansion content
      importCache.set(filepathWithRange, importedContent);
      rawImportCache.set(filepathWithRange, rawContent);
      importedFiles.add(filepathWithRange);

      // Replace the macro with the imported content
      processedContent = processedContent.replace(fullMatch, () => importedContent);
      resolvedInThisCall.add(importKey);
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      throw new Error(`${ERR_API}: Failed to process runtime import for ${filepathWithRange}: ${errorMessage}`);
    } finally {
      // Remove from import stack
      importStack.pop();
    }
  }

  return processedContent;
}

module.exports = {
  processRuntimeImports,
  processRuntimeImport,
  fetchUrlContent,
  closeUnterminatedInlineMarkers,
  hasFrontMatter,
  removeXMLComments,
  neutralizeSystemTags,
  hasGitHubActionsMacros,
  isSafeExpression,
  evaluateExpression,
  processExpressions,
  wrapExpressionsInTemplateConditionals,
  extractAndReplacePlaceholders,
  generatePlaceholderName,
};
