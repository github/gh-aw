import { test, expect } from "@playwright/test";

test.describe("Mermaid Diagram Rendering", () => {
  test("should render mermaid diagrams as SVG on compilation-process page", async ({ page }) => {
    // Navigate to the compilation process page
    await page.goto("/gh-aw/reference/compilation-process/");

    // Wait for the page to be fully loaded
    await page.waitForLoadState("networkidle");

    // Check if there are pre.mermaid elements (mermaid code blocks before transformation)
    const preMermaidCount = await page.locator("pre.mermaid").count();
    console.log(`Found ${preMermaidCount} pre.mermaid elements`);

    // Wait a bit longer for mermaid JavaScript to load and transform diagrams
    await page.waitForTimeout(5000);

    // Check that SVG elements exist on the page (mermaid diagrams should be rendered as SVG)
    const svgElements = await page.locator('svg[id^="mermaid"]').count();
    console.log(`Found ${svgElements} SVG elements with mermaid ID`);

    // The page contains two Mermaid diagrams.
    expect(svgElements).toBeGreaterThanOrEqual(2);

    // Take a screenshot to visualize the rendered diagrams
    await page.screenshot({
      path: "test-results/mermaid-diagrams-rendered.png",
      fullPage: true,
    });

    // Verify that SVGs have proper structure (mermaid generates specific classes)
    const mermaidSvgs = page.locator('svg[id^="mermaid"]');
    const firstSvg = mermaidSvgs.first();
    await expect(firstSvg).toBeVisible();

    // Check that the SVG has g elements (mermaid uses groups for nodes and edges)
    const gElements = await firstSvg.locator("g").count();
    expect(gElements).toBeGreaterThan(0);
  });
});
