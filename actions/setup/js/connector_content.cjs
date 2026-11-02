// @ts-check

require("./shim.cjs");

const { sanitizeTitle } = require("./sanitize_title.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { getFooterMessageParts, buildFooterBlocks, getBodyFooterBlocks, getAgenticWorkflowMetadata } = require("./messages_footer.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { renderReport } = require("./report_ast.cjs");

/**
 * Formatting belongs to the trusted publisher, before transport validation and
 * staging. Raw Go/Node transport APIs do not perform this publication step.
 * The plan must be the normalized snapshot returned by decodePlan().
 * @param {any} plan
 * @param {string} operation
 * @param {Record<string, any>} args
 * @param {{config?: {footer?: boolean|string, body_footer?: string}}} [options]
 * @returns {Record<string, any>}
 */
function formatConnectorArguments(plan, operation, args, { config = {} } = {}) {
  const capability = plan?.spec?.safeOutputs?.[operation];
  if (!capability || !plan.binding?.safeOutputs?.includes(operation)) throw new Error("Connector content operation is not selected by the frozen plan");
  if (!args || typeof args !== "object" || Array.isArray(args)) throw new TypeError("Connector content arguments must be an object");
  const formatted = { ...args };
  for (const [name, input] of Object.entries(/** @type {Record<string, any>} */ capability.inputs || {})) {
    if (!input.sanitize || !Object.hasOwn(args, name)) continue;
    if (input.type !== "string" || typeof args[name] !== "string") throw new TypeError(`Connector content input ${name} must be a string`);
    if (!Number.isSafeInteger(input.maxLength) || input.maxLength < 1) throw new TypeError(`Connector content input ${name} requires a positive maxLength`);
    const minimum = input.minLength ?? 0;
    if (!Number.isSafeInteger(minimum) || minimum < 0 || minimum > input.maxLength) throw new TypeError(`Connector content input ${name} has an invalid minLength`);
    if (input.sanitize === "title") {
      const title = Array.from(sanitizeTitle(args[name], "", Number.MAX_SAFE_INTEGER).trim())
        .slice(0, input.maxLength)
        .join("")
        .trimEnd();
      assertMinimum(name, title, Math.max(20, minimum));
      formatted[name] = title;
    } else if (input.sanitize === "body") {
      const body = sanitizeContent(args[name], { maxLength: Number.MAX_SAFE_INTEGER, maxLines: Number.MAX_SAFE_INTEGER });
      formatted[name] = formatBody(name, body, input.maxLength, minimum, config);
    } else {
      throw new TypeError(`Connector content input ${name} has an unsupported sanitization policy`);
    }
  }
  return formatted;
}

/** @param {string} name @param {string} text @param {number} minimum */
function assertMinimum(name, text, minimum) {
  if (Array.from(text).length < minimum) throw new Error(`Connector content input ${name} must contain at least ${minimum} Unicode code points after sanitization and sizing`);
}

/**
 * @param {{footer?: boolean|string, body_footer?: string}} config
 * @returns {import("./report_ast.cjs").ReportFooter}
 */
function buildConnectorFooter(config) {
  if (config.body_footer !== undefined && typeof config.body_footer !== "string") throw new TypeError("Connector body_footer configuration must be a string");
  // Validate before the compatibility message helper can fall back on malformed JSON.
  if (process.env.GH_AW_SAFE_OUTPUT_MESSAGES) {
    let messages;
    try {
      messages = JSON.parse(process.env.GH_AW_SAFE_OUTPUT_MESSAGES);
    } catch (error) {
      throw new TypeError("Invalid connector safe-output messages JSON", { cause: error });
    }
    if (!messages || typeof messages !== "object" || Array.isArray(messages)) throw new TypeError("Connector safe-output messages configuration must be an object");
  }
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || process.env.GITHUB_WORKFLOW || "Workflow";
  const [owner, repo] = (process.env.GITHUB_REPOSITORY || "").split("/");
  const runId = process.env.GITHUB_RUN_ID || "";
  const runUrl = owner && repo && /^[1-9]\d*$/.test(runId) ? buildWorkflowRunUrl({ runId }, { owner, repo }) : "";
  const parts = parseBoolTemplatable(config.footer, true) ? getFooterMessageParts({ workflowName, runUrl }) : [];
  // A local preview has no Actions run. Keep the helper's generated attribution,
  // but render its absent run link as text instead of inventing a URL.
  const localParts = parts.map(part => (typeof part !== "string" && part.type === "quote_line" ? { ...part, children: part.children.flatMap(node => (node.type === "link" && !node.url ? node.children : [node])) } : part));
  const [workflow, ...fields] = getAgenticWorkflowMetadata(workflowName, runUrl);
  return {
    type: "footer",
    agentTainted: false,
    children: [...getBodyFooterBlocks(config.body_footer, { workflowName, runUrl }), ...buildFooterBlocks(localParts)],
    markers: [{ type: "xml_marker", name: "gh-aw-agentic-workflow", value: workflow.value, fields: fields.filter(field => field.value !== ""), agentTainted: false }],
  };
}

/**
 * Report rendering keeps unclosed agent code/markup from swallowing trusted
 * footer nodes. Size only agent content; count code points, not UTF-16 units.
 * @param {string} name
 * @param {string} body
 * @param {number} maximum
 * @param {number} minimum
 * @param {{footer?: boolean|string, body_footer?: string}} config
 */
function formatBody(name, body, maximum, minimum, config) {
  const footer = buildConnectorFooter(config);
  /** @param {string} text */
  const children = text => parseReportMarkdown(text, { minimumHeadingLevel: 2, agentTainted: true });
  /** @param {string} text */
  const render = text => renderReport({ type: "report", title: "Connector content", children: children(text), footer }).body.trimEnd();
  const protectedLength = Array.from(render("")).length;
  if (protectedLength > maximum) throw new Error(`Connector content input ${name}: generated footer alone exceeds maxLength ${maximum} Unicode code points`);
  const points = Array.from(body);
  let count = points.length;
  let candidate = body;
  let output = render(candidate);
  let length = Array.from(output).length;
  while (length > maximum) {
    count = Math.max(0, count - Math.max(1, length - maximum));
    candidate = points.slice(0, count).join("").trimEnd();
    output = render(candidate);
    length = Array.from(output).length;
  }
  const sizedBody = renderReport({ type: "report", title: "Connector content", children: children(candidate) }).body.trimEnd();
  assertMinimum(name, sizedBody, minimum);
  return output;
}

module.exports = { formatConnectorArguments };
