import { test, expect } from "@playwright/test";

test("the single Connectors reference is linked below the Reference catalog", async ({ page }) => {
  await page.goto("/gh-aw/reference/connectors/");
  await expect(page.getByRole("heading", { name: "Connectors", exact: true })).toBeVisible();
  const sidebar = page.locator("#starlight__sidebar");
  const connector = sidebar.getByRole("link", { name: "Connectors", exact: true });
  await expect(connector).toHaveAttribute("aria-current", "page");
  await expect(connector).toHaveCount(1);
  const reference = sidebar.locator("details").filter({ has: page.locator("summary", { hasText: /^Reference$/ }) });
  await expect(reference.locator('a[href="/gh-aw/reference/connectors/"]')).toHaveCount(0);
  expect(
    await reference.evaluate(element => {
      const link = document.querySelector('#starlight__sidebar a[href="/gh-aw/reference/connectors/"]');
      return link !== null && Boolean(element.compareDocumentPosition(link) & Node.DOCUMENT_POSITION_FOLLOWING);
    })
  ).toBe(true);
});

for (const [previous, anchor] of [
  ["connector-validation", ""],
  ["connector-assembly", "configuration"],
  ["linear-connector", "linear"],
  ["jira-connector", "jira"],
  ["azure-devops-connector", "azure-devops"],
  ["slack-connector", "slack"],
  ["teams-connector", "microsoft-teams"],
  ["confluence-connector", "confluence"],
  ["sentry-connector", "sentry"],
  ["servicenow-connector", "servicenow"],
]) {
  test(`${previous} redirects into the consolidated connector reference`, async ({ page }) => {
    await page.goto(`/gh-aw/reference/${previous}/`);
    await expect(page).toHaveURL(new RegExp(`/gh-aw/reference/connectors/${anchor ? `#${anchor}` : ""}$`));
    if (anchor) await expect(page.locator(`#${anchor}`)).toBeVisible();
  });
}
