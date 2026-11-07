import { test, expect } from "@playwright/test";

const summary = "/gh-aw/reference/enterprise-configuration/#deployment-and-runner-applicability";
const awf = "https://github.com/github/gh-aw-firewall/blob/main/docs/";

test("enterprise reference separates deployment, topology, and support evidence", async ({ page }) => {
  const response = await page.goto(summary);
  expect(response?.status()).toBe(200);

  const content = page.locator(".sl-markdown-content");
  const tables = content.locator("table");
  await expect(tables.nth(0)).toContainText("GitHub deployment");
  await expect(tables.nth(0)).toContainText("github.com");
  await expect(tables.nth(0)).toContainText("*.ghe.com");
  await expect(tables.nth(0)).toContainText("GHES");
  await expect(tables.nth(1)).toContainText("Runner topology");
  await expect(tables.nth(1)).toContainText("GitHub-hosted Linux");
  await expect(tables.nth(1)).toContainText("Self-hosted Linux");
  await expect(tables.nth(1)).toContainText("Self-hosted ARC/DinD");
  await expect(content).toContainText("Component compatibility is not end-to-end product support");
  await expect(content).toContainText("selected engine and inference provider");
  await expect(content).toContainText("Actions runner, and dependent GitHub services");
  await expect(content).toContainText("Those combinations remain unconfirmed");
  await expect(content).toContainText("not Kubernetes ARC");
  await expect(content).toContainText("Navigation follow-up");
  await expect(content).toContainText("That decision is pending");

  for (const document of ["compatibility.md#environment-and-runner-applicability", "enterprise-configuration.md", "arc-dind.md"]) {
    await expect(content.locator(`a[href="${awf}${document}"]`).first()).toBeVisible();
  }
  for (const schema of ["docs/awf-config.schema.json", "src/awf-config-schema.json"]) {
    await expect(content.locator(`a[href="https://github.com/github/gh-aw-firewall/blob/v0.29.7/${schema}"]`)).toBeVisible();
  }
});

for (const route of [
  "reference/self-hosted-runners",
  "reference/arc-dind-copilot-agent",
  "reference/faq",
  "troubleshooting/common-issues",
  "introduction/overview",
  "setup/quick-start",
  "blog/2026-03-18-weekly-update",
  "blog/2026-07-20-weekly-update",
]) {
  test(`${route} links to the consolidated applicability summary`, async ({ page }) => {
    const response = await page.goto(`/gh-aw/${route}/`);
    expect(response?.status()).toBe(200);
    const content = page.locator(".sl-markdown-content");
    await expect(content.locator(`a[href="${summary}"]`).first()).toBeVisible();

    if (route.startsWith("reference/") || route.startsWith("troubleshooting/")) {
      await expect(content.locator(`a[href="${awf}compatibility.md#environment-and-runner-applicability"]`).first()).toBeVisible();
      await expect(content).toContainText("unconfirmed");
    }
  });
}

test("GHES action requirements are scoped and ARC paths remain daemon-visible", async ({ page }) => {
  await page.goto("/gh-aw/reference/enterprise-configuration/#ghes-compatibility-mode");
  const content = page.locator(".sl-markdown-content");
  await expect(content).toContainText("GHES 3.21.x and earlier / Actions Runner 2.327.1+");
  await expect(content).toContainText("not a tested GHES release matrix");
  await expect(content).toContainText("Omitting cache-mode removes the scoped-token cache restriction");
  await expect(content).toContainText("Standalone AWF users must configure");

  await page.goto("/gh-aw/reference/arc-dind-copilot-agent/");
  await expect(content).toContainText("not the separate GitHub Copilot coding-agent service");
  await expect(content).toContainText("historical component-specific fix floors");
  await expect(content.locator("pre").filter({ hasText: "RUNNER_TOOL_CACHE" })).toContainText("/home/runner/_work/_toolcache");
  await expect(content).toContainText("a symlink alone does not expose a daemon-private socket");
});

test("GHES troubleshooting distinguishes inference availability from repository credentials", async ({ page }) => {
  await page.goto("/gh-aw/troubleshooting/common-issues/#copilot-engine-prerequisites-on-ghes");
  const content = page.locator(".sl-markdown-content");
  await expect(content).toContainText("GitHub Connect alone does not establish");
  await expect(content).toContainText("A GHES repository token or ARC registration credential does not grant cloud Copilot inference access");
  const errorTable = content.locator("table").filter({ hasText: "Resource not accessible by personal access token" });
  await expect(errorTable).toContainText("classic PATs are rejected");
  await expect(errorTable).not.toContainText("or classic PAT with");
});
