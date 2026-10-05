---
private: true
emoji: "🧑‍✈️"
description: Smoke test workflow that validates Aider engine functionality
on:
  schedule: every 2 days
  slash_command:
    name: smoke-aider
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["water"]
  reaction: "rocket"
  status-comment: true
permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read
name: Smoke Aider
model: copilot/claude-sonnet-4.6
engine:
  id: aider
strict: true
imports:
  - shared/aider.md
  - shared/reporting.md
network:
  allowed: []
tools:
  edit:
  bash:
    - "*"
safe-outputs:
  allowed-domains: [default-safe-outputs]
  add-comment:
    hide-older-comments: true
    max: 2
  create-issue:
    expires: 2h
    close-older-issues: true
    close-older-key: "smoke-aider"
    labels: [automation, testing]
  add-labels:
    allowed: [smoke-aider]
  messages:
    footer: "> 🧑‍✈️ *[{workflow_name}]({run_url}) — Powered by Aider*{ai_credits_suffix}{history_link}"
    run-started: "🧑‍✈️ Aider initializing... [{workflow_name}]({run_url}) begins on this {event_type}..."
    run-success: "🧑‍✈️ [{workflow_name}]({run_url}) Aider delivered."
    run-failure: "⚠️ [{workflow_name}]({run_url}) {status}. Aider encountered unexpected challenges..."
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
post-steps:
  - name: Verify Aider smoke test evidence
    if: always()
    env:
      SMOKE_FILE_PATH: /tmp/gh-aw/agent/smoke-test-aider-${{ github.run_id }}.txt
      SMOKE_STDIO_PATH: /tmp/gh-aw/agent-stdio.log
    run: |
      node <<'NODE'
      const fs = require("fs");
      const path = require("path");
      const { parseLogEntries } = require(path.join(process.env.RUNNER_TEMP, "gh-aw", "actions", "log_parser_shared.cjs"));
      const content = fs.readFileSync(process.env.SMOKE_FILE_PATH, "utf8");
      if (content.trim() !== "Smoke test passed for Aider") {
        throw new Error("Aider did not create the expected smoke-test file content");
      }
      const events = (parseLogEntries(fs.readFileSync(process.env.SMOKE_STDIO_PATH, "utf8")) || [])
        .filter(event => event.data?.sourceEngine === "aider");
      const commands = new Map(events.filter(event => event.type === "tool.execution_start")
        .map(event => [event.data.toolCallId, event.data.input?.command]));
      const verifiedRepository = events.some(event => {
        if (event.type !== "tool.execution_complete" || event.data.success !== true) return false;
        const command = commands.get(event.data.toolCallId) || "";
        const output = String(event.data.output || "").replace(/\x1b\[[0-9;]*m/g, "");
        return /\bgit\s+(?:--no-pager\s+)?log\b/.test(command) && /^[0-9a-f]{7,40} \S.*$/m.test(output);
      });
      if (!verifiedRepository) {
        throw new Error("Aider did not verify repository access with a successful git log command");
      }
      console.log("Verified Aider file writing, shell execution, repository access, and native session evidence");
      NODE
---

# Smoke Test: Aider Engine Validation

Aider has no MCP client support, so this smoke test exercises only the CLI-native
capabilities: prompt delivery, shell commands, and file editing. Safe outputs are
emitted through the `safeoutputs` MCP CLI.

Reply with the complete SEARCH/REPLACE edit and the verification bash block.
Omit explanatory prose, but do not omit any required check or the safe-output call.

## Test Requirements

1. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-aider-${{ github.run_id }}.txt` with content "Smoke test passed for Aider". Use a *SEARCH/REPLACE* block with an empty SEARCH section, not a shell redirect.
2. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
3. **Repository Access Testing**: Run `git log --oneline -1` in the repository checkout and confirm a commit is reported

## Output

After creating the file, **verify its contents and repository access before
creating the issue**. Include this single command inside a fenced `bash` block
in your reply. Keep the whole command on one line; plain prose and inline code
do not execute. Only report PASS after both checks succeed:

```bash
test "$(cat /tmp/gh-aw/agent/smoke-test-aider-${{ github.run_id }}.txt)" = "Smoke test passed for Aider" && git log --oneline -1 && safeoutputs create_issue --title "Smoke Test: Aider - ${{ github.run_id }}" --body "File writing: PASS. Bash execution: PASS. Repository access: PASS. Overall status: PASS. Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}"
```

If a check fails, create the issue with overall status FAIL and describe the
failure instead. A deterministic post-agent check rejects missing repository
access evidence even if the issue body claims PASS.

- Title: "Smoke Test: Aider - ${{ github.run_id }}"
- Body should include:
  - Test results (✅ or ❌ for each test)
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

**Only if this workflow was triggered by a pull_request event**: invoke
`safeoutputs add_comment --body "..."` with a **very brief**
comment (max 5-10 lines) containing:
- ✅ or ❌ for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, also invoke
`safeoutputs add_labels --labels "smoke-aider"`.