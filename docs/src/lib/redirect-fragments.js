// Astro's static redirects use meta refresh, which drops incoming URL fragments.
// Insert a script first, keeping the meta refresh as the no-JavaScript fallback.

import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const NAMED_ENTITIES = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'" };
const SCRIPT_MARKER = "data-redirect-fragments";
const COMPARISON_BASE = "https://redirect-fragments.invalid/";

function decodeHtmlEntities(value) {
  return value.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (entity, body) => {
    if (body[0] === "#") return String.fromCodePoint(body[1].toLowerCase() === "x" ? parseInt(body.slice(2), 16) : parseInt(body.slice(1), 10));
    const decoded = NAMED_ENTITIES[body.toLowerCase()];
    if (decoded === undefined) throw new Error(`redirect-fragments: unsupported HTML entity ${entity}`);
    return decoded;
  });
}

function attribute(tag, name) {
  const match = new RegExp(`\\s${name}\\s*=\\s*(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`, "i").exec(tag);
  return match ? decodeHtmlEntities(match[1] ?? match[2] ?? match[3]) : undefined;
}

/** Returns the destination and position of the page's only meta refresh. */
export function readMetaRefresh(html) {
  const tags = [...html.matchAll(/<meta\s[^>]*>/gi)].filter(([tag]) => attribute(tag, "http-equiv")?.trim().toLowerCase() === "refresh");
  if (tags.length !== 1) throw new Error(`expected exactly one meta refresh tag, found ${tags.length}`);
  const [tag] = tags[0];
  const content = attribute(tag, "content") ?? "";
  // Start the URL at a non-space to avoid competing whitespace matches.
  const match = /^\d+\s*;\s*url\s*=\s*(\S.*)$/i.exec(content.trim());
  if (!match) throw new Error(`meta refresh has no url: ${tag}`);
  const url = /^(["'])(.*)\1$/.exec(match[1])?.[2] ?? match[1];
  return { index: tags[0].index, url };
}

function assertSupportedDestination(destination) {
  if (/[\u0000-\u0020\u007f\\]/.test(destination)) throw new Error("whitespace, control characters and backslashes are not supported");
  if (destination.startsWith("/") && !destination.startsWith("//")) return;
  if (!/^https?:\/\//i.test(destination)) throw new Error("destinations must be root-relative paths or absolute http(s) URLs");
  new URL(destination);
}

/** Inserts the script only after verifying the generated redirect's destination. */
export function addFragmentScript(html, destination) {
  assertSupportedDestination(destination);
  if (html.includes(SCRIPT_MARKER)) throw new Error("page already contains the fragment script");
  const refresh = readMetaRefresh(html);
  if (new URL(refresh.url, COMPARISON_BASE).href !== new URL(destination, COMPARISON_BASE).href) {
    throw new Error(`meta refresh points at ${refresh.url}, expected ${destination}`);
  }
  const target = JSON.stringify(destination).replace(/</g, "\\u003c");
  const script = `<script ${SCRIPT_MARKER}>var u=new URL(${target},location.href);if(location.hash&&!u.href.includes("#"))u.hash=location.hash;location.replace(u.href)</script>`;
  return html.slice(0, refresh.index) + script + html.slice(refresh.index);
}

/** Static string redirects declared by the project, excluding normal pages. */
export function projectRedirects(routes) {
  return routes
    .filter(route => route.type === "redirect")
    .map(route => {
      const source = route.entrypoint;
      if (typeof route.redirect !== "string") throw new Error(`redirect-fragments: ${source}: object-form redirects are not supported`);
      if (route.params.length > 0) throw new Error(`redirect-fragments: ${source}: dynamic redirects are not supported`);
      assertSupportedDestination(route.redirect);
      return { source, destination: route.redirect };
    });
}

/** @returns {import("astro").AstroIntegration} */
export default function redirectFragments() {
  /** @type {{ source: string, destination: string }[] | undefined} */
  let redirects;

  return {
    name: "redirect-fragments",
    hooks: {
      "astro:routes:resolved": ({ routes }) => {
        redirects = projectRedirects(routes);
      },
      "astro:build:done": async ({ dir, logger }) => {
        if (!redirects) throw new Error("redirect-fragments: routes were not resolved before the build finished");
        for (const { source, destination } of redirects) {
          const relative = source.replace(/^\/+/, "");
          const file = new URL(`${relative}${relative === "" || relative.endsWith("/") ? "" : "/"}index.html`, dir);
          try {
            const html = await readFile(file, "utf8");
            await writeFile(file, addFragmentScript(html, destination));
          } catch (error) {
            throw new Error(`redirect-fragments: ${source} (${fileURLToPath(file)}): ${error.message}`, { cause: error });
          }
        }
        logger.info(`preserved URL fragments on ${redirects.length} redirect pages`);
      },
    },
  };
}
