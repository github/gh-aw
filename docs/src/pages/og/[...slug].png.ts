// Per-page social images: /og/<page id>.png, built at build time.
// Satori lays out the card as SVG (text as paths, so no system fonts needed)
// and sharp rasterises it. The backdrop is src/og/background.png, captured
// from the hero clouds with `npm run og`; the home page uses its own image.
import type { APIRoute, GetStaticPaths } from "astro";
import { getCollection, type CollectionEntry } from "astro:content";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import satori from "satori";
import sharp from "sharp";

type Props = { entry: CollectionEntry<"docs"> };

export const getStaticPaths = (async () => {
  const entries = await getCollection("docs", ({ id }) => id !== "index");
  return entries.map(entry => ({ params: { slug: entry.id }, props: { entry } }));
}) satisfies GetStaticPaths;

// Paths are from the docs root; import.meta.url moves once the build bundles this file.
const asset = (path: string) => readFile(resolve("src", path));

// Loaded once and shared across every page.
const shared = Promise.all([asset("og/fonts/MonaSans-Medium.otf"), asset("og/fonts/MonaSans-SemiBold.otf"), asset("og/background.png"), asset("assets/agentic-workflow-light.svg")]);

// Light-theme values of --aw-color-text / -text-secondary / -text-muted.
const color = { text: "#1c1917", secondary: "#57534e", muted: "#78716c" };

type Node = { type: string; props: Record<string, unknown> };
const h = (type: string, style: Record<string, unknown>, children?: unknown, extra = {}): Node => ({
  type,
  props: { style, children, ...extra },
});

export const GET: APIRoute<Props> = async ({ props: { entry } }) => {
  const [medium, semibold, background, logo] = await shared;
  const { title, description } = entry.data;
  const titleSize = title.length > 60 ? 56 : title.length > 32 ? 64 : 72;
  const path = `github.github.com/gh-aw/${entry.id}`;

  const card = h(
    "div",
    {
      width: 1200,
      height: 630,
      display: "flex",
      flexDirection: "column",
      justifyContent: "space-between",
      padding: "56px 64px",
      backgroundImage: `url(data:image/png;base64,${background.toString("base64")})`,
      fontFamily: "Mona Sans",
      color: color.text,
    },
    [
      h("div", { display: "flex", alignItems: "center", gap: 12, fontSize: 26, fontWeight: 600 }, [
        h("img", { width: 32, height: 32 }, undefined, {
          src: `data:image/svg+xml;base64,${logo.toString("base64")}`,
        }),
        "GitHub Agentic Workflows",
      ]),
      h("div", { display: "flex", flexDirection: "column", gap: 24, maxWidth: 940 }, [
        h("div", { display: "block", fontSize: titleSize, fontWeight: 500, letterSpacing: "-0.03em", lineHeight: 1.08, lineClamp: 3 }, title),
        description && h("div", { display: "block", fontSize: 28, lineHeight: 1.4, color: color.secondary, lineClamp: 2 }, description),
      ]),
      h("div", { fontSize: 22, color: color.muted, fontWeight: 500 }, path),
    ]
  );

  const svg = await satori(card as never, {
    width: 1200,
    height: 630,
    fonts: [
      { name: "Mona Sans", data: medium, weight: 500, style: "normal" },
      { name: "Mona Sans", data: semibold, weight: 600, style: "normal" },
    ],
  });
  // A 256-colour palette keeps each card around 25 KB with no visible change.
  const png = await sharp(Buffer.from(svg)).png({ palette: true, quality: 90, compressionLevel: 9 }).toBuffer();
  return new Response(new Uint8Array(png), { headers: { "Content-Type": "image/png" } });
};
