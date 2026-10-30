// @ts-check

const { normalizeReportText, normalizeReportAllowedURL, normalizeReportImage } = require("./report_ast.cjs");
const { buildAllowedDomains, buildAllowedGitHubReferences, getCurrentRepoSlug, sanitizeUrlProtocols, sanitizeUrlDomains } = require("./sanitize_content_core.cjs");
const { isRepoAllowed } = require("./repo_helpers.cjs");

/** @param {string} line */
function openingFence(line) {
  const match = /^ {0,3}(`{3,}|~{3,})(.*)$/.exec(line);
  return match && (match[1][0] !== "`" || !match[2].includes("`")) ? match : undefined;
}

/**
 * Read a single-line HTML token without interpreting backticks or quoted ">" as delimiters.
 * The token is literal text unless explicitly imported as a supported typed element.
 * @param {string} source
 */
function htmlTag(source) {
  const opening = /^<\/?([A-Za-z][A-Za-z0-9-]*)(?=[\s/>])/.exec(source);
  if (!opening) return undefined;
  let quote = "";
  for (let index = opening[0].length; index < source.length && source[index] !== "\n"; index++) {
    const character = source[index];
    if (quote) {
      if (character === quote) quote = "";
    } else if (character === '"' || character === "'") quote = character;
    else if (character === "<") return undefined;
    else if (character === ">") return { raw: source.slice(0, index + 1), name: opening[1].toLowerCase() };
  }
  return undefined;
}

/**
 * Import only explicit image presentation fields. Unsupported or malformed HTML
 * remains visible, escaped text; it never becomes a raw-HTML AST node.
 * @param {string} raw
 * @param {(value: string) => string} substitute
 * @returns {import("./report_ast.cjs").ReportImage | undefined}
 */
function htmlImage(raw, substitute) {
  if (!/^<img(?=[\s/>])/i.test(raw)) return undefined;
  /** @type {Map<string, string>} */
  const attributes = new Map();
  const attribute = /[ \t]+(src|alt|width|height|align)[ \t]*=[ \t]*(?:"([^"]*)"|'([^']*)'|([^\s"'`=<>]+))/giy;
  let index = 4;
  while (!/^[ \t]*\/?>$/.test(raw.slice(index))) {
    attribute.lastIndex = index;
    const match = attribute.exec(raw);
    if (!match) return undefined;
    const name = match[1].toLowerCase();
    if (attributes.has(name)) return undefined;
    attributes.set(name, substitute((match[2] ?? match[3] ?? match[4]).replace(/&quot;/gi, '"').replace(/&apos;/gi, "'")));
    index = attribute.lastIndex;
  }
  const url = attributes.get("src");
  if (url === undefined) return undefined;
  /** @type {import("./report_ast.cjs").ReportImage} */
  const image = { type: "image", url, alt: attributes.get("alt") ?? "" };
  for (const name of /** @type {const} */ ["width", "height"]) {
    const value = attributes.get(name);
    if (value !== undefined) {
      if (!/^[1-9]\d{0,4}$/.test(value)) return undefined;
      image[name] = Number(value);
    }
  }
  const alignment = attributes.get("align");
  if (alignment !== undefined) {
    const align = alignment.toLowerCase();
    if (align !== "left" && align !== "right" && align !== "center") return undefined;
    image.align = align;
  }
  try {
    return normalizeReportImage(image);
  } catch (error) {
    if (!(error instanceof TypeError)) throw error;
    return undefined;
  }
}

/**
 * Index delimiters once so malformed links cannot repeatedly rescan the suffix.
 * @param {string} source
 */
function markdownDelimiters(source) {
  const brackets = new Uint32Array(source.length);
  const parentheses = new Uint32Array(source.length);
  /** @type {number[]} */
  const bracketStack = [];
  /** @type {number[]} */
  const parenthesisStack = [];
  for (let index = 0; index < source.length; index++) {
    const character = source[index];
    if (character === "\\") {
      index++;
      continue;
    }
    if (character === "\n") bracketStack.length = 0;
    if (/\s/.test(character)) parenthesisStack.length = 0;
    else if (character === "(") parenthesisStack.push(index);
    else if (character === ")") {
      const start = parenthesisStack.pop();
      if (start !== undefined) parentheses[start] = index;
    }
    if (character === "[") bracketStack.push(index);
    else if (character === "]") {
      const start = bracketStack.pop();
      if (start !== undefined) brackets[start] = index;
    }
  }
  return { brackets, parentheses };
}

/**
 * Read preindexed destinations before substitution, so scalar values cannot close a link.
 * @param {string} source
 * @param {number} offset
 * @param {ReturnType<typeof markdownDelimiters>} delimiters
 */
function markdownLink(source, offset, delimiters) {
  const image = source[offset] === "!";
  const start = offset + (image ? 1 : 0);
  if (source[start] !== "[") return undefined;
  const labelEnd = delimiters.brackets[start];
  if (!labelEnd || source[labelEnd + 1] !== "(") return undefined;
  const destinationEnd = delimiters.parentheses[labelEnd + 1];
  if (!destinationEnd) return undefined;
  return {
    raw: source.slice(offset, destinationEnd + 1),
    label: source.slice(start + 1, labelEnd),
    destination: source.slice(labelEnd + 2, destinationEnd).replace(/\\([\\()])/g, "$1"),
    image,
  };
}

/** @param {string} source */
function bareURL(source) {
  const match = /^https:\/\/[^\s<>"'`*]+/i.exec(source);
  if (!match) return undefined;
  const url = match[0];
  const balance = [0, 0, 0];
  for (const character of url) {
    const opening = "([{".indexOf(character);
    const closing = ")]}".indexOf(character);
    if (opening >= 0) balance[opening]--;
    else if (closing >= 0) balance[closing]++;
  }
  let end = url.length;
  while (end) {
    const character = url[end - 1];
    if (/[.,!?;:]/.test(character)) end--;
    else {
      const closing = ")]}".indexOf(character);
      if (closing < 0 || balance[closing] <= 0) break;
      balance[closing]--;
      end--;
    }
  }
  return url.slice(0, end);
}

/** @typedef {import("./report_ast.cjs").ReportInline} ReportInline */
/** @typedef {import("./report_ast.cjs").ReportBlock} ReportBlock */
/** @typedef {{type: "inline_slot", children: ReportInline[]}} ReportInlineSlot */
/** @typedef {Record<string, string | number | boolean | ReportBlock[] | ReportInlineSlot | undefined>} ReportContext */

/**
 * Import the small Markdown dialect used by runtime report templates into typed nodes.
 * Templates are parsed before scalar substitution; values cannot introduce markup.
 * Existing diagnostic fragments are imported separately, never passed through as raw HTML.
 * Inline slots do not enable scalar template substitution in surrounding Markdown.
 * onBlockSlot observes actual typed insertions, not literal placeholder text.
 * @param {string} markdown
 * @param {{context?: ReportContext, inlineSlots?: Record<string, ReportInline[]>, blockSlots?: Record<string, ReportBlock[]>, onBlockSlot?: (key: string) => void, agentTainted?: boolean, minimumHeadingLevel?: number, depth?: number, allowFlagMarkers?: boolean, allowMetadata?: boolean, allowedMentions?: string[], deferMentions?: boolean}} [options]
 * @returns {ReportBlock[]}
 */
function parseReportMarkdown(markdown, { context, inlineSlots, blockSlots, onBlockSlot, agentTainted, minimumHeadingLevel = 3, depth = 0, allowFlagMarkers = false, allowMetadata = false, allowedMentions = [], deferMentions = false } = {}) {
  if (depth > 16) throw new TypeError("Report template nesting exceeds the maximum depth");
  if (agentTainted !== undefined && typeof agentTainted !== "boolean") throw new TypeError("Report agent taint must be a boolean");
  const allowedReferences = agentTainted === false ? null : buildAllowedGitHubReferences();
  const currentRepo = getCurrentRepoSlug();
  const allowedRepos = new Set(allowedReferences?.map(repo => (repo === "repo" ? currentRepo : repo)));
  const referenceAllowed = repository => allowedReferences === null || isRepoAllowed((repository || currentRepo).toLowerCase(), allowedRepos);
  /**
   * Only source nodes receive this origin; inserted nodes keep their own provenance.
   * @template {ReportInline | ReportBlock} T
   * @param {T} node
   * @returns {T}
   */
  const fromSource = node => (agentTainted === undefined ? node : { ...node, agentTainted });
  /** @param {string} key */
  function valueFor(key) {
    const value = context && Object.hasOwn(context, key) ? context[key] : undefined;
    if (context && value === undefined && !Object.hasOwn(blockSlots || {}, key) && !Object.hasOwn(inlineSlots || {}, key)) throw new TypeError("Missing report template field");
    return value;
  }
  /** @param {string} source */
  function substitute(source) {
    return source.replace(/\{(\w+)\}/g, (match, key) => {
      const value = valueFor(key);
      if (typeof value === "object") throw new TypeError("Report nodes cannot appear in scalar fields");
      return value === undefined ? match : String(value);
    });
  }
  /**
   * Summary content is HTML, not Markdown. Import only attribute-free formatting;
   * scalar substitutions and unsupported tags remain text.
   * @param {string} source
   * @returns {string | ReportInline[]}
   */
  function summary(source) {
    /** @type {Record<string, "strong" | "emphasis" | "strike" | "subscript">} */
    const formats = { b: "strong", strong: "strong", i: "emphasis", em: "emphasis", s: "strike", del: "strike", sub: "subscript" };
    /** @type {Array<{name: string, raw: string, children: ReportInline[]}>} */
    const stack = [{ name: "", raw: "", children: [] }];
    const text = value => {
      if (value) stack[stack.length - 1].children.push(fromSource({ type: "text", value: substitute(value) }));
    };
    while (source) {
      const start = source.indexOf("<");
      if (start < 0) {
        text(source);
        break;
      }
      text(source.slice(0, start));
      source = source.slice(start);
      const tag = htmlTag(source);
      if (!tag) {
        text("<");
        source = source.slice(1);
        continue;
      }
      source = source.slice(tag.raw.length);
      const token = /^<(\/?)(b|strong|i|em|s|del|sub|code)>$/i.exec(tag.raw);
      if (!token) {
        text(tag.raw);
        continue;
      }
      const name = token[2].toLowerCase();
      if (name === "code" && !token[1]) {
        const end = /<\/code>/i.exec(source);
        if (end) {
          stack[stack.length - 1].children.push(fromSource({ type: "code", value: substitute(source.slice(0, end.index)) }));
          source = source.slice(end.index + end[0].length);
        } else text(tag.raw);
      } else if (token[1] && stack.length > 1 && stack[stack.length - 1].name === name) {
        const frame = stack[stack.length - 1];
        stack.pop();
        stack[stack.length - 1].children.push(fromSource({ type: formats[name], children: frame.children }));
      } else if (!token[1]) {
        if (stack.length > 16) throw new TypeError("Report summary nesting exceeds the maximum depth");
        stack.push({ name, raw: tag.raw, children: [] });
      } else text(tag.raw);
    }
    while (stack.length > 1) {
      const frame = stack[stack.length - 1];
      stack.pop();
      stack[stack.length - 1].children.push(fromSource({ type: "text", value: frame.raw }), ...frame.children);
    }
    const nodes = stack[0].children;
    const plain = nodes.flatMap(node => (node.type === "text" ? [node.value] : []));
    return plain.length === nodes.length ? plain.join("") : nodes;
  }
  /**
   * @param {string} source
   * @param {number} [inlineDepth]
   * @param {boolean} [inLink]
   * @returns {ReportInline[]}
   */
  function inline(source, inlineDepth = 0, inLink = false) {
    if (inlineDepth > 16) throw new TypeError("Report inline nesting exceeds the maximum depth");
    // Redact the complete fragment before references or formatting split a secret.
    source = normalizeReportText(source, { preserveImageAttributes: true });
    const original = source;
    const delimiters = source.includes("[") ? markdownDelimiters(source) : undefined;
    /** @type {ReportInline[]} */
    const nodes = [];
    const text = value => {
      const previous = nodes.at(-1);
      if (previous?.type === "text" && previous.agentTainted === agentTainted) previous.value += value;
      else nodes.push(fromSource({ type: "text", value }));
    };
    while (source) {
      let match;
      const link = delimiters && markdownLink(original, original.length - source.length, delimiters);
      const autolink = bareURL(source);
      const tag = htmlTag(source);
      if ((match = /^(?: {2,}|\\)\n/.exec(source))) {
        nodes.push(fromSource({ type: "hard_break" }));
      } else if ((match = /^\n/.exec(source))) {
        nodes.push(fromSource({ type: "soft_break" }));
      } else if ((match = /^\\([\\`*_[\]{}()#+.!|<>~-])/.exec(source))) {
        text(match[1]);
      } else if ((match = /^(close[sd]?|fix(?:es|ed)?|resolve[sd]?)\s+((?:[a-zA-Z0-9_-][a-zA-Z0-9_.-]*\/[a-zA-Z0-9_-][a-zA-Z0-9_.-]*)?)(\\?)#([1-9]\d*)\b/i.exec(source)) && !/\w/.test(original[original.length - source.length - 1] || "")) {
        const verb = match[1].toLowerCase();
        if (!referenceAllowed(match[2])) nodes.push(fromSource({ type: "code", value: substitute(match[0]) }));
        else
          nodes.push(
            fromSource({
              type: "closing_reference",
              action: verb.startsWith("clos") ? "closes" : verb.startsWith("fix") ? "fixes" : "resolves",
              target: { number: match[4], ...(match[2] ? { repository: match[2] } : {}) },
              enabled: !match[3],
            })
          );
      } else if (!/[A-Za-z0-9_./-]/.test(original[original.length - source.length - 1] || "") && (match = /^([a-zA-Z0-9_-][a-zA-Z0-9_.-]*\/[a-zA-Z0-9_-][a-zA-Z0-9_.-]*)#([1-9]\d*)\b/.exec(source))) {
        if (referenceAllowed(match[1])) {
          text(match[1]);
          nodes.push(fromSource({ type: "issue_reference", number: match[2] }));
        } else nodes.push(fromSource({ type: "code", value: substitute(match[0]) }));
      } else if ((match = /^#\{(\w+)\}/.exec(source)) && /^[1-9]\d*$/.test(String(valueFor(match[1])))) {
        const number = String(valueFor(match[1]));
        nodes.push(fromSource(referenceAllowed("") ? { type: "issue_reference", number } : { type: "code", value: `#${number}` }));
      } else if ((match = /^#([1-9]\d*)\b/.exec(source))) {
        nodes.push(fromSource(referenceAllowed("") ? { type: "issue_reference", number: match[1] } : { type: "code", value: match[0] }));
      } else if ((match = /^\{(\w+)\}/.exec(source))) {
        if (inlineSlots && Object.hasOwn(inlineSlots, match[1])) nodes.push(...inlineSlots[match[1]].map(node => ({ ...node })));
        else {
          const value = valueFor(match[1]);
          if (Array.isArray(value)) throw new TypeError("Report blocks cannot appear inline");
          if (typeof value === "object") nodes.push(...value.children.map(node => ({ ...node })));
          else text(value === undefined ? match[0] : String(value));
        }
      } else if (
        (match = /^@([A-Za-z0-9](?:[A-Za-z0-9_-]{0,37}[A-Za-z0-9])?(?:\/[A-Za-z0-9._-]+)?)(?![a-zA-Z0-9_/-])/.exec(source)) &&
        !/[A-Za-z0-9]/.test(original[original.length - source.length - 1] || "") &&
        (deferMentions || allowedMentions.some(alias => alias.replace(/^@/, "").toLowerCase() === match[1].toLowerCase()))
      ) {
        nodes.push(fromSource({ type: "mention", login: match[1] }));
      } else if ((match = /^(`+)(?!`)([\s\S]*?)(?<!`)\1(?!`)/.exec(source))) {
        nodes.push(fromSource({ type: "code", value: substitute(match[2]) }));
      } else if ((match = /^`+/.exec(source))) {
        text(match[0]);
      } else if (link && (!inLink || link.image)) {
        /** @type {ReportInline[]} */
        const label = link.image ? [fromSource({ type: "text", value: substitute(link.label) })] : inline(link.label, inlineDepth + 1, true);
        const url = substitute(link.destination);
        try {
          nodes.push(fromSource(link.image ? normalizeReportImage({ type: "image", url, alt: substitute(link.label.replace(/\\([\\`*_[\]{}()#+.!|>~-])/g, "$1")) }) : { type: "link", url: normalizeReportAllowedURL(url), children: label }));
        } catch (error) {
          if (!(error instanceof TypeError)) throw error;
          nodes.push(...label, fromSource({ type: "text", value: link.image ? " (image unavailable: URL must use an allowed HTTPS domain)" : " (link unavailable: invalid HTTPS URL)" }));
        }
        source = source.slice(link.raw.length);
        continue;
      } else if (!inLink && (match = /^<(https:\/\/[^<>\s]+)>/.exec(source))) {
        const url = substitute(match[1]);
        try {
          nodes.push(fromSource({ type: "link", url: normalizeReportAllowedURL(url), children: [fromSource({ type: "text", value: url })] }));
        } catch (error) {
          if (!(error instanceof TypeError)) throw error;
          text("Link unavailable: invalid HTTPS URL");
        }
      } else if (!inLink && autolink && !/[A-Za-z0-9_]/.test(original[original.length - source.length - 1] || "")) {
        const url = substitute(autolink);
        try {
          nodes.push(fromSource({ type: "link", url: normalizeReportAllowedURL(url), children: [fromSource({ type: "text", value: url })] }));
        } catch (error) {
          if (!(error instanceof TypeError)) throw error;
          text("Link unavailable: invalid HTTPS URL");
        }
        source = source.slice(autolink.length);
        continue;
      } else if ((match = /^<sub>([\s\S]*?)<\/sub>/.exec(source))) {
        nodes.push(fromSource({ type: "subscript", children: inline(match[1], inlineDepth + 1, inLink) }));
      } else if ((match = /^<(em|i|strong|b|s|del)>([\s\S]*?)<\/\1>/.exec(source))) {
        const type = ["em", "i"].includes(match[1]) ? "emphasis" : ["strong", "b"].includes(match[1]) ? "strong" : "strike";
        nodes.push(fromSource({ type, children: inline(match[2], inlineDepth + 1, inLink) }));
      } else if (tag) {
        const image = tag.name === "img" ? htmlImage(tag.raw, substitute) : undefined;
        if (image) nodes.push(fromSource(image));
        else text(sanitizeUrlDomains(sanitizeUrlProtocols(substitute(tag.raw)), buildAllowedDomains()));
        source = source.slice(tag.raw.length);
        continue;
      } else if ((match = /^~~(?=\S)([\s\S]*?\S)~~/.exec(source))) {
        nodes.push(fromSource({ type: "strike", children: inline(match[1], inlineDepth + 1, inLink) }));
      } else if ((match = /^(\*{3}|_{3})(?=\S)([\s\S]*?\S)\1/.exec(source))) {
        nodes.push(fromSource({ type: "strong", children: [fromSource({ type: "emphasis", children: inline(match[2], inlineDepth + 2, inLink) })] }));
      } else if ((match = /^(\*\*|__)(?=\S)([\s\S]*?\S)\1/.exec(source))) {
        nodes.push(fromSource({ type: "strong", children: inline(match[2], inlineDepth + 1, inLink) }));
      } else if ((match = /^([*_])(?=\S)((?:\{\w+\}|[^{\n]|\{(?!\w+\}))+?)(?<=\S)\1/.exec(source)) && (match[1] !== "_" || !/\w/.test(original[original.length - source.length - 1] || ""))) {
        nodes.push(fromSource({ type: "emphasis", children: inline(match[2], inlineDepth + 1, inLink) }));
      } else {
        text(source[0]);
        source = source.slice(1);
        continue;
      }
      source = source.slice(match[0].length);
    }
    return nodes;
  }

  const lines = markdown.replace(/\r\n?/g, "\n").split("\n");
  let index = 0;
  const nested = source =>
    parseReportMarkdown(source, { context, inlineSlots, blockSlots, onBlockSlot, agentTainted, minimumHeadingLevel: Math.max(3, minimumHeadingLevel), depth: depth + 1, allowFlagMarkers, allowMetadata, allowedMentions, deferMentions });
  const blockSlot = line => /^\{(\w+)\}$/.exec(line.trim())?.[1];
  const isBlockSlot = line => blockSlots && Object.hasOwn(blockSlots, blockSlot(line) ?? "");
  const isFieldLine = line => /^(?:\{\w+\}\s*)+$/.test(line.trim()) && (!inlineSlots || [...line.matchAll(/\{(\w+)\}/g)].some(field => !Object.hasOwn(inlineSlots, field[1])));
  const isBlockStart = line =>
    openingFence(line) ||
    /^(?:#{1,6}\s|>|<details>(?:\s*$|\s*<summary>)|<\/details>\s*$|[-*+]\s|\d+\.\s|---\s*$)/.test(line) ||
    isFieldLine(line) ||
    isBlockSlot(line) ||
    (allowMetadata && /^(?:<!--|\[gh-aw-workflow-call-id\]: # ")/.test(line.trim())) ||
    (allowFlagMarkers && /^<!-- (?:gh-aw-threat-detected|gh-aw-threat-engine-error) -->$/.test(line.trim()));
  /** @param {string} line */
  function cells(line) {
    return line
      .trim()
      .replace(/^\|/, "")
      .replace(/(?<!\\)\|$/, "")
      .split(/(?<!\\)\|/)
      .map(cell => inline(cell.trim()));
  }
  /**
   * @param {boolean} inDetails
   * @param {number} detailsDepth
   * @returns {ReportBlock[]}
   */
  function blocks(inDetails = false, detailsDepth = depth) {
    if (detailsDepth > 16) throw new TypeError("Report details nesting exceeds the maximum depth");
    /** @type {ReportBlock[]} */
    const nodes = [];
    while (index < lines.length) {
      const line = lines[index];
      if (!line.trim()) {
        index++;
        continue;
      }
      if (inDetails && line.trim() === "</details>") {
        index++;
        break;
      }
      let match;
      const slot = blockSlot(line);
      if (blockSlots && slot !== undefined && Object.hasOwn(blockSlots, slot)) {
        nodes.push(...blockSlots[slot].map(node => ({ ...node })));
        onBlockSlot?.(slot);
        index++;
      } else if (allowMetadata && (match = /^- \[[xX]\] expires <!-- gh-aw-expires: ([^<>]+) --> on .+$/.exec(line.trim()))) {
        nodes.push(fromSource({ type: "expiration", expiresAt: match[1].trim() }));
        index++;
      } else if (allowMetadata && (match = /^\[gh-aw-workflow-call-id\]: # "([^"]+)"$/.exec(line.trim()))) {
        try {
          nodes.push(fromSource({ type: "review_marker", workflowCallId: decodeURIComponent(match[1]) }));
        } catch (error) {
          if (!(error instanceof URIError)) throw error;
          nodes.push(fromSource({ type: "paragraph", children: inline(line) }));
        }
        index++;
      } else if (allowMetadata && (match = /^<!-- ([a-zA-Z][a-zA-Z0-9_-]*)(?:: ?([\s\S]*))? -->$/.exec(line.trim()))) {
        if (["gh-aw-execution", "aw-essential-meta"].includes(match[1])) {
          if (match[2] === undefined) throw new TypeError("JSON report metadata requires a value");
          nodes.push(fromSource({ type: "json_marker", name: match[1], data: JSON.parse(match[2]) }));
        } else if (["gh-aw-agentic-workflow", "gh-aw-failure-issue"].includes(match[1]) && match[2] !== undefined) {
          const [value, ...parts] = match[2].split(/,\s+(?=[a-zA-Z0-9_-]+: )/);
          const fields = parts.map(part => {
            const separator = part.indexOf(": ");
            return { key: part.slice(0, separator), value: part.slice(separator + 2) };
          });
          nodes.push(fromSource({ type: "xml_marker", name: match[1], value: substitute(value), fields: fields.map(field => ({ ...field, value: substitute(field.value) })) }));
        } else {
          nodes.push(fromSource({ type: "xml_marker", name: match[1], ...(match[2] === undefined ? {} : { value: substitute(match[2]), ...(!line.includes(`${match[1]}: `) ? { compact: true } : {}) }) }));
        }
        index++;
      } else if (/^(?:-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
        nodes.push(fromSource({ type: "separator" }));
        index++;
      } else if (context && isFieldLine(line)) {
        for (const field of line.matchAll(/\{(\w+)\}/g)) {
          const value = valueFor(field[1]);
          if (Array.isArray(value)) nodes.push(...value);
          else if (typeof value === "object") nodes.push(fromSource({ type: "paragraph", children: value.children }));
          else if (String(value).trim()) nodes.push(fromSource({ type: "paragraph", children: [fromSource({ type: "text", value: String(value) })] }));
        }
        index++;
      } else if (allowFlagMarkers && (match = /^<!-- (gh-aw-threat-detected|gh-aw-threat-engine-error) -->$/.exec(line.trim()))) {
        nodes.push(fromSource({ type: "marker", name: match[1] === "gh-aw-threat-detected" ? "gh-aw-threat-detected" : "gh-aw-threat-engine-error" }));
        index++;
      } else if ((match = openingFence(line))) {
        const fence = match[1];
        const language = match[2].trim();
        const content = [];
        index++;
        while (index < lines.length) {
          const closing = /^ {0,3}(`+|~+)[ \t]*$/.exec(lines[index]);
          if (closing && closing[1][0] === fence[0] && closing[1].length >= fence.length) break;
          content.push(lines[index++]);
        }
        if (index < lines.length) index++;
        const value = substitute(content.join("\n"));
        if (language === "json") {
          try {
            nodes.push(fromSource({ type: "structured_data", data: JSON.parse(value) }));
            continue;
          } catch (error) {
            if (!(error instanceof SyntaxError)) throw error;
            // Invalid JSON examples remain literal code.
          }
        }
        nodes.push(fromSource({ type: "code_block", language: /^[a-zA-Z0-9_+:-]*$/.test(language) ? language : "text", value, ...(fence[0] === "`" && fence.length > 3 ? { fenceLength: Math.min(fence.length, 128) } : {}) }));
      } else if ((match = /^(#{1,6})\s+(.+)$/.exec(line))) {
        nodes.push(fromSource({ type: "heading", level: Math.max(minimumHeadingLevel, match[1].length), children: inline(match[2]) }));
        index++;
      } else if ((match = /^<details>\s*<summary>(.*)<\/summary>$/.exec(line.trim()))) {
        index++;
        nodes.push(fromSource({ type: "details", summary: summary(match[1]), children: blocks(true, detailsDepth + 1) }));
      } else if (line.trim() === "<details>" && /^<summary>.*<\/summary>$/.test(lines[index + 1]?.trim() || "")) {
        const summarySource = lines[index + 1].trim().slice("<summary>".length, -"</summary>".length);
        index += 2;
        nodes.push(fromSource({ type: "details", summary: summary(summarySource), children: blocks(true, detailsDepth + 1) }));
      } else if (/^>/.test(line)) {
        const quoted = [];
        while (index < lines.length && /^>/.test(lines[index])) quoted.push(lines[index++].replace(/^> ?/, ""));
        const alert = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]$/.exec(quoted[0]);
        nodes.push(fromSource(alert ? { type: "alert", kind: alert[1], children: nested(quoted.slice(1).join("\n")) } : { type: "quote", children: nested(quoted.join("\n")) }));
      } else if ((match = /^([-*+]|\d+\.)\s+(.+)$/.exec(line))) {
        const ordered = /^\d/.test(match[1]);
        /** @type {ReportBlock[][]} */
        const items = [];
        while (index < lines.length) {
          const item = /^([-*+]|\d+\.)\s+(.+)$/.exec(lines[index]);
          if (!item || /^\d/.test(item[1]) !== ordered) break;
          const content = [item[2]];
          index++;
          while (index < lines.length && /^\s{2,}\S/.test(lines[index])) content.push(lines[index++].replace(/^ {2,4}/, ""));
          const task = /^\[([ xX])\] (.*)$/.exec(content[0]);
          if (task) items.push([fromSource({ type: "task", checked: task[1] !== " ", children: inline(task[2]) }), ...nested(content.slice(1).join("\n"))]);
          else items.push(nested(content.join("\n")));
        }
        nodes.push(fromSource({ type: "list", ordered, items }));
      } else if (line.includes("|") && /^\|?[\s:|-]+\|[\s:|-]*$/.test(lines[index + 1] || "")) {
        const tableLines = [line, lines[index + 1]];
        const headers = cells(line);
        /** @type {ReportInline[][][]} */
        const rows = [];
        index += 2;
        while (index < lines.length && lines[index].includes("|") && lines[index].trim()) {
          tableLines.push(lines[index]);
          rows.push(cells(lines[index++]));
        }
        if (rows.every(row => row.length === headers.length)) nodes.push(fromSource({ type: "table", headers, rows }));
        else nodes.push(fromSource({ type: "paragraph", children: [fromSource({ type: "text", value: substitute(tableLines.join("\n")) })] }));
      } else {
        const content = [line];
        index++;
        while (index < lines.length && lines[index].trim() && !isBlockStart(lines[index])) {
          if (lines[index].includes("|") && /^\|?[\s:|-]+\|[\s:|-]*$/.test(lines[index + 1] || "")) break;
          content.push(lines[index++]);
        }
        nodes.push(fromSource({ type: "paragraph", children: inline(content.join("\n")) }));
      }
    }
    return nodes;
  }
  return blocks();
}

module.exports = { parseReportMarkdown };
