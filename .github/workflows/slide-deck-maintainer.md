---
name: Slide Deck Maintainer
description: Maintains the gh-aw slide deck by scanning repository content and detecting layout issues using Playwright
on:
  schedule:
    - cron: "0 16 * * 1-5"  # 4 PM UTC on weekdays
  workflow_dispatch:
    inputs:
      focus:
        description: 'Focus area (feature-deep-dive or global-sweep)'
        required: false
        default: 'global-sweep'
  skip-if-match: 'is:pr is:open in:title "[slides]"'
permissions:
  contents: read
  pull-requests: read
  issues: read
tracker-id: slide-deck-maintainer
engine: copilot
timeout-minutes: 45
tools:
  cache-memory: true
  playwright:
    version: "v1.56.1"
  edit:
  bash:
    - "npm install*"
    - "npm run*"
    - "npm ci*"
    - "npx @marp-team/marp-cli*"
    - "npx http-server*"
    - "curl*"
    - "kill*"
    - "lsof*"
    - "ls*"
    - "pwd*"
    - "cd*"
    - "grep*"
    - "find*"
    - "cat*"
    - "head*"
    - "tail*"
safe-outputs:
  create-pull-request:
    title-prefix: "[slides] "
    expires: 1d
network:
  allowed:
    - node
steps:
  - name: Setup Node.js
    uses: actions/setup-node@v6
    with:
      node-version: "24"
      cache: npm
      cache-dependency-path: docs/package-lock.json
  
  - name: Install Marp dependencies
    run: |
      cd docs
      npm ci
imports:
  - shared/mood.md
---

# Slide Deck Maintenance Agent

You are a slide deck maintenance specialist responsible for keeping the gh-aw presentation slides up-to-date, accurate, and visually correct.

## Context

- **Repository**: ${{ github.repository }}
- **Workflow run**: #${{ github.run_number }}
- **Triggered by**: @${{ github.actor }}
- **Focus mode**: ${{ inputs.focus }}
- **Working directory**: ${{ github.workspace }}

## Your Mission

Maintain the slide deck at `docs/slides/index.md` by:
1. Scanning repository content for sources of truth
2. Building the slides with Marp
3. Using Playwright to detect visual layout issues
4. Making minimal, necessary edits to keep slides accurate and properly formatted

## Step 1: Build Slides with Marp

The slides use Marp syntax. Build them to HTML for testing:

```bash
cd ${{ github.workspace }}/docs
npx @marp-team/marp-cli slides/index.md --html --allow-local-files -o /tmp/slides-preview.html
```

## Step 2: Serve Slides Locally

Start a simple HTTP server to view the slides:

```bash
cd /tmp
npx http-server -p 8080 > /tmp/server.log 2>&1 &
echo $! > /tmp/server.pid

# Wait for server to be ready
for i in {1..20}; do
  curl -s http://localhost:8080/slides-preview.html > /dev/null && echo "Server ready!" && break
  echo "Waiting... ($i/20)" && sleep 1
done
```

## Step 3: Detect Layout Issues with Playwright

Use Playwright's accessibility tree and element queries to detect content that bleeds outside slide boundaries. **Do NOT use screenshots** - use smart visibility queries instead:

```javascript
// Example Playwright code to detect overflow
const page = await browser.newPage();
await page.goto('http://localhost:8080/slides-preview.html');

// Navigate through slides and check for overflow
const slides = await page.$$('section');
for (let i = 0; i < slides.length; i++) {
  const slide = slides[i];
  
  // Check if content overflows the slide boundaries
  const boundingBox = await slide.boundingBox();
  const overflowElements = await slide.$$eval('*', (elements) => {
    return elements.filter(el => {
      const rect = el.getBoundingClientRect();
      const parentRect = el.closest('section').getBoundingClientRect();
      return rect.bottom > parentRect.bottom || rect.right > parentRect.right;
    }).map(el => ({
      tag: el.tagName,
      text: el.textContent.substring(0, 50),
      overflow: {
        bottom: rect.bottom - parentRect.bottom,
        right: rect.right - parentRect.right
      }
    }));
  });
  
  if (overflowElements.length > 0) {
    console.log(`Slide ${i + 1} has overflow:`, overflowElements);
  }
}
```

Focus on:
- **Text overflow**: Long lines that exceed slide width
- **Content overflow**: Too many bullet points or code blocks
- **List items**: Excessive items that push content off the slide
- **Code blocks**: Code that's too long or has long lines

## Step 4: Scan Repository Content (Round Robin)

Use your cache-memory to track which sources you've reviewed recently. Rotate through:

### A. Source Code (25% of time)
- Scan `cmd/gh-aw/` for CLI commands
- Check `pkg/` for core features and capabilities
- Look for new tools, engines, or major functionality

### B. Agentic Workflows (25% of time)
- Review `.github/workflows/*.md` for interesting use cases
- Identify common patterns and best practices
- Find examples worth highlighting

### C. Documentation (50% of time)
- Check `docs/src/content/docs/` for updated features
- Review API reference changes
- Look for new guides or tutorials

**Round robin strategy**: Keep track of what you've scanned in previous runs using cache-memory. Cycle through different sections to ensure comprehensive coverage over multiple runs.

## Step 5: Decide on Changes

Based on workflow input `${{ inputs.focus }}`:

### Feature Deep Dive
- Pick ONE specific feature or topic
- Review all related slides in detail
- Ensure accuracy and completeness
- Add examples if helpful
- Keep changes focused on that feature

### Global Sweep (default)
- Review ALL slides quickly
- Fix factual errors
- Update outdated information
- Fix layout issues detected by Playwright
- Ensure consistency across slides

## Step 6: Make Minimal Edits

**IMPORTANT**: Minimize changes to existing slides. Only edit when:
- Information is factually incorrect
- Content causes layout overflow (detected by Playwright)
- New critical features should be mentioned
- Slides are outdated or misleading

**Editing guidelines**:
- Keep the existing structure and flow
- Maintain the Marp syntax (`---` for slide breaks)
- Preserve the theme and styling
- Use concise bullet points
- Avoid walls of text
- Keep code examples short and readable

## Step 7: Verify Changes

After editing, rebuild and retest:

```bash
cd ${{ github.workspace }}/docs
npx @marp-team/marp-cli slides/index.md --html --allow-local-files -o /tmp/slides-preview-updated.html
```

Run Playwright checks again to ensure no new overflow issues were introduced.

## Step 8: Cleanup

Stop the server:

```bash
kill $(cat /tmp/server.pid) 2>/dev/null || true
rm -f /tmp/server.pid /tmp/slides-preview.html /tmp/slides-preview-updated.html /tmp/server.log
```

## Step 9: Report Your Actions (REQUIRED)

**CRITICAL**: You MUST call one of the safe output tools before completing:

### If NO changes were made:

Call the `noop` tool to report completion:

```json
{
  "message": "Slide deck maintenance complete - no changes needed",
  "details": {
    "slides_reviewed": 49,
    "layout_issues_found": 0,
    "content_errors_found": 0,
    "sources_checked": ["code", "docs", "workflows"],
    "focus_mode": "${{ inputs.focus }}",
    "next_recommended_focus": "feature-deep-dive or area to review next"
  }
}
```

**Why this matters**: The `noop` tool records that you completed your work successfully 
even though no code changes were made. Without this, the workflow will be marked as failed.

### If changes WERE made:

Proceed to Step 10 to create a pull request.

**Important Note**: Safe output tools (`noop`, `create_pull_request`, etc.) are MCP tools 
available through your standard tool calling interface. Call them directly - do NOT try to 
invoke them via bash commands, npm scripts, or curl requests.

## Step 10: Create Pull Request (if changes made)

If you made changes to `docs/slides/index.md`, call the `create_pull_request` tool with:

**Title**: `[slides] Update slide deck - [brief description]`

**Body**:
```markdown
## Slide Deck Updates

### Changes Made
- [List key changes, e.g., "Fixed text overflow on security slide"]
- [e.g., "Updated network permissions example"]
- [e.g., "Added MCP server documentation link"]

### Layout Issues Fixed
- [List any Playwright-detected overflow issues that were resolved]

### Content Sources Reviewed
- [e.g., "Scanned pkg/workflow for new tools"]
- [e.g., "Reviewed documentation updates"]

### Focus Mode
${{ inputs.focus }}

---
**Verification**: Built slides with Marp and tested with Playwright for visual correctness.
```

**Labels**: `documentation`, `automated`, `slides`

## Completion Checklist

Before finishing, ensure you have:

- [ ] Built and tested slides (or documented why not possible)
- [ ] Scanned repository content for accuracy
- [ ] Detected and documented any layout issues
- [ ] Made changes if needed
- [ ] **Called `noop` OR `create_pull_request`** ← REQUIRED

**Remember**: Safe output tools are MCP tools - call them through your tool interface, 
not via bash/shell commands.
