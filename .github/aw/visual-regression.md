---
name: visual-regression
description: Reference prompt for visual regression testing using playwright + cache-memory for baseline screenshot storage across pull requests
---

# Visual Regression Testing

Use `playwright` for screenshots and `cache-memory` to persist baselines between PR runs.

## Example Workflow

```markdown
---
description: Capture screenshots on every PR and compare against cached baselines to detect visual regressions
on:
  pull_request:
    types: [opened, synchronize, reopened]
permissions:
  contents: read
  pull-requests: read
engine: copilot
network:
  allowed:
    - local
    - playwright
tools:
  playwright:
  cache-memory:
    key: visual-regression-baselines-${{ github.event.pull_request.base.ref }}
    retention-days: 30
    allowed-extensions: [".png", ".json"]
  bash:
    - "mkdir *"
    - "cp *"
    - "diff *"
    - "date *"
safe-outputs:
  add-comment:
    max: 1
timeout-minutes: 30
---

Build and serve the app locally, then use Playwright to capture full-page screenshots of key pages into `/tmp/visual-regression/current/`.

Use filesystem-safe timestamps (no colons — colons break artifact uploads):
`date -u "+%Y-%m-%d-%H-%M-%S"`

If `/tmp/gh-aw/cache-memory/baselines/manifest.json` does not exist, copy screenshots there as new baselines and post: "Baselines initialized — N pages captured."

Otherwise compare each screenshot to its baseline. Post a comment summarizing: pages unchanged / pages with diffs. If nothing changed, use the `noop` safe-output.
```

## Key Design Decisions

- **`cache-memory` key per base branch** — scopes baselines to `main`, `develop`, etc.
- **Explicit baseline source** — state whether baselines come from `cache-memory`, a generated artifact, or a branch directory; do not leave baseline origin implicit.
- **`network.allowed: [local, playwright]`** — prevents SSRF; serve app locally, allow browser binary downloads
- **`retention-days: 30`** — beyond the default 7-day cache expiry
- **Filesystem-safe timestamps** — `YYYY-MM-DD-HH-MM-SS`; colons break artifact filenames
- **Minimal permissions** — all PR writes go through `safe-outputs`

## Network-Minimization Reminders

- Prefer local preview (`localhost`/`127.0.0.1`) over external preview environments.
- If external previews are required, allowlist exact domains (no broad wildcards).
- Enable `network.node` only when installing/building Node deps; scope to registries and preview hosts.
- Keep Playwright navigation limited to app-under-test URLs.
