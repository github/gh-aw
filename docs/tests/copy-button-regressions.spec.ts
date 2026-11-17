import { test, expect } from "@playwright/test";

test.describe("Copy entire file regressions", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      // Isolate handler behavior from clipboard permissions and platform fallbacks.
      Object.defineProperty(window, "ClipboardItem", { value: undefined });
      Object.defineProperty(navigator, "clipboard", {
        value: { writeText: async () => {} },
      });

      const listeners = new Set<EventListenerOrEventListenerObject>();
      const add = document.addEventListener.bind(document);
      const remove = document.removeEventListener.bind(document);
      document.addEventListener = (type: string, listener: EventListenerOrEventListenerObject, options?: boolean | AddEventListenerOptions) => {
        if (type === "keydown") listeners.add(listener);
        add(type, listener, options);
      };
      document.removeEventListener = (type: string, listener: EventListenerOrEventListenerObject, options?: boolean | EventListenerOptions) => {
        if (type === "keydown") listeners.delete(listener);
        remove(type, listener, options);
      };
      Object.defineProperty(window, "documentKeydownListenerCount", {
        get: () => listeners.size,
      });
    });
    await page.goto("/gh-aw/setup/working-with-workflows/");
    await expect(page.locator(".copy-entire-file-btn")).toHaveCount(2);
  });

  test("one click fetches the file once on a page with multiple buttons", async ({ page }) => {
    let requests = 0;
    await page.route("https://raw.githubusercontent.com/github/gh-aw/main/*.md", route => {
      requests++;
      return route.fulfill({ contentType: "text/plain", body: "# Instructions" });
    });

    for (const button of await page.locator(".copy-entire-file-btn").all()) {
      const before = requests;
      await button.click();
      await expect(button.locator(".btn-text")).toHaveText("Copied!");
      expect(requests - before).toBe(1);
    }
  });

  test("the dynamically created error dialog is styled", async ({ page }) => {
    await page.route("**/debug.md", route => route.fulfill({ status: 404 }));
    await page.locator('.copy-entire-file-btn[data-filepath$="/debug.md"]').click();

    const overlay = page.locator(".error-modal-overlay").first();
    const modal = overlay.getByRole("dialog");
    await expect(modal).toBeVisible();
    await expect(overlay).toHaveCSS("position", "fixed");
    await expect(overlay).toHaveCSS("display", "flex");
    await expect(overlay).toHaveCSS("z-index", "9999");
    await expect(modal).toHaveCSS("max-width", "500px");
    await expect(modal).toHaveCSS("border-radius", "8px");
    await expect(modal.locator(".error-modal-header")).toHaveCSS("display", "flex");
    await expect(modal.locator(".error-modal-close")).toHaveCSS("width", "32px");
    await expect(modal.locator(".error-modal-body")).toHaveCSS("padding", "24px");
    await expect(modal.locator(".error-modal-details")).toHaveCSS("white-space", "pre-wrap");
  });

  for (const closeWith of ["close button", "outside click", "Escape"] as const) {
    test(`closing with ${closeWith} removes the document keydown listener`, async ({ page }) => {
      await page.route("**/debug.md", route => route.fulfill({ status: 404 }));
      const copyButton = page.locator('.copy-entire-file-btn[data-filepath$="/debug.md"]');
      const listenerCount = () => page.evaluate(() => Reflect.get(window, "documentKeydownListenerCount"));
      const baseline = await listenerCount();

      // Repeat to catch listeners accumulating across multiple dialogs.
      for (let attempt = 0; attempt < 2; attempt++) {
        await copyButton.click();
        const overlay = page.locator(".error-modal-overlay");
        await expect(overlay).toHaveCount(1);
        await expect.poll(listenerCount).toBe(baseline + 1);
        await expect(overlay).toBeVisible();

        if (closeWith === "close button") {
          await overlay.getByRole("button", { name: "Close error dialog" }).click();
        } else if (closeWith === "outside click") {
          await overlay.click({ position: { x: 5, y: 5 } });
        } else {
          await page.keyboard.press("Escape");
        }

        await expect(overlay).toHaveCount(0);
        await expect.poll(listenerCount).toBe(baseline);
        await expect(copyButton).toBeEnabled();
      }
    });
  }
});
