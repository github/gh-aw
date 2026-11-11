---
name: Smoke Agy
description: On-demand smoke test for Agy authentication, inference, AWF, MCP and staged safe outputs.
on:
  workflow_dispatch:
  slash_command:
    name: smoke-agy
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  label_command:
    name: smoke
    events: [pull_request]
    remove_label: false
  reaction: none
  status-comment: false
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
max-ai-credits: 50
features:
  gh-aw-detection: false
engine:
  id: agy
  env:
    ENGINE_CONFORMANCE_SENTINEL: conformance-agy
evals:
  - id: native_challenge_transport
    question: Does the output show native MCP invocation of agy-native native_challenge with the fixture fileNonce rather than a CLI or HTTP substitute?
  - id: native_infrastructure_transport
    question: Does the output show native MCP invocation of mcpscripts native_infrastructure_challenge with the same fixture fileNonce?
  - id: native_completion
    question: Does the agent emit exactly one final native safeoutputs noop with message Conformance probes completed after the probes?
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
