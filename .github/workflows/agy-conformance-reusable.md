---
name: Agy Conformance Reusable (Experimental)
description: Feature-branch reusable entry point for the canonical experimental Agy conformance gate.
on:
  workflow_call:
permissions:
  contents: read
concurrency:
  job-discriminator: ${{ github.run_id }}
strict: true
inlined-imports: true
timeout-minutes: 10
jobs:
  agent:
    timeout-minutes: 10
max-ai-credits: 5
features:
  gh-aw-detection: false
engine:
  id: agy
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-agy
safe-outputs:
  staged: true
  threat-detection: false
  activation-comments: false
  report-failure-as-issue: false
  report-failed-jobs: false
  timeout-minutes: 2
  noop:
    report-as-issue: false
  missing-tool:
    create-issue: false
imports:
  - shared/agy-conformance.md
---

Execute the imported Agy conformance gate once.
