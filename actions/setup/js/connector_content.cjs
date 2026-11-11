// @ts-check

require("./shim.cjs");

const { sanitizeTitle } = require("./sanitize_title.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { getFooterMessageParts, buildFooterBlocks, getBodyFooterBlocks, getAgenticWorkflowMetadata } = require("./messages_footer.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { renderReport, redactReportForRendering, assertRedactedReport } = require("./report_ast.cjs");

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
    } else if (["body", "body-plain", "body-plain-compact"].includes(input.sanitize)) {
      const body = sanitizeContent(args[name], { maxLength: Number.MAX_SAFE_INTEGER, maxLines: Number.MAX_SAFE_INTEGER });
      formatted[name] = formatBody(name, body, input.maxLength, minimum, config, input.sanitize);
    } else {
      throw new TypeError(`Connector content input ${name} has an unsupported sanitization policy`);
    }
  }
  return formatted;
}

/** @param {(import("./report_ast.cjs").ReportBlock | import("./report_ast.cjs").ReportInline)[]} nodes @param {string} separator @returns {string} */
function plainNodes(nodes, separator) {
  return nodes.map(plainNode).join(separator);
}

/** @param {import("./report_ast.cjs").ReportBlock | import("./report_ast.cjs").ReportInline} node @returns {string} */
function plainNode(node) {
  switch (node.type) {
    case "text":
    case "code":
    case "code_block":
      return node.value;
    case "soft_break":
    case "hard_break":
      return "\n";
    case "paragraph":
    case "heading":
    case "strong":
    case "emphasis":
    case "strike":
    case "subscript":
      return plainNodes(node.children, "");
    case "task":
      return `[${node.checked ? "x" : " "}] ${plainNodes(node.children, "")}`;
    case "link":
      return `${plainNodes(node.children, "")} (${node.url})`;
    case "image":
      return `${node.alt} (${node.url})`;
    case "mention":
      return `@${node.login}`;
    case "issue_reference":
      return `#${node.number}`;
    case "closing_reference":
      return node.enabled ? `${node.action} ${node.target.repository || ""}#${node.target.number}` : "";
    case "quote":
    case "alert":
      return plainNodes(node.children, "\n\n");
    case "details":
      return `${typeof node.summary === "string" ? node.summary : plainNodes(node.summary, "")}\n${plainNodes(node.children, "\n\n")}`;
    case "list":
      return node.items.map((item, index) => `${node.ordered ? `${index + 1}.` : "-"} ${plainNodes(item, "\n\n")}`).join("\n");
    case "table":
      return [node.headers, ...node.rows].map(row => row.map(cell => plainNodes(cell, "")).join(" | ")).join("\n");
    case "structured_data":
      return JSON.stringify(node.data);
    case "xml_marker":
      return `${node.name}${node.value === undefined ? "" : `: ${node.value}`}${node.fields?.map(field => `, ${field.key}: ${field.value}`).join("") || ""}`;
    case "json_marker":
      return `${node.name}: ${JSON.stringify(node.data)}`;
    case "review_marker":
      return `gh-aw-workflow-call-id: ${node.workflowCallId}`;
    case "marker":
      return node.name;
    case "expiration":
      return `Expires: ${node.expiresAt}`;
    case "separator":
      return "---";
    default:
      throw new TypeError("Unsupported connector plain-text report node");
  }
}

/** @param {import("./report_ast.cjs").Report} report @param {boolean} compact */
function renderPlainReport(report, compact) {
  const redacted = redactReportForRendering(report);
  assertRedactedReport(redacted);
  const text = [plainNodes(redacted.children, "\n\n"), redacted.footer ? plainNodes(redacted.footer.children, "\n\n") : "", redacted.footer ? plainNodes(redacted.footer.markers, "\n") : ""].filter(Boolean).join("\n\n").trim();
  return compact ? text.replace(/ {2,}/g, " ").replace(/\n{2,}/g, "\n") : text;
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
 * @param {string} format
 */
function formatBody(name, body, maximum, minimum, config, format = "body") {
  const footer = buildConnectorFooter(config);
  /** @param {string} text */
  const children = text => parseReportMarkdown(text, { minimumHeadingLevel: 2, agentTainted: true });
  /** @type {(report: import("./report_ast.cjs").Report) => string} */
  const serialize = format === "body" ? report => renderReport(report).body.trimEnd() : report => renderPlainReport(report, format === "body-plain-compact");
  /** @param {string} text */
  const render = text => serialize({ type: "report", title: "Connector content", children: children(text), footer });
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
  const sizedBody = serialize({ type: "report", title: "Connector content", children: children(candidate) });
  assertMinimum(name, sizedBody, minimum);
  return output;
}

module.exports = { formatConnectorArguments };
