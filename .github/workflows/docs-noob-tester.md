---
emoji: "📝"
name: Documentation Noob Tester
description: Tests documentation as a new user would, identifying confusing or broken steps in getting started guides
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write


engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
timeout-minutes: 30
runtimes:
  node:
    version: "22"
tools:
  cli-proxy: true
  timeout: 120  # Playwright navigation on Astro dev server can take >60s; increase to 120s
  playwright:
  edit:
  bash:
    - "*"
safe-outputs:
  upload-asset:
    max: 10
    allowed-exts: [.png, .jpg, .jpeg, .svg]
network:
  allowed:
    - defaults
    - node

imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[docs-noob-tester] "
      expires: 1d
  - shared/docs-server-lifecycle.md
  - shared/keep-it-short.md
  - shared/otlp.md
pre-agent-steps:
  - name: Install docs dependencies
    env:
      EXPR_GITHUB_WORKSPACE: ${{ github.workspace }}
    run: |
      cd "$EXPR_GITHUB_WORKSPACE/docs" || exit 1
      npm ci
  - name: Start docs server
    env:
      EXPR_GITHUB_WORKSPACE: ${{ github.workspace }}
    run: |
      mkdir -p /tmp/gh-aw/agent
      cd "$EXPR_GITHUB_WORKSPACE/docs" || exit 1
      nohup npm run dev -- --host 127.0.0.1 --port 4321 > /tmp/gh-aw/agent/preview.log 2>&1 &
      PID=$!
      echo $PID > /tmp/gh-aw/agent/server.pid
      echo "Server PID: $PID"
  - name: Wait for server readiness
    # runner-guard:ignore RGS-012 -- loopback-only port/readiness checks for the docs server started above; no external traffic or secrets are sent.
    run: |
      MAX_WAIT=135  # 45 attempts × 3s = 135s max wait
      WAITED=0
      # runner-guard:ignore RGS-012 -- loopback-only port probe for the docs server started above; no external traffic is sent.
      until (echo > /dev/tcp/127.0.0.1/4321) > /dev/null 2>&1; do
        # Check if the server process has already died
        if [ -f /tmp/gh-aw/agent/server.pid ] && ! kill -0 "$(cat /tmp/gh-aw/agent/server.pid)" 2>/dev/null; then
          echo "::error::Documentation server process died before opening port 4321. Server log:"
          cat /tmp/gh-aw/agent/preview.log
          exit 1
        fi
        WAITED=$((WAITED + 3))
        if [ $WAITED -ge $MAX_WAIT ]; then
          echo "::error::Documentation server port 4321 did not open after ${MAX_WAIT}s. Server log:"
          cat /tmp/gh-aw/agent/preview.log
          exit 1
        fi
        echo "Waiting for docs port... ($WAITED/${MAX_WAIT}s)"
        sleep 3
      done
      WAITED=0
      # runner-guard:ignore RGS-012 -- loopback-only readiness request to the docs server started above; no secrets are sent.
      until curl -sf http://localhost:4321/gh-aw/ > /dev/null 2>&1; do
        # Check if the server process has already died
        if [ -f /tmp/gh-aw/agent/server.pid ] && ! kill -0 "$(cat /tmp/gh-aw/agent/server.pid)" 2>/dev/null; then
          echo "::error::Documentation server process died before becoming ready. Server log:"
          cat /tmp/gh-aw/agent/preview.log
          exit 1
        fi
        WAITED=$((WAITED + 3))
        if [ $WAITED -ge $MAX_WAIT ]; then
          echo "::error::Documentation server did not start after ${MAX_WAIT}s. Server log:"
          cat /tmp/gh-aw/agent/preview.log
          exit 1
        fi
        echo "Waiting for server... ($WAITED/${MAX_WAIT}s)"
        sleep 3
      done
      echo "Server ready at http://localhost:4321/gh-aw/!"
  - name: Write server URL for agent
    run: |
      mkdir -p /tmp/gh-aw/agent
      echo "http://localhost:4321/gh-aw/" > /tmp/gh-aw/agent/server-url.txt
      echo "Server URL: http://localhost:4321/gh-aw/"
features:
  gh-aw-detection: true
evals:
  - id: findings-reported
    question: Did the agent report documentation testing findings (via asset upload or report)?
  - id: confusing-steps-identified
    question: Does the agent output identify at least one confusing, broken, or unclear step in the documentation?
  - id: user-perspective-maintained
    question: Does the agent output reflect the perspective of a new user rather than an expert reviewer?
---

# Documentation Noob Testing

You are a brand new user trying to get started with GitHub Agentic Workflows for the first time. Your task is to navigate through the documentation site, follow the getting started guide, and identify any confusing, broken, or unclear steps.

## Context

- Repository: ${{ github.repository }}
- Working directory: ${{ github.workspace }}
- Documentation directory: ${{ github.workspace }}/docs

## Your Mission

Act as a complete beginner who has never used GitHub Agentic Workflows before. Navigate the documentation site, follow tutorials step-by-step, and document any issues you encounter.

> The workflow started the documentation server and verified readiness. It is running at `http://localhost:4321/gh-aw/`.

## Step 1: Navigate Documentation as a Noob

**Using Playwright CLI in gh-aw Workflows**

- ✅ **Correct**: Use `playwright-cli browser_navigate --url "http://localhost:4321/gh-aw/"` to navigate
- ✅ **Correct**: Use `playwright-cli browser_run_code --code "async (page) => { await page.goto('http://localhost:4321/gh-aw/', { waitUntil: 'domcontentloaded', timeout: 30000 }); ... }"` for custom code
- ❌ **Incorrect**: Using bridge IP detection — in CLI mode, `localhost` reaches the dev server directly

**⚠️ CRITICAL: Navigation Timeout Prevention** — Always use `waitUntil: 'domcontentloaded'` to prevent timeout on the Astro development server.

Using Playwright, visit exactly these 3 pages and stop:

Before taking screenshots, create the screenshots directory:
```bash
mkdir -p /tmp/gh-aw/agent/screenshots
```

1. **Visit the home page** (`http://localhost:4321/gh-aw/`)
   - Take a screenshot: `playwright-cli browser_navigate --url "http://localhost:4321/gh-aw/" && playwright-cli browser_take_screenshot --filename /tmp/gh-aw/agent/screenshots/home.png`
   - Note: Is it immediately clear what this tool does?
   - Note: Can you quickly find the "Get Started" or "Quick Start" link?

2. **Follow the Quick Start Guide** (`http://localhost:4321/gh-aw/setup/quick-start/`)
   - Take screenshots of each major section
   - Try to understand each step from a beginner's perspective
   - Questions to consider:
     - Are prerequisites clearly listed?
     - Are installation instructions clear and complete?
     - Are there any assumed knowledge gaps?
     - Do code examples work as shown?
     - Are error messages explained?

3. **Check the CLI Commands page** (`http://localhost:4321/gh-aw/setup/cli/`)
   - Take a screenshot
   - Note: Are the most important commands highlighted?
   - Note: Are examples provided for common use cases?

After visiting all 3 pages, immediately proceed to the report.

## Step 2: Identify Pain Points

As you navigate, specifically look for:

### 🔴 Critical Issues (Block getting started)
- Missing prerequisites or dependencies
- Broken links or 404 pages
- Incomplete or incorrect code examples
- Missing critical information
- Confusing navigation structure
- Steps that don't work as described

### 🟡 Confusing Areas (Slow down learning)
- Unclear explanations
- Too much jargon without definitions
- Lack of examples or context
- Inconsistent terminology
- Assumptions about prior knowledge
- Layout or formatting issues that make content hard to read

### 🟢 Good Stuff (What works well)
- Clear, helpful examples
- Good explanations
- Useful screenshots or diagrams
- Logical flow

## Step 3: Take Screenshots

For each confusing or broken area:
- Take a screenshot showing the issue
- Save it to a descriptive filename (e.g., "confusing-quick-start-step-3.png") in `/tmp/gh-aw/agent/screenshots/`
- Note the page URL and specific section
- Upload the screenshot by calling the `upload_asset` safe-output tool with the absolute file path `path: "/tmp/gh-aw/agent/screenshots/<filename>.png"`.
  Record the returned asset URL.

## Step 4: Create Discussion Report

Create a GitHub discussion titled "📚 Documentation Noob Test Report - [Date]" with:

### Summary
- Date of test: [Today's date]
- Pages visited: [List URLs]
- Overall impression: [1-2 sentences as a new user]

### Critical Issues Found
[List any blocking issues with screenshots]

### Confusing Areas
[List confusing sections with explanations and screenshots]

### What Worked Well
[Positive feedback on clear sections]

### Recommendations
- Prioritized suggestions for improving the getting started experience
- Quick wins that would help new users immediately
- Longer-term documentation improvements

### Screenshots
For each uploaded screenshot, include its asset URL. Format:
```
📎 **[filename.png]** — asset URL: https://github.com/.../blob/.../filename.png?raw=true
```

Label the discussion with: `documentation`, `user-experience`, `automated-testing`

## Step 5: Cleanup

Follow the shared **Documentation Server Lifecycle Management** instructions for cleanup (section "Stopping the Documentation Server").

## Guidelines

- **Be genuinely naive**: Don't assume knowledge of Git, GitHub Actions, or AI workflows
- **Document everything**: Even minor confusion points matter
- **Be specific**: "This is confusing" is less helpful than "I don't understand what 'frontmatter' means"
- **Be constructive**: Focus on helping improve the docs, not just criticizing
- **Be thorough but efficient**: Cover key getting started paths without testing every single page
- **Take good screenshots**: Make sure they clearly show the issue

## Success Criteria

You've successfully completed this task if you:
- Navigated exactly 3 key documentation pages
- Identified specific pain points with examples
- Provided actionable recommendations
- Created a discussion with clear findings and screenshots