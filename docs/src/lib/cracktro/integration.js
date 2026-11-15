// Astro integration: adds the canonical slide list (slides.md) to the talk page's JSON-LD at build time and in dev.
// public/cracktro/index.html stays the hand-written source; only the served or built copy is enhanced.

import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { injectSlideMetadata, loadSlides } from "./slides.js";

const PAGE = "cracktro/";

/** @returns {import("astro").AstroIntegration} */
export default function cracktroMetadata() {
  /** @type {URL} */
  let publicDir;
  let base = "/";
  let pageUrl = "";

  return {
    name: "cracktro-metadata",
    hooks: {
      "astro:config:setup": ({ config, updateConfig }) => {
        if (!config.site) throw new Error("cracktro-metadata needs `site` to build absolute slide URLs");
        publicDir = config.publicDir;
        base = config.base.endsWith("/") ? config.base : `${config.base}/`;
        pageUrl = new URL(`${base}${PAGE}`, config.site).href;
        loadSlides(); // fail fast on an invalid slides.md
        const paths = new Set([`${base}${PAGE}`, `${base}${PAGE}index.html`, `/${PAGE}`, `/${PAGE}index.html`]);
        updateConfig({
          vite: {
            plugins: [
              {
                name: "cracktro-metadata-dev",
                apply: "serve",
                // Registered directly (not returned), so it runs before Vite's public-file handler.
                configureServer(server) {
                  server.middlewares.use(async (req, res, next) => {
                    if ((req.method !== "GET" && req.method !== "HEAD") || !paths.has(new URL(req.url ?? "/", "http://localhost").pathname)) return next();
                    try {
                      const source = await readFile(new URL(`${PAGE}index.html`, publicDir), "utf8");
                      const html = injectSlideMetadata(source, loadSlides(), pageUrl);
                      res.setHeader("Content-Type", "text/html; charset=utf-8");
                      res.setHeader("Content-Length", Buffer.byteLength(html));
                      res.setHeader("Cache-Control", "no-cache");
                      res.end(req.method === "HEAD" ? undefined : html);
                    } catch (error) {
                      next(error);
                    }
                  });
                },
              },
            ],
          },
        });
      },
      "astro:build:done": async ({ dir, logger }) => {
        const file = new URL(`${PAGE}index.html`, dir);
        const slides = loadSlides();
        await writeFile(file, injectSlideMetadata(await readFile(file, "utf8"), slides, pageUrl));
        logger.info(`added ${slides.length} slides to ${fileURLToPath(file)} JSON-LD`);
      },
    },
  };
}
