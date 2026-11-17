// @ts-check
import { rehypeHeadingIds } from "@astrojs/markdown-remark";

/**
 * Keep Starlight's page title instead of a matching opening Markdown h1.
 * @type {import('@astrojs/markdown-remark').RehypePlugin}
 */
export default function rehypeDeduplicateTitle() {
  const addHeadingIds = rehypeHeadingIds();
  if (typeof addHeadingIds !== "function") {
    throw new TypeError("Astro's heading ID plugin must return a transformer");
  }

  return (tree, file) => {
    const frontmatter = file.data.astro?.frontmatter;
    const title = frontmatter?.hero?.title ?? frontmatter?.title;
    if (typeof title !== "string" || !title.trim() || isLandingPage(file.path)) return;

    const heading = tree.children.find(isContent);
    if (heading?.type !== "element" || heading.tagName !== "h1") return;
    const text = headingText(heading);
    if (text === null || normalizeTitle(text) !== normalizeTitle(title)) return;

    // Generate IDs before removing the heading to preserve existing deep links
    // and slug suffixes on subsequent headings with the same text.
    addHeadingIds(tree, file, error => {
      if (error) throw error;
    });
    heading.tagName = "span";
    heading.children = [];
  };
}

/** @param {import('unist').Node} node */
function isContent(node) {
  if (node.type === "comment" || node.type === "mdxjsEsm" || isCommentExpression(node)) return false;
  if ("value" in node && typeof node.value === "string") {
    if (node.type === "text" && !node.value.trim()) return false;
    if (node.type === "raw" && /^\s*<!--[\s\S]*-->\s*$/.test(node.value)) return false;
  }
  return true;
}

/** @param {string | undefined} filePath */
function isLandingPage(filePath) {
  return typeof filePath === "string" && /(?:^|[/\\])src[/\\]content[/\\]docs[/\\]index\.mdx?$/.test(filePath);
}

/** @param {import('unist').Node} node */
function isCommentExpression(node) {
  if (node.type !== "mdxFlowExpression" || !node.data || !("estree" in node.data)) return false;
  const program = node.data.estree;
  return typeof program === "object" && program !== null && "body" in program && Array.isArray(program.body) && program.body.length === 0;
}

/**
 * Compare visible text, but keep headings containing images or expressions.
 * @param {import('hast').Nodes} node
 * @returns {string | null}
 */
function headingText(node) {
  if (node.type === "text") return node.value;
  if (node.type !== "element" || node.tagName === "img") return null;
  if (node.tagName === "br") return " ";
  const children = node.children.map(headingText);
  return children.includes(null) ? null : children.join("");
}

/** @param {string} title */
function normalizeTitle(title) {
  return title.normalize("NFC").trim().replace(/\s+/g, " ").toLowerCase();
}
