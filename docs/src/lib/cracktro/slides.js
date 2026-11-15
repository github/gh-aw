// Canonical Cracktro slide list (.github/skills/cracktro/slides.md): parser and talk-page metadata injection.
// Dependency-free so the Astro config, the dev server and the Playwright tests share one implementation.

import { readFileSync } from "node:fs";

export const SLIDES_FILE = new URL("../../../../.github/skills/cracktro/slides.md", import.meta.url);

const HEADER = "| Heading | Notes | Summary |";
const SEPARATOR = "|---|---|---|";
const NOTES = new Set(["", "illustrative"]);

/**
 * @typedef {{ heading: string, illustrative: boolean, summary: string, line: number }} Slide
 */

/**
 * Parses the slide table. Throws `source:line: message` on any malformed, empty, unknown or duplicate entry.
 * @param {string} text
 * @param {string} [source]
 * @returns {Slide[]}
 */
export function parseSlides(text, source = "slides.md") {
  const lines = text.split(/\r?\n/);
  const fail = (/** @type {number} */ index, /** @type {string} */ message) => {
    throw new Error(`${source}:${index + 1}: ${message}`);
  };
  const isRow = (/** @type {string} */ line) => line.trimStart().startsWith("|");

  const headerIndex = lines.findIndex(isRow);
  if (headerIndex < 0) throw new Error(`${source}: missing slide table (expected header "${HEADER}")`);
  if (lines[headerIndex] !== HEADER) fail(headerIndex, `expected table header "${HEADER}", found "${lines[headerIndex]}"`);
  if (lines[headerIndex + 1] !== SEPARATOR) fail(headerIndex + 1, `expected separator "${SEPARATOR}"`);

  /** @type {Slide[]} */
  const slides = [];
  /** @type {Map<string, number>} */
  const headings = new Map();
  /** @type {Map<string, number>} */
  const summaries = new Map();
  let index = headerIndex + 2;
  for (; index < lines.length && isRow(lines[index]); index++) {
    const line = lines[index];
    if (!line.startsWith("| ") || !line.endsWith(" |")) fail(index, `malformed row, expected "| Heading | Notes | Summary |": ${line}`);
    const cells = line
      .slice(1, -1)
      .split("|")
      .map(cell => cell.trim());
    if (cells.length !== 3) fail(index, `expected 3 cells, found ${cells.length} (cells must not contain "|"): ${line}`);
    const [heading, notes, summary] = cells;
    if (!heading) fail(index, "empty Heading");
    if (!NOTES.has(notes)) fail(index, `unknown Notes value "${notes}" (expected empty or "illustrative")`);
    if (!summary) fail(index, `empty Summary for "${heading}"`);
    if (headings.has(heading)) fail(index, `duplicate Heading "${heading}" (first on line ${headings.get(heading)})`);
    if (summaries.has(summary)) fail(index, `duplicate Summary for "${heading}" (first on line ${summaries.get(summary)})`);
    headings.set(heading, index + 1);
    summaries.set(summary, index + 1);
    slides.push({ heading, illustrative: notes === "illustrative", summary, line: index + 1 });
  }
  if (!slides.length) fail(headerIndex, "slide table has no rows");
  for (; index < lines.length; index++) {
    if (isRow(lines[index])) fail(index, "table row outside the slide table (one table only, no blank lines inside it)");
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
 * schema.org `hasPart` entries: one per slide, in order, linking to its `#N` hash.
 * @param {Slide[]} slides
 * @param {string} pageUrl
 */
export function slideParts(slides, pageUrl) {
  return slides.map((slide, i) => ({
    "@type": "CreativeWork",
    position: i + 1,
    name: slide.heading,
    description: slide.summary,
    url: `${pageUrl}#${i + 1}`,
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
