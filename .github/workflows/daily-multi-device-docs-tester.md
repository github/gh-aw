---
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
tracker-id: daily-multi-device-docs-tester
engine:
  id: claude
  max-turns: 30  # Prevent runaway token usage
strict: true
timeout-minutes: 30
tools:
  playwright:
    version: "v1.56.1"
  bash:
    - "npm install*"
    - "npm run build*"
    - "npm run preview*"
    - "npx playwright*"
    - "curl*"
    - "kill*"
    - "lsof*"
    - "ls*"      # List files for directory navigation
    - "pwd*"     # Print working directory
    - "cd*"      # Change directory
safe-outputs:
  upload-asset:
  create-issue:
    expires: 2d
    labels: [cookie]

network:
  allowed:
    - node

imports:
  - shared/mood.md
  - shared/docs-server-lifecycle.md
  - shared/reporting.md
---

{{#runtime-import? .github/shared-instructions.md}}

# Multi-Device Documentation Testing

You are a documentation testing specialist. Your task is to comprehensively test the documentation site across multiple devices and form factors.

## Context

- Repository: ${{ github.repository }}
- Triggered by: @${{ github.actor }}
- Devices to test: ${{ inputs.devices }}
- Working directory: ${{ github.workspace }}

**IMPORTANT SETUP NOTES:**
1. You're already in the repository root
2. The docs folder is at: `${{ github.workspace }}/docs`
3. Use absolute paths or change directory explicitly
4. Keep token usage low by being efficient with your code and minimizing iterations

## Your Mission

Build the documentation site locally, serve it, and perform comprehensive multi-device testing. Test layout responsiveness, accessibility, interactive elements, and visual rendering across all device types. Use a single Playwright browser instance for efficiency.

## Step 1: Build and Serve

Navigate to the docs folder and build the site:

```bash
cd ${{ github.workspace }}/docs
npm install
npm run build
```

Follow the shared **Documentation Server Lifecycle Management** instructions:
1. Start the preview server (section "Starting the Documentation Preview Server")
2. Wait for server readiness (section "Waiting for Server Readiness")

## Step 2: Device Configuration

Test these device types based on input `${{ inputs.devices }}`:

**Mobile:** iPhone 12 (390x844), iPhone 12 Pro Max (428x926), Pixel 5 (393x851), Galaxy S21 (360x800)
**Tablet:** iPad (768x1024), iPad Pro 11 (834x1194), iPad Pro 12.9 (1024x1366)
**Desktop:** HD (1366x768), FHD (1920x1080), 4K (2560x1440)

## Step 3: Run Playwright Tests

For each device, use Playwright to:
- Set viewport size and navigate to http://localhost:4321
- Take screenshots and run accessibility audits
- Test interactions (navigation, search, buttons)
- Check for layout issues (overflow, truncation, broken layouts)

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

## 📝 Report Formatting Guidelines

**CRITICAL**: Follow these formatting guidelines to create well-structured, readable reports:

### 1. Header Levels
**Use h3 (###) or lower for all headers in your report to maintain proper document hierarchy.**

The issue or discussion title serves as h1, so all content headers should start at h3:
- Use `###` for main sections (e.g., "### Executive Summary", "### Key Metrics")
- Use `####` for subsections (e.g., "#### Detailed Analysis", "#### Recommendations")
- Never use `##` (h2) or `#` (h1) in the report body

### 2. Progressive Disclosure
**Wrap long sections in `<details><summary><b>Section Name</b></summary>` tags to improve readability and reduce scrolling.**

Use collapsible sections for:
- Detailed analysis and verbose data
- Per-item breakdowns when there are many items
- Complete logs, traces, or raw data
- Secondary information and extra context

Example:
```markdown
<details>
<summary><b>View Detailed Analysis</b></summary>

[Long detailed content here...]

</details>
```

### 3. Report Structure Pattern

Your report should follow this structure for optimal readability:

1. **Brief Summary** (always visible): 1-2 paragraph overview of key findings
2. **Key Metrics/Highlights** (always visible): Critical information and important statistics
3. **Detailed Analysis** (in `<details>` tags): In-depth breakdowns, verbose data, complete lists
4. **Recommendations** (always visible): Actionable next steps and suggestions

### Design Principles

Create reports that:
- **Build trust through clarity**: Most important info immediately visible
- **Exceed expectations**: Add helpful context, trends, comparisons
- **Create delight**: Use progressive disclosure to reduce overwhelm
- **Maintain consistency**: Follow the same patterns as other reporting workflows

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
<summary><b>View All Warnings</b></summary>

[Minor issues and potential problems with device names and details]

</details>

<details>
<summary><b>View Detailed Test Results by Device</b></summary>

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

Follow the shared **Documentation Server Lifecycle Management** instructions for cleanup (section "Stopping the Documentation Server").

## Summary

**Always provide a safe output:**
- **If issues found**: Create GitHub issue with test results, findings, and recommendations
- **If no issues found**: Call `noop` tool with completion message including total devices tested and pass status

The workflow requires explicit safe output (either issue creation or noop) to confirm completion.