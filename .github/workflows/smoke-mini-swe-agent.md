---
private: true
description: Smoke test for mini-SWE-agent shell, file editing, and CLI-proxy safe outputs
name: Smoke mini-SWE-agent
on:
  workflow_dispatch:
permissions:
  copilot-requests: write
  contents: read
  issues: read
engine:
  id: mini-swe-agent
  model-provider: github
model: copilot/claude-haiku-5.5
imports:
  - shared/mini-swe-agent.md
network:
  allowed: []
tools:
  edit:
  bash:
    - "*"
safe-outputs:
  create-issue:
    expires: 2h
    close-older-issues: true
    close-older-key: smoke-mini-swe-agent
    labels: [automation, testing]
timeout-minutes: 10
max-turns: 20
sandbox:
  agent:
    id: awf
post-steps:
  - name: Verify mini-SWE-agent smoke evidence
    if: always()
    run: |
      python3 -I - <<'PY'
      import json
      import re
      from pathlib import Path
      assert Path("/tmp/gh-aw/agent/smoke-mini-swe-agent.txt").read_text().strip() == "Smoke test passed for mini-SWE-agent"
      trajectory = json.loads(Path("/tmp/gh-aw/agent/mini-swe-agent.traj.json").read_text())
      assert trajectory["info"]["exit_status"] == "Submitted", "mini-SWE-agent did not submit"
      messages = trajectory["messages"]
      repository_verified = False
      for index, message in enumerate(messages):
          actions = message.get("extra", {}).get("actions", [])
          if any(action.get("command", "").strip() == "git log --oneline -1" for action in actions):
              observation = messages[index + 1]["content"]
              repository_verified = "<returncode>0</returncode>" in observation and bool(re.search(r"^[0-9a-f]{7,40} \S.*$", observation, re.MULTILINE))
      assert repository_verified, "mini-SWE-agent did not successfully inspect the repository"
      PY
---

# Smoke Test: mini-SWE-agent

Inference uses the GitHub provider with the workflow token granted
`copilot-requests: write`; no separate provider API key is required.

Create `/tmp/gh-aw/agent/smoke-mini-swe-agent.txt` containing exactly
`Smoke test passed for mini-SWE-agent`. Read the file back to verify it.
Run `git log --oneline -1` as a separate action to confirm repository access.

Only after these checks succeed, use `safeoutputs create_issue` to create an
issue titled `Smoke Test: mini-SWE-agent - ${{ github.run_id }}`. Include file
writing, shell execution, repository access, and overall PASS/FAIL results,
plus the run URL:
`${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.

If a check fails, report FAIL instead. Finally submit the task using
`echo COMPLETE_TASK_AND_SUBMIT_FINAL_OUTPUT` as a separate action.
