import assert from "node:assert/strict";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { compile } from "@mdx-js/mdx";
import rehypeDeduplicateTitle from "./deduplicateTitle.js";

async function transform(value, path = "page.mdx", title = "Page Title") {
  let result;
  await compile(
    {
      value,
      path: fileURLToPath(new URL(`../../content/docs/${path}`, import.meta.url)),
      data: { astro: { frontmatter: { title } } },
    },
    {
      rehypePlugins: [
        rehypeDeduplicateTitle,
        () => tree => {
          result = tree;
        },
      ],
    }
  );
  return result.children.find(node => node.type === "element");
}

for (const extension of ["md", "mdx"]) {
  test(`${extension}: the landing page retains its matching Markdown title`, async () => {
    const heading = await transform("# Home", `index.${extension}`, "Home");
    assert.equal(heading.tagName, "h1");
  });

  test(`${extension}: a nested index still deduplicates its matching title`, async () => {
    const heading = await transform("# Page Title", `guide/index.${extension}`);
    assert.equal(heading.tagName, "span");
  });
}

for (const expression of ["{/* Introductory comment */}", "{/* First comment */ /* Second comment */}", "{// Introductory comment\n}"]) {
  test(`comment-only MDX expressions do not precede the title: ${expression}`, async () => {
    const heading = await transform(`${expression}\n\n# Page Title`);
    assert.equal(heading.tagName, "span");
  });
}

for (const expression of ['{"Visible introduction"}', '{/* Before */ "Visible introduction" /* After */}', "{/* Before */ <p>Visible introduction</p> /* After */}"]) {
  test(`renderable MDX expressions keep a later matching title: ${expression}`, async () => {
    const heading = await transform(`${expression}\n\n# Page Title`);
    assert.equal(heading.tagName, "h1");
  });
}
