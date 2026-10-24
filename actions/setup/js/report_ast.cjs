// @ts-check

const { canonicalizePublicationText, buildAllowedDomains, sanitizeUrlDomains } = require("./sanitize_content_core.cjs");
const { redactDiagnosticText } = require("./diagnostic_sanitization.cjs");
const { escapeMarkdown } = require("./missing_info_formatter.cjs");
const { createExpirationLine } = require("./ephemerals.cjs");

/**
 * @typedef {{agentTainted?: boolean, sizeProtected?: boolean}} ReportProvenance
 * @typedef {null | boolean | number | string | ReportData[] | {[key: string]: ReportData}} ReportData
 * @typedef {{key: string, value: string}} ReportMarkerField
 * @typedef {ReportProvenance & {type: "image", url: string, alt: string, width?: number, height?: number, align?: "left" | "right" | "center"}} ReportImage
 * @typedef {ReportProvenance & {type: "xml_marker", name: string, value?: string, fields?: ReportMarkerField[]}} ReportXMLMarker
 * @typedef {ReportProvenance & {type: "json_marker", name: string, data: ReportData}} ReportJSONMarker
 * @typedef {ReportProvenance & {type: "closing_reference", action: "closes" | "fixes" | "resolves", target: {number: string | number, repository?: string}, enabled: boolean}} ReportClosingReference
 * @typedef {ReportProvenance & ({type: "text" | "code", value: string} | {type: "soft_break" | "hard_break"} | ReportImage | {type: "mention", login: string} | {type: "issue_reference", number: string} | ReportClosingReference | {type: "strong" | "emphasis" | "strike" | "subscript", children: ReportInline[]} | {type: "link", url: string, children: ReportInline[]})} ReportInline
 * @typedef {ReportProvenance & ({type: "paragraph", children: ReportInline[]} |
 *   {type: "heading", level: number, children: ReportInline[]} |
 *   {type: "code_block", value: string, language: string} |
 *   {type: "list", ordered: boolean, items: ReportBlock[][]} |
 *   {type: "quote", children: ReportBlock[]} |
 *   {type: "alert", kind: string, children: ReportBlock[]} |
 *   {type: "details", summary: string | ReportInline[], children: ReportBlock[]} |
 *   {type: "table", headers: ReportInline[][], rows: ReportInline[][][]} |
 *   {type: "task", checked: boolean, children: ReportInline[]} |
 *   {type: "separator"} |
 *   {type: "structured_data", data: ReportData} |
 *   ReportXMLMarker | ReportJSONMarker |
 *   {type: "expiration", expiresAt: string} |
 *   {type: "marker", name: "gh-aw-threat-detected" | "gh-aw-threat-engine-error"})} ReportBlock
 * @typedef {ReportXMLMarker & {name: "gh-aw-agentic-workflow" | "gh-aw-failure-issue", value: string, fields: ReportMarkerField[]}} ReportMarker
 * @typedef {ReportProvenance & {type: "footer", children: ReportBlock[], expiresAt?: string, markers: ReportMarker[]}} ReportFooter
 * @typedef {ReportProvenance & {type: "report", title: string, children: ReportBlock[], preamble?: ReportBlock[], footer?: ReportFooter}} Report
 */

const ALERT_KINDS = new Set(["NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"]);
const MAX_DEPTH = 16;
/** @type {WeakSet<Report>} */
const redactedReports = new WeakSet();

/**
 * Provenance describes this node, not its descendants. Missing provenance is tainted.
 * @param {ReportProvenance} node
 * @returns {ReportProvenance}
 */
function normalizeReportProvenance(node) {
  if (node.agentTainted !== undefined && typeof node.agentTainted !== "boolean") throw new TypeError("Report agent taint must be a boolean");
  if (node.sizeProtected !== undefined && typeof node.sizeProtected !== "boolean") throw new TypeError("Report size protection must be a boolean");
  return { ...(node.agentTainted === undefined ? {} : { agentTainted: node.agentTainted }), ...(node.sizeProtected === undefined ? {} : { sizeProtected: node.sizeProtected }) };
}

/** @param {ReportInline | undefined} node @returns {node is Extract<ReportInline, {type: "text" | "code"}>} */
function isInlineTextOrCode(node) {
  return node?.type === "text" || node?.type === "code";
}

/** @param {ReportInline | undefined} node */
function hasInlineTextContent(node) {
  return isInlineTextOrCode(node) || (node !== undefined && "children" in node && node.children.some(hasInlineTextContent));
}

/**
 * @param {ReportInline[]} nodes
 * @param {boolean} redactEach
 */
function collectInlineText(nodes, redactEach) {
  return nodes
    .map(node => {
      if (node.type === "text" || node.type === "code") return redactEach ? normalizeReportText(node.value) : node.value;
      if (node.type === "soft_break" || node.type === "hard_break") return "\n";
      if ("children" in node) return collectInlineText(node.children, redactEach);
      return "";
    })
    .join("");
}

/**
 * @param {ReportInline[]} nodes
 * @param {number} start
 * @param {number} end
 * @param {string} replacement
 * @returns {ReportInline[]}
 */
function replaceInlineRange(nodes, start, end, replacement) {
  let offset = 0;
  let inserted = false;
  /**
   * @param {ReportInline[]} current
   * @returns {{nodes: ReportInline[], changed: boolean}}
   */
  function visit(current) {
    let changed = false;
    /** @type {ReportInline[]} */
    const result = [];
    for (const node of current) {
      if (node.type === "text" || node.type === "code") {
        const value = Array.from(node.value);
        const nodeStart = offset;
        offset += value.length;
        const nodeEnd = offset;
        if (nodeStart >= end || nodeEnd <= start) {
          result.push(node);
          continue;
        }
        changed = true;
        const prefixEnd = Math.max(0, Math.min(value.length, start - nodeStart));
        const suffixStart = Math.max(0, Math.min(value.length, end - nodeStart));
        const prefix = value.slice(0, prefixEnd).join("");
        const suffix = value.slice(suffixStart).join("");
        if (prefix) result.push({ ...node, value: prefix });
        if (!inserted && replacement) {
          result.push({ type: "text", value: replacement, agentTainted: false });
          inserted = true;
        }
        if (suffix) result.push({ ...node, value: suffix });
        continue;
      }
      if (node.type === "soft_break" || node.type === "hard_break") {
        const nodeStart = offset;
        offset++;
        if (nodeStart >= start && nodeStart < end) {
          changed = true;
          if (!inserted && replacement) {
            result.push({ type: "text", value: replacement, agentTainted: false });
            inserted = true;
          }
        } else result.push(node);
        continue;
      }
      if ("children" in node) {
        const children = visit(node.children);
        changed ||= children.changed;
        if (children.changed && node.type === "link") result.push(...children.nodes);
        else if (children.nodes.length) result.push({ ...node, children: children.nodes });
        continue;
      }
      result.push(node);
    }
    return { nodes: result, changed };
  }
  return visit(nodes).nodes;
}

/** @param {string} value @param {{redact?: boolean, preserveImageAttributes?: boolean}} [options] */
function normalizeReportText(value, { redact = true, preserveImageAttributes = false } = {}) {
  if (typeof value !== "string") throw new TypeError("Report text must be a string");
  let current = redact ? redactDiagnosticText(value) : value;
  for (let pass = 0; pass < 16; pass++) {
    const normalized = canonicalizePublicationText(current, { preserveImageAttributes }).replace(/\r\n?/g, "\n");
    const redacted = redact ? redactDiagnosticText(normalized) : normalized;
    if (redacted === current) return redacted;
    current = redacted;
  }
  throw new TypeError("Report text contains excessively nested encodings");
}

/** @param {string} value */
function normalizeReportTitle(value) {
  return Array.from(normalizeReportText(value).replace(/\s+/g, " ").trim()).slice(0, 256).join("");
}

/** @param {string} value */
function normalizeReportURL(value) {
  const normalized = normalizeReportText(value);
  if (normalized !== value || /[\s\\]/.test(value)) throw new TypeError("Report links must contain a canonical HTTPS URL");
  let url;
  try {
    url = new URL(value);
  } catch (error) {
    throw new TypeError("Report link is not a valid HTTPS URL", { cause: error });
  }
  if (url.protocol !== "https:" || url.username || url.password) throw new TypeError("Report links must use HTTPS without credentials");
  return url.href;
}

/** @param {string} value */
function normalizeReportAllowedURL(value) {
  const url = normalizeReportURL(value);
  // Renderable destinations cannot use the sanitizer's code-region exemption.
  // Reject hosts the shared URL scanner cannot parse.
  if (!/^https:\/\/[\w.-]+(?::\d+)?\//.test(url) || sanitizeUrlDomains(url, buildAllowedDomains()) !== url) throw new TypeError("Report URLs must use an allowed HTTPS domain");
  return url;
}

/** @param {ReportImage} node @returns {ReportImage} */
function normalizeReportImage(node) {
  for (const dimension of [node.width, node.height]) {
    if (dimension !== undefined && (typeof dimension !== "number" || !Number.isInteger(dimension) || dimension < 1 || dimension > 10000)) throw new TypeError("Report image dimensions must be integers between 1 and 10000");
  }
  if (node.align !== undefined && !["left", "right", "center"].includes(node.align)) throw new TypeError("Invalid report image alignment");
  return {
    type: "image",
    url: normalizeReportAllowedURL(node.url),
    alt: normalizeReportText(node.alt).replace(/\s+/g, " "),
    ...(node.width === undefined ? {} : { width: node.width }),
    ...(node.height === undefined ? {} : { height: node.height }),
    ...(node.align === undefined ? {} : { align: node.align }),
    ...normalizeReportProvenance(node),
  };
}

/** @param {number} depth */
function checkDepth(depth) {
  if (depth > MAX_DEPTH) throw new TypeError("Report nesting exceeds the maximum depth");
}

/**
 * JSON values keep their meaning (including entity-looking strings), not Markdown
 * text normalization. Reject lossy serialization such as undefined, NaN, or Date.
 * @param {ReportData} value
 * @param {number} depth
 * @returns {ReportData}
 */
function normalizeReportData(value, depth) {
  checkDepth(depth);
  if (value === null || typeof value === "boolean") return value;
  if (typeof value === "string") return redactDiagnosticText(value);
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (Array.isArray(value)) {
    if (Object.getOwnPropertySymbols(value).length || Object.getOwnPropertyNames(value).length !== value.length + 1) throw new TypeError("Report structured data must contain only JSON values");
    return Array.from({ length: value.length }, (_, index) => {
      const property = Object.getOwnPropertyDescriptor(value, String(index));
      if (!property || !Object.hasOwn(property, "value")) throw new TypeError("Report structured data must contain only JSON properties");
      return normalizeReportData(property.value, depth + 1);
    });
  }
  if (typeof value !== "object" || (Object.getPrototypeOf(value) !== Object.prototype && Object.getPrototypeOf(value) !== null) || Object.getOwnPropertySymbols(value).length) {
    throw new TypeError("Report structured data must contain only JSON values");
  }
  const keys = new Set();
  const entries = Object.entries(Object.getOwnPropertyDescriptors(value)).map(([key, property]) => {
    if (!property.enumerable || !Object.hasOwn(property, "value")) throw new TypeError("Report structured data must contain only JSON properties");
    const normalizedKey = redactDiagnosticText(key);
    if (keys.has(normalizedKey)) throw new TypeError("Duplicate report structured data key after redaction");
    keys.add(normalizedKey);
    return [normalizedKey, normalizeReportData(property.value, depth + 1)];
  });
  return Object.fromEntries(entries);
}

/** @param {string} name */
function validateMarkerName(name) {
  if (!/^[a-zA-Z][a-zA-Z0-9_-]*$/.test(name)) throw new TypeError("Invalid report metadata name");
}

/**
 * @param {ReportXMLMarker} node
 * @returns {ReportXMLMarker}
 */
function normalizeXMLMarker(node) {
  if (node.type !== "xml_marker") throw new TypeError("Expected a report XML marker");
  validateMarkerName(node.name);
  if (node.fields?.length && node.value === undefined) throw new TypeError("Report metadata fields require a marker value");
  const keys = new Set([node.name]);
  const fields = node.fields?.map(field => {
    if (!/^[a-zA-Z0-9_-]+$/.test(field.key) || keys.has(field.key)) throw new TypeError("Invalid or duplicate report metadata field");
    keys.add(field.key);
    return { key: field.key, value: normalizeMarkerValue(field.value) };
  });
  const value = node.value === undefined ? undefined : fields !== undefined ? normalizeMarkerValue(node.value) : normalizeReportText(node.value).trim().replace(/,/g, "%2C");
  if (value?.includes("\n")) throw new TypeError("Report metadata must fit on one line");
  return { type: "xml_marker", name: node.name, ...(value === undefined ? {} : { value }), ...(fields ? { fields } : {}), ...normalizeReportProvenance(node) };
}

/**
 * @param {ReportInline[]} nodes
 * @param {number} depth
 * @returns {ReportInline[]}
 */
function normalizeInlines(nodes, depth, inLink = false, deferRedaction = false) {
  checkDepth(depth);
  /** @type {ReportInline[]} */
  const result = [];
  for (let nodeIndex = 0; nodeIndex < nodes.length; nodeIndex++) {
    const node = nodes[nodeIndex];
    const provenance = normalizeReportProvenance(node);
    const start = result.length;
    switch (node.type) {
      case "soft_break":
      case "hard_break":
        result.push({ type: node.type });
        break;
      case "image":
        result.push(normalizeReportImage(node));
        break;
      case "mention":
        if (!/^[A-Za-z0-9](?:[A-Za-z0-9_-]{0,37}[A-Za-z0-9])?(?:\/[A-Za-z0-9._-]+)?$/.test(node.login)) throw new TypeError("Invalid report mention");
        result.push({ type: "mention", login: node.login });
        break;
      case "issue_reference":
        if (!/^[1-9]\d*$/.test(node.number)) throw new TypeError("Invalid report issue reference");
        result.push({ type: "issue_reference", number: node.number });
        break;
      case "closing_reference": {
        if (!["closes", "fixes", "resolves"].includes(node.action) || typeof node.enabled !== "boolean") throw new TypeError("Invalid report closing directive");
        if (
          !node.target ||
          (typeof node.target.number === "number" && !Number.isSafeInteger(node.target.number)) ||
          !/^[1-9]\d*$/.test(String(node.target.number)) ||
          (node.target.repository !== undefined && !/^[a-zA-Z0-9_-][a-zA-Z0-9_.-]*\/[a-zA-Z0-9_-][a-zA-Z0-9_.-]*$/.test(node.target.repository))
        ) {
          throw new TypeError("Invalid report closing target");
        }
        result.push({ type: "closing_reference", action: node.action, target: { number: String(node.target.number), ...(node.target.repository === undefined ? {} : { repository: node.target.repository }) }, enabled: node.enabled });
        break;
      }
      case "text":
      case "code": {
        const adjacentRun = deferRedaction || hasInlineTextContent(nodes[nodeIndex - 1]) || hasInlineTextContent(nodes[nodeIndex + 1]);
        const value = normalizeReportText(node.value, { redact: !adjacentRun }).replace(/\s+/g, " ");
        if (!value) break;
        const previous = result.at(-1);
        if (previous?.type === node.type && previous.agentTainted === node.agentTainted && previous.sizeProtected === node.sizeProtected) previous.value = (previous.value + value).replace(/\s+/g, " ");
        else result.push({ type: node.type, value });
        break;
      }
      case "strong":
      case "emphasis":
      case "strike":
      case "subscript":
      case "link": {
        if (node.type === "link" && inLink) throw new TypeError("Report links cannot contain other links");
        const children = normalizeInlines(node.children, depth + 1, inLink || node.type === "link", true);
        if (!children.length) break;
        result.push(node.type === "link" ? { type: "link", url: normalizeReportURL(node.url), children } : { type: node.type, children });
        break;
      }
      default:
        throw new TypeError("Unsupported report inline node");
    }
    for (let index = start; index < result.length; index++) Object.assign(result[index], provenance);
  }
  /** @type {ReportInline[]} */
  const normalized = [];
  for (let index = 0; index < result.length;) {
    const node = result[index];
    if (node.type !== "text" && node.type !== "code") {
      normalized.push(node);
      index++;
      continue;
    }
    let end = index + 1;
    while (end < result.length && result[end].type === node.type) end++;
    const run = result.slice(index, end);
    const isSameOrigin = run.every(inline => inline.agentTainted === node.agentTainted && inline.sizeProtected === node.sizeProtected);
    const hasAdjacentInlineRun = deferRedaction || hasInlineTextContent(result[index - 1]) || hasInlineTextContent(result[end]);
    if (isSameOrigin && !hasAdjacentInlineRun) {
      const value = normalizeReportText(run.map(inline => (inline.type === "text" || inline.type === "code" ? inline.value : "")).join("")).replace(/\s+/g, " ");
      if (value) normalized.push({ type: node.type, value, ...normalizeReportProvenance(node) });
    } else normalized.push(...run);
    index = end;
  }
  return normalized;
}

/**
 * @param {ReportBlock[]} nodes
 * @param {number} depth
 * @returns {ReportBlock[]}
 */
function normalizeBlocks(nodes, depth) {
  checkDepth(depth);
  /** @type {ReportBlock[]} */
  const result = [];
  for (const node of nodes) {
    const provenance = normalizeReportProvenance(node);
    const start = result.length;
    switch (node.type) {
      case "paragraph":
      case "task":
      case "heading": {
        const children = normalizeInlines(node.children, depth + 1);
        if (!children.length || children.every(child => child.type === "text" && !child.value.trim())) break;
        if (node.type === "heading") {
          if (!Number.isInteger(node.level)) throw new TypeError("Report heading level must be an integer");
          result.push({ type: "heading", level: Math.max(depth ? 3 : 2, Math.min(6, node.level)), children });
        } else if (node.type === "task") result.push({ type: "task", checked: Boolean(node.checked), children });
        else result.push({ type: "paragraph", children });
        break;
      }
      case "code_block": {
        if (!/^[a-zA-Z0-9_+-]*$/.test(node.language)) throw new TypeError("Invalid report code language");
        const value = normalizeReportText(node.value);
        if (value.trim()) result.push({ type: "code_block", language: node.language, value });
        break;
      }
      case "list": {
        const items = node.items.map(item => normalizeBlocks(item, depth + 1)).filter(item => item.length);
        if (items.length) result.push({ type: "list", ordered: Boolean(node.ordered), items });
        break;
      }
      case "details":
      case "alert":
      case "quote": {
        const children = normalizeBlocks(node.children, depth + 1);
        if (node.type === "alert" && !ALERT_KINDS.has(node.kind)) throw new TypeError("Unsupported report alert kind");
        if (!children.length) break;
        if (node.type === "details") {
          const summary = typeof node.summary === "string" ? normalizeReportText(node.summary).replace(/\s+/g, " ").trim() : normalizeInlines(node.summary, depth + 1);
          if (!summary.length) throw new TypeError("Report details must have a summary");
          result.push({ type: "details", summary, children });
        } else if (node.type === "alert") result.push({ type: "alert", kind: node.kind, children });
        else result.push({ type: "quote", children });
        break;
      }
      case "table": {
        if (!node.headers.length || node.rows.some(row => row.length !== node.headers.length)) throw new TypeError("Report table rows must match the header width");
        result.push({
          type: "table",
          headers: node.headers.map(cell => normalizeInlines(cell, depth + 1)),
          rows: node.rows.map(row => row.map(cell => normalizeInlines(cell, depth + 1))),
        });
        break;
      }
      case "marker":
        if (node.name !== "gh-aw-threat-detected" && node.name !== "gh-aw-threat-engine-error") throw new TypeError("Unsupported report flag marker");
        result.push({ type: "marker", name: node.name });
        break;
      case "separator":
        result.push({ type: "separator" });
        break;
      case "xml_marker":
        result.push(normalizeXMLMarker(node));
        break;
      case "json_marker":
        validateMarkerName(node.name);
        result.push({ type: "json_marker", name: node.name, data: normalizeReportData(node.data, depth + 1) });
        break;
      case "structured_data": {
        result.push({ type: "structured_data", data: normalizeReportData(node.data, depth + 1) });
        break;
      }
      case "expiration": {
        const date = new Date(node.expiresAt);
        if (Number.isNaN(date.getTime())) throw new TypeError("Invalid report expiration date");
        result.push({ type: "expiration", expiresAt: date.toISOString() });
        break;
      }
      default:
        throw new TypeError("Unsupported report block node");
    }
    for (let index = start; index < result.length; index++) Object.assign(result[index], provenance);
  }
  return result;
}

/** @param {string} value */
function normalizeMarkerValue(value) {
  return normalizeReportText(value).replace(/\s+/g, " ").trim().replace(/</g, "%3C").replace(/>/g, "%3E").replace(/,/g, "%2C");
}

/**
 * @param {ReportFooter} footer
 * @returns {ReportFooter}
 */
function normalizeFooter(footer) {
  if (footer.type !== "footer") throw new TypeError("Expected a report footer");
  const markers = footer.markers.map(marker => {
    if (marker.name !== "gh-aw-agentic-workflow" && marker.name !== "gh-aw-failure-issue") throw new TypeError("Unsupported report metadata marker");
    const normalized = normalizeXMLMarker(marker);
    return { ...marker, value: normalizeMarkerValue(marker.value), fields: normalized.fields || [] };
  });
  if (new Set(markers.map(marker => marker.name)).size !== markers.length) throw new TypeError("Duplicate report metadata marker");
  /** @type {ReportFooter} */
  const result = { type: "footer", children: normalizeBlocks(footer.children, 0), markers, ...normalizeReportProvenance(footer) };
  if (footer.expiresAt !== undefined) {
    const date = new Date(footer.expiresAt);
    if (Number.isNaN(date.getTime())) throw new TypeError("Invalid report expiration date");
    return { ...result, expiresAt: date.toISOString() };
  }
  return result;
}

/**
 * Normalize before rendering; reject raw Markdown/HTML nodes rather than bypassing escaping.
 * @param {Report} report
 * @returns {Report}
 */
function normalizeReport(report) {
  if (report.type !== "report") throw new TypeError("Expected a report document");
  const title = normalizeReportTitle(report.title);
  if (!title) throw new TypeError("Report title must not be empty");
  return {
    type: "report",
    title,
    children: normalizeBlocks(report.children, 0),
    ...(report.preamble ? { preamble: normalizeBlocks(report.preamble, 0) } : {}),
    ...(report.footer ? { footer: normalizeFooter(report.footer) } : {}),
    ...normalizeReportProvenance(report),
  };
}

/** @param {string} value */
function escapeReportText(value) {
  return value
    .split(/((?<![A-Za-z0-9])@[a-zA-Z0-9][a-zA-Z0-9_/-]*)/g)
    .map((part, index) =>
      index % 2 === 1
        ? `\`${part}\``
        : escapeMarkdown(part.replace(/&/g, "&amp;"))
            .replace(/\\([()])/g, "$1")
            .replace(/[|#~!]/g, "\\$&")
            .replace(/^([-+])(?= )/, "\\$1")
            .replace(/^(\d+)\.(?= )/, "$1\\.")
    )
    .join("");
}

/** @param {string} value */
function codeFence(value) {
  return "`".repeat(Array.from(value.matchAll(/`+/g)).reduce((length, match) => Math.max(length, match[0].length + 1), 3));
}

/** @param {ReportInline[]} nodes @returns {ReportInline[]} */
function redactInlines(nodes) {
  // Complete visible-text runs before serialization without changing origin boundaries.
  const sourceText = collectInlineText(nodes, false);
  const completeText = normalizeReportText(sourceText).replace(/\s+/g, " ");
  const independentlyRedactedText = collectInlineText(nodes, true).replace(/\s+/g, " ");
  if (completeText !== independentlyRedactedText) {
    const source = Array.from(sourceText);
    const complete = Array.from(completeText);
    let prefixLength = 0;
    while (prefixLength < source.length && prefixLength < complete.length && source[prefixLength] === complete[prefixLength]) prefixLength++;
    let suffixLength = 0;
    while (suffixLength < source.length - prefixLength && suffixLength < complete.length - prefixLength && source[source.length - suffixLength - 1] === complete[complete.length - suffixLength - 1]) {
      suffixLength++;
    }
    const replacement = complete.slice(prefixLength, complete.length - suffixLength).join("");
    return redactInlines(replaceInlineRange(nodes, prefixLength, source.length - suffixLength, replacement));
  }
  return nodes.map(node => {
    if (isInlineTextOrCode(node)) return { ...node, value: normalizeReportText(node.value).replace(/\s+/g, " ") };
    return "children" in node ? { ...node, children: redactInlines(node.children) } : node;
  });
}

/** @param {string} value @param {boolean} [neutralizeMentions] */
function escapeSummaryText(value, neutralizeMentions = true) {
  const escaped = value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  return neutralizeMentions ? escaped.replace(/(?<![A-Za-z0-9])@[a-zA-Z0-9][a-zA-Z0-9_/-]*/g, mention => `<code>${mention}</code>`) : escaped;
}

/** @param {ReportInline[]} nodes @param {boolean} [html] */
function renderInlines(nodes, html = false) {
  /** @type {ReportInline[]} */
  const runs = [];
  for (const node of nodes) {
    const previous = runs.at(-1);
    if (isInlineTextOrCode(node) && isInlineTextOrCode(previous) && previous.type === node.type) {
      previous.value = (previous.value + node.value).replace(/\s+/g, " ");
    } else runs.push({ ...node });
  }
  const fragments = runs.map(node => {
    if (node.type === "text") return html ? escapeSummaryText(node.value) : escapeReportText(node.value);
    if (node.type === "code") {
      if (html) return `<code>${escapeSummaryText(node.value, false)}</code>`;
      const fence = "`".repeat(Array.from(node.value.matchAll(/`+/g)).reduce((length, match) => Math.max(length, match[0].length + 1), 1));
      const padding = /^[` ]|[` ]$/.test(node.value) && !/^ +$/.test(node.value) ? " " : "";
      return `${fence}${padding}${node.value}${padding}${fence}`;
    }
    switch (node.type) {
      case "soft_break":
        return html ? " " : "\n";
      case "hard_break":
        return html ? " " : "  \n";
      case "image": {
        if (html || node.width !== undefined || node.height !== undefined || node.align !== undefined) {
          const attributes = [
            `src="${escapeHTMLAttribute(node.url)}"`,
            `alt="${escapeHTMLAttribute(node.alt)}"`,
            ...(node.width === undefined ? [] : [`width="${node.width}"`]),
            ...(node.height === undefined ? [] : [`height="${node.height}"`]),
            ...(node.align === undefined ? [] : [`align="${node.align}"`]),
          ];
          return `<img ${attributes.join(" ")}>`;
        }
        return `![${escapeMarkdown(node.alt.replace(/&/g, "&amp;")).replace(/@/g, "&#64;")}](${escapeURLDestination(node.url)})`;
      }
      case "mention":
        return `@${node.login}`;
      case "issue_reference":
        return `#${node.number}`;
      case "closing_reference":
        return `${node.action[0].toUpperCase()}${node.action.slice(1)} ${node.target.repository || ""}${node.enabled ? "#" : "\\#"}${node.target.number}`;
      case "strong":
        return html ? `<b>${renderInlines(node.children, true)}</b>` : `**${renderInlines(node.children)}**`;
      case "emphasis":
        return html ? `<em>${renderInlines(node.children, true)}</em>` : `*${renderInlines(node.children)}*`;
      case "strike":
        return html ? `<del>${renderInlines(node.children, true)}</del>` : `~~${renderInlines(node.children)}~~`;
      case "subscript":
        return `<sub>${renderInlines(node.children, html)}</sub>`;
      case "link":
        return html ? `<a href="${escapeHTMLAttribute(node.url)}">${renderInlines(node.children, true)}</a>` : `[${renderInlines(node.children)}](${escapeURLDestination(node.url)})`;
    }
  });
  let rendered = "";
  for (const fragment of fragments) {
    // Adjacent generated code delimiters must not merge into a different run.
    if (rendered.endsWith("`") && fragment.startsWith("`")) rendered += " ";
    rendered += fragment;
  }
  // Standalone generated HTML can turn following Markdown code into raw HTML.
  // An encoded space keeps images and subscript delimiters in inline context.
  return rendered.includes("\n") ? rendered.replace(/^([ \t]*)(<(?:img [^<>]*|\/?sub)>[ \t]*)$/gm, "$1&#32;$2") : rendered;
}

/** @param {string} value */
function escapeHTMLAttribute(value) {
  return value.replace(/[&<>"'@|]/g, character => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;", "@": "&#64;", "|": "&#124;" })[character]);
}

/** @param {string} url */
function escapeURLDestination(url) {
  return url.replace(/[()<>"'`]/g, char => `%${char.charCodeAt(0).toString(16).toUpperCase()}`);
}

/** @param {ReportBlock[]} nodes */
function renderBlocks(nodes) {
  return nodes
    .map(node => {
      switch (node.type) {
        case "paragraph":
          return renderInlines(node.children).trim();
        case "task":
          return `[${node.checked ? "x" : " "}] ${renderInlines(node.children).trim()}`;
        case "separator":
          return "---";
        case "xml_marker":
          return renderXMLMarker(node);
        case "json_marker":
          return `<!-- ${node.name}: ${JSON.stringify(node.data).replace(/[<>&]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`)} -->`;
        case "structured_data": {
          const value = JSON.stringify(node.data, null, 2);
          const fence = codeFence(value);
          return `${fence}json\n${value}\n${fence}`;
        }
        case "expiration":
          return createExpirationLine(new Date(node.expiresAt));
        case "heading":
          return `${"#".repeat(node.level)} ${renderInlines(node.children).trim()}`;
        case "code_block": {
          const fence = codeFence(node.value);
          return `${fence}${node.language}\n${node.value}${node.value.endsWith("\n") ? "" : "\n"}${fence}`;
        }
        case "list":
          return node.items
            .map((item, index) => {
              const marker = node.ordered ? `${index + 1}. ` : "- ";
              return marker + renderBlocks(item).replace(/\n/g, "\n" + " ".repeat(marker.length));
            })
            .join("\n");
        case "quote":
        case "alert":
          return (node.type === "alert" ? `[!${node.kind}]\n` : "") + renderBlocks(node.children);
        case "details":
          return `<details>\n<summary>${typeof node.summary === "string" ? escapeSummaryText(node.summary) : renderInlines(node.summary, true)}</summary>\n\n${renderBlocks(node.children)}\n\n</details>`;
        case "table": {
          const row = cells => `| ${cells.map(cell => renderInlines(cell).replace(/(?<!\\)\|/g, "\\|")).join(" | ")} |`;
          return [row(node.headers), `| ${node.headers.map(() => "---").join(" | ")} |`, ...node.rows.map(row)].join("\n");
        }
        case "marker":
          return `<!-- ${node.name} -->`;
      }
    })
    .map((value, index) =>
      nodes[index].type === "quote" || nodes[index].type === "alert"
        ? value
            .split("\n")
            .map(line => (line ? `> ${line}` : ">"))
            .join("\n")
        : value
    )
    .join("\n\n");
}

/** @param {ReportXMLMarker} marker */
function renderXMLMarker(marker) {
  const value = marker.value?.replace(/[<>]/g, character => (character === "<" ? "%3C" : "%3E"));
  const fields = marker.fields?.map(field => `, ${field.key}: ${field.value}`).join("") || "";
  return `<!-- ${marker.name}${value === undefined ? "" : `: ${value}`}${fields} -->`;
}

/** @param {ReportFooter} footer */
function renderFooter(footer) {
  const attribution = renderBlocks(footer.children);
  const expiration = footer.expiresAt ? `> ${createExpirationLine(new Date(footer.expiresAt))}` : "";
  const markers = footer.markers.map(renderXMLMarker).join("\n");
  return [[attribution, expiration].filter(Boolean).join("\n"), markers].filter(Boolean).join("\n\n");
}

/**
 * Safety taint is independent of agent-origin provenance, which must survive redaction.
 * @param {unknown} value
 * @param {WeakSet<object>} [ancestors]
 */
function assertNoTaintedStrings(value, ancestors = new WeakSet()) {
  if (typeof value === "string") {
    const canonical = normalizeReportText(value, { redact: false });
    if (redactDiagnosticText(value) !== value || redactDiagnosticText(canonical) !== canonical) throw new TypeError("Report contains an unredacted string");
  } else if (value !== null && typeof value === "object") {
    if (ancestors.has(value)) throw new TypeError("Redacted report must not contain cycles");
    ancestors.add(value);
    if (Array.isArray(value)) {
      for (const item of value) assertNoTaintedStrings(item, ancestors);
    } else {
      for (const [key, item] of Object.entries(value)) {
        assertNoTaintedStrings(key, ancestors);
        assertNoTaintedStrings(item, ancestors);
      }
    }
    ancestors.delete(value);
  }
}

/**
 * Reject unprocessed trees and recheck strings so a stale proof cannot hide new secrets.
 * @param {Report} report
 */
function assertRedactedReport(report) {
  if (!redactedReports.has(report)) throw new TypeError("Report must pass redaction before rendering");
  assertNoTaintedStrings(report);
  /**
   * @param {ReportInline[]} nodes
   */
  function inlines(nodes) {
    const text = normalizeReportText(collectInlineText(nodes, false), { redact: false }).replace(/\s+/g, " ");
    if (redactDiagnosticText(text) !== text) throw new TypeError("Report contains an unredacted inline run");
    for (const node of nodes) if ("children" in node) inlines(node.children);
  }
  /** @param {ReportBlock[]} nodes */
  function blocks(nodes) {
    for (const node of nodes) {
      switch (node.type) {
        case "paragraph":
        case "heading":
        case "task":
          inlines(node.children);
          break;
        case "details":
          if (Array.isArray(node.summary)) inlines(node.summary);
          blocks(node.children);
          break;
        case "alert":
        case "quote":
          blocks(node.children);
          break;
        case "list":
          node.items.forEach(blocks);
          break;
        case "table":
          [...node.headers, ...node.rows.flat()].forEach(inlines);
          break;
      }
    }
  }
  blocks(report.children);
  if (report.preamble) blocks(report.preamble);
  if (report.footer) blocks(report.footer.children);
}

/**
 * Prepare a tree for the mandatory assertion before serialization or escaping.
 * @param {Report} report
 * @returns {Report}
 */
function redactReportForRendering(report) {
  const normalized = normalizeReport(report);
  /** @param {ReportBlock[]} nodes @returns {ReportBlock[]} */
  function blocks(nodes) {
    return nodes.map(node => {
      switch (node.type) {
        case "paragraph":
        case "heading":
        case "task":
          return { ...node, children: redactInlines(node.children) };
        case "details":
          return { ...node, summary: Array.isArray(node.summary) ? redactInlines(node.summary) : node.summary, children: blocks(node.children) };
        case "alert":
        case "quote":
          return { ...node, children: blocks(node.children) };
        case "list":
          return { ...node, items: node.items.map(blocks) };
        case "table":
          return { ...node, headers: node.headers.map(redactInlines), rows: node.rows.map(row => row.map(redactInlines)) };
        default:
          return node;
      }
    });
  }
  const redacted = {
    ...normalized,
    children: blocks(normalized.children),
    ...(normalized.preamble ? { preamble: blocks(normalized.preamble) } : {}),
    ...(normalized.footer ? { footer: { ...normalized.footer, children: blocks(normalized.footer.children) } } : {}),
  };
  redactedReports.add(redacted);
  return redacted;
}

/**
 * @param {Report} report
 * @returns {{title: string, body: string}}
 */
function renderReport(report) {
  const redacted = redactReportForRendering(report);
  assertRedactedReport(redacted);
  const parts = [redacted.preamble ? renderBlocks(redacted.preamble) : "", renderBlocks(redacted.children), redacted.footer ? renderFooter(redacted.footer) : ""];
  const result = { title: redacted.title, body: parts.filter(Boolean).join("\n\n").trim() + "\n" };
  assertNoTaintedStrings(result);
  return result;
}

module.exports = { normalizeReport, normalizeReportText, normalizeReportTitle, normalizeReportURL, normalizeReportAllowedURL, normalizeReportImage, redactReportForRendering, assertRedactedReport, renderReport };
