// Canonical Cracktro slide list (.github/skills/cracktro/slides.md): parser, talk-page metadata injection and the
// generated kiosk closed-captions script (captions.js).
// Dependency-free so the Astro config, the dev server and the Playwright tests share one implementation.

import { readFileSync } from "node:fs";

export const SLIDES_FILE = new URL("../../../../.github/skills/cracktro/slides.md", import.meta.url);

// A slide: `### <Heading> {#<id>}`, then `#### <Field>` sections in FIELDS order (Notes optional).
const SLIDE = /^### (.*?)\s*\{#([^}]*)\}\s*$/;
const FIELD = /^#### (.*?)\s*$/;
const FIELDS = ["Notes", "Summary", "Keywords", "Captions"];
const NOTES = new Set(["illustrative"]);
// A stable `#id` link anchor: lowercase kebab-case.
const ID = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const FENCE = /^\s*(```|~~~)/;

/**
 * @typedef {{ id: string, heading: string, illustrative: boolean, summary: string, keywords: string[], captions: string[], line: number }} Slide
 */

/**
 * Parses the slide list. Throws `source:line: message` on any malformed, invalid, empty, unknown or duplicate entry.
 * Text before the first `###` heading outside a code fence is free-form documentation.
 * @param {string} text
 * @param {string} [source]
 * @returns {Slide[]}
 */
export function parseSlides(text, source = "slides.md") {
  const lines = text.split(/\r?\n/);
  const fail = (/** @type {number} */ index, /** @type {string} */ message) => {
    throw new Error(`${source}:${index + 1}: ${message}`);
  };

  let start = 0;
  for (let fenced = false; start < lines.length; start++) {
    if (FENCE.test(lines[start])) fenced = !fenced;
    else if (!fenced && lines[start].startsWith("### ")) break;
  }
  if (start === lines.length) throw new Error(`${source}: no slides (expected "### <Heading> {#<id>}" sections)`);

  /** @type {{ index: number, heading: string, id: string, fields: Map<string, { index: number, lines: { index: number, text: string }[] }> }[]} */
  const blocks = [];
  /** @type {{ index: number, lines: { index: number, text: string }[] } | undefined} */
  let field;
  for (let index = start; index < lines.length; index++) {
    const line = lines[index];
    if (line.startsWith("#")) {
      const slide = SLIDE.exec(line);
      const name = FIELD.exec(line)?.[1];
      if (slide) {
        blocks.push({ index, heading: slide[1], id: slide[2], fields: new Map() });
        field = undefined;
        continue;
      }
      if (name !== undefined) {
        const block = blocks[blocks.length - 1];
        const label = `"${block.heading}"`;
        const order = FIELDS.indexOf(name);
        if (order < 0) fail(index, `unknown field "${name}" for ${label} (expected ${FIELDS.join(", ")})`);
        if (block.fields.has(name)) fail(index, `duplicate field "${name}" for ${label}`);
        const later = [...block.fields.keys()].find(other => FIELDS.indexOf(other) > order);
        if (later) fail(index, `field "${name}" for ${label} must come before "${later}" (order: ${FIELDS.join(", ")})`);
        field = { index, lines: [] };
        block.fields.set(name, field);
        continue;
      }
      fail(index, `expected "### <Heading> {#<id>}" or "#### <Field>", found "${line}"`);
    }
    if (!line.trim()) continue;
    if (!field) fail(index, `text outside a "#### <Field>" section of "${blocks[blocks.length - 1].heading}": ${line}`);
    field.lines.push({ index, text: line });
  }

  /** @type {Slide[]} */
  const slides = [];
  /** @type {Map<string, number>} */
  const ids = new Map();
  /** @type {Map<string, number>} */
  const headings = new Map();
  /** @type {Map<string, number>} */
  const summaries = new Map();
  for (const { index, heading, id, fields } of blocks) {
    if (!heading) fail(index, "empty Heading");
    if (!ID.test(id)) fail(index, `invalid Id "${id}" for "${heading}" (expected lowercase kebab-case)`);
    if (ids.has(id)) fail(index, `duplicate Id "${id}" (first on line ${ids.get(id)})`);
    for (const name of FIELDS.slice(1)) if (!fields.has(name)) fail(index, `missing "#### ${name}" for "${heading}"`);
    const join = (/** @type {string} */ name) =>
      fields
        .get(name)
        ?.lines.map(line => line.text.trim())
        .join(" ") ?? "";
    const at = (/** @type {string} */ name) => /** @type {number} */ fields.get(name)?.index;
    const notes = join("Notes");
    if (fields.has("Notes") && !NOTES.has(notes)) fail(at("Notes"), `unknown Notes value "${notes}" (expected "illustrative", or omit Notes)`);
    const summary = join("Summary");
    if (!summary) fail(at("Summary"), `empty Summary for "${heading}"`);
    const keywords = join("Keywords")
      .split(",")
      .map(keyword => keyword.trim());
    if (keywords.some(keyword => !keyword)) fail(at("Keywords"), `empty keyword for "${heading}" (Keywords is a non-empty, comma-separated list)`);
    const seen = new Set();
    for (const keyword of keywords) {
      if (seen.has(keyword.toLowerCase())) fail(at("Keywords"), `duplicate keyword "${keyword}" for "${heading}"`);
      seen.add(keyword.toLowerCase());
    }
    // Captions: one "- " list item per speaker turn; indented lines continue the turn.
    /** @type {string[]} */
    const captions = [];
    for (const line of /** @type {{ index: number, lines: { index: number, text: string }[] }} */ fields.get("Captions").lines) {
      if (/^[-*](?:\s|$)/.test(line.text)) captions.push(line.text.slice(1).trim());
      else if (captions.length && /^\s/.test(line.text)) captions[captions.length - 1] = `${captions[captions.length - 1]} ${line.text.trim()}`.trim();
      else fail(line.index, `Captions for "${heading}" must be a "- " list, one speaker turn per item: ${line.text}`);
      if (!captions[captions.length - 1]) fail(line.index, `empty caption turn for "${heading}"`);
    }
    if (captions.length < 2) fail(at("Captions"), `Captions for "${heading}" need at least two speaker turns ("- " list items)`);
    const headingKey = heading.toLowerCase();
    if (headings.has(headingKey)) fail(index, `duplicate Heading "${heading}" (first on line ${headings.get(headingKey)})`);
    if (summaries.has(summary)) fail(at("Summary"), `duplicate Summary for "${heading}" (first on line ${summaries.get(summary)})`);
    ids.set(id, index + 1);
    headings.set(headingKey, index + 1);
    summaries.set(summary, at("Summary") + 1);
    slides.push({ id, heading, illustrative: notes === "illustrative", summary, keywords, captions, line: index + 1 });
  }
  return slides;
}

/**
 * Reads and parses the canonical slide list (fresh on every call).
 * @param {URL | string} [file]
 */
export function loadSlides(file = SLIDES_FILE) {
  return parseSlides(readFileSync(file, "utf8"), file instanceof URL ? file.pathname : file);
}

/**
 * schema.org `hasPart` entries: one per slide, in order, linking to its stable `#id` hash. `keywords` is the
 * comma-separated schema.org text form; the deck's slide search reads it too.
 * @param {Slide[]} slides
 * @param {string} pageUrl
 */
export function slideParts(slides, pageUrl) {
  return slides.map((slide, i) => ({
    "@type": "CreativeWork",
    position: i + 1,
    name: slide.heading,
    description: slide.summary.replace(/^Band header \([^)]*\):\s*/, ""),
    keywords: slide.keywords.join(", "),
    url: `${pageUrl}#${slide.id}`,
  }));
}

/**
 * JSON for an inline `<script type="application/ld+json">`: escapes characters that could close the script or break lines.
 * @param {unknown} value
 */
export function serializeJsonLd(value) {
  return JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, char => `\\u${char.charCodeAt(0).toString(16).padStart(4, "0")}`);
}

const JSON_LD = /(<script\b[^>]*\btype=["']application\/ld\+json["'][^>]*>)([\s\S]*?)(<\/script\s*>)/gi;

/**
 * Sets `hasPart` on the page's single PresentationDigitalDocument JSON-LD block. Every other field and every
 * byte outside the block are preserved; running it again on its own output is a no-op.
 * @param {string} html
 * @param {Slide[]} slides
 * @param {string} pageUrl absolute talk URL, which must equal the JSON-LD `url`
 */
export function injectSlideMetadata(html, slides, pageUrl) {
  const blocks = [...html.matchAll(JSON_LD)].filter(match => {
    let data;
    try {
      data = JSON.parse(match[2]);
    } catch (error) {
      throw new Error(`talk page JSON-LD is not valid JSON: ${/** @type {Error} */ error.message}`);
    }
    return data?.["@type"] === "PresentationDigitalDocument";
  });
  if (blocks.length !== 1) throw new Error(`talk page must have exactly one PresentationDigitalDocument JSON-LD block, found ${blocks.length}`);
  const [block] = blocks;
  const data = JSON.parse(block[2]);
  if (data.url !== pageUrl) throw new Error(`talk page JSON-LD url "${data.url}" does not match "${pageUrl}"`);
  data.hasPart = slideParts(slides, pageUrl);
  const start = /** @type {number} */ block.index;
  return html.slice(0, start) + block[1] + serializeJsonLd(data) + block[3] + html.slice(start + block[0].length);
}

/**
 * The deck's `captions.js`: `window.CracktroCaptions.scripts` maps each slide id, in order, to its caption turns.
 * Generated by the docs build and dev server; JSON escaping keeps it a single, inert script statement.
 * @param {Slide[]} slides
 */
export function renderCaptions(slides) {
  const scripts = Object.fromEntries(slides.map(slide => [slide.id, slide.captions]));
  return `// Generated from .github/skills/cracktro/slides.md (Captions) by the docs build; do not edit.\nwindow.CracktroCaptions = ${serializeJsonLd({ scripts })};\n`;
}

/**
 * The voice track's sync key for a slide's caption turns: FNV-1a 32-bit over the UTF-16 code units of the turns
 * joined by "\n", as 8 hex digits. runtime.js computes the same key and plays a recorded segment only when they match.
 * @param {string[]} captions
 */
export function captionHash(captions) {
  const text = captions.join("\n");
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) h = Math.imul(h ^ text.charCodeAt(i), 0x01000193) >>> 0;
  return h.toString(16).padStart(8, "0");
}
