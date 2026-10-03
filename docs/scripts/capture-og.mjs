// Screenshots the dev-only /og/home/ page to public/og-home-1200x630.png.
// Run `npm run dev` first, then `npm run og` (OG_URL overrides the address).
import { chromium } from "@playwright/test";
import { fileURLToPath } from "node:url";

const url = process.env.OG_URL ?? "http://localhost:4321/gh-aw/og/home/";
const out = fileURLToPath(new URL("../public/og-home-1200x630.png", import.meta.url));

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 });
await page.goto(url, { waitUntil: "networkidle" });
await page.evaluate(() => document.fonts.ready);
// The dithered background fades in once its first frame is drawn.
await page.waitForSelector(".hero-bg-a-canvas.is-ready");
await page.waitForTimeout(1200);
await page.locator(".og").screenshot({ path: out, animations: "disabled" });
await browser.close();
console.log(`Wrote ${out}`);
