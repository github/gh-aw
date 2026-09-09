---
private: true
emoji: "🧪"
name: Smoke CI
description: Smoke CI workflow that exercises safe outputs through deterministic samples replay
on:
  push:
    branches: [main]
    paths: ['cmd/**', 'pkg/**', '*.go', 'go.mod', 'actions/setup/js/**']
  schedule: every 2 days
  pull_request:
    types: [opened, synchronize, reopened]
    paths: ['cmd/**', 'pkg/**', '*.go', 'go.mod', 'actions/setup/js/**']
concurrency:
  group: smoke-ci-${{ github.ref }}
  cancel-in-progress: true
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: codex
model: copilot/gpt-5.3-codex
imports:
  - shared/otlp.md
tools:
  cache-memory: true
  comment-memory: true
  repo-memory:
    branch-name: memory/smoke-ci
    description: "Smoke CI persisted repo-memory entries"
    file-glob:
      - "*.md"
  github:
safe-outputs:
  create-issue:
    max: 1
    title-prefix: "[smoke-ci] "
    labels: [ai-generated]
    close-older-issues: true
    close-older-key: "smoke-ci-memory-safe-outputs"
    samples:
      - temporary_id: "#aw_smokeci"
        title: "safe-outputs samples replay"
        body: |
          Deterministic samples replay for run ${{ github.run_id }}.

          This issue is produced by `safe-outputs.create-issue.samples`, not by an agent.
  add-comment:
    hide-older-comments: true
    max: 1
    samples:
      - item_number: "#aw_smokeci"
        body: "smoke-ci samples replay comment for run ${{ github.run_id }}."
  add-labels:
    max: 1
    allowed: [ai-generated]
    samples:
      - item_number: "#aw_smokeci"
        labels: [ai-generated]
  remove-labels:
    max: 1
    allowed: [ai-generated]
    samples:
      - item_number: "#aw_smokeci"
        labels: [ai-generated]
  update-issue:
    body:
    max: 1
    target: "*"
    samples:
      - issue_number: "#aw_smokeci"
        operation: append
        body: "smoke-ci samples replay confirmed the update-issue handler for run ${{ github.run_id }}."
  update-pull-request:
    body: true
    max: 1
    target: "*"
  threat-detection: false
timeout-minutes: 5
strict: true
features:
  gh-aw-detection: false
  samples: true
sandbox:
  agent:
    runtime: cloud-hypervisor
    id: awf
---

Safe outputs for this workflow are produced by the deterministic samples replay
driver (`features.samples: true`), so this prompt is never sent to an agent. It is
kept only so the workflow compiles with the standard prompt-generation steps.

Do not run any shell commands.
