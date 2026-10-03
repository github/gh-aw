// Captures the dev-only social preview pages:
//   /og-preview/home/       → public/og-home-1200x630.png (the site's card)
//   /og-preview/background/ → src/og/background.png (backdrop for per-page cards)
// Run `npm run dev` first, then `npm run og` (OG_ORIGIN overrides the address).
import { chromium } from "@playwright/test";
import { fileURLToPath } from "node:url";

const origin = process.env.OG_ORIGIN ?? "http://localhost:4321";
const shots = [
  { path: "/gh-aw/og-preview/home/", out: "../public/og-home-1200x630.png" },
  { path: "/gh-aw/og-preview/background/", out: "../src/og/background.png" },
];

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 });
for (const { path, out } of shots) {
  const file = fileURLToPath(new URL(out, import.meta.url));
  await page.goto(origin + path, { waitUntil: "networkidle" });
  await page.evaluate(() => document.fonts.ready);
  // The dithered background fades in once its first frame is drawn.
  await page.waitForSelector(".hero-bg-a-canvas.is-ready");
  await page.waitForTimeout(1200);
  await page.locator(".og").screenshot({ path: file, animations: "disabled" });
  console.log(`Wrote ${file}`);
}
await browser.close();
