---
private: true
emoji: "✂️"
name: "Ponytail Reviewer"
description: Reviews pull requests for unnecessary complexity using Ponytail
on:
  pull_request:
    types: [ready_for_review]
    paths-ignore:
      - '*.md'
      - 'docs/**'
      - '.changeset/**'
  slash_command:
    strategy: centralized
    name: ponytail
    events: [pull_request_comment, pull_request_review_comment]
engine:
  id: codex
model: copilot/gpt-5.3-codex
permissions:
  contents: read
  pull-requests: read
  copilot-requests: write
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
features:
  gh-aw-detection: true
imports:
  - uses: shared/pr-review-base.md
    with:
      min-integrity: approved
  - shared/otlp.md
  - shared/pr-diff-data-fetch.md
skills:
  - DietrichGebert/ponytail/skills/ponytail-review@2ed6c52c9d7e5e56942508591085fd45dea277d3
cache:
  key: pr-prefetch-${{ github.event.pull_request.head.sha || github.event.issue.number }}
  path: /tmp/gh-aw/agent
  restore-keys:
    - pr-prefetch-${{ github.event.pull_request.number || github.event.issue.number }}-
safe-outputs:
  create-pull-request-review-comment:
    max: 10
  submit-pull-request-review:
    max: 1
    allowed-events: [COMMENT]
timeout-minutes: 10
---

# Ponytail Reviewer

Review the pull request exclusively for unnecessary complexity and over-engineering by applying the installed `ponytail-review` skill.

## Context

- Repository: `${{ github.repository }}`
- Pull request: `#${{ github.event.issue.number || github.event.pull_request.number }}`

## Process

1. Verify that `/tmp/gh-aw/agent/pr-meta.json` and `/tmp/gh-aw/agent/pr-diff.patch` exist and are non-empty. If not, call `noop` with a brief explanation and stop.
2. Read the pre-fetched metadata, diff, and `/tmp/gh-aw/agent/pr-review-comments.json`. Do not fetch the pull request again.
3. Locate and read the installed `ponytail-review` skill's `SKILL.md`, then follow it exactly.
4. Review changed lines only. Skip generated files, lock files, correctness bugs, security issues, and performance issues.
5. Avoid duplicating existing review comments.
6. For each high-signal finding, add one inline `create-pull-request-review-comment` using Ponytail's one-line format. Limit the review to the 10 most impactful opportunities.
7. If findings exist, submit one `COMMENT` review whose body ends with the skill's required net-lines metric.
8. If nothing should be cut, call `noop` with `Lean already. Ship.` and stop.