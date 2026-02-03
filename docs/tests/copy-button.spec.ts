import { test, expect } from '@playwright/test';

test.describe('Copy Entire File Button', () => {
  test('should successfully copy file content when clicked', async ({ page, context }) => {
    // Grant clipboard permissions
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);

    // Navigate to the agentic authoring page
    await page.goto('/gh-aw/tools/creating-workflows/');
    
    // Debug: print URL and take screenshot
    console.log('Current URL:', page.url());
    await page.screenshot({ path: 'test-debug-screenshot.png', fullPage: true });

    // Wait for the page to be fully loaded
    await page.waitForLoadState('networkidle');

    // Find the "Copy full instructions" button
    const copyButton = page.locator('.copy-entire-file-btn', {
      hasText: 'Copy full instructions',
    });

    // Ensure the button is visible
    await expect(copyButton).toBeVisible();

    // Click the copy button
    await copyButton.click();

    // Wait for the button text to change to "Copying…"
    await expect(copyButton.locator('.btn-text')).toHaveText('Copying…');

    // Wait for the button text to change to "Copied!"
    await expect(copyButton.locator('.btn-text')).toHaveText('Copied!', {
      timeout: 10000,
    });

    // Verify that content was copied to clipboard
    const clipboardContent = await page.evaluate(() =>
      navigator.clipboard.readText()
    );

    // Verify the clipboard contains the dictation instructions
    expect(clipboardContent).toContain('# Dictation Instructions');
    expect(clipboardContent).toContain('Fix text-to-speech errors');
    expect(clipboardContent).toContain('Project Glossary');

    // Wait for the button to reset to original text
    await expect(copyButton.locator('.btn-text')).toHaveText(
      'Copy full instructions',
      { timeout: 3000 }
    );
  });

  test('should handle network errors gracefully', async ({ page, context }) => {
    // Grant clipboard permissions
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);

    // Navigate to the agentic authoring page
    await page.goto('/gh-aw/tools/creating-workflows/');

    // Wait for the page to be fully loaded
    await page.waitForLoadState('networkidle');

    // Intercept the fetch request and make it fail
    await page.route('**/*.instructions.md', (route) => {
      route.abort('failed');
    });

    // Find the "Copy full instructions" button
    const copyButton = page.locator('.copy-entire-file-btn', {
      hasText: 'Copy full instructions',
    });

    // Ensure the button is visible
    await expect(copyButton).toBeVisible();

    // Click the copy button
    await copyButton.click();

    // Wait for the button text to change to "Error"
    await expect(copyButton.locator('.btn-text')).toHaveText('Error', {
      timeout: 5000,
    });

    // Check that the error modal is displayed
    const errorModal = page.locator('.error-modal');
    await expect(errorModal).toBeVisible({ timeout: 2000 });
    
    // Verify modal content
    await expect(errorModal.locator('#error-modal-title')).toContainText('Error Copying File');
    await expect(errorModal.locator('.error-modal-body')).toContainText('An error occurred');
    
    // Close the modal by clicking the close button
    await errorModal.locator('.error-modal-close').click();
    
    // Verify modal is closed
    await expect(errorModal).not.toBeVisible();

    // Wait for the button to reset to original text
    await expect(copyButton.locator('.btn-text')).toHaveText(
      'Copy full instructions',
      { timeout: 4000 }
    );
  });

  test('should handle HTTP error responses', async ({ page, context }) => {
    // Grant clipboard permissions
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);

    // Navigate to the agentic authoring page
    await page.goto('/gh-aw/tools/creating-workflows/');

    // Wait for the page to be fully loaded
    await page.waitForLoadState('networkidle');

    // Intercept the fetch request and return 404
    await page.route('**/*.instructions.md', (route) => {
      route.fulfill({
        status: 404,
        body: 'Not Found',
      });
    });

    // Find the "Copy full instructions" button
    const copyButton = page.locator('.copy-entire-file-btn', {
      hasText: 'Copy full instructions',
    });

    // Ensure the button is visible
    await expect(copyButton).toBeVisible();

    // Click the copy button
    await copyButton.click();

    // Wait for the button text to change to "Error"
    await expect(copyButton.locator('.btn-text')).toHaveText('Error', {
      timeout: 5000,
    });

    // Check that the error modal is displayed
    const errorModal = page.locator('.error-modal');
    await expect(errorModal).toBeVisible({ timeout: 2000 });
    
    // Verify modal contains error details
    await expect(errorModal.locator('.error-modal-details')).toContainText('Failed to fetch file: 404');
    
    // Close the modal by clicking outside
    await page.locator('.error-modal-overlay').click({ position: { x: 5, y: 5 } });
    
    // Verify modal is closed
    await expect(errorModal).not.toBeVisible();

    // Wait for the button to reset to original text
    await expect(copyButton.locator('.btn-text')).toHaveText(
      'Copy full instructions',
      { timeout: 4000 }
    );
  });
});
