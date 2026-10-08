import { test, expect } from "@playwright/test";

test("work-queue command table rows stay compact on desktop", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/gh-aw/reference/work-queue/");

  const table = page
    .locator(".sl-markdown-content table")
    .filter({ has: page.locator("th", { hasText: "Command" }) })
    .first();
  const wrapper = table.locator("xpath=..");
  const stateRow = table.locator("tbody tr").filter({ hasText: "state [--graph GRAPH]" });

  await expect(table.locator("th").nth(1)).toHaveText("Purpose");
  expect(await wrapper.evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1);
  expect((await stateRow.boundingBox())?.height).toBeLessThan(200);
});
