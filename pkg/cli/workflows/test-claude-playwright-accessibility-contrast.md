---
on:
  workflow_dispatch:
permissions:
  issues: read
engine: claude
tools:
  playwright:
    allowed_domains: ["github.com", "*.github.com"]
safe-outputs:
  create-issue:
    title-prefix: "[Accessibility] "
    labels: [accessibility, contrast]
    max: 1
---

# Test Playwright Accessibility Contrast

This is a test workflow that uses Playwright to take a screenshot of github.com/github/gh-aw and analyzes it for accessibility color contrast issues.

Please:
1. Navigate to https://github.com/github/gh-aw using Playwright
2. Take a screenshot of the page
3. Analyze the screenshot for color contrast accessibility issues
4. Check if text elements meet WCAG 2.1 AA contrast requirements (4.5:1 for normal text, 3:1 for large text)
5. If any contrast issues are found, create an issue using the safe-outputs with:
   - Title: "[Accessibility] Color contrast issues found on gh-aw repository page"
   - Body: Detailed description of the contrast issues found, including specific elements and their contrast ratios
   - Include the screenshot as evidence

Focus on critical elements like:
- Main navigation text
- README content text
- Button text and backgrounds
- Link colors against backgrounds
- Code snippet text in the README

If no accessibility issues are found, report that the page passes contrast accessibility checks.