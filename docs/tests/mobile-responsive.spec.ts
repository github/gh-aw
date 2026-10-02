import { test, expect } from '@playwright/test';

test.describe('Mobile and Responsive Layout', () => {
  const formFactors = [
    { name: '360px Mobile', width: 360, height: 800 },
    { name: 'iPhone 16 (Mobile)', width: 393, height: 852 },
    { name: '412px Mobile', width: 412, height: 915 },
    { name: '428px Mobile', width: 428, height: 926 },
    { name: 'iPad (768px)', width: 768, height: 1024 },
    { name: 'iPad Pro 11 (834px)', width: 834, height: 1194 },
    { name: 'iPad Pro 12.9 Portrait (1024px)', width: 1024, height: 1366 },
    { name: 'iPad Landscape (1024px)', width: 1024, height: 768 },
    { name: 'Desktop Portrait', width: 1080, height: 1920 },
    { name: 'Desktop Landscape', width: 1920, height: 1080 },
  ];

  const pages = [
    { url: '/gh-aw/', name: 'home page' },
    { url: '/gh-aw/introduction/overview/', name: 'content page' },
  ];

  test('should include markdown table data-label attributes without JavaScript', async ({ browser }) => {
    const context = await browser.newContext({
      javaScriptEnabled: false,
      viewport: { width: 393, height: 852 },
    });
    const page = await context.newPage();

    await page.goto('/gh-aw/reference/engines/');
    await page.waitForLoadState('domcontentloaded');

    const firstTableCell = page.locator('.sl-markdown-content table tbody td').first();
    await expect(firstTableCell).toBeVisible();
    await expect(firstTableCell).toHaveAttribute('data-label', 'AI engine');

    await context.close();
  });

  test('should wrap markdown tables in a scroll wrapper without JavaScript', async ({ browser }) => {
    const context = await browser.newContext({
      javaScriptEnabled: false,
      viewport: { width: 768, height: 1024 },
    });
    const page = await context.newPage();

    await page.goto('/gh-aw/reference/engines/');
    await page.waitForLoadState('domcontentloaded');

    // The rehype plugin should have added the wrapper div at build time
    const wrapper = page.locator('.sl-markdown-content .table-scroll-wrapper').first();
    await expect(wrapper).toBeVisible();

    // The table must be a direct child of the wrapper
    const tableInWrapper = page.locator('.sl-markdown-content .table-scroll-wrapper > table').first();
    await expect(tableInWrapper).toBeVisible();

    await context.close();
  });

  test('should wrap ALL markdown tables in a scroll wrapper on the engines reference page', async ({ browser }) => {
    const context = await browser.newContext({
      javaScriptEnabled: false,
      viewport: { width: 768, height: 1024 },
    });
    const page = await context.newPage();

    await page.goto('/gh-aw/reference/engines/');
    await page.waitForLoadState('domcontentloaded');

    // Count all tables in markdown content area
    const tableCount = await page.locator('.sl-markdown-content table').count();
    expect(tableCount).toBeGreaterThan(0);

    // Count tables that are direct children of .table-scroll-wrapper
    const wrappedTableCount = await page.locator('.sl-markdown-content .table-scroll-wrapper > table').count();

    // Every table must have a scroll wrapper for consistent horizontal scrolling on all viewports
    expect(wrappedTableCount).toBe(tableCount);

    await context.close();
  });

  test('should have WCAG 2.5.5-compliant touch target size for mobile table cells', async ({ browser }) => {
    const context = await browser.newContext({
      javaScriptEnabled: true,
      viewport: { width: 390, height: 844 },
    });
    const page = await context.newPage();

    await page.goto('/gh-aw/reference/engines/');
    await page.waitForLoadState('networkidle');

    // On mobile (<=640px), table cells are rendered as stacked cards (label above value).
    // Each cell's rendered height must meet the WCAG 2.5.5 AAA minimum touch target of 44 px.
    // Measure the rendered height rather than min-height: the stacked layout reaches it through
    // its label, line height and padding, so no explicit min-height is needed.
    const shortestCell = await page.evaluate(() => {
      const cells = Array.from(document.querySelectorAll('.sl-markdown-content table tbody td'));
      if (cells.length === 0) return 0;
      return Math.min(...cells.map((td) => td.getBoundingClientRect().height));
    });

    expect(shortestCell).toBeGreaterThanOrEqual(44);

    await context.close();
  });

  test('should expose a functional home page skip link target', async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 800 });
    await page.goto('/gh-aw/');
    await page.waitForLoadState('networkidle');

    const skipLink = page.locator('a[href="#starlight__main"]');
    await expect(skipLink).toHaveCount(1);
    await expect(page.locator('main#starlight__main')).toBeVisible();
    await expect(page.locator('#main-content')).toHaveCount(1);
  });

  // Below 50rem (800px) the header collapses to logo, search and a menu button
  // that opens one sheet; at 800px and up the nav and Get started sit in the header.
  for (const viewport of [
    { name: 'mobile', width: 390, height: 844, menu: true },
    { name: 'tablet', width: 768, height: 1024, menu: true },
    { name: 'large tablet', width: 834, height: 1194, menu: false },
    { name: 'tablet landscape', width: 1024, height: 768, menu: false },
  ]) {
    test(`should navigate through the responsive header on ${viewport.name}`, async ({ page }) => {
      await page.setViewportSize({ width: viewport.width, height: viewport.height });
      await page.goto('/gh-aw/');
      await page.waitForLoadState('networkidle');

      const menuButton = page.locator('.site-menu-toggle');
      let quickStartLink;
      if (viewport.menu) {
        await expect(menuButton).toBeVisible();
        await menuButton.click();
        await expect(menuButton).toHaveAttribute('aria-expanded', 'true');
        quickStartLink = page.locator('#site-menu .mobile-menu-cta[href$="setup/quick-start/"]');
      } else {
        await expect(menuButton).toBeHidden();
        await expect(page.locator('.site-header-nav')).toBeVisible();
        quickStartLink = page.locator('.site-header-cta[href$="setup/quick-start/"]');
      }

      await expect(quickStartLink).toBeVisible();
      await quickStartLink.click();
      await expect(page).toHaveURL(/\/gh-aw\/setup\/quick-start\/$/);
    });
  }

  for (const formFactor of formFactors) {
    test.describe(`${formFactor.name}`, () => {
      test.beforeEach(async ({ page }) => {
        await page.setViewportSize({ 
          width: formFactor.width, 
          height: formFactor.height 
        });
      });

      for (const testPage of pages) {
        test(`should render ${testPage.name} correctly`, async ({ page }) => {
          await page.goto(testPage.url);
          await page.waitForLoadState('networkidle');

          // Verify page loads
          await expect(page).toHaveTitle(/GitHub Agentic Workflows/);

          // Verify the site header is visible (landing sections use <header> too)
          const header = page.locator('header.header');
          await expect(header).toBeVisible();

          // Verify main content is visible
          const main = page.locator('main');
          await expect(main).toBeVisible();

          // Check for horizontal scrollbar (should not exist)
          const scrollMetrics = await page.evaluate(() => ({
            scrollWidth: document.scrollingElement?.scrollWidth ?? document.documentElement.scrollWidth,
            clientWidth: document.scrollingElement?.clientWidth ?? document.documentElement.clientWidth,
          }));
          expect(scrollMetrics.scrollWidth).toBeLessThanOrEqual(scrollMetrics.clientWidth + 1); // Allow 1px tolerance
        });
      }

      test('should have proper content spacing on mobile', async ({ page }) => {
        if (formFactor.width < 800) {
          await page.goto('/gh-aw/introduction/overview/');
          await page.waitForLoadState('networkidle');

          // Content should have proper padding
          const contentPanel = page.locator('.content-panel').first();
          await expect(contentPanel).toBeVisible();

          // Sidebar should be hidden on mobile (below 50rem / 800px) until the menu opens
          const sidebar = page.locator('#starlight__sidebar');
          await expect(sidebar).not.toBeVisible();
        }
      });

      test('should show persistent sidebar on large tablet and desktop', async ({ page }) => {
        if (formFactor.width >= 800) {
          await page.goto('/gh-aw/introduction/overview/');
          await page.waitForLoadState('networkidle');

          // Sidebar is persistent from Starlight's 50rem (800px) breakpoint up, and the
          // mobile menu button is gone
          const sidebar = page.locator('#starlight__sidebar');
          await expect(sidebar).toBeVisible();
          await expect(page.locator('.site-menu-toggle')).toBeHidden();
        }
      });
    });
  }

  // Regression test for https://github.com/github/gh-aw/issues/45211
  // Verify the site-title link is not obstructed by overflowing nav links on
  // iPad Pro 12.9 (1024px portrait) where the 7-item full nav used to overflow.
  test('site-title link is unobstructed at iPad Pro 12.9 (1024px) width', async ({ browser }) => {
    const context = await browser.newContext({
      viewport: { width: 1024, height: 1366 },
      javaScriptEnabled: true,
    });
    const page = await context.newPage();

    await page.goto('/gh-aw/');
    await page.waitForLoadState('networkidle');

    // At 1024px the compact three-link nav shows and the mobile menu button is hidden.
    const fullNav = page.locator('.site-header-nav');
    await expect(fullNav).toBeVisible();
    await expect(fullNav.locator('a')).toHaveCount(3);

    const menuButton = page.locator('.site-menu-toggle');
    await expect(menuButton).toBeHidden();

    // The site-title must be visible and its bounding box must not be covered by
    // any sibling element (i.e. the element at its centre must be the title itself
    // or a descendant of it).
    const siteTitle = page.locator('.site-title').first();
    await expect(siteTitle).toBeVisible();

    const box = await siteTitle.boundingBox();
    expect(box).not.toBeNull();
    if (box) {
      const centerX = box.x + box.width / 2;
      const centerY = box.y + box.height / 2;
      const isUnobstructed = await page.evaluate(
        ([x, y]) => {
          const el = document.elementFromPoint(x, y);
          if (!el) return false;
          const titleEl = document.querySelector('.site-title');
          return titleEl ? titleEl.contains(el) || el === titleEl : false;
        },
        [centerX, centerY] as [number, number],
      );
      expect(isUnobstructed).toBe(true);
    }

    await context.close();
  });

  // Regression test for https://github.com/github/gh-aw/issues/51015
  // The Galaxy S21 (360px) width is the narrowest tested device. Verify the
  // site-title logo icon stays fully visible within the header even though
  // its invisible 44x44 touch-target padding is clipped by the shrunk
  // title-wrapper flex item at this width.
  test('site-title logo stays visible at the narrowest tested width (360px)', async ({ browser }) => {
    const context = await browser.newContext({ viewport: { width: 360, height: 800 } });
    const page = await context.newPage();

    await page.goto('/gh-aw/');
    await page.waitForLoadState('networkidle');

    const logo = page.locator('.site-title img').first();
    await expect(logo).toBeVisible();

    const logoBox = await logo.boundingBox();
    expect(logoBox).not.toBeNull();
    if (logoBox) {
      // The logo's own visible box (not the larger invisible touch-target
      // padding around it) must be entirely within the viewport.
      expect(logoBox.x).toBeGreaterThanOrEqual(0);
      expect(logoBox.x + logoBox.width).toBeLessThanOrEqual(360);
    }

    await context.close();
  });

  // Regression test for https://github.com/github/gh-aw/issues/51015
  // The theme toggle uses a visually-hidden native <select> for progressive
  // enhancement/accessibility. Native <select> elements report their
  // intrinsic option content via `scrollWidth` regardless of author CSS
  // (browsers do not let `overflow` clip a form control's internal content),
  // so that metric cannot be constrained here. What we *can* and must
  // guarantee is that the control's actual rendered footprint on screen
  // stays effectively invisible and doesn't push into the visible layout.
  test('hidden theme-select control has no visible footprint', async ({ browser }) => {
    const context = await browser.newContext({ viewport: { width: 360, height: 800 } });
    const page = await context.newPage();

    await page.goto('/gh-aw/');
    await page.waitForLoadState('networkidle');

    // Below 50rem the theme control lives in the menu sheet, so open it first.
    await page.locator('.site-menu-toggle').click();
    const select = page.locator('#site-menu starlight-theme-select select');
    await expect(select).toHaveCount(1);

    // The control is styled to `width: 1px; height: 1px`. Allow a small
    // tolerance for sub-pixel rounding across browser engines rather than
    // asserting an exact 1px match.
    const NEAR_ZERO_PX_THRESHOLD = 4;
    const box = await select.boundingBox();
    expect(box).not.toBeNull();
    if (box) {
      expect(box.width).toBeLessThanOrEqual(NEAR_ZERO_PX_THRESHOLD);
      expect(box.height).toBeLessThanOrEqual(NEAR_ZERO_PX_THRESHOLD);
    }

    await context.close();
  });

  // Regression test for https://github.com/github/gh-aw/issues/29545
  // Verify the navigation dropdown is fully within the viewport when large
  // user fonts cause header elements to shift on Android Chrome.
  test('mobile menu sheet stays within viewport with large user fonts', async ({ browser }) => {
    const VIEWPORT_WIDTH = 393;
    const context = await browser.newContext({
      // Simulate Android Chrome with the user's accessibility font size set to
      // "Large" — typically 1.3× the default, so override the page root font-size.
      viewport: { width: VIEWPORT_WIDTH, height: 852 },
      javaScriptEnabled: true,
    });
    const page = await context.newPage();

    await page.goto('/gh-aw/introduction/overview/');
    await page.waitForLoadState('networkidle');

    // Simulate large OS-level font scaling by overriding the root font size.
    // Done after navigation so the document exists and the style tag can attach.
    await page.addStyleTag({ content: 'html { font-size: 20px !important; }' });

    // The menu button should be visible on a narrow mobile viewport.
    const menuButton = page.locator('.site-menu-toggle');
    await expect(menuButton).toBeVisible();

    // Open the menu sheet (Starlight's sidebar popover on docs pages).
    await menuButton.click();

    const dropdown = page.locator('#starlight__sidebar');
    await expect(dropdown).toBeVisible();

    // The sheet must be fully within the viewport horizontally.
    const dropdownBox = await dropdown.boundingBox();
    expect(dropdownBox).not.toBeNull();
    if (dropdownBox) {
      expect(dropdownBox.x).toBeGreaterThanOrEqual(0);
      expect(dropdownBox.x + dropdownBox.width).toBeLessThanOrEqual(VIEWPORT_WIDTH + 1); // 1px tolerance
    }

    await context.close();
  });

  // Verify mobile navigation toggle: the menu sheet's site links become visible on narrow viewports.
  // Addresses the manual verification recommendation from the 2026-06-24 multi-device docs test report.
  test('menu button toggles navigation visibility on mobile viewport', async ({ browser }) => {
    const context = await browser.newContext({
      viewport: { width: 390, height: 844 },
      javaScriptEnabled: true,
    });
    const page = await context.newPage();

    await page.goto('/gh-aw/introduction/overview/');
    await page.waitForLoadState('networkidle');

    // The menu button must be present and focusable on a narrow mobile viewport.
    const menuButton = page.locator('.site-menu-toggle');
    await expect(menuButton).toBeVisible();
    await expect(menuButton).toHaveAttribute('aria-expanded', 'false');
    await expect(menuButton).toHaveAttribute('aria-controls', 'starlight__sidebar');

    // The sheet must be hidden before the button is clicked.
    const sheet = page.locator('#starlight__sidebar');
    await expect(sheet).toBeHidden();

    // Click the button; the sheet must become visible with the site links on top
    // and the docs tree below, in one menu.
    await menuButton.click();
    await expect(menuButton).toHaveAttribute('aria-expanded', 'true');
    await expect(sheet).toBeVisible();

    const navLinks = sheet.locator('.mobile-nav a');
    await expect(navLinks).toHaveCount(3);
    for (const link of await navLinks.all()) {
      await expect(link).toBeVisible();
    }
    await expect(sheet.locator('.mobile-nav a[aria-current="page"]')).toHaveText('Docs');
    await expect(sheet.locator('ul.top-level').first()).toBeVisible();

    // A second click must close the sheet.
    await menuButton.click();
    await expect(menuButton).toHaveAttribute('aria-expanded', 'false');
    await expect(sheet).toBeHidden();

    // Esc closes it too and returns focus to the button.
    await menuButton.click();
    await expect(sheet).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(sheet).toBeHidden();
    await expect(menuButton).toHaveAttribute('aria-expanded', 'false');
    await expect(menuButton).toBeFocused();

    await context.close();
  });

  // Regression test for the 2026-08-08 multi-device docs test report.
  // The home page primary CTA must stay tappable on mobile breakpoints,
  // including after the navigation menu has been opened and dismissed, so that
  // no leftover overlay intercepts touch or keyboard activation.
  const mobileCtaViewports = [
    { name: '360px Mobile', width: 360, height: 800 },
    { name: 'iPhone 16 (Mobile)', width: 393, height: 852 },
    { name: '428px Mobile', width: 428, height: 926 },
  ];

  for (const viewport of mobileCtaViewports) {
    test(`home page primary CTA stays tappable after opening and dismissing the menu at ${viewport.name}`, async ({
      browser,
    }) => {
      const context = await browser.newContext({
        viewport: { width: viewport.width, height: viewport.height },
        javaScriptEnabled: true,
      });
      const page = await context.newPage();

      await page.goto('/gh-aw/');
      await page.waitForLoadState('networkidle');

      const cta = page.locator('.aw-hero .aw-cta-row a.aw-btn').first();
      await expect(cta).toBeVisible();
      await expect(cta).toHaveAttribute('href', '/gh-aw/setup/creating-workflows/');

      // The CTA must be the topmost element at its centre point, i.e. nothing
      // (hero canvas, overlay, sticky header) intercepts the tap.
      const expectCtaHittable = async () => {
        await expect(cta).toBeInViewport();
        const box = await cta.boundingBox();
        expect(box).not.toBeNull();
        if (!box) return;
        const isTopmost = await page.evaluate(
          ([x, y]) => {
            const el = document.elementFromPoint(x, y);
            const ctaEl = document.querySelector('.aw-hero .aw-cta-row a.aw-btn');
            if (!el || !ctaEl) return false;
            return el === ctaEl || ctaEl.contains(el);
          },
          [box.x + box.width / 2, box.y + box.height / 2] as [number, number],
        );
        expect(isTopmost).toBe(true);
      };

      await expectCtaHittable();

      // Open the mobile navigation menu, then dismiss it with Escape.
      const menuButton = page.locator('.site-menu-toggle');
      await expect(menuButton).toBeVisible();
      await menuButton.click();

      const sheet = page.locator('#site-menu');
      await expect(sheet).toBeVisible();

      await page.keyboard.press('Escape');
      await expect(sheet).toBeHidden();
      await expect(menuButton).toHaveAttribute('aria-expanded', 'false');

      // After dismissal the CTA must still be tappable and must navigate.
      await expectCtaHittable();
      await cta.click();
      await page.waitForLoadState('networkidle');
      await expect(page).toHaveURL(/\/gh-aw\/setup\/creating-workflows\//);

      await context.close();
    });
  }
});
