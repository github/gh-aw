---
private: true
name: Smoke Copilot Dynamic Workflows Repro
description: Isolate unattended URL approval from packaged dynamic-workflow execution
intent: Distinguish URL permission denials from failures to run packaged Copilot dynamic workflows.
on:
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
  version: 1.0.90
  bare: true
  dynamic-workflows: true
  args: ["--experimental"]
checkout:
  pull-request: false
network:
  allowed: [defaults, github]
tools:
  bash: ["curl"]
safe-outputs:
  staged: true
  report-failure-as-issue: false
  report-failed-jobs: false
  missing-tool:
    create-issue: false
  missing-data:
    create-issue: false
  report-incomplete:
    create-issue: false
  noop:
    report-as-issue: false
timeout-minutes: 5
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Minimal Reproducer

Copilot CLI 1.0.90 is pinned to reproduce the investigated permission behavior,
not to track the newer version used by the dedicated dynamic-workflow smoke.

Run only these two checks. Do not retry denials, change permissions, or stop
before attempting the second check.

1. Call bash once with `curl --max-time 20 -sI https://github.com`.
   Record the exact result or permission error. The firewall allows GitHub,
   but CLI URL approval is intentionally absent.
2. Call `run_dynamic_workflow` once for `smoke-copilot-dynamic-workflow` with
   `{"marker":"GH_AW_DYNAMIC_WORKFLOW_SMOKE_OK"}`. Wait for completion and inspect
   the run using `dynamic_workflows_manage` with `operation: "inspect-run"`.
   PASS requires status `completed` and exact result
   `{"status":"PASS","marker":"GH_AW_DYNAMIC_WORKFLOW_SMOKE_OK","packageVersion":1,"supportFile":"support/.fixture.json","subagents":1}`.
   Record the run ID and actual result or error; if the tool is unavailable,
   record UNAVAILABLE. Do not author or install a replacement workflow.

Finish with one `noop` message containing both outcomes, the dynamic run ID,
and the actual errors. A URL denial does not prove a dynamic-workflow denial;
registration and a green Actions job do not prove the dynamic workflow passed.
