import { test, expect, type Page } from "@playwright/test";

const sections = [
  {
    label: "Guides",
    preview: ["GitHub Actions Primer", "Using at Scale in Organizations", "More..."],
    more: "/gh-aw/guides/mcps/",
    other: "/gh-aw/guides/network-configuration/",
    count: 8,
  },
  {
    label: "Design Patterns",
    preview: ["BatchOps", "CentralRepoOps", "More..."],
    more: "/gh-aw/patterns/chat-ops/",
    other: "/gh-aw/patterns/workqueue-ops/",
    count: 16,
  },
  {
    label: "Reference",
    preview: ["FAQ", "AI Engines", "Cost Management", "Safe Outputs", "Workflows", "More..."],
    more: "/gh-aw/reference/workflow-structure/",
    other: "/gh-aw/reference/engines/copilot/",
    count: 79,
  },
];

function sidebarGroup(page: Page, label: string) {
  return page.locator("#starlight__sidebar details").filter({
    has: page.locator("summary", { hasText: new RegExp(`^${label}$`) }),
  });
}

test("specification links show only their titles in the desktop sidebar and mobile menu", async ({ page }) => {
  const specRoute = "/gh-aw/specs/unified-agent-session-specification/";
  await page.goto("/gh-aw/introduction/overview/");
  const specs = sidebarGroup(page, "Specs");
  await specs.locator("summary").click();
  const specLink = specs.getByRole("link", { name: "Unified Agent Session Specification", exact: true });
  await expect(specLink).toHaveAttribute("href", specRoute);

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(specRoute);
  const menu = page.getByRole("button", { name: "Menu", exact: true });
  const toc = page.locator("mobile-starlight-toc nav");
  await expect(menu).toBeHidden();
  await expect(toc).toBeHidden();
  await page.getByRole("button", { name: "Zen mode" }).click();
  await expect(toc).toBeVisible();
  await menu.click();
  await expect(specLink).toHaveAttribute("aria-current", "page");
  await expect(specLink).toBeVisible();
});

for (const section of sections) {
  test(`${section.label} shows its preview and More outside its section`, async ({ page }) => {
    await page.goto("/gh-aw/introduction/overview/");
    const group = sidebarGroup(page, section.label);
    await expect(group.locator("a")).toHaveText(section.preview.slice(0, -1));
    await expect(group.getByRole("button", { name: "More...", exact: true, includeHidden: true })).toHaveAttribute("aria-expanded", "false");
    if (section.label === "Reference") await expect(group).toHaveAttribute("open", "");
  });

  for (const route of [section.more, section.other]) {
    test(`${route} expands ${section.label} and highlights the current page`, async ({ page }) => {
      await page.goto(route);
      const group = sidebarGroup(page, section.label);
      await expect(group).toHaveAttribute("open", "");
      await expect(group.locator("a")).toHaveCount(section.count);
      await expect(group.locator('a[aria-current="page"]')).toHaveAttribute("href", route);
      await expect(group.getByRole("link", { name: "More...", exact: true })).toHaveCount(0);
    });
  }

  test(`${section.label} expands in place and stays expanded across navigation, but resets on reload`, async ({ page }) => {
    await page.goto("/gh-aw/introduction/overview/?sidebar=test#overview");
    const group = sidebarGroup(page, section.label);
    if ((await group.getAttribute("open")) === null) await group.locator("summary").click();
    const url = page.url();
    const historyLength = await page.evaluate(() => history.length);
    const more = group.getByRole("button", { name: "More...", exact: true });
    await more.focus();
    await more.press("Space");
    await expect(page).toHaveURL(url);
    expect(await page.evaluate(() => history.length)).toBe(historyLength);
    await expect(group.locator("a")).toHaveCount(section.count);
    await expect(group).toHaveAttribute("open", "");
    await expect(group.getByRole("button", { name: "More...", exact: true })).toHaveCount(0);
    await expect(group.locator("a:focus")).toHaveCount(1);

    await group.locator(`a[href="${section.more}"]`).click();
    await expect(group.locator('a[aria-current="page"]')).toHaveAttribute("href", section.more);
    await page.locator('#starlight__sidebar .top-level a[href="/gh-aw/introduction/overview/"]').click();
    await expect(group.locator("a")).toHaveCount(section.count);
    await page.reload();
    await expect(group.locator("a")).toHaveText(section.preview.slice(0, -1));
    await expect(group.getByRole("button", { name: "More...", exact: true, includeHidden: true })).toHaveCount(1);
  });

  test(`${section.label} has the full catalog on mobile`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(section.more);
    await page.getByRole("button", { name: "Menu", exact: true }).click();
    const group = sidebarGroup(page, section.label);
    await expect(group.locator('a[aria-current="page"]')).toBeVisible();
    await expect(group.locator("a")).toHaveCount(section.count);
  });

  test(`${section.label} expands in place on mobile and remembers its own choice only`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/gh-aw/introduction/overview/");
    await page.getByRole("button", { name: "Menu", exact: true }).click();
    const group = sidebarGroup(page, section.label);
    if ((await group.getAttribute("open")) === null) await group.locator("summary").click();
    const url = page.url();
    await group.getByRole("button", { name: "More...", exact: true }).click();
    await expect(page).toHaveURL(url);
    await expect(group.locator("a")).toHaveCount(section.count);
    await expect(group.locator("a:focus")).toBeVisible();
    await page.reload();
    await page.getByRole("button", { name: "Menu", exact: true }).click();
    await expect(group.locator("a")).toHaveText(section.preview.slice(0, -1));
    for (const other of sections.filter(item => item !== section)) {
      await expect(sidebarGroup(page, other.label).locator("a")).toHaveText(other.preview.slice(0, -1));
    }
  });
}

test("Guides keeps Using MCPs and Network Configuration in positions three and four", async ({ page }) => {
  for (const route of ["/gh-aw/guides/mcps/", "/gh-aw/introduction/overview/"]) {
    await page.goto(route);
    const group = sidebarGroup(page, "Guides");
    if (route === "/gh-aw/introduction/overview/") {
      if ((await group.getAttribute("open")) === null) await group.locator("summary").click();
      await group.getByRole("button", { name: "More...", exact: true }).click();
    }
    await expect(group.locator("a").nth(2)).toHaveText("Using MCPs");
    await expect(group.locator("a").nth(3)).toHaveText("Network Configuration");
  }
});

test("Working with Workflows belongs to Setup and keeps Guides compact", async ({ page }) => {
  await page.goto("/gh-aw/setup/working-with-workflows/");
  await expect(page.getByRole("heading", { name: "Working with Workflows", exact: true })).toBeVisible();
  await expect(sidebarGroup(page, "Guides").locator("a")).toHaveText(sections[0].preview.slice(0, -1));
  await expect(sidebarGroup(page, "Setup").getByRole("link", { name: "Working with Workflows", exact: true })).toHaveAttribute("aria-current", "page");
});

test("the old Working with Workflows URL redirects and preserves section bookmarks", async ({ page }) => {
  await page.goto("/gh-aw/guides/working-with-workflows/#editing-workflows");
  await expect(page).toHaveURL(/\/gh-aw\/setup\/working-with-workflows\/#editing-workflows$/);
  await expect(page.locator("#editing-workflows")).toBeVisible();
});

test("earlier workflow-guide redirects point directly to Setup", async ({ page }) => {
  await page.goto("/gh-aw/guides/reusing-workflows/");
  await expect(page).toHaveURL(/\/gh-aw\/setup\/working-with-workflows\/#adding-existing-workflows$/);
  await expect(page.locator("#adding-existing-workflows")).toBeVisible();
});

test("Workshop lives under Guides and retains its curriculum links", async ({ page }) => {
  const response = await page.goto("/gh-aw/guides/workshop/");
  expect(response?.status()).toBe(200);
  await expect(page.getByRole("heading", { name: "Workshop", exact: true })).toBeVisible();
  await expect(page.locator(".sl-markdown-content").getByRole("link", { name: "Interactive Workshop", exact: true })).toHaveAttribute("href", "https://githubnext.github.io/gh-aw-workshop/");
  const group = sidebarGroup(page, "Guides");
  await expect(group).toHaveAttribute("open", "");
  await expect(group.locator("a")).toHaveCount(8);
  const workshopLink = group.getByRole("link", { name: "Agentic Automation Workshop (opens in a new tab)", exact: true });
  await expect(workshopLink).toHaveAttribute("href", "https://githubnext.github.io/gh-aw-workshop/");
  await expect(workshopLink).toHaveAttribute("class", /sidebar-external/);
  await expect(workshopLink).toHaveAttribute("target", "_blank");
  await expect(workshopLink).toHaveAttribute("rel", "noopener");
});

test("the old Workshop URL redirects to Guides and preserves bookmarks", async ({ page }) => {
  await page.goto("/gh-aw/workshop/#j=terminal&s=daily-status&t=00-welcome");
  await expect(page).toHaveURL(/\/gh-aw\/guides\/workshop\/#j=terminal&s=daily-status&t=00-welcome$/);
  await expect(page.getByRole("heading", { name: "Workshop", exact: true })).toBeVisible();
});

test("redirects preserve a simple incoming fragment", async ({ page }) => {
  await page.goto("/gh-aw/workshop/#x");
  await expect(page).toHaveURL(/\/gh-aw\/guides\/workshop\/#x$/);
});

test("a redirect's own fragment wins over the requested one", async ({ page }) => {
  await page.goto("/gh-aw/reference/servicenow-connector/#other");
  await expect(page).toHaveURL(/\/gh-aw\/reference\/connectors\/#servicenow$/);
  await expect(page.locator("#servicenow")).toBeVisible();
});

for (const guide of [
  { slug: "safe-rollout", title: "Safe Rollout", label: "Safe Rollout in Organizations", anchor: "rollout-ladder" },
  { slug: "sharing-workflows", title: "Sharing Workflows in the Organization", label: "Sharing Workflows in Organizations", anchor: "sharing-layers" },
  { slug: "measuring-impact", title: "Measuring Impact", label: "Measuring Impact", anchor: "timing-of-cost-and-outcomes" },
]) {
  test(`${guide.title} lives under Guides and redirects its Practices bookmark`, async ({ page }) => {
    const response = await page.goto(`/gh-aw/guides/${guide.slug}/`);
    expect(response?.status()).toBe(200);
    await expect(page.getByRole("heading", { name: guide.title, exact: true })).toBeVisible();
    const group = sidebarGroup(page, "Guides");
    await expect(group).toHaveAttribute("open", "");
    await expect(group.getByRole("link", { name: guide.label, exact: true })).toHaveAttribute("aria-current", "page");
    await page.goto(`/gh-aw/practices/${guide.slug}/#${guide.anchor}`);
    await expect(page).toHaveURL(new RegExp(`/gh-aw/guides/${guide.slug}/#${guide.anchor}$`));
    await expect(page.locator(`#${guide.anchor}`)).toBeVisible();
  });
}

for (const [oldPath, newPath] of [
  ["organization-practices/safe-rollout", "guides/safe-rollout"],
  ["organization-practices/sharing-workflows", "guides/sharing-workflows"],
  ["guides/organization-practices/safe-rollout", "guides/safe-rollout"],
  ["guides/organization-practices/sharing-workflows", "guides/sharing-workflows"],
  ["reference/measuring-impact", "guides/measuring-impact"],
]) {
  test(`${oldPath} redirects directly to ${newPath}`, async ({ page, request }) => {
    const response = await request.get(`/gh-aw/${oldPath}/`, { maxRedirects: 0 });
    if (response.status() >= 300 && response.status() < 400) {
      expect(response.headers().location).toBe(`/gh-aw/${newPath}/`);
    } else {
      expect(await response.text()).toContain(`url=/gh-aw/${newPath}/`);
    }
    await page.goto(`/gh-aw/${oldPath}/`);
    await expect(page).toHaveURL(new RegExp(`/gh-aw/${newPath}/$`));
  });
}

test("expanding one section keeps the other sections compact", async ({ page }) => {
  await page.goto("/gh-aw/patterns/chat-ops/");
  await expect(sidebarGroup(page, "Guides").locator("a")).toHaveText(sections[0].preview.slice(0, -1));
  await expect(sidebarGroup(page, "Gallery").locator("a")).toHaveCount(5);
  await expect(sidebarGroup(page, "Reference").locator("a")).toHaveText(sections[2].preview.slice(0, -1));

  await page.goto("/gh-aw/guides/safe-rollout/");
  await expect(sidebarGroup(page, "Design Patterns").locator("a")).toHaveText(sections[1].preview.slice(0, -1));
});

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });

  for (const section of sections) {
    test(`${section.label} is expanded on the server`, async ({ page }) => {
      await page.goto(section.more);
      await expect(sidebarGroup(page, section.label).locator("a")).toHaveCount(section.count);
      await page.goto("/gh-aw/introduction/overview/");
      const group = sidebarGroup(page, section.label);
      await expect(group.locator("a")).toHaveText(section.preview);
      await expect(group.getByRole("link", { name: "More...", exact: true, includeHidden: true })).toHaveAttribute("href", section.more);
    });
  }
});
