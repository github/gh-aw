import { test, expect, type Page } from "@playwright/test";

async function openPresentation(page: Page) {
  await page.goto("/gh-aw/");
  await page.getByRole("button", { name: "Start slideshow" }).click();
  await expect(page.locator("#landing-slideshow")).toBeVisible();
}

async function draw(page: Page, dx: number, dy: number, shift = false) {
  await expect(page.locator("html")).not.toHaveAttribute("data-slideshow-direction");
  const bounds = await page.locator("[data-slideshow-ink]").boundingBox();
  if (!bounds) throw new Error("Drawing layer is not visible");
  const start = { x: bounds.x + bounds.width * 0.3, y: bounds.y + bounds.height * 0.3 };
  await page.mouse.move(start.x, start.y);
  if (shift) await page.keyboard.down("Shift");
  await page.mouse.down();
  await page.mouse.move(start.x + dx, start.y + dy, { steps: 4 });
  await page.mouse.up();
  if (shift) await page.keyboard.up("Shift");
}

test("draws colored rectangles, squares, arrows and resizable emoji stamps", async ({ page }) => {
  await openPresentation(page);
  await page.getByRole("button", { name: "Drawing tools", exact: true }).click();
  await page.getByRole("button", { name: "Draw rectangle (Shift for square)", exact: true }).click();
  await page.getByRole("button", { name: "Red ink" }).click();
  await draw(page, 160, 90);
  const ink = page.locator("[data-slideshow-ink]");
  await expect(ink.locator('[data-drawing-kind="rect"]')).toHaveCount(1);
  await expect(ink.locator("rect").last()).toHaveAttribute("stroke", "#ee0000");
  await draw(page, -110, -50, true);
  const square = ink.locator('[data-drawing-kind="rect"]').last().locator("rect").last();
  expect(Number(await square.getAttribute("width"))).toBeCloseTo(Number(await square.getAttribute("height")), 1);

  await page.getByRole("button", { name: "Draw arrow", exact: true }).click();
  await draw(page, 140, -80);
  await expect(ink.locator('[data-drawing-kind="arrow"]')).toHaveCount(1);
  const arrow = ink.locator('[data-drawing-kind="arrow"] path');
  await expect(arrow).toHaveCount(2);
  expect((await arrow.last().getAttribute("d"))?.match(/L /g)).toHaveLength(3);
  await page.getByRole("button", { name: "Thinking emoji", exact: true }).click();
  await draw(page, 0, 0);
  await expect(ink.locator("text")).toHaveText("🤔");
  await expect(ink.locator("text")).toHaveAttribute("font-size", "64");
  await draw(page, 60, 80);
  const stamp = ink.locator("text").last();
  expect(Number(await stamp.getAttribute("font-size"))).toBeGreaterThan(64);
  expect(await stamp.getAttribute("transform")).not.toMatch(/^rotate\(0 /);
  await expect(page.getByRole("button", { name: "Undo drawing", exact: true })).toBeEnabled();
});

test("keeps ink with its slide, preserves it on resize, and supports undo and clear", async ({ page }) => {
  await openPresentation(page);
  await page.getByRole("button", { name: "Drawing tools", exact: true }).click();
  await page.getByRole("button", { name: "Draw arrow", exact: true }).click();
  await draw(page, 100, 80);
  const ink = page.locator("[data-slideshow-ink]");
  const original = await ink.locator('[data-drawing-kind="arrow"]').evaluate(element => element.outerHTML);
  await page.setViewportSize({ width: 1024, height: 700 });
  expect(await ink.locator('[data-drawing-kind="arrow"]').evaluate(element => element.outerHTML)).toBe(original);
  await page.getByRole("button", { name: "Next slide", exact: true }).click();
  await expect(ink.locator("[data-drawing-kind]")).toHaveCount(0);
  await draw(page, 100, 80);
  await page.getByRole("button", { name: "Previous slide", exact: true }).click();
  await expect(page.locator("[data-slideshow-status]")).toContainText("1 / 9");
  await expect(ink.locator('[data-drawing-kind="arrow"]')).toHaveCount(1);
  expect(await ink.locator('[data-drawing-kind="arrow"]').evaluate(element => element.outerHTML)).toBe(original);
  await page.getByRole("button", { name: "Undo drawing", exact: true }).click();
  await expect(ink.locator("[data-drawing-kind]")).toHaveCount(0);
  await page.getByRole("button", { name: "Next slide", exact: true }).click();
  await expect(page.locator("[data-slideshow-status]")).toContainText("2 / 9");
  await expect(ink.locator("[data-drawing-kind]")).toHaveCount(1);
  await page.keyboard.press("Control+z");
  await expect(ink.locator("[data-drawing-kind]")).toHaveCount(0);
  await draw(page, 100, 80);
  await page.getByRole("button", { name: "Clear slide drawings", exact: true }).click();
  await expect(ink.locator("[data-drawing-kind]")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Undo drawing", exact: true })).toBeDisabled();
});

test("cancels unfinished strokes and restores demo interaction in pointer mode", async ({ page }) => {
  await openPresentation(page);
  await page.keyboard.press("PageDown");
  await page.getByRole("button", { name: "Drawing tools", exact: true }).click();
  await page.getByRole("button", { name: "Draw arrow", exact: true }).click();
  const bounds = await page.locator("[data-slideshow-ink]").boundingBox();
  if (!bounds) throw new Error("Drawing layer is not visible");
  await page.mouse.move(bounds.x + bounds.width / 2, bounds.y + bounds.height / 2);
  await page.mouse.down();
  await page.locator("[data-slideshow-ink]").dispatchEvent("pointercancel", { pointerId: 1 });
  await page.mouse.up();
  await expect(page.locator("[data-slideshow-ink] [data-drawing-kind]")).toHaveCount(0);
  await page.getByRole("button", { name: "Interact with slide", exact: true }).click();
  await expect(page.locator("[data-slideshow-ink]")).toHaveCSS("pointer-events", "none");
  const tabs = page.locator('[data-wf-picker] [role="tab"]');
  await tabs.nth(1).click();
  await expect(tabs.nth(1)).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Escape");
  await expect(page.locator("#slideshow-drawing-panel")).not.toBeVisible();
  await expect(page.locator("#landing-slideshow")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator("#landing-slideshow")).not.toBeVisible();
  await page.getByRole("button", { name: "Start slideshow" }).click();
  await expect(page.locator("[data-slideshow-ink] [data-drawing-kind]")).toHaveCount(0);
  await expect(page.locator("#landing-slideshow")).toBeVisible();
});

test("Escape exits drawing mode and clears every slide without exiting the presentation", async ({ page }) => {
  await openPresentation(page);
  await page.getByRole("button", { name: "Drawing tools", exact: true }).click();
  await page.getByRole("button", { name: "Draw arrow", exact: true }).click();
  await draw(page, 100, 80);
  await page.getByRole("button", { name: "Next slide", exact: true }).click();
  await draw(page, 100, 80);
  await page.getByRole("button", { name: "Drawing tools", exact: true }).click();
  await expect(page.locator("#slideshow-drawing-panel")).not.toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator("#landing-slideshow")).toBeVisible();
  await expect(page.locator("[data-slideshow-ink]")).toHaveAttribute("data-tool", "pointer");
  await expect(page.locator("[data-slideshow-ink] [data-drawing-kind]")).toHaveCount(0);
  await page.getByRole("button", { name: "Previous slide", exact: true }).click();
  await expect(page.locator("[data-slideshow-status]")).toContainText("1 / 9");
  await expect(page.locator("[data-slideshow-ink] [data-drawing-kind]")).toHaveCount(0);
  await page.getByRole("button", { name: "Drawing tools", exact: true }).click();
  await page.getByRole("button", { name: "Smile emoji", exact: true }).click();
  await draw(page, 0, 0);
  await expect(page.locator("[data-slideshow-ink] text")).toHaveCount(1);
  await page.keyboard.press("Escape");
  await expect(page.locator("#landing-slideshow")).toBeVisible();
  await expect(page.locator("#slideshow-drawing-panel")).not.toBeVisible();
  await expect(page.locator("[data-slideshow-ink] [data-drawing-kind]")).toHaveCount(0);
  await page.keyboard.press("Escape");
  await expect(page.locator("#landing-slideshow")).not.toBeVisible();
});

test("shows drawing instructions only in a hover or keyboard-focus tooltip", async ({ page }) => {
  await openPresentation(page);
  await page.getByRole("button", { name: "Drawing tools", exact: true }).click();
  const panel = page.getByRole("group", { name: "Draw on slides", exact: true });
  await expect(panel.locator("p")).toHaveCount(0);
  const tooltip = panel.getByRole("tooltip", { includeHidden: true });
  const help = panel.getByRole("button", { name: "Drawing help", exact: true });
  await expect(tooltip).not.toBeVisible();
  await help.hover();
  await expect(tooltip).toBeVisible();
  await expect(tooltip).toContainText("Drag to draw.");
  await page.mouse.move(0, 0);
  await expect(tooltip).not.toBeVisible();
  await help.focus();
  await expect(tooltip).toBeVisible();
});

test("expands snippets with larger type and smooth transitions without changing slides", async ({ page }) => {
  await openPresentation(page);
  const source = page.getByRole("button", { name: "Expand daily-issue-summary.md", exact: true });
  const originalSize = await source.locator("pre").evaluate(element => parseFloat(getComputedStyle(element).fontSize));
  await source.click();
  const zoom = page.getByRole("dialog", { name: "daily-issue-summary.md", exact: true });
  await expect(zoom).toBeVisible();
  const size = await zoom.locator("pre").evaluate(element => parseFloat(getComputedStyle(element).fontSize));
  expect(size).toBeGreaterThan(originalSize * 1.5);
  await expect(zoom.locator("pre")).toContainText("safe-outputs:");
  await expect.poll(async () => (await zoom.boundingBox())?.width).toBe(page.viewportSize()?.width);
  await expect.poll(async () => (await zoom.boundingBox())?.height).toBe(page.viewportSize()?.height);
  expect(await zoom.evaluate(element => getComputedStyle(element).transitionDuration)).toContain("0.22s");
  await page.keyboard.press("ArrowRight");
  await expect(page.locator("[data-slideshow-status]")).toContainText("1 / 9");
  await page.keyboard.press("Escape");
  await expect(zoom).not.toBeVisible();
  await expect(source).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(zoom).toBeVisible();
  await zoom.getByRole("button", { name: "Close expanded snippet" }).click();
  await expect(zoom).not.toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator("main [data-snippet-trigger]")).toHaveCount(0);
  await expect(page.locator("main .va")).toHaveAttribute("aria-hidden", "true");
});

test("expands constrained cards across the zoom content without changing the landing layout", async ({ page }) => {
  await page.goto("/gh-aw/");
  const landingCard = page.locator("main .va-chat");
  const landingWidth = (await landingCard.boundingBox())?.width;
  await openPresentation(page);
  const source = page.locator("[data-slideshow-slides] .va-chat");
  const originalWidth = (await source.boundingBox())?.width;
  expect(originalWidth).toBeGreaterThan(0);
  await source.click();
  const zoom = page.getByRole("dialog", { name: "Coding agent prompt", exact: true });
  const content = zoom.locator("[data-snippet-content]");
  const card = content.locator("[data-slideshow-snippet]");
  await expect(zoom).toBeVisible();
  await expect
    .poll(async () => {
      const contentWidth = (await content.boundingBox())?.width;
      const cardWidth = (await card.boundingBox())?.width;
      return contentWidth && cardWidth ? Math.abs(contentWidth - cardWidth) : Infinity;
    })
    .toBeLessThan(2);
  expect((await card.boundingBox())?.width).toBeGreaterThan(originalWidth! * 1.5);
  await page.keyboard.press("Escape");
  await expect.poll(async () => (await source.boundingBox())?.width).toBeCloseTo(originalWidth!, 0);
  await page.keyboard.press("Escape");
  await expect.poll(async () => (await landingCard.boundingBox())?.width).toBeCloseTo(landingWidth!, 0);
});

test("edits enlarged code as plain text without changing the original slide", async ({ page }) => {
  await openPresentation(page);
  const source = page.getByRole("button", { name: "Expand daily-issue-summary.md", exact: true });
  const original = await source.locator("pre").evaluate(element => element.outerHTML);
  await source.click();
  const zoom = page.getByRole("dialog", { name: "daily-issue-summary.md", exact: true });
  const editor = zoom.getByRole("textbox", { name: "Edit daily-issue-summary.md", exact: true });
  await expect(editor).toHaveAttribute("contenteditable", "plaintext-only");
  await expect(editor).toHaveAttribute("aria-multiline", "true");
  await expect(zoom.locator("[data-snippet-edit-hint]")).toBeVisible();
  const text = "on: daily\n\n# <strong>Live demo</strong>\ngh aw compile";
  await editor.fill(text);
  await expect.poll(() => editor.innerText()).toBe(text);
  await expect(editor.locator("strong")).toHaveCount(0);
  await editor.press("ControlOrMeta+End");
  await editor.press("Enter");
  await page.keyboard.insertText("# edited live");
  await expect(editor).toContainText("# edited live");
  await editor.press("ArrowUp");
  await editor.press("Home");
  await expect(page.locator("[data-slideshow-status]")).toContainText("1 / 9");
  await editor.press("ControlOrMeta+z");
  await expect(editor).not.toContainText("# edited live");
  await expect(zoom).toBeVisible();
  await editor.press("Escape");
  await expect(zoom).not.toBeVisible();
  await expect(source).toBeFocused();
  expect(await source.locator("pre").evaluate(element => element.outerHTML)).toBe(original);
  await expect(source.locator("[contenteditable]")).toHaveCount(0);
  await source.press("Enter");
  await expect(editor).toContainText("safe-outputs:");
  await expect(editor).not.toContainText("Live demo");
});

test("makes standalone code editable and leaves non-code expanded content read-only", async ({ page }) => {
  await openPresentation(page);
  await page.keyboard.press("PageDown");
  await expect(page.locator("[data-slideshow-status]")).toContainText("2 / 9");
  await page.getByRole("button", { name: "Expand repo-assist output", exact: true }).click();
  const zoom = page.locator("[data-slideshow-snippet-dialog]");
  await expect(zoom).toBeVisible();
  await expect(zoom.getByRole("textbox")).toHaveCount(0);
  await expect(zoom.locator("[data-snippet-edit-hint]")).not.toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Exit slideshow", exact: true }).focus();
  await page.keyboard.press("PageDown");
  await page.keyboard.press("PageDown");
  await page.keyboard.press("PageDown");
  await expect(page.locator("[data-slideshow-status]")).toContainText("5 / 9");
  const inlineCode = page.getByRole("button", { name: "Expand gh aw compile", exact: true });
  await inlineCode.click();
  const editor = zoom.getByRole("textbox");
  await expect(editor).toHaveCount(1);
  await expect(editor).toHaveAttribute("contenteditable", "plaintext-only");
  await editor.fill("gh aw compile demo.md");
  await expect(editor).toHaveText("gh aw compile demo.md");
  await zoom.getByRole("button", { name: "Close expanded snippet" }).click();
  await expect(inlineCode).toHaveText("gh aw compile");
  await inlineCode.click();
  await expect(editor).toHaveText("gh aw compile");
});

test("expands output mocks and terminal snippets and respects reduced motion", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await openPresentation(page);
  await page.keyboard.press("PageDown");
  await page.getByRole("button", { name: "Expand repo-assist output", exact: true }).click();
  let zoom = page.getByRole("dialog", { name: "repo-assist output", exact: true });
  await expect(zoom).toBeVisible();
  await expect(zoom).toHaveCSS("transition-duration", "0s");
  await expect(zoom.locator(".gh")).toHaveCSS("position", "relative");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Exit slideshow", exact: true }).focus();
  await page.keyboard.press("PageDown");
  await page.keyboard.press("PageDown");
  await page.keyboard.press("PageDown");
  await page.getByRole("button", { name: "Expand Compile in the terminal", exact: true }).focus();
  await page.keyboard.press("Space");
  zoom = page.getByRole("dialog", { name: "Compile in the terminal", exact: true });
  await expect(zoom).toBeVisible();
  await expect(zoom).toContainText("gh aw compile");
  await page.keyboard.press("Escape");
});
