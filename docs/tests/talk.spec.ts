import { test, expect } from "@playwright/test";
import { readFileSync, readdirSync } from "node:fs";
import { injectSlideMetadata, loadSlides, parseSlides, serializeJsonLd, slideParts } from "../src/lib/cracktro/slides.js";

// The ordered slide list lives in .github/skills/cracktro/slides.md; a slide's number is its row position.
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
const at = (n: number) => `${String(n).padStart(2, "0")} / ${TOTAL}`;
const SECURITY = slideNo("Security");

test.use({ reducedMotion: "reduce" });

test("presents the slides listed in slides.md in bands without the removed sections", async ({ page }) => {
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
    await expect(page.locator(".count")).toHaveText(at(index + 1));
    await expect(page.locator(".slide.active").getByRole("heading").first()).toHaveText(heading);
    await page.evaluate(() => new Promise(requestAnimationFrame));
  }
  expect(errors).toEqual([]);
});

test("preserves navigation, slide links, speaker notes, and sound controls", async ({ page }) => {
  await page.goto("/gh-aw/cracktro/#16");
  await expect(page.locator(".count")).toHaveText(at(16));
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

  await page.keyboard.press("Space");
  await expect(page).toHaveURL(/\/gh-aw\/cracktro\/#17$/);
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator(".count")).toHaveText(at(16));
  await page.reload();
  await expect(page.locator(".count")).toHaveText(at(16));
  await page.keyboard.press("Home");
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator(".count")).toHaveText(at(1));
  await page.keyboard.press("End");
  await page.keyboard.press("ArrowRight");
  await expect(page.locator(".count")).toHaveText(at(TOTAL));
});

test("moves through the bands, up/down landing on each band's first slide", async ({ page }) => {
  const count = page.locator(".count");
  await page.goto("/gh-aw/cracktro/#1");
  await page.keyboard.press("ArrowDown");
  await expect(count).toHaveText(at(2));
  await expect(page.locator(".slide.active").getByRole("heading").first()).toHaveText("Agentic Workflows");
  await page.keyboard.press("ArrowRight");
  await expect(count).toHaveText(at(3));
  await page.keyboard.press("ArrowDown");
  await expect(count).toHaveText(at(SECURITY));
  // left from a band header wraps to the last slide of the band above
  await page.keyboard.press("ArrowLeft");
  await expect(count).toHaveText(at(SECURITY - 1));
  await page.keyboard.press("ArrowUp");
  await expect(count).toHaveText(at(1));
  await page.keyboard.press("ArrowUp");
  await expect(count).toHaveText(at(1));
});

test("swaps the slide dots for arrows on narrow screens", async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 720 });
  await page.goto("/gh-aw/cracktro/#1");
  await expect(page.getByRole("button", { name: "Slide 1", exact: true })).toBeHidden();
  const prev = page.getByRole("button", { name: "Previous slide", exact: true });
  const next = page.getByRole("button", { name: "Next slide", exact: true });
  await expect(prev).toBeDisabled();
  await next.click();
  await expect(page.locator(".count")).toHaveText(at(2));
  // clicks in the gaps between the arrows do not fall through to the slide click handler
  const nb = (await page.getByRole("button", { name: "Next band", exact: true }).boundingBox())!;
  await page.mouse.click(nb.x - 3, nb.y + nb.height / 2);
  await expect(page.locator(".count")).toHaveText(at(2));
  await prev.click();
  await expect(page.locator(".count")).toHaveText(at(1));

  await page.setViewportSize({ width: 1280, height: 720 });
  await expect(page.getByRole("button", { name: "Slide 1", exact: true })).toBeVisible();
  await expect(next).toBeHidden();
  await expect(page.getByRole("button", { name: "Previous band", exact: true })).toBeHidden();
  await expect(page.getByRole("button", { name: "Next band", exact: true })).toBeHidden();
});

test("jumps between bands with the compact track buttons", async ({ page }) => {
  const count = page.locator(".count");
  const prevBand = page.getByRole("button", { name: "Previous band", exact: true });
  const nextBand = page.getByRole("button", { name: "Next band", exact: true });
  await page.setViewportSize({ width: 320, height: 640 });
  await page.goto("/gh-aw/cracktro/#1");
  await expect(prevBand).toBeDisabled();
  await expect(nextBand).toBeEnabled();
  await nextBand.click();
  await expect(count).toHaveText(at(2));
  // from an interior slide, next band lands on the header below and previous band on the header above
  await page.goto("/gh-aw/cracktro/#3");
  await expect(count).toHaveText(at(3));
  await nextBand.click();
  await expect(count).toHaveText(at(SECURITY));
  await page.goto(`/gh-aw/cracktro/#${SECURITY + 2}`);
  await expect(count).toHaveText(at(SECURITY + 2));
  await prevBand.click();
  await expect(count).toHaveText(at(2));
  await prevBand.click();
  await expect(count).toHaveText(at(1));
  await expect(prevBand).toBeDisabled();
  await page.goto(`/gh-aw/cracktro/#${TOTAL}`);
  await expect(count).toHaveText(at(TOTAL));
  await expect(nextBand).toBeDisabled();
  await expect(page.getByRole("button", { name: "Next slide", exact: true })).toBeDisabled();
  await expect(prevBand).toBeEnabled();
  // keyboard activation of a focused button navigates exactly once
  await page.goto("/gh-aw/cracktro/#3");
  await expect(count).toHaveText(at(3));
  await nextBand.focus();
  await page.keyboard.press("Enter");
  await expect(count).toHaveText(at(SECURITY));

  // at 320px the four buttons fit left of the sound toggle, which stays clear of the count
  const boxes = await page.evaluate(() =>
    [...document.querySelectorAll(".arrows button"), document.querySelector(".snd")!, document.querySelector(".count")!].map(e => {
      const r = e.getBoundingClientRect();
      return { left: r.left, right: r.right };
    })
  );
  expect(boxes).toHaveLength(6);
  const [, , , lastArrow, snd, countBox] = boxes;
  expect(boxes[0].left).toBeGreaterThanOrEqual(0);
  for (let i = 1; i < 4; i++) expect(boxes[i].left).toBeGreaterThan(boxes[i - 1].right);
  expect(snd.left).toBeGreaterThan(lastArrow.right);
  expect(countBox.left).toBeGreaterThan(snd.right);
  expect(countBox.right).toBeLessThanOrEqual(320);
});

test.describe("compact navigation on touch screens", () => {
  test.use({ hasTouch: true, viewport: { width: 320, height: 640 } });

  test("navigates once per tap and cancels the touch defaults", async ({ page }) => {
    const count = page.locator(".count");
    await page.goto("/gh-aw/cracktro/#3");
    await expect(count).toHaveText(at(3));
    await page.getByRole("button", { name: "Next slide", exact: true }).tap();
    await expect(count).toHaveText(at(4));
    await page.getByRole("button", { name: "Next band", exact: true }).tap();
    await expect(count).toHaveText(at(SECURITY));

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
      return { prevented, seen, count: document.querySelector(".count")!.textContent };
    });
    expect(result.prevented).toEqual([true, true]);
    expect(result.seen).toEqual([]);
    expect(result.count).toBe(at(1));
    await expect(count).toHaveText(at(1));
    await expect(page.locator(".arrows")).toHaveCSS("touch-action", "manipulation");
  });
});

test("ships local assets with the docs and links to the talk from the footer", async ({ page, request }) => {
  await page.goto("/gh-aw/");
  await page.locator(".site-footer").getByRole("link", { name: "Interactive talk", exact: true }).click();
  await expect(page).toHaveURL(/\/gh-aw\/cracktro\/#1$/);

  const assets = ["deck.js", "fonts/MonaspaceNeonVF.woff", "fonts/Monaspace-LICENSE.txt", "../fonts/MonaSansVF.woff2"];
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
    expect(asset === "avatars" || allowedAvatar.test(`assets/${asset}`), asset).toBe(true);
  }
  const source = await (await request.get("/gh-aw/cracktro/deck.js")).text();
  expect(source.match(/\bwho:\s|(?<![.\w])github(?:next)?\/(?!squad\b)[\w.-]+|\b[a-f0-9]{40}\b/i)).toBeNull();
  for (const ref of source.match(/assets\/avatars\/[^"'`\s)]*/g) ?? []) expect(ref).toMatch(allowedAvatar);
  expect(source.match(/https:\/\/[^"'\s]+/g)).toEqual(["https://github.github.com/gh-aw/introduction/overview/", "https://github.com/chrizbo/agentics-beyond-code"]);

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

test.describe("slides.md metadata", () => {
  const table = (...rows: string[]) => ["# Slides", "", "| Heading | Notes | Summary |", "|---|---|---|", ...rows, ""].join("\n");

  test("parses every row with a unique, public summary", () => {
    expect(SLIDES.length).toBeGreaterThan(0);
    expect(parseSlides(table("| A |  | First. |", "| B | illustrative | Second. |"))).toEqual([
      { heading: "A", illustrative: false, summary: "First.", line: 5 },
      { heading: "B", illustrative: true, summary: "Second.", line: 6 },
    ]);
    for (const slide of SLIDES) {
      expect(`${slide.heading} ${slide.summary}`, slide.heading).not.toMatch(PRIVATE_REFS);
      expect(slide.summary, slide.heading).not.toMatch(/https?:|@[\w-]|#\d/);
    }
  });

  test("rejects malformed slide lists with the offending line", () => {
    const cases: [string, RegExp][] = [
      [table("| A | First. |"), /:5: expected 3 cells, found 2/],
      [table("| A |  | First. | extra |"), /:5: expected 3 cells, found 4/],
      [table("| A |  | First."), /:5: malformed row/],
      [table("|  |  | First. |"), /:5: empty Heading/],
      [table("| A |  |  |"), /:5: empty Summary for "A"/],
      [table("| A | synthetic | First. |"), /:5: unknown Notes value "synthetic"/],
      [table("| A |  | First. |", "| A |  | Second. |"), /:6: duplicate Heading "A" \(first on line 5\)/],
      [table("| Human agent processes |  | First. |", "| Human Agent Processes |  | Second. |"), /:6: duplicate Heading "Human Agent Processes" \(first on line 5\)/],
      [table("| A |  | Same. |", "| B |  | Same. |"), /:6: duplicate Summary for "B" \(first on line 5\)/],
      [table("| A |  | First. |", "B | illustrative | Last slide. |"), /:6: malformed row, expected "\| Heading \| Notes \| Summary \|": B \| illustrative \| Last slide\. \|/],
      [table("| A |  | First. |", "", "| B |  | Second. |"), /:7: table row outside the slide table/],
      [table(), /:3: slide table has no rows/],
      ["| Heading | Notes |\n|---|---|\n| A |  |\n", /:1: expected table header/],
      ["| Heading | Notes | Summary |\n|---|---|\n", /:2: expected separator/],
      ["no table", /missing slide table/],
    ];
    for (const [text, error] of cases) expect(() => parseSlides(text, "slides.md"), String(error)).toThrow(error);
  });

  test("injects escaped, idempotent JSON-LD and leaves the rest of the page untouched", () => {
    const hostile = parseSlides(table("| A </script><script>alert(1)</script> |  | Uses $& and $1 & <b>tags</b>\u2028. |"));
    const html = injectSlideMetadata(SOURCE_HTML, hostile, PAGE_URL);
    expect(html.match(/<\/script>/gi)).toHaveLength(SOURCE_HTML.match(/<\/script>/gi)!.length);
    expect(html).not.toContain("<b>");
    const [doc] = jsonLd(html);
    expect(doc.hasPart).toEqual(slideParts(hostile, PAGE_URL));
    expect(doc.hasPart[0].description).toBe("Uses $& and $1 & <b>tags</b>\u2028.");
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

test("opens every slide from its metadata #N link", async ({ page, request }) => {
  const [doc] = jsonLd(await (await request.get("/gh-aw/cracktro/")).text());
  await page.goto("/gh-aw/cracktro/");
  for (const part of doc.hasPart) {
    const hash = new URL(part.url).hash;
    await page.evaluate(h => (location.hash = h), hash);
    await expect(page.locator(".count")).toHaveText(at(part.position));
    await expect(page.locator(".slide.active").getByRole("heading").first()).toHaveText(part.name);
  }
});
