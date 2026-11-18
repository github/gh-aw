import { test, expect, type Page } from "@playwright/test";
import { readFileSync, readdirSync } from "node:fs";
import { injectSlideMetadata, loadSlides, parseSlides, serializeJsonLd, slideParts } from "../src/lib/cracktro/slides.js";

// The ordered slide list lives in .github/skills/cracktro/slides.md; a slide's number is its row position, its link the stable #id.
const SLIDES = loadSlides();
const PAGE_URL = "https://github.github.com/gh-aw/cracktro/";
const SOURCE_HTML = readFileSync(new URL("../public/cracktro/index.html", import.meta.url), "utf8");
// Same check as the deck.js privacy test: no handles, private repository references or full commit SHAs.
const PRIVATE_REFS = /\bwho:\s|(?<![.\w])github(?:next)?\/(?!squad\b)[\w.-]+|\b[a-f0-9]{40}\b/i;
const jsonLd = (html: string) => [...html.matchAll(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/g)].map(m => JSON.parse(m[1])).filter(d => d["@type"] === "PresentationDigitalDocument");
const TOTAL = SLIDES.length;
const slideNo = (heading: string) => {
  const n = SLIDES.findIndex(s => s.heading === heading) + 1;
  if (!n) throw new Error(`slide "${heading}" missing from slides.md`);
  return n;
};
const slideId = (heading: string) => SLIDES[slideNo(heading) - 1].id;
// the current slide is the active dot of the slide mini-map (the deck shows no slide counter)
const CURRENT = '.dots button.on[aria-label^="Slide "]';
const at = (n: number) => `Slide ${n}`;
const SECURITY = slideNo("Security");
const HANDOFFS = slideNo("Event-driven Hand-offs");

test.use({ reducedMotion: "reduce" });

test("presents the slides listed in slides.md in bands without the removed sections", async ({ page }) => {
  test.slow();
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.goto("/gh-aw/cracktro/");
  await expect(page).toHaveTitle("GitHub Agentic Workflows Talk");
  await expect(page.locator(".slide")).toHaveCount(TOTAL);
  await expect(page.locator(".stage")).not.toContainText(/the crew|the story|akeso|security at (?:hyper)?scale/i);

  const headings = SLIDES.map(s => s.heading);
  for (const [index, heading] of headings.entries()) {
    await page.getByRole("button", { name: `Slide ${index + 1}`, exact: true }).click();
    await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(index + 1));
    await expect(page.locator(".slide.active").getByRole("heading").first()).toHaveText(heading);
    await page.evaluate(() => new Promise(requestAnimationFrame));
  }
  expect(errors).toEqual([]);
});

test("preserves navigation, slide links, speaker notes, and sound controls", async ({ page }) => {
  await page.goto(`/gh-aw/cracktro/#${HANDOFFS}`);
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(HANDOFFS));
  await page.keyboard.press("n");
  await expect(page.locator(".notes")).toBeVisible();
  await expect(page.locator(".notes")).toContainText("LabelOps");
  await page.keyboard.press("n");
  await expect(page.locator(".notes")).toBeHidden();

  await page.getByRole("button", { name: "Toggle sound (M)", exact: true }).click();
  expect(await page.evaluate(() => localStorage.getItem("aw-mute"))).toBe("1");
  await page.keyboard.press("m");
  expect(await page.evaluate(() => localStorage.getItem("aw-mute"))).toBe("0");
  await page.getByRole("button", { name: "Toggle sound (M)", exact: true }).evaluate(button => button.blur());

  // Space follows the main line from Event-driven Hand-offs to Repo Commands
  await page.keyboard.press("Space");
  await expect(page).toHaveURL(new RegExp(`/gh-aw/cracktro/#${slideId("Repo Commands")}$`));
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(HANDOFFS));
  await page.reload();
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(HANDOFFS));
  await page.keyboard.press("Home");
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(1));
  await page.keyboard.press("End");
  await page.keyboard.press("ArrowRight");
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(TOTAL));
});

test("fuzzy-searches slide titles, keywords, summaries and text from the dock's search dialog", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  const count = page.locator(CURRENT);
  const dialog = page.getByRole("dialog", { name: "Search slides" });
  const input = dialog.getByRole("combobox", { name: "Search slides" });
  const options = dialog.getByRole("option");
  await page.goto("/gh-aw/cracktro/#3");
  await expect(dialog).toBeHidden();

  // the search button sits just left of the sound toggle
  const [search, sound] = await Promise.all([page.locator(".fzb").boundingBox(), page.locator(".snd").boundingBox()]);
  expect(search!.x + search!.width).toBeLessThanOrEqual(sound!.x);
  expect(Math.abs(search!.y - sound!.y)).toBeLessThan(1);

  // an empty query lists every slide; a typo still finds the slide by its title, with the matched bounds marked
  await page.getByRole("button", { name: "Search slides (/)", exact: true }).click();
  await expect(dialog).toBeVisible();
  await expect(input).toBeFocused();
  await expect(options).toHaveCount(TOTAL);
  await input.fill("zero secrt");
  await expect(options.first()).toContainText("Zero-secret agents");
  await expect(options.first().locator(".fz-t mark")).toHaveText(["Zero", "secret"]);
  await page.keyboard.press("Enter");
  await expect(dialog).toBeHidden();
  await expect(count).toHaveAttribute("aria-label", at(slideNo("Zero-secret agents")));
  // deck keys work again once the dialog closes
  await page.keyboard.press("ArrowRight");
  await expect(count).toHaveAttribute("aria-label", at(slideNo("Zero-secret agents") + 1));

  // "/" opens it; summaries from slides.md are searchable and shown; arrows pick a result; Escape leaves the deck alone
  await page.keyboard.press("/");
  await expect(dialog).toBeVisible();
  const summary = SLIDES[SECURITY + 1].summary;
  const words = summary
    .split(/\W+/)
    .filter(w => w.length > 6)
    .slice(0, 2);
  await input.fill(words.join(" "));
  await expect(options.filter({ hasText: summary.slice(0, 40) }).first()).toBeVisible();
  // slides.md keywords match even when the slide never shows the word, and the matched keyword is displayed
  await input.fill("dogfooding");
  await expect(options).toHaveCount(1);
  await expect(options.first().locator(".fz-t")).toContainText("AW factory");
  await expect(options.first().locator(".fz-k mark")).toHaveText("dogfooding");
  await input.fill("no such slide qqqzzz");
  await expect(options).toHaveCount(0);
  await expect(dialog).toContainText("No slides match");
  await input.fill(`#${SECURITY}`);
  await expect(options).toHaveCount(1);
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(count).toHaveAttribute("aria-label", at(slideNo("Zero-secret agents") + 1));
  await page.keyboard.press("/");
  await input.fill("");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await expect(options.nth(2)).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Enter");
  await expect(count).toHaveAttribute("aria-label", at(3));
  expect(errors).toEqual([]);
});

test("jumps to any slide from the dock's full-screen slide map", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  const count = page.locator(CURRENT);
  const dialog = page.getByRole("dialog", { name: "Slide map" });
  const tiles = dialog.getByRole("button", { name: /^Go to slide \d+:/ });
  const button = page.getByRole("button", { name: "Slide map (G)", exact: true });
  await page.goto("/gh-aw/cracktro/#3");
  await expect(dialog).toBeHidden();
  await expect(button).toHaveAttribute("title", "Slide map (G)");

  // the map lists every slide, marks and focuses the current one, and a click jumps there
  await button.click();
  await expect(dialog).toBeVisible();
  await expect(tiles).toHaveCount(TOTAL);
  // each band is a region headed by its title and slide range, above its tiles
  const security = dialog.getByRole("region", { name: /^Security #/ });
  await expect(security.getByRole("heading", { level: 3 })).toHaveText(new RegExp(`^Security #${SECURITY}–\\d+$`));
  await expect(security.getByRole("button", { name: new RegExp(`^Go to slide ${SECURITY}:`) })).toBeVisible();
  // a column's examples are stacked below its pattern tile
  const kids = dialog.getByRole("group", { name: "Daily Improvers examples" });
  await expect(kids.getByRole("button", { name: new RegExp(`^Go to slide ${slideNo("Repo Assist")}:`) })).toBeVisible();
  const current = dialog.locator('[aria-current="true"]');
  await expect(current).toHaveAccessibleName(/^Go to slide 3:/);
  await expect(current).toBeFocused();
  await dialog.getByRole("button", { name: new RegExp(`^Go to slide ${SECURITY}:`) }).click();
  await expect(dialog).toBeHidden();
  await expect(count).toHaveAttribute("aria-label", at(SECURITY));

  // G opens it, Right then Enter picks the next slide, and Escape closes it without navigating
  await page.keyboard.press("g");
  await expect(dialog).toBeVisible();
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("Enter");
  await expect(dialog).toBeHidden();
  await expect(count).toHaveAttribute("aria-label", at(SECURITY + 1));
  await page.keyboard.press("g");
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(count).toHaveAttribute("aria-label", at(SECURITY + 1));
  expect(errors).toEqual([]);
});

test("moves through the bands, up/down landing on each band's first slide", async ({ page }) => {
  const count = page.locator(CURRENT);
  await page.goto("/gh-aw/cracktro/#1");
  await page.keyboard.press("ArrowDown");
  await expect(count).toHaveAttribute("aria-label", at(2));
  await expect(page.locator(".slide.active").getByRole("heading").first()).toHaveText("Agentic Workflows");
  await page.keyboard.press("ArrowRight");
  await expect(count).toHaveAttribute("aria-label", at(3));
  // down walks the Continuous AI column first, then leaves for the next band
  await page.keyboard.press("ArrowDown");
  await expect(count).toHaveAttribute("aria-label", at(slideNo("Continuous AI")));
  await page.keyboard.press("ArrowDown");
  await expect(count).toHaveAttribute("aria-label", at(SECURITY));
  // left from a band header wraps to the last main-line slide of the band above, skipping its column of examples
  await page.keyboard.press("ArrowLeft");
  await expect(count).toHaveAttribute("aria-label", at(slideNo("Turn Automation into Value")));
  await page.keyboard.press("ArrowUp");
  await expect(count).toHaveAttribute("aria-label", at(1));
  await page.keyboard.press("ArrowUp");
  await expect(count).toHaveAttribute("aria-label", at(1));
  // Page Up/Page Down always switch bands
  await page.keyboard.press("PageDown");
  await expect(count).toHaveAttribute("aria-label", at(2));
  await page.keyboard.press("PageDown");
  await expect(count).toHaveAttribute("aria-label", at(SECURITY));
  await page.keyboard.press("PageUp");
  await expect(count).toHaveAttribute("aria-label", at(2));
});

test("gives every navigation button a keyboard shortcut", async ({ page }) => {
  const count = page.locator(CURRENT);
  await page.goto("/gh-aw/cracktro/#3");
  for (const name of ["Previous band", "Previous slide", "Next slide", "Next band", "Slide map (G)", "Search slides (/)", "Toggle sound (M)"])
    await expect(page.getByRole("button", { name, exact: true })).toHaveAttribute("aria-keyshortcuts", /\S/);
  // every button (chrome, map tiles, header tiles, in-slide controls) has a title naming its shortcut
  const untitled = await page.locator("button:not(.vnb), [data-go]").evaluateAll(els => els.filter(el => !/\(.+\)/.test(el.getAttribute("title") || "")).map(el => el.className));
  expect(untitled).toEqual([]);
  // Shift+Down switches bands even on a column head
  await page.keyboard.press("Shift+ArrowDown");
  await expect(count).toHaveAttribute("aria-label", at(SECURITY));
  await page.keyboard.press("Shift+ArrowUp");
  await expect(count).toHaveAttribute("aria-label", at(2));
  // a typed slide number then Enter jumps there
  for (const k of [...String(HANDOFFS), "Enter"]) await page.keyboard.press(k);
  await expect(count).toHaveAttribute("aria-label", at(HANDOFFS));
  // U returns from any example to its pattern
  const head = slideNo("Daily Improvers");
  await page.goto(`/gh-aw/cracktro/#${head + 2}`);
  await page.keyboard.press("u");
  await expect(count).toHaveAttribute("aria-label", at(head));
  // P opens a slide's original prompt; Escape closes it
  await page.goto(`/gh-aw/cracktro/#${slideNo("Speech To Agent")}`);
  await page.keyboard.press("p");
  await expect(page.getByRole("dialog", { name: "Original prompt" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "Original prompt" })).toBeHidden();
});

test("hangs CI reuse below workflow anatomy", async ({ page }) => {
  const parent = slideNo("Anatomy of a workflow");
  const child = slideNo("Reuse your CI");
  const count = page.locator(CURRENT);
  await page.goto(`/gh-aw/cracktro/#${parent}`);
  await expect(page.getByRole("button", { name: "1 example below", exact: true })).toBeVisible();
  await page.keyboard.press("ArrowDown");
  await expect(count).toHaveAttribute("aria-label", at(child));
  await expect(page.locator(".slide.active").getByRole("heading").first()).toHaveText("Reuse your CI");
  await page.keyboard.press("ArrowUp");
  await expect(count).toHaveAttribute("aria-label", at(parent));
  await page.locator(".vnb").click();
  await expect(count).toHaveAttribute("aria-label", at(child));
  await page.getByRole("button", { name: "Back to the pattern", exact: true }).click();
  await expect(count).toHaveAttribute("aria-label", at(parent));
  await page.getByRole("button", { name: "Slide map (G)", exact: true }).click();
  const kids = page.getByRole("group", { name: "Anatomy of a workflow examples", exact: true });
  await expect(kids.getByRole("button", { name: new RegExp(`^Go to slide ${child}:`) })).toBeVisible();
});

test("keeps the CI reuse workflow inside its code panel", async ({ page }) => {
  await page.goto(`/gh-aw/cracktro/#${slideNo("Reuse your CI")}`);
  await page.evaluate(() => document.fonts.ready);
  const overflowing = await page.locator(".cwa-ln").evaluateAll(lines => lines.filter(line => line.scrollWidth > line.clientWidth).map(line => line.textContent));
  expect(overflowing).toEqual([]);
});

test("walks a pattern's column of examples with up/down and hints at it from the toolbar", async ({ page }) => {
  const count = page.locator(CURRENT);
  const vnb = page.locator(".vnb");
  const head = slideNo("Daily Improvers");
  const first = slideNo("Repo Assist");
  const last = slideNo("Go Fan");
  const n = last - head;
  await page.goto(`/gh-aw/cracktro/#${slideNo("Speech To Agent")}`);
  await expect(vnb).toBeHidden();
  // Right from an example of the previous column continues from its pattern to the next main-line slide
  await page.goto(`/gh-aw/cracktro/#${slideNo("Decide · Draft · Record")}`);
  await page.keyboard.press("ArrowRight");
  await expect(count).toHaveAttribute("aria-label", at(slideNo("Issues as intent")));
  await page.keyboard.press("ArrowRight");
  await expect(count).toHaveAttribute("aria-label", at(head));
  // the head lights a toolbar button hinting that a column of examples lies below
  await expect(page.getByRole("button", { name: `${n} examples below` })).toBeVisible();
  await expect(vnb).toHaveText(`${n}`);
  await page.keyboard.press("ArrowDown");
  await expect(count).toHaveAttribute("aria-label", at(first));
  await expect(page.locator(".slide.active").getByRole("heading").first()).toHaveText("Repo Assist");
  await expect(vnb).toHaveText(`1/${n}`);
  await page.keyboard.press("ArrowUp");
  await expect(count).toHaveAttribute("aria-label", at(head));
  // the toolbar button steps down the column; Right from an example rejoins the main line
  await vnb.click();
  await vnb.click();
  await expect(count).toHaveAttribute("aria-label", at(head + 2));
  await page.keyboard.press("ArrowRight");
  await expect(count).toHaveAttribute("aria-label", at(last + 1));
  await expect(vnb).toBeHidden();
  // Left from the main line skips the column back to its head
  await page.keyboard.press("ArrowLeft");
  await expect(count).toHaveAttribute("aria-label", at(head));
  // on the last example the button leads back up to the pattern
  await page.goto(`/gh-aw/cracktro/#${last}`);
  await page.getByRole("button", { name: "Back to the pattern" }).click();
  await expect(count).toHaveAttribute("aria-label", at(head));
  // past the bottom of the column, Down moves on to the next band (trust the process)
  await page.goto(`/gh-aw/cracktro/#${last}`);
  await expect(count).toHaveAttribute("aria-label", at(last));
  await page.keyboard.press("ArrowDown");
  await expect(count).toHaveAttribute("aria-label", at(slideNo("For PMs") + 1));
});

test("swaps the slide dots for arrows on narrow screens", async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 720 });
  await page.goto("/gh-aw/cracktro/#1");
  await expect(page.getByRole("button", { name: "Slide 1", exact: true })).toBeHidden();
  const prev = page.getByRole("button", { name: "Previous slide", exact: true });
  const next = page.getByRole("button", { name: "Next slide", exact: true });
  await expect(prev).toBeDisabled();
  await next.click();
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(2));
  // clicks in the gaps between the arrows do not fall through to the slide click handler
  const nb = (await page.getByRole("button", { name: "Next band", exact: true }).boundingBox())!;
  await page.mouse.click(nb.x - 3, nb.y + nb.height / 2);
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(2));
  await prev.click();
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(1));

  await page.setViewportSize({ width: 1280, height: 720 });
  await expect(page.getByRole("button", { name: "Slide 1", exact: true })).toBeVisible();
  await expect(next).toBeHidden();
  await expect(page.getByRole("button", { name: "Previous band", exact: true })).toBeHidden();
  await expect(page.getByRole("button", { name: "Next band", exact: true })).toBeHidden();
});

test("jumps between bands with the compact track buttons", async ({ page }) => {
  const count = page.locator(CURRENT);
  const prevBand = page.getByRole("button", { name: "Previous band", exact: true });
  const nextBand = page.getByRole("button", { name: "Next band", exact: true });
  await page.setViewportSize({ width: 320, height: 640 });
  await page.goto("/gh-aw/cracktro/#1");
  await expect(prevBand).toBeDisabled();
  await expect(nextBand).toBeEnabled();
  await nextBand.click();
  await expect(count).toHaveAttribute("aria-label", at(2));
  // from an interior slide, next band lands on the header below and previous band on the header above
  const interior = slideNo("Anatomy of a workflow");
  await page.goto(`/gh-aw/cracktro/#${interior}`);
  await expect(count).toHaveAttribute("aria-label", at(interior));
  await nextBand.click();
  await expect(count).toHaveAttribute("aria-label", at(SECURITY));
  await page.goto(`/gh-aw/cracktro/#${SECURITY + 2}`);
  await expect(count).toHaveAttribute("aria-label", at(SECURITY + 2));
  await prevBand.click();
  await expect(count).toHaveAttribute("aria-label", at(2));
  await prevBand.click();
  await expect(count).toHaveAttribute("aria-label", at(1));
  await expect(prevBand).toBeDisabled();
  await page.goto(`/gh-aw/cracktro/#${TOTAL}`);
  await expect(count).toHaveAttribute("aria-label", at(TOTAL));
  await expect(nextBand).toBeDisabled();
  await expect(page.getByRole("button", { name: "Next slide", exact: true })).toBeDisabled();
  await expect(prevBand).toBeEnabled();
  // keyboard activation of a focused button navigates exactly once
  await page.goto("/gh-aw/cracktro/#3");
  await expect(count).toHaveAttribute("aria-label", at(3));
  await nextBand.focus();
  await page.keyboard.press("Enter");
  await expect(count).toHaveAttribute("aria-label", at(SECURITY));

  // at 320px the four buttons, slide map, search and the sound toggle fit on one row without overlapping
  const boxes = await page.evaluate(() =>
    [...document.querySelectorAll(".arrows button"), ...[".smb", ".fzb", ".snd"].map(s => document.querySelector(s)!)].map(e => {
      const r = e.getBoundingClientRect();
      return { left: r.left, right: r.right };
    })
  );
  expect(boxes).toHaveLength(7);
  expect(boxes[0].left).toBeGreaterThanOrEqual(0);
  for (let i = 1; i < 7; i++) expect(boxes[i].left).toBeGreaterThan(boxes[i - 1].right);
  expect(boxes[6].right).toBeLessThanOrEqual(320);
  // kiosk and the column button sit on a second row above the dock, inside the screen and clear of it and each other
  await page.goto(`/gh-aw/cracktro/#${slideNo("Daily Improvers")}`);
  await expect(page.locator(".vnb")).toBeVisible();
  const raised = await page.evaluate(() =>
    [".ksb", ".vnb", ".arrows button", ".smb", ".fzb", ".snd"]
      .flatMap(s => [...document.querySelectorAll(s)])
      .map(e => {
        const r = e.getBoundingClientRect();
        return { left: r.left, right: r.right, top: r.top, bottom: r.bottom };
      })
  );
  const overlap = (a: (typeof raised)[0], b: (typeof raised)[0]) => a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
  for (const [i, box] of raised.slice(0, 2).entries()) {
    expect(box.left).toBeGreaterThanOrEqual(0);
    expect(box.right).toBeLessThanOrEqual(320);
    for (const other of raised.slice(i + 1)) expect(overlap(box, other)).toBe(false);
  }
});

test.describe("compact navigation on touch screens", () => {
  test.use({ hasTouch: true, viewport: { width: 320, height: 640 } });

  test("navigates once per tap and cancels the touch defaults", async ({ page }) => {
    const count = page.locator(CURRENT);
    await page.goto("/gh-aw/cracktro/#3");
    await expect(count).toHaveAttribute("aria-label", at(3));
    await page.getByRole("button", { name: "Next slide", exact: true }).tap();
    await expect(count).toHaveAttribute("aria-label", at(slideNo("Anatomy of a workflow")));
    await page.getByRole("button", { name: "Next band", exact: true }).tap();
    await expect(count).toHaveAttribute("aria-label", at(SECURITY));

    // rapid taps: every touchend is cancelled (no synthetic click, no double-tap zoom), never reaches
    // the window swipe handlers, and moves exactly one band per tap
    const result = await page.evaluate(() => {
      const btn = document.querySelector('[aria-label="Previous band"]') as HTMLButtonElement;
      const seen: string[] = [];
      for (const type of ["touchstart", "touchend"]) addEventListener(type, () => seen.push(type));
      const r = btn.getBoundingClientRect();
      const fire = (type: string) => {
        const t = new Touch({ identifier: 1, target: btn, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 });
        const e = new TouchEvent(type, { bubbles: true, cancelable: true, touches: type === "touchend" ? [] : [t], changedTouches: [t] });
        btn.dispatchEvent(e);
        return e.defaultPrevented;
      };
      const prevented: boolean[] = [];
      for (let i = 0; i < 2; i++) {
        fire("touchstart");
        prevented.push(fire("touchend"));
      }
      return { prevented, seen, count: document.querySelector('.dots button.on[aria-label^="Slide "]')!.getAttribute("aria-label") };
    });
    expect(result.prevented).toEqual([true, true]);
    expect(result.seen).toEqual([]);
    expect(result.count).toBe(at(1));
    await expect(count).toHaveAttribute("aria-label", at(1));
    await expect(page.locator(".arrows")).toHaveCSS("touch-action", "manipulation");
  });

  test("dims the idle toolbar without swallowing the first tap or hiding keyboard focus", async ({ page }) => {
    const count = page.locator(CURRENT);
    const arrows = page.locator(".arrows");
    const sound = page.locator(".snd");
    await page.goto("/gh-aw/cracktro/#3");
    await expect(count).toHaveAttribute("aria-label", at(3));
    await expect(arrows).toHaveCSS("opacity", "1");
    await expect(page.locator("body")).toHaveClass(/\bnav-idle\b/, { timeout: 5000 });
    await expect(arrows).toHaveCSS("opacity", "0.4");
    await expect(sound).toHaveCSS("opacity", "0.4");
    // the first tap on a dimmed button both wakes the toolbar and navigates, exactly once
    await page.getByRole("button", { name: "Next slide", exact: true }).tap();
    await expect(count).toHaveAttribute("aria-label", at(slideNo("Anatomy of a workflow")));
    await expect(page.locator("body")).not.toHaveClass(/\bnav-idle\b/);
    await expect(arrows).toHaveCSS("opacity", "1");
    // a keyboard-focused control stays fully readable while the rest of the toolbar dims again
    await page.keyboard.press("Shift");
    await page.getByRole("button", { name: "Next band", exact: true }).focus();
    await expect(page.locator("body")).toHaveClass(/\bnav-idle\b/, { timeout: 5000 });
    await expect(arrows).toHaveCSS("opacity", "1");
    await expect(sound).toHaveCSS("opacity", "0.4");
  });
});

test("draws edge to edge on phones and keeps the wide-screen dock opaque", async ({ page }) => {
  await page.goto("/gh-aw/cracktro/#1");
  await expect(page.locator('meta[name="viewport"]')).toHaveAttribute("content", /viewport-fit=cover/);
  await expect(page.locator('meta[name="apple-mobile-web-app-capable"]')).toHaveAttribute("content", "yes");
  await expect(page.locator('meta[name="apple-mobile-web-app-status-bar-style"]')).toHaveAttribute("content", "black-translucent");
  // the dim only applies to the compact (phone) toolbar
  await page.evaluate(() => document.body.classList.add("nav-idle"));
  await expect(page.locator(".snd")).toHaveCSS("opacity", "1");
});

test("starts the slide-change whoosh silent instead of with a full-scale bass burst", async ({ page }) => {
  await page.goto("/gh-aw/cracktro/#1");
  // render one slide change offline: a GainNode passes at full gain until its first scheduled event,
  // so an oscillator started before its envelope used to blast an unshaped burst that clicks phone speakers
  const level = await page.evaluate(async () => {
    const SR = 48000;
    const w = window as unknown as { AudioContext: unknown; sfx: { slide(dir: number, music?: string): void } };
    let ac: OfflineAudioContext | undefined;
    w.AudioContext = class extends OfflineAudioContext {
      constructor() {
        super(1, SR, SR);
        ac = this;
      }
      resume() {
        return Promise.resolve();
      }
    };
    w.sfx.slide(1);
    const d = (await ac!.startRendering()).getChannelData(0);
    let peak = 0,
      jump = 0,
      early = 0;
    for (let i = 1; i < d.length; i++) {
      const v = Math.abs(d[i]);
      peak = Math.max(peak, v);
      jump = Math.max(jump, Math.abs(d[i] - d[i - 1]));
      if (i >= 0.1 * SR && i < 0.135 * SR) early = Math.max(early, v);
    }
    return { peak, jump, early };
  });
  expect(level.peak).toBeGreaterThan(0.01);
  expect(level.early).toBeLessThan(0.02);
  expect(level.jump).toBeLessThan(0.05);
  expect(level.peak).toBeLessThan(0.3);
});

test("ships local assets with the docs and links to the talk from the footer", async ({ page, request }) => {
  await page.goto("/gh-aw/");
  await page.locator(".site-footer").getByRole("link", { name: "Interactive talk", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/gh-aw/cracktro/#${SLIDES[0].id}$`));

  const assets = ["runtime.js", "deck.js", "fonts/MonaspaceNeonVF.woff", "fonts/Monaspace-LICENSE.txt", "../fonts/MonaSansVF.woff2"];
  for (const asset of assets) {
    const response = await request.get(`/gh-aw/cracktro/${asset}`);
    expect(response.ok(), asset).toBe(true);
    expect((await response.body()).length, asset).toBeGreaterThan(0);
    expect(response.headers()["content-type"], asset).not.toContain("text/html");
  }
  await page.evaluate(() => document.fonts.ready);
  expect(await page.evaluate(() => document.fonts.check('16px "Monaspace Neon"'))).toBe(true);
});

test("publishes a social card for link previews", async ({ page, request }) => {
  await page.goto("/gh-aw/cracktro/");
  const image = "https://github.github.com/gh-aw/cracktro/og-image.png";
  await expect(page.locator('meta[property="og:image"]')).toHaveAttribute("content", image);
  await expect(page.locator('meta[name="twitter:image"]')).toHaveAttribute("content", image);
  await expect(page.locator('meta[name="twitter:card"]')).toHaveAttribute("content", "summary_large_image");
  const response = await request.get("/gh-aw/cracktro/og-image.png");
  expect(response.ok()).toBe(true);
  expect(response.headers()["content-type"]).toContain("image/png");
});

test("publishes synthetic examples without profile assets or private source references", async ({ page, request }) => {
  const allowedAvatar = /^assets\/avatars\/(?:mona\.jpg|ducky\.jpg|mona-full\.png)$/;
  for (const asset of readdirSync(new URL("../public/cracktro/assets/", import.meta.url), { recursive: true }) as string[]) {
    const normalized = asset.replaceAll("\\", "/");
    expect(normalized === "avatars" || allowedAvatar.test(`assets/${normalized}`), asset).toBe(true);
  }
  // index.html loads runtime.js before deck.js; check both, in load order.
  const source = (await Promise.all(["runtime.js", "deck.js"].map(async file => (await request.get(`/gh-aw/cracktro/${file}`)).text()))).join("\n");
  expect(source.match(/\bwho:\s|(?<![.\w])github(?:next)?\/(?!squad\b)[\w.-]+|\b[a-f0-9]{40}\b/i)).toBeNull();
  for (const ref of source.match(/assets\/avatars\/[^"'`\s)]*/g) ?? []) expect(ref).toMatch(allowedAvatar);
  expect(source.match(/https:\/\/[^"'\s]+/g)).toEqual(["https://github.github.com/gh-aw/introduction/overview/", "https://api.github.com/repositories/", "https://github.com/chrizbo/agentics-beyond-code"]);

  const externalRequests: string[] = [];
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.route("**/*", route => {
    if (new URL(route.request().url()).origin === "http://localhost:4321") return route.continue();
    externalRequests.push(route.request().url());
    return route.abort();
  });
  await page.clock.install();
  await page.goto(`/gh-aw/cracktro/#${slideNo("Mine · Refine · Apply")}`);
  await page.clock.runFor(15_000);
  for (const author of await page.locator(".mra-who").allTextContents()) {
    expect(author).toMatch(/^(?:linter miner|lintmonster|copilot) · /);
  }
  for (const href of await page.locator("svg image").evaluateAll(images => images.map(image => image.getAttribute("href")))) {
    expect(href).toMatch(allowedAvatar);
  }
  await expect(page.locator(".mra-repo-t")).toContainText("gh-aw · May–Nov 2026");
  await page.keyboard.press("n");
  for (const slide of SLIDES.flatMap((s, i) => (s.illustrative ? [i + 1] : []))) {
    await page.getByRole("button", { name: `Slide ${slide}`, exact: true }).click();
    await expect(page.locator(".notes")).toContainText(/illustrative|synthetic|simulated/i);
  }
  expect(errors).toEqual([]);
  expect(externalRequests).toEqual([]);
});

test("keeps role and avatar circles flat", async ({ page }) => {
  test.slow();
  await page.goto("/gh-aw/cracktro/");
  for (const index of SLIDES.keys()) {
    await page.getByRole("button", { name: `Slide ${index + 1}`, exact: true }).click();
    const shaded = await page.locator(".slide.active").evaluate(slide => [
      ...[...slide.querySelectorAll("circle")].flatMap(circle => {
        const id = getComputedStyle(circle).fill.match(/url\("?#([^")]+)/)?.[1];
        return id && slide.ownerDocument.getElementById(id)?.tagName === "radialGradient" ? [`circle → #${id}`] : [];
      }),
      ...[...slide.querySelectorAll<HTMLElement>("*")].flatMap(el => {
        const style = getComputedStyle(el);
        return style.borderRadius === "50%" && style.backgroundImage.includes("radial-gradient") ? [el.className.toString()] : [];
      }),
    ]);
    expect(shaded, SLIDES[index].heading).toEqual([]);
  }
});

test("keeps every key on the host side of the zero-secret agents diagram", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.goto(`/gh-aw/cracktro/#${slideNo("Zero-secret agents")}`);
  await expect(page.locator(".slide.active h2")).toContainText("Zero-secret agents");
  await expect(page.locator(".zs-vm-g .zs-key")).toHaveCount(0);
  await expect(page.locator(".zs-svg .zs-key")).toHaveCount(2);
  expect(errors).toEqual([]);
});

test.describe("agentic engineering slides", () => {
  const open = async (page: Page, heading: string, ms: number) => {
    const errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.clock.install({ time: 0 });
    await page.clock.pauseAt(1000);
    await page.goto(`/gh-aw/cracktro/#${slideNo(heading)}`);
    await expect(page.locator(".slide.active h2")).toHaveText(heading);
    // Slide timelines derive state from elapsed time, so jumping ahead is equivalent to waiting.
    await page.clock.runFor(500);
    await page.clock.fastForward(ms);
    await page.clock.runFor(200);
    return errors;
  };

  test("routes every gate but the last to skip before the agent runs", async ({ page }) => {
    const errors = await open(page, "Don’t Run the Agent", 19_000);
    await expect(page.locator(".dra-svg .dra-gate")).toHaveCount(7);
    for (const text of ["on: types", "if: !draft", "skip-if-match", "skip-if-no-match", "on.steps:", "jobs: filter:", "noop record", "skip-if-decision", "on.admission", "→ Cost management"]) {
      await expect(page.locator(".dra-svg")).toContainText(text);
    }
    await expect(page.locator(".dra-svg .dra-out.on")).toHaveCount(7);
    await expect(page.locator("#draE")).toHaveClass(/\bon\b/);
    await page.keyboard.press("n");
    await expect(page.locator(".notes")).toContainText("Any deny skips that invocation");
    await expect(page.locator(".notes")).toContainText("abstain as unknown");
    await expect(page.locator(".notes")).toContainText("Cost management");
    expect(errors).toEqual([]);
  });

  test("appends a validated ledger record from a trusted job", async ({ page }) => {
    const errors = await open(page, "Memory with a History", 12_000);
    await expect(page.locator("#mhlR4")).toHaveClass(/\bon\b/);
    await expect(page.locator(".mhl-svg")).toContainText("push_ledger_changes");
    expect(errors).toEqual([]);
  });

  test("chains three runs through dispatches and completion signals", async ({ page }) => {
    const errors = await open(page, "Agents Trigger Agents", 12_500);
    await expect(page.locator(".att-svg .att-run.done")).toHaveCount(3);
    await expect(page.locator(".att-svg")).toContainText("dispatch_workflow");
    await expect(page.locator(".att-svg")).toContainText("workflow_run");
    expect(errors).toEqual([]);
  });
});

test("places the enterprise tracing, access and event slides beside their neighbours", async ({ page }) => {
  expect(slideNo("Distributed tracing")).toBe(slideNo("OpenTelemetry") + 1);
  expect(slideNo("GitHub App permissions")).toBe(slideNo("Operator-owned infrastructure") + 1);
  expect(slideNo("Multi-repo PRs")).toBe(slideNo("GitHub App permissions") + 1);
  expect(slideNo("OIDC token broker")).toBe(slideNo("Multi-repo PRs") + 1);
  expect(slideNo("Webhook gateway")).toBe(slideNo("Connectors") + 1);
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  const NEW: [string, string | null][] = [
    ["Distributed tracing", null],
    ["GitHub App permissions", null],
    ["Multi-repo PRs", null],
    ["OIDC token broker", "Experimental"],
    ["Webhook gateway", "Experimental"],
  ];
  for (const [heading, badge] of NEW) {
    const n = slideNo(heading);
    await page.goto(`/gh-aw/cracktro/#${n}`);
    await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(n));
    const slide = page.locator(".slide.active");
    await expect(slide.locator("h2")).toHaveText(heading);
    await expect(slide.locator(".xsv")).toBeVisible();
    await expect(slide.locator(".xsv [data-s]").first()).toBeAttached();
    if (badge) await expect(slide.locator(".xbadge")).toContainText(badge);
    else await expect(slide.locator(".xbadge")).toHaveCount(0);
  }
  // Arrow navigation reaches the slide after each new one.
  await page.goto(`/gh-aw/cracktro/#${slideNo("Distributed tracing")}`);
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(slideNo("Distributed tracing")));
  await page.keyboard.press("ArrowRight");
  await expect(page.locator(".slide.active h2")).toHaveText("GitHub-native governance");
  expect(errors).toEqual([]);
});

test("labels exactly the slides that show experimental features", async ({ page }) => {
  // pkg/workflow/data/experimental_features.json is the single source of truth for experimental features.
  const { features } = JSON.parse(readFileSync(new URL("../../pkg/workflow/data/experimental_features.json", import.meta.url), "utf8")) as {
    features: { id: string; cracktro?: { slide: string; badge: string }[] }[];
  };
  const badges = new Map<string, string>();
  for (const feature of features) {
    for (const { slide, badge } of feature.cracktro ?? []) {
      const existing = badges.get(slide);
      expect(existing ?? badge, `${feature.id}: slide "${slide}" has conflicting badges`).toBe(badge);
      badges.set(slide, badge);
    }
  }
  const expected = [...badges.keys()].sort((a, b) => slideNo(a) - slideNo(b));
  await page.goto("/gh-aw/cracktro/");
  const labelled = await page.evaluate(() =>
    [...document.querySelectorAll("section.slide")]
      .filter(s => s.querySelector(":scope > .xbadge"))
      .map(s => ({
        heading: (s.querySelector("h2")?.textContent || "").replace(/\s+/g, " ").trim(),
        badge: (s.querySelector(":scope > .xbadge > span")?.textContent || "").trim(),
      }))
  );
  expect(labelled.map(l => l.heading)).toEqual(expected);
  for (const { heading, badge } of labelled) {
    expect(badge, heading).toBe(badges.get(heading));
    expect(SLIDES[slideNo(heading) - 1].summary, heading).toMatch(/experimental/i);
  }
});

test("keeps the frame loop running through the routed steps of the enterprise diagrams", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  for (const heading of ["Distributed tracing", "Webhook gateway"]) {
    await page.goto(`/gh-aw/cracktro/#${slideNo(heading)}`);
    await expect(page.locator(".slide.active h2")).toHaveText(heading);
    await expect(page.locator(".slide.active .xsv [data-s='7'].on").first()).toBeAttached({ timeout: 12_000 });
  }
  expect(errors).toEqual([]);
});

test("steps the cost guardrails slide from execution controls to admission", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.goto(`/gh-aw/cracktro/#${slideNo("Cost guardrails")}`);
  const slide = page.locator(".slide.active");
  await expect(slide.locator("h2")).toHaveText("Cost guardrails");
  await expect(slide.locator(".cg-node")).toHaveCount(5);
  await expect(slide.locator(".cg-status b")).toHaveText(/EVENTS|THROTTLE/);
  const expected = ["CHECKED|RUNNING", "NOTICE", "HARD STOP", "BLOCKED", "APPROVED", "DENIED"];
  for (const label of expected) {
    await page.keyboard.press("ArrowRight");
    await expect(slide.locator(".cg-status b")).toHaveText(new RegExp(label), { timeout: 6000 });
  }
  await expect(slide.locator(".cg-run em")).toHaveText("ALLOWANCE 250");
  await expect(slide.locator(".cg-mode.on b")).toHaveText("ADMIT");
  await page.keyboard.press("ArrowLeft");
  await expect(slide.locator(".cg-status b")).toHaveText("ADMISSION");
  await expect(page.locator(".slide.active h2")).toHaveText("Cost guardrails");
  expect(errors).toEqual([]);
});

test("steps the budget steering slide from the 80% notice to a submitted pull request", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.goto(`/gh-aw/cracktro/#${slideNo("Budget steering")}`);
  const slide = page.locator(".slide.active");
  await expect(slide.locator("h2")).toHaveText("Budget steering");
  await expect(slide.locator(".bst-lamp.on")).toHaveCount(0);
  await page.keyboard.press("ArrowRight");
  await expect(slide.locator(".bst-lamp.on")).toHaveCount(1);
  await expect(slide.locator(".bst-m.nt .big b")).toHaveText("402 / 500", { timeout: 8000 });
  await page.keyboard.press("ArrowRight");
  await expect(slide.locator(".bst-m.ok")).toContainText("create_pull_request", { timeout: 15000 });
  await expect(slide.locator(".bst-lamp.on")).toHaveCount(2);
  await page.keyboard.press("ArrowLeft");
  await expect(slide.locator(".bst-m.ok")).toHaveCount(0);
  await expect(page.locator(".slide.active h2")).toHaveText("Budget steering");
  expect(errors).toEqual([]);
});

test("splits inline skills and agents, defers context and preserves step navigation", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  const number = slideNo("Inlined Skills & Agents");
  await page.goto(`/gh-aw/cracktro/#${number}`);
  const slide = page.locator(".slide.isa");
  const diagram = slide.locator(".isa-svg");
  await expect(slide).toHaveClass(/active/);
  await expect(diagram).not.toHaveClass(/\bs1\b/);
  const measured = slide.getByRole("region", { name: "Measured inline skills and agents usage on gh-aw" });
  await expect(measured).toBeVisible();
  const layout = await slide.evaluate(el => {
    const diagram = el.querySelector(".isa-svg")!.getBoundingClientRect();
    const strip = el.querySelector(".isa-measured")!.getBoundingClientRect();
    const caption = el.querySelector(".isa-caption")!.getBoundingClientRect();
    return {
      separated: diagram.bottom <= strip.top,
      captionClear: strip.bottom <= caption.top,
      contained: caption.bottom <= el.getBoundingClientRect().bottom,
      labelsFit: [...el.querySelectorAll<HTMLElement>(".isa-example, .isa-inventory")].every(node => node.scrollWidth <= node.clientWidth),
    };
  });
  expect(layout).toEqual({ separated: true, captionClear: true, contained: true, labelsFit: true });

  await page.keyboard.press("ArrowRight");
  await expect(diagram).toHaveClass(/\bs1\b/);
  await expect(slide.locator(".isa-blk.skill").first()).toHaveAttribute("transform", /translate\(556\.0 /);
  await page.keyboard.press("ArrowRight");
  await expect(slide.locator(".isa-take")).toHaveClass(/on/, { timeout: 10_000 });
  await expect(slide.locator(".isa-ml.aft.sm.on")).toHaveCount(2);
  const context = await slide.evaluate(el => {
    const height = (selector: string) => el.querySelector<SVGGraphicsElement>(selector)!.getBBox().height;
    return {
      first: height("#isaPa0"),
      last: height("#isaPa5"),
      monolith: height("#isaPb5"),
    };
  });
  expect(context.first).toBeLessThan(context.last);
  expect(context.last).toBeLessThan(context.monolith);

  await page.keyboard.press("ArrowLeft");
  await expect(diagram).not.toHaveClass(/\bs2\b/);
  await expect(slide.locator(".isa-take")).not.toHaveClass(/on/);
  await page.keyboard.press("ArrowLeft");
  await expect(diagram).not.toHaveClass(/\bs1\b/);
  await expect(slide.locator(".isa-blk.core")).toHaveAttribute("transform", /translate\(36\.0 /);
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await expect(page.locator(".slide.active h2")).toHaveText("Audit & Optimize");
  await page.keyboard.press("ArrowLeft");
  await expect(slide).toHaveClass(/active/);
  await expect(slide.locator(".isa-take")).toHaveClass(/on/);
  expect(errors).toEqual([]);
});

test.describe("slides.md metadata", () => {
  const table = (...rows: string[]) => ["# Slides", "", "| Id | Heading | Notes | Summary | Keywords |", "|---|---|---|---|---|", ...rows, ""].join("\n");

  test("parses every row with a unique id, a unique, public summary and keywords", () => {
    expect(SLIDES.length).toBeGreaterThan(0);
    expect(parseSlides(table("| a | A |  | First. | one |", "| b-2 | B | illustrative | Second. | two words, CI/CD ,x |"))).toEqual([
      { id: "a", heading: "A", illustrative: false, summary: "First.", keywords: ["one"], line: 5 },
      { id: "b-2", heading: "B", illustrative: true, summary: "Second.", keywords: ["two words", "CI/CD", "x"], line: 6 },
    ]);
    for (const slide of SLIDES) {
      expect(`${slide.heading} ${slide.summary} ${slide.keywords.join(" ")}`, slide.heading).not.toMatch(PRIVATE_REFS);
      expect(`${slide.summary} ${slide.keywords.join(" ")}`, slide.heading).not.toMatch(/https?:|@[\w-]|#\d/);
    }
  });

  test("rejects malformed slide lists with the offending line", () => {
    const cases: [string, RegExp][] = [
      [table("| a | A | First. | k |"), /:5: expected 5 cells, found 4/],
      [table("| a | A |  | First. | k | extra |"), /:5: expected 5 cells, found 6/],
      [table("| a | A |  | First. | k"), /:5: malformed row/],
      [table("| A | A |  | First. | k |"), /:5: invalid Id "A" for "A"/],
      [table("|  | A |  | First. | k |"), /:5: invalid Id "" for "A"/],
      [table("| a | A |  | First. | k |", "| a | B |  | Second. | k |"), /:6: duplicate Id "a" \(first on line 5\)/],
      [table("| a |  |  | First. | k |"), /:5: empty Heading/],
      [table("| a | A |  |  | k |"), /:5: empty Summary for "A"/],
      [table("| a | A |  | First. |  |"), /:5: empty keyword for "A"/],
      [table("| a | A |  | First. | k,, l |"), /:5: empty keyword for "A"/],
      [table("| a | A |  | First. | Key, key |"), /:5: duplicate keyword "key" for "A"/],
      [table("| a | A | synthetic | First. | k |"), /:5: unknown Notes value "synthetic"/],
      [table("| a | A |  | First. | k |", "| b | A |  | Second. | k |"), /:6: duplicate Heading "A" \(first on line 5\)/],
      [table("| a | Human agent processes |  | First. | k |", "| b | Human Agent Processes |  | Second. | k |"), /:6: duplicate Heading "Human Agent Processes" \(first on line 5\)/],
      [table("| a | A |  | Same. | k |", "| b | B |  | Same. | k |"), /:6: duplicate Summary for "B" \(first on line 5\)/],
      [table("| a | A |  | First. | k |", "b | B | illustrative | Last slide. | k |"), /:6: malformed row, expected "\| Id \| Heading \| Notes \| Summary \| Keywords \|": b \| B \| illustrative \| Last slide\. \| k \|/],
      [table("| a | A |  | First. | k |", "", "| b | B |  | Second. | k |"), /:7: table row outside the slide table/],
      [table(), /:3: slide table has no rows/],
      ["| Heading | Notes | Summary | Keywords |\n|---|---|---|---|\n| A |  | First. | k |\n", /:1: expected table header/],
      ["| Id | Heading | Notes | Summary | Keywords |\n|---|---|---|---|\n", /:2: expected separator/],
      ["no table", /missing slide table/],
    ];
    for (const [text, error] of cases) expect(() => parseSlides(text, "slides.md"), String(error)).toThrow(error);
  });

  test("injects escaped, idempotent JSON-LD and leaves the rest of the page untouched", () => {
    const hostile = parseSlides(table("| a | A </script><script>alert(1)</script> |  | Uses $& and $1 & <b>tags</b>\u2028. | </script>, a&b |"));
    const html = injectSlideMetadata(SOURCE_HTML, hostile, PAGE_URL);
    expect(html.match(/<\/script>/gi)).toHaveLength(SOURCE_HTML.match(/<\/script>/gi)!.length);
    expect(html).not.toContain("<b>");
    const [doc] = jsonLd(html);
    expect(doc.hasPart).toEqual(slideParts(hostile, PAGE_URL));
    expect(doc.hasPart[0].description).toBe("Uses $& and $1 & <b>tags</b>\u2028.");
    expect(doc.hasPart[0].keywords).toBe("</script>, a&b");
    expect(serializeJsonLd({ s: "</script>&\u2029" })).toBe('{"s":"\\u003c/script\\u003e\\u0026\\u2029"}');

    const real = injectSlideMetadata(SOURCE_HTML, SLIDES, PAGE_URL);
    expect(injectSlideMetadata(real, SLIDES, PAGE_URL)).toBe(real);
    const [start, end] = [SOURCE_HTML.indexOf('<script type="application/ld+json">'), SOURCE_HTML.indexOf("</script>")];
    expect(real.slice(0, start)).toBe(SOURCE_HTML.slice(0, start));
    expect(real.slice(real.length - (SOURCE_HTML.length - end))).toBe(SOURCE_HTML.slice(end));
    const { hasPart, ...rest } = jsonLd(real)[0];
    expect(rest).toEqual(jsonLd(SOURCE_HTML)[0]);
    expect(hasPart).toHaveLength(SLIDES.length);
    // The hand-written source stays free of generated metadata; the build adds it.
    expect(jsonLd(SOURCE_HTML)[0].hasPart).toBeUndefined();

    expect(() => injectSlideMetadata(SOURCE_HTML, SLIDES, "https://example.com/talk/")).toThrow(/does not match/);
    expect(() => injectSlideMetadata("<head></head>", SLIDES, PAGE_URL)).toThrow(/found 0/);
    const block = SOURCE_HTML.slice(start, end + "</script>".length);
    expect(() => injectSlideMetadata(SOURCE_HTML.replace(block, block + block), SLIDES, PAGE_URL)).toThrow(/found 2/);
  });
});

test("serves the slide list in the talk page head without running the deck", async ({ request }) => {
  const response = await request.get("/gh-aw/cracktro/");
  expect(response.ok()).toBe(true);
  const html = await response.text();
  expect(html).toBe(injectSlideMetadata(SOURCE_HTML, SLIDES, PAGE_URL));
  const head = html.slice(0, html.indexOf("</head>"));
  const docs = jsonLd(head);
  expect(docs).toHaveLength(1);
  expect(docs[0].url).toBe(PAGE_URL);
  expect(docs[0].hasPart).toEqual(slideParts(SLIDES, PAGE_URL));
  expect(docs[0].hasPart.every((part: { description: string }) => !part.description.startsWith("Band header ("))).toBe(true);
  expect(head.match(/https:\/\/[^"\s]+/g)!.every(url => url.startsWith("https://schema.org") || url.startsWith("https://github.github.com/gh-aw/"))).toBe(true);
});

test("opens every slide from its metadata #id link", async ({ page, request }) => {
  test.slow();
  const [doc] = jsonLd(await (await request.get("/gh-aw/cracktro/")).text());
  await page.goto("/gh-aw/cracktro/");
  for (const part of doc.hasPart) {
    const hash = new URL(part.url).hash;
    await page.evaluate(h => (location.hash = h), hash);
    await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(part.position));
    await expect(page.locator(".slide.active").getByRole("heading").first()).toHaveText(part.name);
    await expect(page).toHaveURL(new RegExp(`${hash}$`));
  }
});

test("names every slide with its slides.md id and rewrites legacy #N links", async ({ page }) => {
  await page.goto("/gh-aw/cracktro/");
  const ids = await page.locator(".dots button").evaluateAll(dots =>
    dots.map(dot => {
      (dot as HTMLElement).click();
      return location.hash.slice(1);
    })
  );
  expect(ids).toEqual(SLIDES.map(s => s.id));
  await page.goto(`/gh-aw/cracktro/#${SECURITY}`);
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(SECURITY));
  await expect(page).toHaveURL(new RegExp(`#${slideId("Security")}$`));
  await page.goto("/gh-aw/cracktro/#no-such-slide");
  await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(1));
});

test("opens every hard-coded tour from the hash with all its slides found", async ({ page }) => {
  const warnings: string[] = [];
  page.on("console", msg => msg.type() === "warning" && warnings.push(msg.text()));
  await page.goto("/gh-aw/cracktro/#finale&tour=overview");
  await expect(page.locator(".tourp")).toContainText("Overview");
  await expect(page.locator(".tourp-n")).toHaveText(/^(\d+)\/\1$/);
  await page.keyboard.press("g");
  await expect(page.getByRole("group", { name: "Tours" }).getByRole("button")).toHaveCount(7);
  expect(warnings.filter(w => w.includes("CracktroDeck"))).toEqual([]);
});

test("keeps an example in a view only together with its pattern slide", async ({ page }) => {
  const view = (s: string[]) => Buffer.from(JSON.stringify({ s })).toString("base64url");
  await page.goto(`/gh-aw/cracktro/#github-agentic-workflows&view=${view(["github-agentic-workflows", "repo-assist"])}`);
  await expect(page.locator(".tourp-n")).toHaveText("1/1");
  await page.evaluate(h => (location.hash = h), `#daily-improvers&view=${view(["daily-improvers", "repo-assist"])}`);
  await expect(page.locator(".tourp-n")).toHaveText("1/2");
  await page.keyboard.press("ArrowDown");
  await expect(page).toHaveURL(/#repo-assist&view=/);

  // in design mode, unpicking a pattern drops its examples and disables their tiles
  await page.keyboard.press("g");
  await page.getByRole("button", { name: /^Design a view/ }).click();
  const pattern = page.getByRole("button", { name: /: Daily Improvers$/ }),
    example = page.getByRole("button", { name: /: Repo Assist$/ });
  await expect(example).toHaveAttribute("aria-pressed", "true");
  await pattern.click();
  await expect(example).toHaveAttribute("aria-pressed", "false");
  await expect(example).toHaveAttribute("aria-disabled", "true");
  await example.click();
  await expect(example).toHaveAttribute("aria-pressed", "false");
  await expect(page.getByRole("group", { name: "Design a view" })).toContainText(`0 of ${SLIDES.length} slides`);
  await pattern.click();
  await expect(example).not.toHaveAttribute("aria-disabled");
});

// A tiny second deck on runtime.js alone: no talk content, themes, solo labels or sound service.
const TINY_DECK = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Tiny deck</title></head><body>
<script src="./runtime.js"></script>
<script>
  window.hooks = [];
  let steps = 0;
  const query = new URLSearchParams(location.search);
  const last = query.get("last");
  const examples = query.has("examples");
  window.CracktroDeck.create({
    slides: ({ h, letters, current }) => {
      const slide = (group, title, extra = {}) => ({ group, notes: title + " notes", el: h("section", { class: "slide" }, h("h2", {}, letters(title))), ...extra });
      return [
        slide("intro", "Hello", {
          enter: d => hooks.push("enter:" + d),
          onNext: () => steps++ < 1 && hooks.push("step") > 0,
          onKey: e => e.key === "x" && hooks.push("key:" + current().notes) > 0,
        }),
        slide("outro", "Goodbye"),
        slide("part", "Alpha", examples ? { key: "alpha" } : {}),
        ...(examples ? [slide("part", "Alpha example", { under: "alpha" })] : []),
        slide("part", "Beta"),
      ];
    },
    bands: { part: ["Part one", "The only band"] },
    tours: { ends: { title: "Both ends", summary: "first and last", slides: ["hello", "goodbye"] }, part: { title: "Just the part", bands: ["part"] }, ghost: { slides: ["nope"] } },
    last: last === "none" ? null : last || "outro",
    kioskInterval: +query.get("kiosk") || undefined,
  });
</script></body></html>`;

test.describe("reusable runtime", () => {
  const open = async (page: Page, path: string) => {
    const requests: string[] = [];
    const errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    page.on("request", request => requests.push(new URL(request.url()).pathname));
    await page.route("**/gh-aw/cracktro/tiny.html*", route => route.fulfill({ contentType: "text/html", body: TINY_DECK }));
    await page.goto(path);
    return { requests, errors };
  };
  const heading = (page: Page) => page.locator(".slide.active h2");

  test("runs a second deck without the talk's content, theme or sound", async ({ page }) => {
    const { requests, errors } = await open(page, "/gh-aw/cracktro/tiny.html#2");
    await expect(page.locator(".slide")).toHaveCount(5);
    await expect(page.locator(CURRENT)).toHaveAttribute("aria-label", at(2));
    await expect(heading(page)).toHaveText("Part one");
    await expect(page.locator(".snd")).toHaveCount(0);
    await expect(page.locator(".stage")).not.toContainText(/agentic|github/i);
    expect(await page.locator(".dots .drow").evaluateAll(rows => rows.map(r => r.getAttribute("title")))).toEqual(["intro", "Part one", "outro"]);
    expect(requests.filter(p => /\/(?:deck|ops-visuals)\.js$/.test(p))).toEqual([]);
    expect(await page.evaluate(() => typeof (window as unknown as { sfx?: unknown }).sfx)).toBe("undefined");

    await page.keyboard.press("ArrowRight");
    await expect(page).toHaveURL(/#alpha$/);
    await expect(heading(page)).toHaveText("Alpha");
    await page.keyboard.press("End");
    await expect(page).toHaveURL(/#goodbye$/);
    await expect(heading(page)).toHaveText("Goodbye");
    await page.keyboard.press("m");
    await expect(page).toHaveURL(/#goodbye$/);
    expect(errors).toEqual([]);
  });

  test("calls slide hooks and follows the URL hash", async ({ page }) => {
    const { errors } = await open(page, "/gh-aw/cracktro/tiny.html");
    await expect(heading(page)).toHaveText("Hello");
    await page.keyboard.press("x");
    await page.keyboard.press("ArrowRight");
    await expect(page).toHaveURL(/#hello$/);
    expect(await page.evaluate(() => (window as unknown as { hooks: string[] }).hooks)).toEqual(["enter:1", "key:Hello notes", "step"]);
    await page.keyboard.press("ArrowRight");
    await expect(page).toHaveURL(/#part-one$/);
    await page.evaluate(() => (location.hash = "#beta"));
    await expect(heading(page)).toHaveText("Beta");
    await page.keyboard.press("PageUp");
    await expect(heading(page)).toHaveText("Hello");
    await page.keyboard.press("PageDown");
    await expect(heading(page)).toHaveText("Part one");
    await page.keyboard.press("Home");
    await expect(heading(page)).toHaveText("Hello");
    expect(errors).toEqual([]);
  });

  test("keeps rows in first-appearance order when the last group is absent", async ({ page }) => {
    for (const last of ["none", "finale"]) {
      const { errors } = await open(page, `/gh-aw/cracktro/tiny.html?last=${last}#2`);
      await expect(heading(page)).toHaveText("Goodbye");
      await page.keyboard.press("End");
      await expect(heading(page)).toHaveText("Beta");
      expect(errors).toEqual([]);
    }
  });

  test("walks a tour from the hash and leaves it on a jump outside", async ({ page }) => {
    const { errors } = await open(page, "/gh-aw/cracktro/tiny.html#goodbye&tour=ends");
    await expect(heading(page)).toHaveText("Goodbye");
    await expect(page.locator(".tourp")).toBeVisible();
    await expect(page.locator(".tourp")).toContainText("Both ends");
    await expect(page.locator(".tourp")).toContainText("2/2");
    await expect(page.locator(".dots button:visible")).toHaveCount(2);
    await page.keyboard.press("ArrowLeft");
    await expect(heading(page)).toHaveText("Hello");
    await expect(page).toHaveURL(/#hello&tour=ends$/);
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("ArrowRight");
    await expect(heading(page)).toHaveText("Goodbye");
    await page.keyboard.press("Home");
    await expect(heading(page)).toHaveText("Hello");
    await page.keyboard.press("PageDown");
    await expect(heading(page)).toHaveText("Goodbye");

    // a hash slide outside the tour opens the tour's first slide; an unknown or empty tour shows every slide
    await page.evaluate(() => (location.hash = "#alpha&tour=part"));
    await expect(heading(page)).toHaveText("Alpha");
    await expect(page.locator(".tourp")).toContainText("Just the part");
    await page.evaluate(() => (location.hash = "#hello&tour=part"));
    await expect(heading(page)).toHaveText("Part one");
    await expect(page).toHaveURL(/#part-one&tour=part$/);
    await page.evaluate(() => (location.hash = "#alpha&tour=ghost"));
    await expect(page).toHaveURL(/#alpha$/);
    await expect(page.locator(".tourp")).toBeHidden();

    // jumping to a slide outside the tour (map, search, header tile) leaves it
    await page.evaluate(() => (location.hash = "#alpha&tour=ends"));
    await expect(heading(page)).toHaveText("Hello");
    await page.getByRole("button", { name: "Slide 3", exact: true }).evaluate(b => (b as HTMLElement).click());
    await expect(heading(page)).toHaveText("Alpha");
    await expect(page).toHaveURL(/#alpha$/);
    await expect(page.locator(".tourp")).toBeHidden();
    expect(errors).toEqual([]);
  });

  test("picks tours from the bottom of the slide map", async ({ page }) => {
    const { errors } = await open(page, "/gh-aw/cracktro/tiny.html#beta");
    await page.keyboard.press("g");
    const tours = page.getByRole("group", { name: "Tours" });
    await expect(tours.getByRole("button")).toHaveText([/^All slides\s*5 slides$/, /^Both ends\s*2 slides · first and last$/, /^Just the part\s*3 slides$/, /^Design a view/]);
    await expect(tours.getByRole("button", { name: /^All slides/ })).toHaveAttribute("aria-pressed", "true");
    await tours.getByRole("button", { name: /^Both ends/ }).click();
    await expect(page.locator(".smp")).toBeHidden();
    await expect(heading(page)).toHaveText("Hello");
    await expect(page).toHaveURL(/#hello&tour=ends$/);
    await page.keyboard.press("g");
    await expect(page.locator(".smp-t.out")).toHaveCount(3);
    await tours.getByRole("button", { name: /^All slides/ }).click();
    await expect(page).toHaveURL(/#hello$/);
    await expect(page.locator(".tourp")).toBeHidden();
    // X leaves a tour from the keyboard
    await page.evaluate(() => (location.hash = "#goodbye&tour=ends"));
    await expect(page.locator(".tourp")).toBeVisible();
    await page.keyboard.press("x");
    await expect(page).toHaveURL(/#goodbye$/);
    await expect(page.locator(".tourp")).toBeHidden();
    expect(errors).toEqual([]);
  });

  test("designs a custom view from slides and whole bands and shares it as a link", async ({ page }) => {
    const { errors } = await open(page, "/gh-aw/cracktro/tiny.html#hello");
    await page.keyboard.press("g");
    await page.getByRole("button", { name: /^Design a view/ }).click();
    const bar = page.getByRole("group", { name: "Design a view" });
    await expect(bar).toBeVisible();
    await expect(bar).toContainText("0 of 5 slides");
    await expect(bar.getByRole("button", { name: "Apply view" })).toBeDisabled();
    await page.keyboard.press("a");
    await expect(bar).toContainText("5 of 5 slides");
    await page.keyboard.press("c");
    await expect(bar).toContainText("0 of 5 slides");
    // a tile toggles instead of navigating; a band checkbox takes the whole band
    await page.getByRole("button", { name: /^Go to slide 5:/ }).click();
    await expect(page.getByRole("button", { name: /^Go to slide 5:/ })).toHaveAttribute("aria-pressed", "true");
    await page.getByRole("checkbox", { name: "Include the whole Part one band" }).check();
    await expect(bar).toContainText("4 of 5 slides");
    await page.getByRole("button", { name: /^Go to slide 4:/ }).click();
    await expect(page.getByRole("checkbox", { name: "Include the whole Part one band" })).toHaveJSProperty("indeterminate", true);
    await expect(bar).toContainText("3 of 5 slides");
    await bar.getByRole("textbox", { name: "View title" }).fill("Part and outro");
    const link = await bar.getByRole("textbox", { name: "Link to this view" }).inputValue();
    const payload = link.match(/#part-one&view=([\w-]+)$/)![1];
    // whole bands travel by group (slides added to them later join the view), the rest by slide id
    expect(JSON.parse(Buffer.from(payload, "base64url").toString())).toEqual({ t: "Part and outro", b: ["outro"], s: ["part-one", "alpha"] });

    await bar.getByRole("button", { name: "Apply view" }).click();
    await expect(page.locator(".smp")).toBeHidden();
    await expect(heading(page)).toHaveText("Part one");
    await expect(page.locator(".tourp")).toContainText("Part and outro");
    await page.keyboard.press("End");
    await expect(heading(page)).toHaveText("Goodbye");
    await expect(page).toHaveURL(new RegExp(`#goodbye&view=${payload}$`));

    // the shared link reopens the same view in a fresh page
    await page.goto(link);
    await expect(heading(page)).toHaveText("Part one");
    await expect(page.locator(".tourp")).toContainText("Part and outro");
    await page.keyboard.press("ArrowLeft");
    await expect(heading(page)).toHaveText("Part one");
    await page.getByRole("button", { name: "Leave the tour and show all slides" }).click();
    await expect(page).toHaveURL(/#part-one$/);
    await page.keyboard.press("ArrowLeft");
    await expect(heading(page)).toHaveText("Hello");
    expect(errors).toEqual([]);
  });

  // A fake Fullscreen API (real fullscreen is unreliable headless): ?fs=grant enters, ?fs=deny rejects, ?fs=none
  // has no API (iPhone Safari); window.fsCalls records requests and exits. Run before goto, like page.clock.install.
  const fakeFullscreen = (page: Page) =>
    page.addInitScript(() => {
      const mode = new URLSearchParams(location.search).get("fs");
      if (!mode) return;
      const w = window as unknown as { fsCalls: string[] };
      w.fsCalls = [];
      let full: Element | null = null;
      const change = () => document.dispatchEvent(new Event("fullscreenchange"));
      for (const k of ["fullscreenElement", "webkitFullscreenElement"]) Object.defineProperty(Document.prototype, k, { configurable: true, get: () => full });
      const request =
        mode === "none"
          ? undefined
          : function (this: Element) {
              w.fsCalls.push("request");
              if (mode === "deny") return Promise.reject(new TypeError("Permissions check failed"));
              full = this;
              change();
              return Promise.resolve();
            };
      for (const k of ["requestFullscreen", "webkitRequestFullscreen"]) Object.defineProperty(Element.prototype, k, { configurable: true, writable: true, value: request });
      Document.prototype.exitFullscreen = function () {
        w.fsCalls.push("exit");
        full = null;
        change();
        return Promise.resolve();
      };
    });
  const fsCalls = (page: Page) => page.evaluate(() => (window as unknown as { fsCalls: string[] }).fsCalls);
  const KIOSK_MS = 15_000;

  test("leaves the deck on manual navigation unless kiosk mode is started", async ({ page }) => {
    await page.clock.install();
    const { errors } = await open(page, "/gh-aw/cracktro/tiny.html#hello");
    await expect(heading(page)).toHaveText("Hello");
    await page.clock.runFor(4 * KIOSK_MS);
    await expect(heading(page)).toHaveText("Hello");
    await expect(page.locator("body")).not.toHaveClass(/\bkiosk\b/);
    const kiosk = page.locator(".ksb");
    await expect(kiosk).toHaveAttribute("aria-pressed", "false");
    await expect(kiosk).toHaveAttribute("title", "Start kiosk mode: rotate slides in fullscreen (K)");
    await expect(kiosk).toHaveAttribute("aria-keyshortcuts", "K");
    expect(errors).toEqual([]);
  });

  test("rotates every slide in fullscreen kiosk mode, examples included, wrapping to the first", async ({ page }) => {
    await page.clock.install();
    await fakeFullscreen(page);
    const { errors } = await open(page, "/gh-aw/cracktro/tiny.html?examples&fs=grant#hello");
    await expect(heading(page)).toHaveText("Hello");
    // a held K (auto-repeat) starts kiosk mode once instead of toggling it on and off
    await page.keyboard.down("k");
    await page.keyboard.down("k");
    await page.keyboard.up("k");
    const kiosk = page.getByRole("button", { name: "Stop kiosk mode (K or Esc)" });
    await expect(kiosk).toHaveAttribute("aria-pressed", "true");
    await expect(page.locator("body")).toHaveClass(/\bkiosk\b/);
    expect(await fsCalls(page)).toEqual(["request"]);
    // the presentation chrome hides; only the stop button stays
    for (const chrome of [".dots", ".progress", ".fzb", ".smb"]) await expect(page.locator(chrome)).toBeHidden();
    await expect(kiosk).toBeVisible();

    await page.clock.runFor(KIOSK_MS - 1000);
    await expect(heading(page)).toHaveText("Hello");
    // Hello's onNext step does not hold the rotation; the column's example is shown after its pattern
    for (const title of ["Part one", "Alpha", "Alpha example", "Beta", "Goodbye", "Hello"]) {
      await page.clock.runFor(title === "Part one" ? 1000 : KIOSK_MS);
      await expect(heading(page)).toHaveText(title);
    }

    // a manual move restarts the countdown
    await page.keyboard.press("PageDown");
    await expect(heading(page)).toHaveText("Part one");
    await page.clock.runFor(KIOSK_MS - 1000);
    await expect(heading(page)).toHaveText("Part one");
    await page.clock.runFor(1000);
    await expect(heading(page)).toHaveText("Alpha");

    // the rotation holds while a dialog is open; Escape closes the dialog without stopping kiosk mode
    await page.keyboard.press("g");
    await expect(page.locator(".smp")).toBeVisible();
    await page.clock.runFor(2 * KIOSK_MS);
    await page.keyboard.press("Escape");
    await expect(page.locator(".smp")).toBeHidden();
    await expect(heading(page)).toHaveText("Alpha");
    await expect(page.locator("body")).toHaveClass(/\bkiosk\b/);

    // the button stops it, leaves the fullscreen it entered, restores the chrome and leaves no timer behind
    await kiosk.click();
    await expect(page.getByRole("button", { name: "Start kiosk mode: rotate slides in fullscreen (K)" })).toHaveAttribute("aria-pressed", "false");
    await expect(page.locator("body")).not.toHaveClass(/\bkiosk\b/);
    expect(await fsCalls(page)).toEqual(["request", "exit"]);
    await expect(page.locator(".dots")).toBeVisible();
    await page.clock.runFor(4 * KIOSK_MS);
    await expect(heading(page)).toHaveText("Alpha");
    expect(errors).toEqual([]);
  });

  test("rotates only a tour's slides and stops when fullscreen ends", async ({ page }) => {
    await page.clock.install();
    await fakeFullscreen(page);
    const { errors } = await open(page, "/gh-aw/cracktro/tiny.html?examples&fs=grant#beta&tour=part");
    await expect(heading(page)).toHaveText("Beta");
    await page.keyboard.press("k");
    await expect(page.locator("body")).toHaveClass(/\bkiosk\b/);
    await expect(page.locator(".tourp")).toBeHidden();
    for (const title of ["Part one", "Alpha", "Alpha example", "Beta", "Part one"]) {
      await page.clock.runFor(KIOSK_MS);
      await expect(heading(page)).toHaveText(title);
    }
    await expect(page).toHaveURL(/#part-one&tour=part$/);

    // Escape in real fullscreen never reaches the page: the browser leaves fullscreen, which ends kiosk mode
    await page.evaluate(() => document.exitFullscreen());
    await expect(page.locator("body")).not.toHaveClass(/\bkiosk\b/);
    await expect(page.locator(".tourp")).toBeVisible();
    await page.clock.runFor(4 * KIOSK_MS);
    await expect(heading(page)).toHaveText("Part one");
    expect(await fsCalls(page)).toEqual(["request", "exit"]);
    expect(errors).toEqual([]);
  });

  test("rotates in the window when fullscreen is refused or missing and stops on Escape", async ({ page }) => {
    await page.clock.install();
    await fakeFullscreen(page);
    for (const fs of ["deny", "none"]) {
      const { errors } = await open(page, `/gh-aw/cracktro/tiny.html?fs=${fs}#hello`);
      await expect(heading(page)).toHaveText("Hello");
      await page.getByRole("button", { name: "Start kiosk mode: rotate slides in fullscreen (K)" }).click();
      await expect(page.locator("body")).toHaveClass(/\bkiosk\b/);
      expect(await fsCalls(page)).toEqual(fs === "deny" ? ["request"] : []);
      await page.clock.runFor(KIOSK_MS);
      await expect(heading(page)).toHaveText("Part one");
      await expect(page.locator("body")).toHaveClass(/\bkiosk\b/);
      await page.keyboard.press("Escape");
      await expect(page.locator("body")).not.toHaveClass(/\bkiosk\b/);
      await expect(page.locator(".ksb")).toHaveAttribute("aria-pressed", "false");
      await page.clock.runFor(4 * KIOSK_MS);
      await expect(heading(page)).toHaveText("Part one");
      expect(await fsCalls(page)).toEqual(fs === "deny" ? ["request"] : []);
      expect(errors).toEqual([]);
    }
  });
});
