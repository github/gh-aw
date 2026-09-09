---
title: Playwright
description: Configure Playwright browser automation for testing web applications, accessibility analysis, and visual testing in your agentic workflows
sidebar:
  order: 720
---

Playwright enables headless browser control for accessibility testing, visual regression detection, end-to-end testing, and web scraping.

## Configuration

The built-in Playwright tool is CLI-only by default. It is token-efficient because it does not load MCP tool schemas into the agent context, avoids Docker overhead, and reaches local development servers through `localhost`. If an older workflow still sets `mode: cli`, it continues to work for compatibility, but omitting `mode` is preferred.

```yaml wrap
tools:
  playwright:
```

The compiler installs `@playwright/cli` as a global npm package, its skills, and
Chromium before the agent runs. The default `open` browser is Chromium. Select
additional browsers with `browsers`. Playwright's `chromium` download is the
Chrome for Testing distribution; `chrome` and `chrome-for-testing` are accepted
aliases:

```yaml wrap
tools:
  playwright:
    browsers: [chrome, firefox]
```

The supported values are `chrome`, `chrome-for-testing`, `chromium`, `firefox`,
and `webkit`.
Requested browsers are downloaded with retries before the agent starts; package
and browser installation during agent execution is prohibited. The agent
invokes `playwright-cli <command>` from bash:

```bash wrap
playwright-cli open "https://example.com"
playwright-cli screenshot --filename /tmp/screenshot.png
playwright-cli snapshot
playwright-cli eval "() => document.title"
playwright-cli run-code "async (page) => { await page.goto('https://example.com'); return await page.title(); }"
```

With a restricted `tools.bash` allowlist, `playwright-cli:*` is added
automatically. Explicit Bash entries are needed only for supporting lifecycle
commands such as `npm`, `curl`, and `kill`.

### Version

The `version` field pins the `@playwright/cli` npm package. Omit it to use the compiler default.

```yaml wrap
tools:
  playwright:
    version: "0.1.18"
```

### Network Access

Domain access is controlled by the top-level [`network:`](/gh-aw/reference/network/) field. Playwright can reach `localhost` and `127.0.0.1` by default. A local server
started in the same AWF sandbox does not require `network.allowed: local`. Use
ecosystem identifiers and explicit external domains together:

```yaml wrap
network:
  allowed:
    - defaults
    - playwright                 # enables browser downloads
    - "example.com"              # matches example.com and subdomains
    - "*.staging.example.com"    # wildcard pattern
```

Allowing `example.com` automatically allows its subdomains.

### AWF Sandbox Policy

When the workflow runs inside the AWF sandbox (`sandbox.agent` enabled, or the
firewall enabled by default for the configured engine), the compiler injects
an additional policy prompt reinforcing the secure browser topology: bind
local servers to `127.0.0.1` only, wait for a loopback readiness check before
navigating, keep `localhost`/`127.0.0.1` on the proxy bypass list, and never
install packages or browsers at runtime. This guidance takes precedence over
generic Playwright CLI skill suggestions such as `npm install`/`npx` fallback
installation or navigating to arbitrary example domains.

### Browser Support and Sessions

Chromium is the default. Use Firefox or WebKit with `--browser` when selected
for provisioning:

```bash wrap
playwright-cli open "https://example.com"                  # Chromium
playwright-cli -s=firefox open "https://example.com" --browser=firefox
playwright-cli -s=webkit open "https://example.com" --browser=webkit
playwright-cli -s=firefox close
playwright-cli -s=webkit close
```

Named sessions (`-s=<name>`) keep cookies and storage isolated, which is useful
for comparing authenticated and anonymous flows.

### Publishing Screenshots

Files under `/tmp` are ephemeral. To let users retrieve a screenshot, configure
an artifact safe output and have the agent publish the file:

```aw wrap
---
safe-outputs:
  upload-artifact:
    allowed-paths: ["/tmp/*.png"]
    max-uploads: 1
    retention-days: 7
---

Capture `/tmp/home.png`, then call `upload_artifact` with
`name: "home-screenshot"` and `path: "/tmp/home.png"`.
```

## Migrate from Playwright MCP

Remove `mode: mcp`. The built-in integration is CLI-only, so no replacement
`mode` field is needed. The compiler now reports `mode: mcp` as an error.

Replace MCP tool calls in prompts with equivalent `playwright-cli` commands run through bash:

| Playwright MCP tool | Playwright CLI command |
| --- | --- |
| `browser_navigate` | `playwright-cli goto <url>` |
| `browser_snapshot` | `playwright-cli snapshot` |
| `browser_take_screenshot` | `playwright-cli screenshot --filename <path>` |
| `browser_click` | `playwright-cli click <ref>` |
| `browser_evaluate` | `playwright-cli eval "() => document.title"` |

Use `localhost` directly for development servers because Playwright CLI runs on the runner. Remove Playwright MCP container arguments and MCP-specific tool names such as `mcp__playwright__browser_navigate` from prompts and engine allowlists.

## What if you really want to use MCP?

The built-in tool no longer manages Playwright MCP. Configure it as a custom server under `mcp-servers` and select the package version explicitly:

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

Custom MCP servers are not covered by the built-in Playwright compatibility or version tracking. Pin and update the package deliberately, restrict `allowed` to the required tools, and follow the [custom MCP server guidance](/gh-aw/guides/mcps/#manually-configuring-a-custom-mcp-server).

## Common Use Cases

### Accessibility Testing

```aw wrap
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

Use snapshots for structural and manual checks of headings, labels, alternative
text, and keyboard flows. Comprehensive WCAG checks (such as axe-core and
programmatic contrast analysis) require dependencies prepared before the agent
runs; the AWF sandbox prohibits runtime installation. Create focused issues for
actionable findings.
```

### Visual Regression Testing

Use `steps:` to start the dev server before the agent runs, and pin Playwright to prevent baseline drift from browser-engine upgrades:

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
    version: "0.1.18"  # pins `@playwright/cli` npm package; see Configuration > Version
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

The dev server is running at http://localhost:4321/. Check for visual regressions
on the home, getting-started, and reference pages across three viewports:

- Mobile: 375×812
- Tablet: 768×1024
- Desktop: 1440×900

For each viewport, resize and screenshot:

```bash
playwright-cli open "http://localhost:4321/"
playwright-cli resize 375 812
playwright-cli screenshot --filename=/tmp/mobile-screenshot.png --full-page
```

Compare against baseline and report differences as a PR comment with screenshots.
If there are no regressions, call noop.
```

### End-to-End Testing

```aw wrap
---
on:
  workflow_dispatch:

tools:
  playwright:
  bash: [":*"]

network:
  allowed:
    - defaults
    - playwright

permissions:
  contents: read
---

# E2E Testing

Start the dev server on localhost:3000, then drive a full user journey with
`playwright-cli open "http://localhost:3000"`. Report any failures with
screenshots.
```

## Learn More

- [Tools Reference](/gh-aw/reference/tools/) — All tool configurations
- [Network Permissions](/gh-aw/reference/network/) — Network access control
- [Network Configuration Guide](/gh-aw/guides/network-configuration/) — Common patterns
- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) — Configure output creation
- [Frontmatter](/gh-aw/reference/frontmatter/) — All frontmatter options
