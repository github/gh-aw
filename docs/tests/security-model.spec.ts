import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.goto("/gh-aw/");
});

test("explains the security boundaries in an ordered flow", async ({ page }) => {
  const section = page.locator("main #security-model");
  await expect(section.getByRole("heading", { level: 3 })).toHaveText(["Trigger", "Agent", "Safe outputs"]);
  const jira = section.getByRole("link", { name: /Jira/ });
  await expect(jira).toBeVisible();
  await expect(jira).toContainText("Via authenticated repository dispatch");
  await expect(jira).toHaveAttribute("href", "/gh-aw/reference/triggers/");
  const inputs = section.locator(".agent").getByRole("link", { name: /Integrity filtered Inputs/ });
  await expect(inputs).toBeVisible();
  await expect(inputs).toHaveAttribute("href", "/gh-aw/reference/integrity/");
  await expect(section.locator(".trigger .input-gate")).toHaveCount(0);
  await expect(section).not.toContainText("Sanitized inputs");
  for (const name of ["GitHub Copilot", "Codex", "Claude Code"]) {
    await expect(section.getByRole("img", { name, exact: true })).toBeVisible();
  }
  await expect(section).toContainText("0 secrets");
  await expect(section).toContainText("Credentials stay outside the agent");
  await expect(section).toContainText("Buffered as artifacts, not applied here");
  await expect(section).toContainText("Separate job. Scoped write permissions.");
  await expect(section.getByRole("link", { name: /MicroVM/ })).toContainText("optional / preview");
  await expect(section.getByRole("link", { name: /Self-hosted/ })).toBeVisible();
  await expect(section.getByRole("link", { name: /Agent firewall/ })).toBeVisible();
  await expect(section.getByRole("link", { name: "OIDC credential providers supported", exact: true })).toHaveAttribute("href", "/gh-aw/reference/auth/");
  const checks = section.locator(".output-path");
  await expect(checks).toContainText(/Sanitization.*Threat detection.*Writes applied/s);
});

test("fits mobile and desktop in both themes without horizontal overflow", async ({ page }) => {
  const section = page.locator("main #security-model");
  for (const theme of ["light", "dark"]) {
    await page.evaluate(value => (document.documentElement.dataset.theme = value), theme);
    for (const width of [375, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await section.scrollIntoViewIfNeeded();
      const layout = await section.evaluate(element => ({
        width: element.clientWidth,
        scrollWidth: element.scrollWidth,
        cards: Array.from(element.querySelectorAll(".phase")).map(card => {
          const bounds = card.getBoundingClientRect();
          return { left: bounds.left, right: bounds.right, top: bounds.top };
        }),
      }));
      expect(layout.scrollWidth).toBeLessThanOrEqual(layout.width);
      expect(layout.cards.every(card => card.left >= 0 && card.right <= width)).toBe(true);
      if (width === 1440) expect(new Set(layout.cards.map(card => card.top)).size).toBe(1);
      else expect(layout.cards[1].top).toBeGreaterThan(layout.cards[0].top);
    }
  }
});

test("includes the security flow as its own slideshow slide and restores it on exit", async ({ page }) => {
  await page.getByRole("button", { name: "Start slideshow" }).click();
  const dialog = page.getByRole("dialog", { name: "GitHub Agentic Workflows presentation" });
  await expect(dialog).toBeVisible();
  const slides = dialog.locator("[data-slideshow-slides] > section");
  const index = await slides.evaluateAll(elements => elements.findIndex(element => element.id === "security-model"));
  expect(index).toBeGreaterThan(0);
  for (let step = 0; step < index; step++) await page.keyboard.press("ArrowRight");
  await expect(dialog.getByRole("heading", { name: "Read. Propose. Check. Apply.", exact: true })).toBeVisible();
  await expect(dialog.locator("#security-model .phase")).toHaveCount(3);
  await expect(dialog.getByRole("link", { name: /Jira/ })).toBeVisible();
  await expect(dialog.getByRole("img", { name: "Codex", exact: true })).toBeVisible();
  await expect(dialog.getByRole("img", { name: "Claude Code", exact: true })).toBeVisible();
  for (const size of [
    { width: 1440, height: 900 },
    { width: 1024, height: 600 },
  ]) {
    await page.setViewportSize(size);
    await expect
      .poll(async () => {
        const link = await dialog.locator("#security-model .architecture-link").boundingBox();
        const toolbar = await dialog.getByRole("group", { name: "Slide navigation" }).boundingBox();
        return link && toolbar ? link.y + link.height < toolbar.y : false;
      })
      .toBe(true);
  }
  await page.keyboard.press("Escape");
  await expect(page.locator("main #security-model")).toBeVisible();
});
