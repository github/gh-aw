---
private: true
description: Smoke test for the LangGraph agent graph shell, file tools, and CLI-proxy safe outputs
name: Smoke LangGraph
on:
  workflow_dispatch:
permissions:
  copilot-requests: write
  contents: read
  issues: read
engine:
  id: langgraph
model: copilot/claude-haiku-5.5
imports:
  - shared/langgraph.md
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
    close-older-key: smoke-langgraph
    labels: [automation, testing]
timeout-minutes: 10
max-turns: 20
sandbox:
  agent:
    id: awf
post-steps:
  - name: Verify LangGraph smoke evidence
    if: always()
    run: |
      python3 -I - <<'PY'
      import json
      import re
      from pathlib import Path
      assert Path("/tmp/gh-aw/agent/smoke-langgraph.txt").read_text().strip() == "Smoke test passed for LangGraph"
      trajectory = json.loads(Path("/tmp/gh-aw/agent/langgraph.traj.json").read_text())
      assert trajectory["exit_status"] == "Completed", "LangGraph did not complete"
      calls = {}
      repository_verified = False
      for message in trajectory["messages"]:
          data = message["data"]
          for call in data.get("tool_calls") or []:
              calls[call["id"]] = call
          call = calls.get(data.get("tool_call_id"))
          if call and call["name"] == "run_shell" and call["args"].get("command", "").strip() == "git log --oneline -1":
              output = data["content"]
              repository_verified = output.startswith("exit code: 0\n") and bool(re.search(r"^[0-9a-f]{7,40} \S.*$", output, re.MULTILINE))
      assert repository_verified, "LangGraph did not successfully inspect the repository"
      PY
---

# Smoke Test: LangGraph

Use the `write_file` tool to create `/tmp/gh-aw/agent/smoke-langgraph.txt`
containing exactly `Smoke test passed for LangGraph`. Use `read_file` to read
it back and verify it. Use `run_shell` to run exactly `git log --oneline -1` as
a separate command to confirm repository access.

Only after these checks succeed, use `run_shell` to call `safeoutputs create_issue`
and create an issue titled `Smoke Test: LangGraph - ${{ github.run_id }}`.
Include file writing, file reading, shell execution, repository access, and
overall PASS/FAIL results, plus the run URL:
`${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.

If a check fails, report FAIL instead. Finish with a short summary and no tool calls.
