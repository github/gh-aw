import { test, expect } from "@playwright/test";

// Docs prose is capped at a comfortable reading measure; tables, diagrams and
// images may run wider. See .github/skills/docs-design/SKILL.md.
const PROSE_WIDTH = 620;
const MIN_GUTTER = 60;

test("docs prose stays within the reading measure, centred with gutters", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto("/gh-aw/reference/work-queue/");

  const layout = await page.evaluate(() => {
    const box = (el: Element | null) => {
      if (!el) throw new Error("Element not found");
      const r = el.getBoundingClientRect();
      return { left: r.left, right: r.right, width: r.width };
    };
    return {
      sidebar: box(document.querySelector("#starlight__sidebar")),
      toc: box(document.querySelector(".right-sidebar-panel")),
      title: box(document.querySelector("h1#_top")),
      footer: box(document.querySelector(".content-panel footer")),
      prose: [...document.querySelectorAll(".sl-markdown-content > :not(.table-scroll-wrapper, pre.mermaid, figure, picture, img, video)")].map(el => box(el)),
      tables: [...document.querySelectorAll(".sl-markdown-content > .table-scroll-wrapper")].map(el => box(el)),
    };
  });

  for (const el of [layout.title, layout.footer, ...layout.prose]) {
    expect(el.width).toBeLessThanOrEqual(PROSE_WIDTH + 1);
    expect(el.left - layout.sidebar.right).toBeGreaterThanOrEqual(MIN_GUTTER);
    expect(layout.toc.left - el.right).toBeGreaterThanOrEqual(MIN_GUTTER);
  }

  // Centred between the sidebar and "On this page"
  const p = layout.prose[0];
  expect(Math.abs(p.left - layout.sidebar.right - (layout.toc.left - p.right))).toBeLessThanOrEqual(2);

  // Wide tables may run past the measure, still clear of the navigation
  expect(Math.max(...layout.tables.map(t => t.width))).toBeGreaterThan(PROSE_WIDTH);
  for (const t of layout.tables) {
    expect(t.left - layout.sidebar.right).toBeGreaterThanOrEqual(MIN_GUTTER);
    expect(layout.toc.left - t.right).toBeGreaterThanOrEqual(MIN_GUTTER);
  }
});

test("the landing page keeps its own wide layout", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto("/gh-aw/");
  expect(
    await page
      .locator(".aw-hero-inner")
      .first()
      .evaluate(hero => getComputedStyle(hero).maxWidth)
  ).toBe("1152px");
});
