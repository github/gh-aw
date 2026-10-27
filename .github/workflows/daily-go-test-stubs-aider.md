---
private: true
emoji: "🧪"
description: Daily scan of Go packages for missing test coverage and automatic addition of test stubs using Aider
on:
  schedule: daily
  workflow_dispatch:
permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read
sandbox:
  agent:
    id: awf
tracker-id: daily-go-test-stubs-aider
engine:
  id: aider
model: copilot/auto
strict: true
network:
  allowed: []
runtimes:
  go:
    version: "1.27.3"
tools:
  edit:
  bash:
    - "*"
safe-outputs:
  steer: true
  create-pull-request:
    expires: 2d
    title-prefix: "[aider] "
    labels: [automation, testing]
    draft: false
  missing-tool:
timeout-minutes: 30
imports:
  - shared/aider.md
  - shared/otlp.md
  - shared/reporting.md
features:
  gh-aw-detection: true
---

# Daily Go Test Stubs — Aider

You are an automated coding agent that improves Go test coverage by finding untested packages and adding
minimal test stubs. Aider has no MCP client; invoke safe-output tools through
the `safeoutputs` MCP CLI. Follow the Aider execution constraints above: one shell command per line, and
no heredocs. Use the supplied command to generate stubs, not model-authored edits.

## Step 1 — Generate stubs for packages with no test files

No source files need to be attached before starting. Your first reply must put
this exact command in a fenced `bash` block. It discovers packages and creates
stubs for at most three of them using their actual Go package names.

```bash
workspace="$(pwd -P)" && go list -f '{{if and (not .TestGoFiles) (not .XTestGoFiles)}}{{.Dir}} {{.Name}}{{end}}' ./... > /tmp/gh-aw/agent/aider-untested-packages && count=0 && while IFS= read -r row; do if [ -n "$row" ]; then directory="${row% *}"; package="${row##* }"; resolved="$(realpath -- "$directory")" || exit 1; case "$resolved" in "$workspace"|"$workspace"/*) ;; *) printf 'Refusing to write outside workspace: %s\n' "$resolved" >&2; exit 1 ;; esac; (set -C; printf 'package %s\n\nimport "testing"\n\nfunc TestPlaceholder_TODO(t *testing.T) {\n\tt.Skip("not yet implemented")\n}\n' "$package" > "$resolved/${package}_test.go") || exit 1; printf '%s %s\n' "$resolved" "$package"; count=$((count + 1)); if [ "$count" -eq 3 ]; then break; fi; fi; done < /tmp/gh-aw/agent/aider-untested-packages
```

The command prints one directory and package name for each created stub. It
excludes packages with internal or external tests, testdata, hidden directories,
and nested modules. Existing files are never overwritten. If the command fails,
report incomplete; an error is not evidence that all packages have tests.

## Step 2 — Inspect the generation result

Read the returned command output and exit status on the next reflection.
The generated `<package>_test.go` files already declare the correct package,
import `"testing"`, and contain `TestPlaceholder_TODO` with
`t.Skip("not yet implemented")`. For example, a command package in a hyphenated
directory declares `package main`, not a name derived from that directory.
Do not rewrite the generated declarations or repeat generation.

## Step 3 — Commit and create the pull request

If any stubs were created, run these commands (one per line):

```bash
make fmt-go && go test ./... -run '^TestPlaceholder_TODO$' && git checkout -b add-test-stubs-$GITHUB_RUN_ID && git add -A && git commit -m "Add test stubs for uncovered packages" && safeoutputs create_pull_request --title "Add test stubs for uncovered packages" --body "Automatically generated test stubs for packages with zero test coverage. Stubs use t.Skip and are ready to be filled in." --branch "add-test-stubs-$GITHUB_RUN_ID" || safeoutputs report_incomplete --reason "Could not format, test, commit, or submit the generated test stubs — no pull request confirmed."
```

If no packages needed stubs (all packages already had tests), run instead:

```bash
safeoutputs noop --message "All Go packages already have test files — no stubs needed."
```

## Step 4 — Exit rule

**Always** invoke exactly one `safeoutputs` CLI command before finishing.
Use `report_incomplete` for failed or unfinished work; `noop` is only for a confirmed
absence of packages needing stubs.