import { test, expect } from "@playwright/test";
import { readdirSync } from "node:fs";

test.use({ reducedMotion: "reduce" });

test("presents the forty-five slides without the removed sections", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.goto("/gh-aw/cracktro/");
  await expect(page).toHaveTitle("GitHub Agentic Workflows Talk");
  await expect(page.locator(".slide")).toHaveCount(45);
  await expect(page.locator(".stage")).not.toContainText(/the crew|the story|akeso|security at (?:hyper)?scale/i);

  const headings = [
    "GitHubAgenticWorkflows",
    "AI-poweredworkflowsfor GitHub",
    "Anatomy of a workflow",
    "Turn Intent to Automation",
    "Agentic attacks",
    "Safety and control",
    "Supply chain hardening",
    "Safe outputs",
    "Continuous Security",
    "Stop manually translating intent into code.",
    "Human Agent Processes",
    "Pull Request Review²",
    "Issues as intent",
    "Repo Assist",
    "Daily Unsloppers",
    "Merge fast, unslop daily",
    "Research · Assign · Observe",
    "Query · Ask · Grow",
    "Mine · Refine · Apply",
    "Explore · Reuse · Lint",
    "Semantic clustering",
    "Spec · Model · Test",
    "Go Fan",
    "Spec-driven testing",
    "Docs that heal",
    "Speak · Map · Grep",
    "Scan · Fix · Learn",
    "Agents that debug agents",
    "Audit & Optimize",
    "Cost management",
    "Model routing",
    "Agent memory",
    "Ephemerals",
    "Index & search",
    "Experiments, graders & evals",
    "Any agent, any provider",
    "Private enclaves",
    "Connectors",
    "Patch to PR",
    "Enterprise ready",
    "Centralized campaigns",
    "G\u0131tHub Agentic Workflows",
    "First commit to GA",
    "How PRs land",
    "How workflows evolve",
  ];
  for (const [index, heading] of headings.entries()) {
    await page.getByRole("button", { name: `Slide ${index + 1}`, exact: true }).click();
    await expect(page.locator(".count")).toHaveText(`${String(index + 1).padStart(2, "0")} / 45`);
    await expect(page.locator(".slide.active").getByRole("heading").first()).toHaveText(heading);
    await page.evaluate(() => new Promise(requestAnimationFrame));
  }
  expect(errors).toEqual([]);
});

test("preserves navigation, slide links, speaker notes, and sound controls", async ({ page }) => {
  await page.goto("/gh-aw/cracktro/#11");
  await expect(page.locator(".count")).toHaveText("11 / 45");
  await page.keyboard.press("n");
  await expect(page.locator(".notes")).toBeVisible();
  await expect(page.locator(".notes")).toContainText("LabelOps");
  await page.keyboard.press("n");
  await expect(page.locator(".notes")).toBeHidden();

  await page.getByRole("button", { name: "Toggle sound (M)", exact: true }).click();
  expect(await page.evaluate(() => localStorage.getItem("aw-mute"))).toBe("1");
  await page.keyboard.press("m");
  expect(await page.evaluate(() => localStorage.getItem("aw-mute"))).toBe("0");
  await page.getByRole("button", { name: "Toggle sound (M)", exact: true }).evaluate(button => button.blur());

  await page.keyboard.press("Space");
  await expect(page).toHaveURL(/\/gh-aw\/cracktro\/#12$/);
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator(".count")).toHaveText("11 / 45");
  await page.reload();
  await expect(page.locator(".count")).toHaveText("11 / 45");
  await page.keyboard.press("Home");
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator(".count")).toHaveText("01 / 45");
  await page.keyboard.press("End");
  await page.keyboard.press("ArrowRight");
  await expect(page.locator(".count")).toHaveText("45 / 45");
});

test("swaps the slide dots for arrows on narrow screens", async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 720 });
  await page.goto("/gh-aw/cracktro/#1");
  await expect(page.getByRole("button", { name: "Slide 1", exact: true })).toBeHidden();
  const prev = page.getByRole("button", { name: "Previous slide", exact: true });
  const next = page.getByRole("button", { name: "Next slide", exact: true });
  await expect(prev).toBeDisabled();
  await next.click();
  await expect(page.locator(".count")).toHaveText("02 / 45");
  await prev.click();
  await expect(page.locator(".count")).toHaveText("01 / 45");

  await page.setViewportSize({ width: 1280, height: 720 });
  await expect(page.getByRole("button", { name: "Slide 1", exact: true })).toBeVisible();
  await expect(next).toBeHidden();
});

test("ships local assets with the docs and links to the talk from the footer", async ({ page, request }) => {
  await page.goto("/gh-aw/");
  await page.locator(".site-footer").getByRole("link", { name: "Interactive talk", exact: true }).click();
  await expect(page).toHaveURL(/\/gh-aw\/cracktro\/#1$/);

  const assets = ["deck.js", "fonts/MonaspaceNeonVF.woff", "fonts/Monaspace-LICENSE.txt", "../fonts/MonaSansVF.woff2"];
  for (const asset of assets) {
    const response = await request.get(`/gh-aw/cracktro/${asset}`);
    expect(response.ok(), asset).toBe(true);
    expect((await response.body()).length, asset).toBeGreaterThan(0);
    expect(response.headers()["content-type"], asset).not.toContain("text/html");
  }
  await page.evaluate(() => document.fonts.ready);
  expect(await page.evaluate(() => document.fonts.check('16px "Monaspace Neon"'))).toBe(true);
});

test("publishes a social card for link previews", async ({ page, request }) => {
  await page.goto("/gh-aw/cracktro/");
  const image = "https://github.github.com/gh-aw/cracktro/og-image.png";
  await expect(page.locator('meta[property="og:image"]')).toHaveAttribute("content", image);
  await expect(page.locator('meta[name="twitter:image"]')).toHaveAttribute("content", image);
  await expect(page.locator('meta[name="twitter:card"]')).toHaveAttribute("content", "summary_large_image");
  const response = await request.get("/gh-aw/cracktro/og-image.png");
  expect(response.ok()).toBe(true);
  expect(response.headers()["content-type"]).toContain("image/png");
});

test("publishes synthetic examples without profile assets or private source references", async ({ page, request }) => {
  const allowedAvatar = /^assets\/avatars\/(?:pelikhan|lpcox|dsyme|mnkiefer|ievangelist)\.jpg$/;
  for (const asset of readdirSync(new URL("../public/cracktro/assets/", import.meta.url), { recursive: true }) as string[]) {
    expect(asset === "avatars" || allowedAvatar.test(`assets/${asset}`), asset).toBe(true);
  }
  const source = await (await request.get("/gh-aw/cracktro/deck.js")).text();
  expect(source.match(/\bwho:\s|(?<![.\w])github(?:next)?\/[\w.-]+|\b[a-f0-9]{40}\b/i)).toBeNull();
  for (const ref of source.match(/assets\/avatars\/[^"'`\s)]*/g) ?? []) expect(ref).toMatch(allowedAvatar);
  expect(source.match(/https:\/\/[^"'\s]+/g)).toEqual(["https://github.github.com/gh-aw/introduction/overview/"]);

  const externalRequests: string[] = [];
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.route("**/*", route => {
    if (new URL(route.request().url()).origin === "http://localhost:4321") return route.continue();
    externalRequests.push(route.request().url());
    return route.abort();
  });
  await page.clock.install();
  await page.goto("/gh-aw/cracktro/#19");
  await page.clock.runFor(15_000);
  for (const author of await page.locator(".mra-who").allTextContents()) {
    expect(author).toMatch(/^(?:linter miner|lintmonster|copilot) · /);
  }
  for (const href of await page.locator("svg image").evaluateAll(images => images.map(image => image.getAttribute("href")))) {
    expect(href).toMatch(allowedAvatar);
  }
  await expect(page.locator(".mra-repo-t")).toContainText("gh-aw · May–Nov 2026");
  await page.keyboard.press("n");
  for (const slide of [2, 3, 5, 7, 8, 9, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42]) {
    await page.getByRole("button", { name: `Slide ${slide}`, exact: true }).click();
    await expect(page.locator(".notes")).toContainText(/illustrative|synthetic|simulated/i);
  }
  expect(errors).toEqual([]);
  expect(externalRequests).toEqual([]);
});
