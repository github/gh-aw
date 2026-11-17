import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { after, before, test } from "node:test";
import { fileURLToPath } from "node:url";
import { build } from "astro";

const docsRoot = fileURLToPath(new URL("../", import.meta.url));
let root;

const cases = [
  {
    name: "matching",
    body: "<!-- Introductory comment -->\n\n# Page Title\n\nContent.\n\n| Column |\n| --- |\n| Cell |\n\n## Page Title",
    headings: 1,
    anchor: "page-title",
    section: "page-title-1",
  },
  {
    name: "formatted",
    title: "A Smart Title & Links",
    body: "# A **smart** `Title` &amp;   [Links](#details)\n\n## Details",
    headings: 1,
    anchor: "a-smart-title--links",
  },
  { name: "distinct", body: "# A different title", headings: 2 },
  { name: "later", body: "Introduction.\n\n# Page Title", headings: 2 },
  { name: "section", body: "## Page Title", headings: 1, section: "page-title" },
  { name: "code", body: "```md\n# Page Title\n```\n\n# Page Title", headings: 2 },
  { name: "no-heading", body: "Content without a heading.", headings: 1 },
  { name: "image", body: "# Page Title ![Badge](data:image/svg+xml,%3Csvg%3E%3C/svg%3E)", headings: 2 },
  {
    name: "hero",
    extra: 'hero:\n  title: "Hero Title"\n',
    body: "# Hero Title",
    headings: 1,
    anchor: "hero-title",
  },
  {
    name: "distinct-hero",
    extra: 'hero:\n  title: "Hero Title"\n',
    body: "# Page Title",
    headings: 2,
  },
];

before(async () => {
  await mkdir(join(docsRoot, ".astro"), { recursive: true });
  root = await mkdtemp(join(docsRoot, ".astro/page-titles-"));
  const content = join(root, "src/content/docs");
  await mkdir(content, { recursive: true });
  await writeFile(
    join(root, "astro.config.mjs"),
    `
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import productionConfig from ${JSON.stringify(join(docsRoot, "astro.config.mjs"))};
export default defineConfig({
  site: 'https://github.github.com',
  base: '/gh-aw/',
  markdown: productionConfig.markdown,
  integrations: [starlight({
    title: 'Docs',
    components: { PageTitle: ${JSON.stringify(join(docsRoot, "src/components/PageTitle.astro"))} },
  })],
});
`
  );
  await writeFile(
    join(root, "src/content.config.ts"),
    `
import { defineCollection } from 'astro:content';
import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';
export const collections = { docs: defineCollection({ loader: docsLoader(), schema: docsSchema() }) };
`
  );
  await writeFile(join(content, "index.md"), "---\ntitle: Home\n---\n# Home\n");
  for (const extension of ["md", "mdx"]) {
    for (const scenario of cases) {
      const imports = extension === "mdx" ? "import { Card } from '@astrojs/starlight/components';\n\n" : "";
      const body = extension === "mdx" && scenario.name === "matching" ? scenario.body.replace("<!-- Introductory comment -->\n\n", "{/* Introductory comment */}\n\n") : scenario.body;
      await writeFile(join(content, `${scenario.name}-${extension}.${extension}`), `---\ntitle: ${JSON.stringify(scenario.title ?? "Page Title")}\n${scenario.extra ?? ""}---\n${imports}${body}\n`);
    }
    await writeFile(join(content, "expression.mdx"), '---\ntitle: Page Title\n---\n# Page Title {"details"}\n');
  }
  await writeFile(join(content, "unified-agent-session-specification.md"), await readFile(join(docsRoot, "src/content/docs/specs/unified-agent-session-specification.md"), "utf8"));
  await build({ root, logLevel: "silent" });
});

after(async () => {
  if (root) await rm(root, { recursive: true, force: true });
});

for (const extension of ["md", "mdx"]) {
  for (const scenario of cases) {
    test(`${extension}: ${scenario.name} keeps only redundant opening titles out of the output`, async () => {
      const html = await readFile(join(root, "dist", `${scenario.name}-${extension}`, "index.html"), "utf8");
      assert.equal([...html.matchAll(/<h1\b/g)].length, scenario.headings);
      assert.match(html, /<h1 id="_top"/);
      if (scenario.anchor) {
        assert.ok(html.includes(`<span id="${scenario.anchor}"></span>`), "original title anchor is preserved");
        assert.ok(!html.includes(`href="#${scenario.anchor}"`), "duplicate title is not in the table of contents");
      }
      if (scenario.name === "matching") {
        assert.match(html, /class="table-scroll-wrapper"/);
        assert.match(html, /<td data-label="Column">Cell<\/td>/);
      }
      if (scenario.section) assert.ok(html.includes(`id="${scenario.section}"`), "section heading keeps its original slug");
    });
  }
}

test("the unified session specification renders its title once", async () => {
  const html = await readFile(join(root, "dist/unified-agent-session-specification/index.html"), "utf8");
  assert.equal([...html.matchAll(/<h1\b/g)].length, 1);
  assert.match(html, /<h1 id="_top"[^>]*>Unified Agent Session Specification<\/h1>/);
  assert.match(html, /<span id="unified-agent-session-specification"><\/span>/);
});

test("the landing page keeps its own title", async () => {
  const html = await readFile(join(root, "dist/index.html"), "utf8");
  assert.equal([...html.matchAll(/<h1\b/g)].length, 1);
  assert.match(html, /<h1 id="home">Home<\/h1>/);
});

test("headings with MDX expressions keep their dynamic content", async () => {
  const html = await readFile(join(root, "dist/expression/index.html"), "utf8");
  assert.equal([...html.matchAll(/<h1\b/g)].length, 2);
  assert.match(html, /Page Title details<\/h1>/);
});
