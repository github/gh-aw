import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { after, before, test } from "node:test";
import { fileURLToPath } from "node:url";
import { build } from "astro";
import { escapeJsonLd } from "../src/lib/json-ld.js";

const docsRoot = fileURLToPath(new URL("../", import.meta.url));
const payload = "</ScRiPt><script data-jsonld-probe>globalThis.__ghAwJsonLdProbe=1</script><!--";
let root;

before(async () => {
  await mkdir(join(docsRoot, ".astro"), { recursive: true });
  root = await mkdtemp(join(docsRoot, ".astro/jsonld-security-"));
  const content = join(root, "src/content/docs");
  await mkdir(join(content, "blog"), { recursive: true });
  await writeFile(
    join(root, "astro.config.mjs"),
    `
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightBlog from 'starlight-blog';
export default defineConfig({
  site: 'https://github.github.com',
  base: '/gh-aw/',
  integrations: [
    starlight({
      title: 'GitHub Agentic Workflows',
      components: { Head: ${JSON.stringify(join(docsRoot, "src/components/CustomHead.astro"))} },
      plugins: [starlightBlog({ authors: { tester: { name: ${JSON.stringify(payload)} } } })],
    }),
  ],
});
`
  );
  await writeFile(
    join(root, "src/content.config.ts"),
    `
import { defineCollection } from 'astro:content';
import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';
import { blogSchema } from 'starlight-blog/schema';
export const collections = {
  docs: defineCollection({ loader: docsLoader(), schema: docsSchema({ extend: blogSchema }) }),
};
`
  );
  await writeFile(join(content, "index.md"), "---\ntitle: Home\n---\nHome.\n");
  for (const kind of ["docs", "blog"]) {
    for (const field of ["title", "description"]) {
      const data = {
        title: field === "title" ? payload : "Ordinary title",
        description: field === "description" ? payload : "Ordinary description",
        ...(kind === "blog" ? { date: "2026-01-13", authors: ["tester"], tags: [payload] } : {}),
      };
      const frontmatter = Object.entries(data)
        .map(([key, value]) => `${key}: ${key === "date" ? value : JSON.stringify(value)}`)
        .join("\n");
      await writeFile(join(content, kind === "blog" ? `blog/${field}.md` : `${field}.md`), `---\n${frontmatter}\n---\nTest page.\n`);
    }
  }
  await build({ root, logLevel: "silent" });
});

after(async () => {
  if (root) await rm(root, { recursive: true, force: true });
});

function schemasFromHtml(html) {
  const scripts = [...html.matchAll(/<script\b[^>]*type="application\/ld\+json"[^>]*>([\s\S]*?)<\/script>/gi)];
  assert.ok(scripts.length > 0, "expected JSON-LD");
  return scripts.map(([, raw]) => {
    assert.ok(!raw.includes("<"), "JSON-LD contains an HTML tag opener");
    return JSON.parse(raw);
  });
}

test("escapes script delimiters without changing JSON values", () => {
  const value = { headline: payload, nested: { text: 'Quotes " & backslash \\ and <tag>' } };
  assert.deepEqual(JSON.parse(escapeJsonLd(JSON.stringify(value))), value);
  assert.ok(!escapeJsonLd(JSON.stringify(value)).includes("<"));
});

for (const kind of ["docs", "blog"]) {
  for (const field of ["title", "description"]) {
    test(`escapes ${kind} ${field} in every production-rendered JSON-LD block`, async () => {
      const path = kind === "blog" ? `blog/${field}` : field;
      const html = await readFile(join(root, "dist", path, "index.html"), "utf8");
      const schemas = schemasFromHtml(html);
      const article = schemas.find(schema => schema["@type"] === (kind === "blog" ? "BlogPosting" : "TechArticle"));
      assert.ok(article, "expected the custom head's article schema");
      assert.equal(article[field === "title" ? "headline" : field], payload);
      if (kind === "blog") {
        const pluginArticle = schemas.flatMap(schema => schema["@graph"] ?? []).find(schema => schema["@type"] === "BlogPosting");
        assert.ok(pluginArticle, "expected the blog plugin's independent article schema");
        assert.equal(pluginArticle[field === "title" ? "headline" : field], payload);
        assert.equal(pluginArticle.author[0].name, payload);
        assert.deepEqual(pluginArticle.keywords, [payload]);
      }
    });
  }
}

test("preserves homepage structured data", async () => {
  const [schema] = schemasFromHtml(await readFile(join(root, "dist/index.html"), "utf8"));
  assert.ok(schema["@graph"].some(item => item["@type"] === "WebSite"));
});
