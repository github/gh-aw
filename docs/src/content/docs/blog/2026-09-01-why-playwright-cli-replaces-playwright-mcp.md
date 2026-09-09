---
title: "Why the Built-In Playwright Tool Is Now CLI-Only"
description: "gh-aw removed built-in Playwright MCP support in favor of @playwright/cli: a smaller attack surface and a lighter token footprint for coding agents."
authors:
  - copilot
  - pelikhan
date: 2026-09-01
metadata:
  seoDescription: "gh-aw's built-in tools.playwright now uses @playwright/cli only. Here's why CLI beats MCP for security and token efficiency, and how to migrate."
---

The built-in `tools.playwright` integration in [`gh-aw`](https://github.com/github/gh-aw) used to support two modes: a Docker-based MCP server (`mode: mcp`) and a CLI-based integration (`mode: cli`). As of this change, the built-in tool only supports CLI mode, and the compiler rejects `mode: mcp` with migration guidance instead of quietly starting a container. Workflows that still need the full Playwright MCP server can configure it explicitly under `mcp-servers`. Here is why CLI became the only built-in option.

## Fewer tokens spent on tool schemas

MCP servers advertise their tools to the agent by loading a schema for every available function into the model's context window. Playwright MCP exposes a wide surface of browser-automation tools — navigation, snapshots, clicks, evaluation, tracing, and more — and all of that schema has to be paid for in tokens on every turn, whether or not the agent uses most of it.

`@playwright/cli` instead exposes a single command, `playwright-cli`, that the agent invokes directly from bash with a subcommand such as `goto`, `snapshot`, or `click`. The agent only needs to have seen `playwright-cli --help` once (or installed skills via `playwright-cli install --skills`) to know how to drive the browser. There is no persistent tool schema competing with the rest of the workflow's context for space, which matters for coding agents that also need room to reason about code, tests, and long-running tasks.

## A smaller, more auditable attack surface

The built-in MCP mode ran Playwright inside a Docker container with its own image, arguments, and lifecycle that the compiler had to track, pin, and update independently. Every one of those knobs — container image version, extra MCP arguments, mounted volumes — was one more thing that could silently drift out of date or be misconfigured across workflows.

CLI mode collapses that surface. `@playwright/cli` is a single npm package installed directly on the runner, with one version to track and one command surface to allow through the shell permission system. Because it runs on the runner instead of in a separate container, it also reaches local development servers through `localhost` directly, without needing extra network plumbing between a container and the host. Less machinery means less to get wrong, and less to review when auditing what a workflow can do.

## MCP is still available, just not built-in

Some workflows genuinely benefit from MCP's persistent state and richer introspection — for example, exploratory automation or self-healing tests that need to reason iteratively over page structure across many turns. That use case has not gone away; it is just no longer a hidden default. Configure it explicitly:

```yaml wrap
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
```

Making this explicit means the dependency, the pinned version, and the exact allowed tool list are all visible in the workflow source, instead of being implied by a one-word `mode: mcp` setting. See the [Playwright reference](/gh-aw/reference/playwright/) for the full migration table from MCP tool names to `playwright-cli` subcommands.
