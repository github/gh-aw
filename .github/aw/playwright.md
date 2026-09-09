---
description: Configure and use the built-in Playwright CLI integration in GitHub Agentic Workflows.
---

# Playwright

Use the built-in `playwright` tool for browser automation, accessibility checks,
end-to-end flows, and visual regression testing. The integration uses
`@playwright/cli`; it does not expose Playwright MCP tools.

## Configure the tool

Enable Playwright in workflow frontmatter:

```yaml
tools:
  playwright:
```

The compiler installs the pinned default `@playwright/cli` package, its agent
skills, and Chromium before the agent starts. The default `open` session uses
Chromium. To use other browser engines, list them in `browsers`. Playwright's
`chromium` download is the Chrome for Testing distribution; `chrome` and
`chrome-for-testing` are accepted aliases:

```yaml
tools:
  playwright:
    browsers: [chromium, firefox, webkit]
```

Supported values are `chrome`, `chrome-for-testing`, `chromium`, `firefox`, and
`webkit`. The broader Playwright install-target list also contains system browser
channels and platform-specific tools, but those are not portable browser engines
for this field. Do not add steps such as `npx playwright install` or
`npm exec playwright install`; the compiler provisions the selected engines, and
browser installation during agent execution is prohibited. Pin `version` only
when reproducible browser output is required, such as for visual baselines:

```yaml
tools:
  playwright:
    version: "0.1.18"
```

Omit `mode`; the built-in Playwright integration is CLI-only by default. The
explicit `mode: cli` setting remains accepted for compatibility, but it is not
needed and should be removed from workflows that still carry it. `mode: mcp` is
not supported by the built-in tool. If MCP is required, configure and pin
`@playwright/mcp` explicitly under `mcp-servers` and allow only the required
tools.

## Configure network access

Playwright can reach `localhost` and `127.0.0.1` by default; do not add `local`
for a server started in the same AWF sandbox. Add only the ecosystems and
external domains the browser needs:

```yaml
network:
  allowed:
    - defaults
    - playwright
    - "docs.example.com"
```

The `playwright` ecosystem permits browser downloads. An explicit domain also
permits its subdomains. Prefer a local server over an external preview, and
avoid broad wildcard domains.

## Use Playwright CLI

Run `playwright-cli` through bash. With a restricted Bash allowlist, the compiler
automatically allows `playwright-cli:*`; list only supporting lifecycle commands
such as `npm`, `curl`, and `kill`. Before opening a browser, inspect package
scripts, Playwright configuration, and test filenames to determine whether the
task is likely to run Playwright Test. If it is, use `--browser=chromium`, which
selects Playwright's Chrome for Testing engine. Start with a snapshot and use its
element refs for later actions:

```bash
playwright-cli open --browser=chromium "https://docs.example.com"
playwright-cli snapshot
playwright-cli click e15
playwright-cli fill e22 "search text" --submit
playwright-cli screenshot --filename=/tmp/docs.png
playwright-cli close
```

Useful commands include:

| Goal | Command |
|---|---|
| Open a browser and URL | `playwright-cli open --browser=<browser> <url>` |
| Navigate the open page | `playwright-cli goto <url>` |
| Inspect the page and get refs | `playwright-cli snapshot` |
| Limit snapshot size | `playwright-cli snapshot --depth=4` |
| Click or fill an element | `playwright-cli click <ref>` / `playwright-cli fill <ref> <text>` |
| Evaluate JavaScript | `playwright-cli eval "() => document.title"` |
| Capture a screenshot | `playwright-cli screenshot --filename=<path>` |
| Return only the command result | `playwright-cli --raw <command>` |
| Close the browser | `playwright-cli close` |

Prefer refs from the latest snapshot over brittle CSS selectors. Use `--raw`
when piping a result or comparing snapshots so page status output does not
pollute the data.

Use `playwright-cli open --browser=chromium` for Chrome for Testing.
Use `playwright-cli open --browser=firefox` or
`playwright-cli open --browser=webkit` for the other provisioned browsers.
Use named sessions when independent cookies or storage are useful:

```bash
playwright-cli -s=authenticated open --browser=chromium "https://app.example.com/login"
playwright-cli -s=public open --browser=chromium "https://app.example.com/"
playwright-cli -s=authenticated close
playwright-cli -s=public close
```

## Run against a local application

Prepare dependencies in deterministic workflow steps, but start the server from
the agent when the agent needs to control its lifecycle:

```yaml
steps:
  - name: Prepare application
    working-directory: ./web
    run: npm ci

tools:
  playwright:
  bash:
    - "npm run dev *"
    - "curl *"
    - "kill *"

network:
  allowed:
    - defaults
    - playwright
```

The server process then runs in the same sandbox and network namespace as
`playwright-cli`, so its loopback URL is reachable without exposing a host port
or adding an external domain. It remains available across the agent's tool calls
until it exits, the agent stops it, or the sandbox is torn down.

Direct the agent to start the server in the background, retain its PID, and use
one `curl` command with built-in retries and exponential backoff for readiness:

```bash
npm run dev -- --host 127.0.0.1 > /tmp/web-server.log 2>&1 &
server_pid=$!
curl --fail --silent --show-error --retry 10 --retry-connrefused \
  --retry-all-errors --retry-max-time 30 http://127.0.0.1:4321/ >/dev/null
playwright-cli open --browser=chromium "http://127.0.0.1:4321/"
playwright-cli resize 1440 900
playwright-cli screenshot --filename=/tmp/home.png
playwright-cli close
kill "$server_pid"
```

If commands run in separate shell calls, write the PID to a file under `/tmp`
and read it back for cleanup. Redirect server logs to `/tmp` so they do not
consume the agent context; inspect only the relevant tail when startup fails.

## Publish screenshots

Files under `/tmp`, including screenshots, disappear when the run sandbox ends.
Declare `upload-artifact` and instruct the agent to publish files users need to
retrieve:

```markdown
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

## Follow the AWF sandbox policy

When Playwright runs in the AWF sandbox:

- Never install packages, browsers, or system dependencies at runtime. Report a
  missing CLI or browser instead.
- Navigate only to loopback URLs or domains listed in `network.allowed`.
- Do not bind local servers to `0.0.0.0`, publish ports, or use preview tunnels.
- Do not change browser proxy settings, proxy environment variables, or the
  `localhost`/`127.0.0.1` proxy bypass.
- Close the browser and stop any server process started during the task.

These rules differ from using the standalone `awf` command to wrap a host-side
Playwright test. Standalone AWF uses `--allow-domains localhost` to expose
selected host ports to its container. In a gh-aw agent sandbox, start the server
inside the sandbox and keep it on loopback instead.

## Accessibility and troubleshooting

Snapshots enable structural and manual inspection of headings, labels,
alternative text, and keyboard flows. Comprehensive WCAG testing (for example,
axe-core or programmatic contrast analysis) needs dependencies prepared in
workflow steps before the agent runs; the AWF sandbox prohibits runtime installs.

For failures, inspect `playwright-cli console` and `playwright-cli requests`;
use `playwright-cli request <index>` for a request's details. Surround a failing
flow with `playwright-cli tracing-start` and `playwright-cli tracing-stop`, and
inspect the relevant tail of redirected local-server logs (for example,
`tail -n 100 /tmp/web-server.log`).

## Sample workflow

This workflow checks a public documentation site and reports actionable
accessibility findings:

```markdown
---
on:
  workflow_dispatch:

permissions:
  contents: read

tools:
  playwright:

network:
  allowed:
    - defaults
    - playwright
    - "docs.example.com"

safe-outputs:
  create-issue:
    title-prefix: "[accessibility] "
    labels: [accessibility]
    max: 3
  noop:
---

# Accessibility review

Open https://docs.example.com with `playwright-cli open --browser=chromium`.
Inspect the page snapshot, keyboard navigation, form labels, image alternatives,
and heading structure.
Create focused issues for actionable findings. If there are none, call `noop`.
Always close the browser before finishing.
```

For visual comparisons, pin the Playwright CLI version, define the baseline
source explicitly, keep screenshots under `/tmp`, and use `cache-memory` when
baselines must persist across runs.

## Related guidance

- [`visual-regression.md`](visual-regression.md) for baseline storage and
  comparison patterns
- [`network.md`](network.md) for domain allowlisting
- [`mcp-clis.md`](mcp-clis.md) for CLI-mounted MCP servers, which are separate
  from the built-in Playwright CLI integration
- [Playwright CLI](https://github.com/microsoft/playwright-cli) for the complete
  command reference
