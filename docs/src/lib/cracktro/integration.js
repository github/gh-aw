// Astro integration: adds the canonical slide list (slides.md) to the talk page's JSON-LD and generates the deck's
// closed captions (captions.js) from it, at build time and in dev. public/cracktro/index.html stays the hand-written
// source; only the served or built copy is enhanced.

import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { injectSlideMetadata, loadSlides, renderCaptions } from "./slides.js";

const PAGE = "cracktro/";
const CAPTIONS = `${PAGE}captions.js`;

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
        const captionPaths = new Set([`${base}${CAPTIONS}`, `/${CAPTIONS}`]);
        updateConfig({
          vite: {
            plugins: [
              {
                name: "cracktro-metadata-dev",
                apply: "serve",
                // Registered directly (not returned), so it runs before Vite's public-file handler.
                configureServer(server) {
                  server.middlewares.use(async (req, res, next) => {
                    const path = new URL(req.url ?? "/", "http://localhost").pathname;
                    const captions = captionPaths.has(path);
                    if ((req.method !== "GET" && req.method !== "HEAD") || !(captions || paths.has(path))) return next();
                    try {
                      const body = captions ? renderCaptions(loadSlides()) : injectSlideMetadata(await readFile(new URL(`${PAGE}index.html`, publicDir), "utf8"), loadSlides(), pageUrl);
                      res.setHeader("Content-Type", captions ? "text/javascript; charset=utf-8" : "text/html; charset=utf-8");
                      res.setHeader("Content-Length", Buffer.byteLength(body));
                      res.setHeader("Cache-Control", "no-cache");
                      res.end(req.method === "HEAD" ? undefined : body);
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
        const captions = new URL(CAPTIONS, dir);
        await writeFile(captions, renderCaptions(slides));
        logger.info(`generated ${fileURLToPath(captions)} from slides.md captions`);
      },
    },
  };
}
