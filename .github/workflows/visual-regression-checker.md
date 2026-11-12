---
private: true
emoji: "👁️"
description: Visual regression checker that captures and compares screenshots on every pull request using Playwright
on:
  pull_request:
    types: [opened, synchronize]
    paths:
      - 'docs/package.json'
      - 'docs/package-lock.json'
      - 'docs/src/**/*.css'
      - 'docs/src/**/*.tsx'
      - 'docs/src/**/*.astro'
      - 'docs/astro.config.mjs'
permissions:
  contents: read
  pull-requests: read
engine:
  id: copilot
  copilot-sdk: true
imports:
  - shared/otlp.md
  - shared/reporting.md
  - path: shared/ci-node-setup.md
    inputs:
      node-cache: npm
      node-cache-dependency-path: docs/package-lock.json
tools:
  cli-proxy: true
  playwright:
  bash:
    - "npm *"
    - "npx *"
    - "node *"
    - "curl http://localhost:4321/gh-aw/:*"
network:
  allowed:
    - defaults
    - playwright
    - local
    - node
jobs:
  agent:
    timeout-minutes: 15
safe-outputs:
  timeout-minutes: 10
  add-comment:
    max: 1
timeout-minutes: 15
steps:
  - name: Checkout repository
    uses: actions/checkout@v7.0.1
    with:
      persist-credentials: false

  - name: Checkout agentics workflows
    uses: actions/checkout@v7.0.1
    with:
      repository: githubnext/agentics
      ref: main
      path: .agentics
      persist-credentials: false
      sparse-checkout: workflows

  - name: Install dependencies
    working-directory: ./docs
    timeout-minutes: 5
    run: npm ci

  - name: Build documentation
    working-directory: ./docs
    timeout-minutes: 5
    env:
      AGENTICS_WORKFLOWS_DIR: ${{ github.workspace }}/.agentics/workflows
    run: npm run build

evals:
  - id: viewport-coverage
    question: Does the agent output record screenshot results for each required mobile, tablet, and desktop viewport?
  - id: findings-reported
    question: When visual differences or accessibility issues are observed, does the proposed PR comment describe those observations? Return UNKNOWN when neither is observed.
  - id: clean-run-noop
    question: When all inspected pages have no visual differences or accessibility issues, does the agent request noop? Return UNKNOWN when that clean-run scenario is not established.
---

# Visual Regression Checker

You are a visual quality agent. Setup installed `docs/node_modules` and built `docs/dist` for this run. Use these provisioned directories without installing dependencies or inspecting the process environment. Capture the built documentation inside the sandbox with the provisioned Playwright CLI.

## Steps

1. **Start the sandbox-local preview** — Run `npm --prefix docs run preview -- --host 127.0.0.1 --port 4321` with the Bash tool's background/async mode. Keep its process handle for cleanup. Do not start a runner-host server, bind to `0.0.0.0`, install packages, or change proxy configuration. Poll `curl http://localhost:4321/gh-aw/ --fail --silent --show-error --connect-timeout 2 --max-time 5` for at most 30 seconds until it returns successfully; stop and report a setup failure if it does not become ready. Loopback preview traffic uses the sandbox's existing local bypass; do not inspect environment variables to discover proxy settings.
2. **Capture screenshots** — Open `playwright-cli open http://localhost:4321/gh-aw/`, then use the pinned CLI's commands (not MCP `browser_*` tool names):
   - **Mobile**: `playwright-cli resize 375 812`, then `playwright-cli screenshot --filename=/tmp/gh-aw/agent/screenshot-mobile.png --full-page`
   - **Tablet**: resize to 768 × 1024, navigate, screenshot
   - **Desktop**: resize to 1440 × 900, navigate, screenshot
3. **Accessibility snapshot** — For each page, run `playwright-cli snapshot` and report observed accessibility concerns, not a claim of exhaustive compliance.
4. **Compare only with an available baseline** — No baseline is provisioned by this workflow. Its absence must not block screenshots or accessibility inspection. Record visual comparison as **unavailable**, never **unchanged**, when no authorized baseline is available. Do not fetch a baseline from an unapproved location.
5. **Report** — Post a summary comment with:
   - A table listing each page, viewport, capture status (captured / error), and comparison status (unchanged / changed / unavailable)
   - Any accessibility issues found

Post the summary as a pull request comment using the `add_comment` safe-output tool.
Call `noop` only when actual baseline comparison established no differences and inspection found no accessibility issues; missing comparison evidence is not a clean result. If basic capture fails, report the concrete failure with `report_incomplete`; an unavailable baseline alone is not a capture failure.

Always close the browser with `playwright-cli close`, stop the preview using its process handle, and verify it stopped before finishing.