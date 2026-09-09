---
on:
  workflow_dispatch:
permissions: read-all
engine: copilot
tools:
  playwright:
    mode: cli
  bash:
    - "curl *"
network:
  allowed:
    - defaults
    - playwright
---

# Test Playwright CLI Mode

This workflow tests the `mode: cli` configuration for the Playwright tool.

When `mode: cli` is set, the compiler installs `@playwright/cli` via npm instead
of launching the Docker-based MCP server, providing a token-efficient alternative
for coding agents.

Please perform the following tasks:

1. Run `playwright-cli open http://127.0.0.1:4321` to navigate to a local page
2. Run `playwright-cli screenshot` to capture the current page
3. Create an issue confirming that Playwright CLI mode is working correctly
