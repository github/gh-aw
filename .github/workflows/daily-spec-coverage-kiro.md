---
private: true
emoji: "🧭"
description: Daily review of specification documents for coverage gaps and outdated content
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
sandbox:
  agent:
    id: awf
tracker-id: daily-spec-coverage-kiro
engine:
  id: copilot
  copilot-sdk: true
strict: true
network:
  allowed:
    - defaults
    - github
tools:
  github:
    mode: local
    toolsets: [repos, issues]
  bash:
    - cat
    - grep
    - find
    - wc
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[spec-coverage] "
    labels: [automation, documentation]
    max: 1
    close-older-issues: true
    close-older-key: daily-spec-coverage-kiro
  missing-tool:
timeout-minutes: 20
imports:
  - shared/otlp.md
  - shared/reporting.md
features:
  gh-aw-detection: true
---

# Daily Spec Coverage Review

Audit the specification and documentation files in this repository for coverage gaps, stale
references, and missing sections.

## Step 1 — List specification files

Find all markdown files under `.github/aw/` that describe specification or syntax concepts:

```bash
find .github/aw -name "*.md" | sort | head -30
```

## Step 2 — Check for stale cross-references

For each file in `.github/aw/`, scan for `[text](filename.md)` links and verify the referenced
file exists:

```bash
grep -rh "\[.*\]([a-z].*\.md)" .github/aw/ \
  | grep -oP '\(([^)]+\.md)\)' | tr -d '()' | sort -u \
  | while read f; do
      [ -f ".github/aw/$f" ] || echo "BROKEN: $f"
    done
```

## Step 3 — Find spec files without a description front-matter field

```bash
for f in .github/aw/*.md; do
  if ! grep -q "^description:" "$f"; then echo "NO DESC: $f"; fi
done | head -10
```

## Step 4 — Search for open issues mentioning spec gaps

Use the GitHub MCP `list_issues` tool to fetch the 5 most-recently-created open issues from
`${{ github.repository }}` that contain "spec" or "docs" in their title. Record issue numbers
and titles.

## Step 5 — Report

Use the `reporting` skill to format the report body. Use `###` (or lower) headers only — never
`#` or `##`. Keep recommended next actions visible at the top; wrap the full list of broken
cross-references, files missing frontmatter, and related issues in a
`<details><summary><b>Full Findings</b></summary>` block.

Use the `create_issue` safe-output tool to post the daily report:

- **Title**: `[spec-coverage] Daily Spec Coverage Report — ${{ github.run_id }}`
- **Body**:
  - `### Summary` with recommended next actions (if any)
  - `<details><summary><b>Full Findings</b></summary>` wrapping broken cross-references found,
    files without description frontmatter, and open issues mentioning spec gaps

If all checks pass, call `noop` with `"Spec coverage audit passed — no gaps found."`.