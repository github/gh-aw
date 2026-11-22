---
title: Playwright
description: Configure Playwright browser automation for testing web applications, accessibility analysis, and visual testing in your agentic workflows
sidebar:
  order: 720
---

Playwright enables headless browser control for accessibility testing, visual regression detection, end-to-end testing, and web scraping.

## Configuration

The built-in Playwright tool is CLI-only. It skips MCP tool schemas and Docker overhead, and reaches local development servers through `localhost`. Older workflows may set `mode: cli`, but omitting `mode` is preferred.

```yaml wrap
tools:
  playwright:
```

Before the agent runs, the compiler installs `@playwright/cli`, its skills, and Chromium. Use `browsers` to provision additional browsers:

```yaml wrap
tools:
  playwright:
    browsers: [chrome, firefox]
```

Supported values are `chrome`, `chrome-for-testing`, `chromium`, `firefox`, and `webkit`. `chrome` and `chrome-for-testing` are aliases for `chromium`, which downloads the Chrome for Testing distribution. Browser downloads happen before the agent starts, with retries; runtime package or browser installation is not allowed.

The agent runs Playwright through `playwright-cli <command>` from bash:

```bash wrap
playwright-cli open "https://example.com"
playwright-cli screenshot --filename /tmp/screenshot.png
playwright-cli snapshot
playwright-cli eval "() => document.title"
playwright-cli run-code "async (page) => { await page.goto('https://example.com'); return await page.title(); }"
```

With a restricted `tools.bash` allowlist, `playwright-cli:*` is added automatically. Add only supporting commands such as `npm`, `curl`, and `kill`.

### Version

`version` pins the `@playwright/cli` npm package. Omit it to use the compiler default.

```yaml wrap
tools:
  playwright:
    version: "0.1.18"
```

### Network Access

Domain access is controlled by the top-level [`network:`](/gh-aw/reference/network/) field. Playwright always reaches `localhost` and `127.0.0.1`, so a local server in the same AWF sandbox does not need `network.allowed: local`:

```yaml wrap
network:
  allowed:
    - defaults
    - playwright                 # enables browser downloads
    - "example.com"              # matches example.com and subdomains
    - "*.staging.example.com"    # wildcard pattern
```

### AWF Sandbox Policy

Inside the AWF sandbox (`sandbox.agent` or the default firewall), the compiler adds a policy prompt that binds local servers to `127.0.0.1`, waits for a loopback readiness check before navigating, and keeps `localhost`/`127.0.0.1` on the proxy bypass list. It takes precedence over generic CLI skill advice, such as `npx` fallbacks.

### Browser Support and Sessions

Chromium is the default. Select Firefox or WebKit (once provisioned) with `--browser`:

```bash wrap
playwright-cli open "https://example.com"                  # Chromium
playwright-cli -s=firefox open "https://example.com" --browser=firefox
playwright-cli -s=webkit open "https://example.com" --browser=webkit
playwright-cli -s=firefox close
playwright-cli -s=webkit close
```

Named sessions (`-s=<name>`) isolate cookies and storage, such as for authenticated versus anonymous flows.

### Publishing Screenshots

To publish a screenshot, save it in the workspace and attach it with an artifact safe output:

```aw wrap
---
on:
  workflow_dispatch:
tools:
  playwright:
safe-outputs:
  upload-artifact:
    allowed-paths: ["*.png"]
    max-uploads: 1
    retention-days: 7
---

Create the `output` directory in the workspace and capture `output/home.png`,
then call `upload_artifact` with `name: "home-screenshot"` and
`path: "output/home.png"`.
```

To publish an existing `/tmp` screenshot, copy it into the workspace first. See [Artifact Uploads](/gh-aw/reference/safe-outputs/#artifact-uploads-upload-artifact) for path restrictions.

## Migrate from Playwright MCP

Remove `mode: mcp`; no replacement `mode` is needed, and the compiler reports `mode: mcp` as an error. Replace MCP tool calls in prompts with `playwright-cli` commands run through bash:

| Playwright MCP tool | Playwright CLI command |
| --- | --- |
| `browser_navigate` | `playwright-cli goto <url>` |
| `browser_snapshot` | `playwright-cli snapshot` |
| `browser_take_screenshot` | `playwright-cli screenshot --filename <path>` |
| `browser_click` | `playwright-cli click <ref>` |
| `browser_evaluate` | `playwright-cli eval "() => document.title"` |

Use `localhost` for development servers, since the CLI runs on the runner. Also remove Playwright MCP container arguments and MCP tool names such as `mcp__playwright__browser_navigate` from prompts and engine allowlists.

## What if you still want MCP?

Configure Playwright MCP as a custom `mcp-servers` entry with a pinned package version:

```aw wrap
---
mcp-servers:
  playwright:
    command: npx
    args:
      - --yes
      - "@playwright/mcp@0.0.79"
      - --no-sandbox
    allowed:
      - browser_navigate
      - browser_snapshot
      - browser_take_screenshot

network:
  allowed:
    - defaults
    - node
    - playwright
---
```

Custom servers fall outside built-in version tracking, so pin and update the package deliberately, restrict `allowed` to required tools, and follow the [custom MCP server guidance](/gh-aw/guides/mcps/#manually-configuring-a-custom-mcp-server).

## Common Use Cases

### Accessibility Testing

````aw wrap
---
on:
  schedule: daily

tools:
  playwright:

network:
  allowed:
    - defaults
    - playwright
    - "docs.example.com"

permissions:
  contents: read

safe-outputs:
  create-issue:
    title-prefix: "[a11y] "
    labels: [accessibility, automated]
    max: 3
---

# Accessibility Audit

Use Playwright to check docs.example.com for WCAG 2.1 Level AA compliance.

```bash
playwright-cli open "https://docs.example.com"
playwright-cli snapshot
```

Snapshots cover headings, labels, alternative text, and keyboard flows. WCAG checks such as axe-core need dependencies prepared before the agent runs, since the AWF sandbox prohibits runtime installation. Create focused issues for actionable findings.
````

### Visual Regression Testing

Use `steps:` to start the dev server before the agent runs, and pin Playwright to avoid baseline drift from browser upgrades:

```aw wrap
---
on:
  pull_request:
    types: [opened, synchronize]
    paths:
      - 'docs/src/**/*.css'
      - 'docs/src/**/*.tsx'
      - 'docs/src/**/*.astro'
      - 'docs/astro.config.mjs'

steps:
  - uses: actions/checkout@v7
    with:
      persist-credentials: false
  - working-directory: ./docs
    run: npm ci && npm run build && npm run dev &
  - run: |
      # wait for dev server (max 30s)
      for i in $(seq 1 30); do
        curl -sf http://localhost:4321/ >/dev/null && exit 0
        sleep 1
      done
      exit 1

tools:
  playwright:
    version: "0.1.18"  # pins `@playwright/cli`; see Configuration
  bash:
    - "npm *"
    - "curl http://localhost:*"

network:
  allowed:
    - defaults
    - playwright
    - node

permissions:
  contents: read

safe-outputs:
  add-comment:
    max: 1
  noop:
---

# Visual Regression Check

The dev server runs at http://localhost:4321/. Check the home, getting-started, and reference pages at three viewports: mobile (375×812), tablet (768×1024), and desktop (1440×900).

Resize and capture a screenshot for each viewport:

```bash
playwright-cli open "http://localhost:4321/"
playwright-cli resize 375 812
playwright-cli screenshot --filename=/tmp/mobile-screenshot.png --full-page
```

Compare against baseline and report differences as a PR comment with screenshots.
If there are no regressions, call noop.
```

## Learn More

- [Tools Reference](/gh-aw/reference/tools/) — All tool configurations
- [Network Permissions](/gh-aw/reference/network/) — Network access control
- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) — Configure output creation
