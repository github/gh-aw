---
private: true
emoji: "🧪"
name: Smoke Copilot Dynamic Workflow
description: Smoke-test packaged Copilot CLI dynamic workflows and subagent execution
intent: Detect regressions that make packaged Copilot dynamic workflows unavailable or unable to complete in gh-aw.
on:
  workflow_dispatch:
  workflow_call:
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
  dynamic-workflows: true
  args: ["--experimental"]
checkout:
  pull-request: false
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: smoke-copilot-dynamic-workflow
    labels: [automation, testing]
timeout-minutes: 10
features:
  gh-aw-detection: false
evals:
  - id: dynamic-workflow-completed
    question: Does the report show a completed smoke-copilot-dynamic-workflow run with the exact marker GH_AW_DYNAMIC_WORKFLOW_SMOKE_OK, packageVersion 1, supportFile support/.fixture.json, and one subagent?
sandbox:
  agent:
    id: awf
---

# Smoke Test: Copilot CLI Dynamic Workflow

Run the registered dynamic workflow `smoke-copilot-dynamic-workflow` exactly once
with arguments `{"marker":"GH_AW_DYNAMIC_WORKFLOW_SMOKE_OK"}`. Use the dynamic
workflow run tool, not a task agent, shell script, or a newly authored workflow.
The saved extension is packaged under
`.github/extensions/smoke-copilot-dynamic-workflow/`.

Wait for the run to finish and inspect its durable result. PASS requires status
`completed` and this exact result:

```json
{
  "status": "PASS",
  "marker": "GH_AW_DYNAMIC_WORKFLOW_SMOKE_OK",
  "packageVersion": 1,
  "supportFile": "support/.fixture.json",
  "subagents": 1
}
```

Never infer success from registration, logs, or the expected marker alone.
If discovery, packaging, execution, or verification fails, report FAIL with the
actual error. Do not install extensions or synthesize a replacement result.

Create one issue titled **"Smoke Test: Copilot Dynamic Workflow - ${{ github.run_id }}"**
with overall PASS/FAIL, the dynamic run ID and status, actual result or error,
and run URL ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}.
Keep the report under ten lines. If a report cannot be produced, use `noop`
with the blocking reason; do not claim PASS.
