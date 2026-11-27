import { test, expect } from "@playwright/test";

test("work-queue pattern provides deployment and links to the technical reference", async ({ page }) => {
  await page.goto("/gh-aw/patterns/workqueue-ops/");

  const content = page.locator(".sl-markdown-content");
  await expect(content.locator("#deploy-a-native-queue")).toHaveText("Deploy a native queue");
  await expect(content.locator("pre").filter({ hasText: "# README check worker" })).toHaveCount(1);
  await expect(content.locator("pre").filter({ hasText: "# README check dispatcher" })).toHaveCount(1);
  await expect(content.getByRole("link", { name: "scheduling reference", exact: true })).toHaveAttribute("href", "/gh-aw/reference/work-queue/#repository-scheduling-settings");
  await expect(content.locator('a[href*="/guides/deploy-work-queue/"]')).toHaveCount(0);
  await expect(page.locator('#starlight__sidebar a[href*="/guides/deploy-work-queue/"]')).toHaveCount(0);

  await page.goto("/gh-aw/reference/work-queue/");
  await expect(page.locator(".sl-markdown-content").getByRole("link", { name: "minimal deployment example" })).toHaveAttribute("href", "/gh-aw/patterns/workqueue-ops/#deploy-a-native-queue");
});

test("retired work-queue guide redirects to the pattern deployment section", async ({ page }) => {
  await page.goto("/gh-aw/guides/deploy-work-queue/#publish-the-worker-and-dispatcher");

  await expect(page).toHaveURL(/\/gh-aw\/patterns\/workqueue-ops\/#deploy-a-native-queue$/);
  await expect(page.locator("#deploy-a-native-queue")).toBeVisible();
});
