import { test, expect } from "@playwright/test";

const engines = ["copilot", "claude", "codex", "gemini", "pi", "agy"];

for (const engine of engines) {
  test(`${engine} landing page stays introductory and links to its reference`, async ({ page }) => {
    await page.goto(`/gh-aw/engines/${engine}/`);

    const content = page.locator(".sl-markdown-content");
    const words = (await content.innerText()).split(/\s+/).length;
    expect(words).toBeLessThan(350);
    await expect(content.locator("#connect-your-account")).toBeVisible();
    await expect(content.locator(`a[href="/gh-aw/reference/engines/${engine}/"]`).first()).toBeVisible();
    await expect(content).not.toContainText("Session trace mapping");
    await expect(content).not.toContainText("does not require an experimental opt-in");
    await expect(content).not.toContainText("Regression fixtures");
  });

  test(`${engine} technical reference is rendered and available in navigation`, async ({ page }) => {
    const response = await page.goto(`/gh-aw/reference/engines/${engine}/`);
    expect(response?.status()).toBe(200);

    const content = page.locator(".sl-markdown-content");
    await expect(content.locator(`a[href="/gh-aw/engines/${engine}/"]`)).toBeVisible();
    await expect(content.locator("#selection-and-authentication, #providers-and-authentication")).toBeVisible();
    await expect(page.locator(`#starlight__sidebar a[href="/gh-aw/reference/engines/${engine}/"]`).first()).toHaveCount(1);
  });
}

test("shared reference links to per-engine contracts and preserves moved-section bookmarks", async ({ page }) => {
  await page.goto("/gh-aw/reference/engines/");
  const content = page.locator(".sl-markdown-content");

  for (const engine of engines) {
    await expect(content.locator(`a[href="/gh-aw/reference/engines/${engine}/"]`).first()).toBeVisible();
  }
  await expect(content.locator("#engine-feature-comparison")).toBeVisible();
  const anchors = [
    "engine-environment-variables",
    "engine-command-line-arguments",
    "custom-engine-command",
    "copilot-custom-configuration",
    "copilot-release-checksum-pins",
    "copilot-bring-your-own-key-byok-mode",
    "azure-foundry-openai",
    "copilot-sdk-support",
    "sdk-driver-environment-variables",
    "pi-extensions-extensions",
    "claude-tool-enforcement-security-model",
    "gateway-side-enforcement",
    "shared-imported-engines",
    "unsupported-engine-samples",
  ];
  for (const anchor of anchors) {
    const target = page.locator(`#${anchor}`);
    await expect(target).toHaveCount(1);
    await expect(target).toBeVisible();
  }
  await expect(content.locator('a[href="/gh-aw/reference/third-party-agent/#unsupported-engine-samples"]')).toBeVisible();
});

test("custom engine reference owns imported definitions and unsupported samples", async ({ page }) => {
  await page.goto("/gh-aw/reference/third-party-agent/");
  const content = page.locator(".sl-markdown-content");

  await expect(content.locator("#shared-imported-engines")).toBeVisible();
  await expect(content.locator("#unsupported-engine-samples")).toBeVisible();
  await expect(content.locator("pre").first()).toContainText("id: example-engine");
  const protectedCapture = content.locator("#protected-session-capture");
  await expect(protectedCapture).toBeVisible();
  await expect(content.locator("pre").filter({ hasText: "protected-session-capture: true" })).toContainText("execution:");
  await expect(content.locator('a[href$="unified-agent-session-specification/#protected-declarative-session-ingestion"]')).toBeVisible();
  await expect(content).toContainText("Native capture protects storage, not the origin of guest-side bytes.");
  await expect(content).toContainText("native exit code remains authoritative over any shared exit-code sidecar");
  await expect(content).toContainText("unified session-artifact uploads are eligible only after verification, publication, and cleanup all succeed");
  await expect(content).toContainText("compilation rejects this combination");
  await expect(content).not.toContainText("engine: opencode");
});
