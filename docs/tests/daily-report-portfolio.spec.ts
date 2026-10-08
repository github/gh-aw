import { test, expect } from "@playwright/test";

test("daily report portfolio renders its ledger diagrams and source walkthrough", async ({ page }) => {
  await page.goto("/gh-aw/patterns/daily-report-portfolio/");
  await expect(page.getByRole("heading", { name: "Daily Report Portfolio", exact: true })).toBeVisible();
  const diagrams = page.locator("pre.mermaid svg");
  await expect(diagrams).toHaveCount(3);
  for (const diagram of await diagrams.all()) {
    await expect(diagram).toBeVisible();
    await expect(diagram.locator("g").first()).toBeAttached();
  }
  await expect(page.locator("pre.mermaid .error-text")).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "Dispatcher source and consuming prompt" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "One ledger from admission to verified Result" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Common pitfalls" })).toBeVisible();
});
