---
private: true
emoji: "📝"
name: Multi-Device Docs Tester
description: Tests documentation site functionality and responsive design across multiple device form factors
on:
  schedule: daily
  workflow_dispatch:
    inputs:
      devices:
        description: 'Device types to test (comma-separated: mobile,tablet,desktop)'
        required: false
        default: 'mobile,tablet,desktop'
permissions:
  contents: read
  issues: read
  pull-requests: read

sandbox:
  agent:
    id: awf
tracker-id: daily-multi-device-docs-tester
max-turns: 80  # 10 devices × ~5 turns each + setup/report overhead
model: openai/gpt-5.4
engine:
  id: pi
  model-provider: openai
strict: true
timeout-minutes: 30
runtimes:
  node:
    version: "24"
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
  timeout: 120  # Multi-device runs include preview startup and Playwright tests
  playwright:
  bash:
    - "*"
safe-outputs:
  upload-artifact:
    max-uploads: 3
    retention-days: 30
    skip-archive: true
    defaults:
      if-no-files: ignore
  create-issue:
    expires: 2d
    labels: [cookie]

network:
  allowed:
    - node
    - chrome
    - playwright

imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[multi-device-docs] "
      expires: 3d

  - shared/otlp.md
pre-agent-steps:
  - name: Checkout agentics workflows
    uses: actions/checkout@v7
    with:
      repository: githubnext/agentics
      path: .agentics
      persist-credentials: false
      sparse-checkout: workflows
  - name: Resolve slide deck PDF
    env:
      EXPR_GITHUB_WORKSPACE: ${{ github.workspace }}
    run: |
      cd "$EXPR_GITHUB_WORKSPACE/docs" || exit 1
      node ../scripts/ensure-docs-slide-pdf.js
  - name: Configure Playwright CLI launch options
    env:
      EXPR_GITHUB_WORKSPACE: ${{ github.workspace }}
    run: |
      mkdir -p "$EXPR_GITHUB_WORKSPACE/.playwright"
      cat > "$EXPR_GITHUB_WORKSPACE/.playwright/cli.config.json" <<'EOF'
      {
        "browser": {
          "launchOptions": {
            "chromiumSandbox": false,
            "args": ["--no-sandbox", "--disable-setuid-sandbox"]
          }
        }
      }
      EOF
  - name: Playwright browser launch preflight
    id: playwright-preflight
    env:
      EXPR_GITHUB_WORKSPACE: ${{ github.workspace }}
    run: |
      PREFLIGHT_LOG="$EXPR_GITHUB_WORKSPACE/.playwright/preflight.log"
      set +e
      playwright-cli open --config "$EXPR_GITHUB_WORKSPACE/.playwright/cli.config.json" about:blank > "$PREFLIGHT_LOG" 2>&1
      PREFLIGHT_STATUS=$?
      playwright-cli close >> "$PREFLIGHT_LOG" 2>&1 || true
      if [ $PREFLIGHT_STATUS -ne 0 ]; then
        echo "preflight_failed=1" >> "$GITHUB_OUTPUT"
        echo "preflight_log=$PREFLIGHT_LOG" >> "$GITHUB_OUTPUT"
        echo "Playwright preflight failed; agent will report infrastructure blocker separately."
      else
        echo "preflight_failed=0" >> "$GITHUB_OUTPUT"
      fi
  - name: Install and build docs
    env:
      EXPR_GITHUB_WORKSPACE: ${{ github.workspace }}
      AGENTICS_WORKFLOWS_DIR: ${{ github.workspace }}/.agentics/workflows
    run: |
      cd "$EXPR_GITHUB_WORKSPACE/docs" || exit 1
      npm install
      npm run build
features:
  gh-aw-detection: true
evals:
  - id: device_tests_completed
    question: Did the agent test the documentation site across the requested device form factors?
  - id: results_reported
    question: Did the agent report the multi-device test results and any responsive design or functionality findings?
---

{{#runtime-import? .github/shared-instructions.md}}

# Multi-Device Documentation Testing

You are a documentation testing specialist. Your task is to comprehensively test the documentation site across multiple devices and form factors.

## Context

- Repository: ${{ github.repository }}
- Triggered by: @${{ github.actor }}
- Devices to test: ${{ inputs.devices }}
- Working directory: ${{ github.workspace }}

**🚨 MANDATORY: You MUST call either `noop` or `create-issue` before exiting, regardless of outcome.**
This workflow has `strict: true` — it will fail if no safe output is produced. Call `noop` if all tests pass (or if testing could not be completed for any reason), and `create-issue` if problems are found. Do this as your LAST action before finishing.

**IMPORTANT SETUP NOTES:**
1. You're already in the repository root
2. The docs folder is at: `${{ github.workspace }}/docs`
3. Use absolute paths or change directory explicitly
4. Keep token usage low by being efficient with your code and minimizing iterations
5. **Playwright is available as `playwright-cli` commands in bash** — use `playwright-cli <command>` to automate the browser
6. Use this Playwright config for every browser command: `${{ github.workspace }}/.playwright/cli.config.json`
7. If `${{ github.workspace }}/.playwright/preflight.log` contains a Chromium startup error, treat this run as an infrastructure blocker (not a docs regression)

## Your Mission

Start the documentation preview server and perform comprehensive multi-device testing. Test layout responsiveness, accessibility, interactive elements, and visual rendering across all device types. Use a single Playwright browser instance for efficiency.

## Step 1: Start Server

The docs dependencies are already installed and the site is already built. Start the Astro preview server inside this container:

```bash
cd "${{ github.workspace }}/docs"
LOG_FILE="/tmp/docs-server.log"
nohup npm run preview -- --port 4321 > "$LOG_FILE" 2>&1 &
echo "Server PID: $!, log: $LOG_FILE"
```

Then wait for the server to be ready:

```bash
LOG_FILE="/tmp/docs-server.log"
MAX_WAIT=120
WAITED=0
until curl -sf http://localhost:4321/gh-aw/ > /dev/null 2>&1; do
  WAITED=$((WAITED + 3))
  if [ $WAITED -ge $MAX_WAIT ]; then
    echo "Server log:" && cat "$LOG_FILE"
    echo "ERROR: Server did not start after ${MAX_WAIT}s" && exit 1
  fi
  sleep 3
done
echo "Docs server ready at http://localhost:4321/gh-aw/"
```

## Step 2: Device Configuration

Test these device types based on input `${{ inputs.devices }}`:

**Mobile:** iPhone 12 (390x844), iPhone 12 Pro Max (428x926), Pixel 5 (393x851), Galaxy S21 (360x800)
**Tablet:** iPad (768x1024), iPad Pro 11 (834x1194), iPad Pro 12.9 (1024x1366)
**Desktop:** HD (1366x768), FHD (1920x1080), 4K (2560x1440)

## Step 3: Run Playwright Tests

**Using Playwright in gh-aw Workflows (CLI mode)**

Playwright is pre-installed as `@playwright/cli`. Use `playwright-cli <command>` in bash — no MCP tools or Docker container is involved:

- ✅ **Correct**: `playwright-cli open --config "${{ github.workspace }}/.playwright/cli.config.json" "http://localhost:4321/gh-aw/"`
- ✅ **Correct**: `playwright-cli goto "http://localhost:4321/gh-aw/"`
- ✅ **Correct**: Use `playwright-cli run-code "async page => { ... }"` for custom Playwright code
- ❌ **Incorrect**: Do NOT use `playwright-cli browser_*` command names in this workflow (they are MCP tool names, not playwright-cli commands)
- ❌ **Incorrect**: Do NOT try to `require('playwright')` or create standalone Node.js scripts
- ❌ **Incorrect**: Do NOT use `mcp__playwright__*` tool names — those are the deprecated MCP mode

**⚠️ CRITICAL: Navigation Timeout Prevention**

Use `waitUntil: 'domcontentloaded'` for navigation to keep checks fast and consistent:

```bash
playwright-cli run-code "async page => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('http://localhost:4321/gh-aw/', { waitUntil: 'domcontentloaded', timeout: 30000 });
  return { url: page.url(), title: await page.title() };
}"
```

Before device testing, run this preflight gate:

```bash
PREFLIGHT_LOG="${{ github.workspace }}/.playwright/preflight.log"
if [ -f "$PREFLIGHT_LOG" ] && grep -qi "error\|failed\|operation not permitted" "$PREFLIGHT_LOG"; then
  echo "Playwright preflight failed before docs checks. See $PREFLIGHT_LOG"
  cat "$PREFLIGHT_LOG"
  # Call noop and stop. Do not classify this as a documentation regression.
fi
```

For each device viewport, use playwright-cli to:
- Open browser with `--config "${{ github.workspace }}/.playwright/cli.config.json"` once per run
- Set viewport size and navigate to `http://localhost:4321/gh-aw/`
- Take screenshots and run accessibility audits
- Test interactions (navigation, search, buttons)
- Check for layout issues (overflow, truncation, broken layouts)

For mobile and tablet viewports (width ≤1050px), test the responsive header navigation explicitly:
- Click `.hamburger-btn` and verify its `aria-expanded` attribute is `true`.
- Target a visible `.tablet-dropdown .dropdown-link[href$="setup/quick-start/"]:visible` rather than a generic `nav a[href]`, which can select the hidden desktop navigation.
- Click the visible link and verify that it navigates to its expected URL.

## Step 4: Analyze Results

Organize findings by severity:
- 🔴 **Critical**: Blocks functionality or major accessibility issues
- 🟡 **Warning**: Minor issues or potential problems
- 🟢 **Passed**: Everything working as expected

## Step 5: Report Results

### If NO Issues Found

**YOU MUST CALL** the `noop` tool to log completion:

```json
{
  "noop": {
    "message": "Multi-device documentation testing complete. All {device_count} devices tested successfully with no issues found."
  }
}
```

**DO NOT just write this message in your output text** - you MUST actually invoke the `noop` tool. The workflow will fail if you don't call it.

### If Issues ARE Found

Create a GitHub issue titled "🔍 Multi-Device Docs Testing Report - [Date]" with:

```markdown
### Test Summary
- Triggered by: @${{ github.actor }}
- Workflow run: [§${{ github.run_id }}](https://github.com/${{ github.repository }}/actions/runs/${{ github.run_id }})
- Devices tested: {count}
- Test date: [Date]

### Results Overview
- 🟢 Passed: {count}
- 🟡 Warnings: {count}
- 🔴 Critical: {count}

### Critical Issues
[List critical issues that block functionality or major accessibility problems - keep visible]

<details>
<summary>View All Warnings</summary>

[Minor issues and potential problems with device names and details]

</details>

<details>
<summary>View Detailed Test Results by Device</summary>

#### Mobile Devices
[Test results, screenshots, findings]

#### Tablet Devices
[Test results, screenshots, findings]

#### Desktop Devices
[Test results, screenshots, findings]

</details>

### Accessibility Findings
[Key accessibility issues - keep visible as these are important]

### Recommendations
[Actionable recommendations for fixing issues - keep visible]
```

Label with: `documentation`, `testing`, `automated`

## Step 6: Cleanup

No manual server cleanup is required. The server process will be cleaned up automatically when the agent job exits.

## Summary

**⚠️ MANDATORY: Always provide a safe output before finishing:**
- **If issues found**: Create GitHub issue with test results, findings, and recommendations
- **If no issues found**: Call `noop` tool with completion message including total devices tested and pass status
- **If testing could not be completed** (e.g., server failed to start, permission errors): Call `noop` with an explanation of what was attempted and what blocked completion

The workflow will fail if you do not call either the `create-issue` or `noop` tool before exiting, regardless of whether testing succeeded or not.

### Output Format

Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body. Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks for progressive disclosure.

Structure reports as: overview → key metrics/issues → collapsible detail → next actions.