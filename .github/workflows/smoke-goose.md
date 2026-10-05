---
private: true
emoji: "🪿"
description: Smoke test workflow that validates Goose engine functionality
on:
  schedule: every 2 days
  slash_command:
    name: smoke-goose
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["water"]
  reaction: "rocket"
  status-comment: true
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
name: Smoke Goose
model: copilot/auto
engine:
  id: goose
max-turns: 30
max-ai-credits: 5
strict: true
imports:
  - shared/goose.md
  - shared/gh.md
  - shared/reporting-otlp.md
  - shared/otlp.md
  - shared/token-telemetry-check.md
  - shared/smoke-test-brevity.md
network:
  allowed:
    - defaults
    - github
    - go
runtimes:
  go:
    version: "1.26.8"
tools:
  cache-memory: true
  github:
    toolsets: [repos, pull_requests]
  edit:
  bash:
    - "*"
mcp-scripts:
  fetch-homepage:
    description: Fetch the GitHub homepage and verify its content for the Goose smoke test.
    env:
      SMOKE_STATE: ${{ runner.temp }}/smoke-goose
    script: |
      const fs = require("node:fs");
      const path = require("node:path");
      const response = await fetch("https://github.com", { signal: AbortSignal.timeout(30000) });
      if (!response.ok) throw new Error(`GitHub homepage returned HTTP ${response.status}`);
      const containsGitHub = (await response.text()).includes("GitHub");
      if (!containsGitHub) throw new Error("GitHub homepage did not contain GitHub");
      fs.mkdirSync(process.env.SMOKE_STATE, { recursive: true, mode: 0o700 });
      fs.writeFileSync(path.join(process.env.SMOKE_STATE, "web-fetch.json"), JSON.stringify({ containsGitHub }), { mode: 0o600 });
      return { containsGitHub };
post-steps:
  - name: Assert Goose smoke evidence
    if: always()
    uses: actions/github-script@v9
    env:
      SMOKE_STATE: ${{ runner.temp }}/smoke-goose
      SMOKE_ROOT: /tmp/gh-aw/agent
      SMOKE_RUN_ID: ${{ github.run_id }}
      SMOKE_EXECUTION: ${{ steps.agentic_execution.outcome }}
    with:
      script: |
        const assert = require("node:assert/strict");
        const fs = require("node:fs");
        const path = require("node:path");
        const read = file => {
          const stat = fs.lstatSync(file);
          assert.ok(stat.isFile() && !stat.isSymbolicLink(), "Smoke evidence must be a regular file");
          assert.ok(stat.size <= 16384, "Smoke evidence exceeds 16 KiB");
          return fs.readFileSync(file, "utf8");
        };
        assert.equal(process.env.SMOKE_EXECUTION, "success", "Goose execution failed");
        const root = process.env.SMOKE_ROOT;
        const run = process.env.SMOKE_RUN_ID;
        const result = JSON.parse(read(path.join(root, `smoke-test-goose-${run}.json`)));
        assert.deepEqual(Object.keys(result).sort(), ["bash", "build", "fileWrite", "pullRequests", "runtime", "webFetch"]);
        for (const key of ["bash", "build", "fileWrite", "runtime", "webFetch"]) {
          assert.equal(result[key], true, `Goose smoke check ${key} did not pass`);
        }
        assert.equal(result.pullRequests.length, 2, "Exactly two merged PRs are required");
        assert.equal(new Set(result.pullRequests.map(pr => pr.number)).size, 2, "PRs must be distinct");
        for (const pr of result.pullRequests) {
          assert.deepEqual(Object.keys(pr).sort(), ["number", "title"]);
          assert.ok(Number.isSafeInteger(pr.number) && pr.number > 0);
          const { data } = await github.rest.pulls.get({ ...context.repo, pull_number: pr.number });
          assert.ok(data.merged_at, `PR ${pr.number} is not merged`);
          assert.equal(pr.title, data.title);
        }
        assert.deepEqual(JSON.parse(read(path.join(process.env.SMOKE_STATE, "web-fetch.json"))), { containsGitHub: true });
        assert.match(read(path.join(root, `smoke-test-goose-${run}.txt`)), /^Smoke test passed for Goose at .+\n?$/);
        const binary = fs.lstatSync(path.join(process.env.GITHUB_WORKSPACE, "gh-aw"));
        assert.ok(binary.isFile() && !binary.isSymbolicLink() && (binary.mode & 0o111) !== 0, "gh-aw build output is missing");
        const log = fs.readFileSync("/tmp/gh-aw/agent-stdio.log", "utf8");
        assert.ok(log.includes("[goose-harness] verified Goose 1.53.0"), "Pinned CLI verification is missing");
        assert.ok(!log.includes("Failed to start extension"), "A Goose MCP extension failed to connect");
        const events = log.split("\n").filter(line => line.trim().startsWith("{")).map(line => JSON.parse(line));
        assert.ok(events.some(event => event.type === "complete"), "Goose did not finish a structured run");
        const requests = events.flatMap(event => event.message?.content || []).filter(item =>
          item.type === "toolRequest" && item.toolCall?.status === "success" && item.toolCall.value.name.startsWith("github__"));
        const responses = events.flatMap(event => event.message?.content || []).filter(item =>
          item.type === "toolResponse" && item.toolResult?.status === "success" && item.toolResult.value.isError !== true);
        assert.ok(requests.some(request => responses.some(response => response.id === request.id)), "No successful native GitHub MCP round-trip");
        await core.summary.addHeading("Goose smoke: PASS", 2).addRaw("All six checks passed with host-verified file, build, web-fetch, PR, and native MCP evidence.\n").write();
safe-outputs:
  allowed-domains: [default-safe-outputs]
  add-comment:
    hide-older-comments: true
    max: 2
  create-issue:
    expires: 2h
    close-older-issues: true
    close-older-key: "smoke-goose"
    labels: [automation, testing]
  add-labels:
    allowed: [smoke-goose]
  messages:
    footer: "> 🪿 *[{workflow_name}]({run_url}) — Powered by Goose*{ai_credits_suffix}{history_link}"
    run-started: "🪿 Goose initializing... [{workflow_name}]({run_url}) begins on this {event_type}..."
    run-success: "🪿 [{workflow_name}]({run_url}) Goose delivered."
    run-failure: "⚠️ [{workflow_name}]({run_url}) {status}. Goose encountered unexpected challenges..."
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Goose Engine Validation

## Test Requirements

Perform the six checks in order. Never infer or invent success from a missing tool,
failed command, or unavailable response. Report a failure explicitly and do not
mark overall PASS unless every check passed.

1. **GitHub MCP Testing**: Use native GitHub MCP tools to fetch details of exactly 2 distinct merged pull requests from ${{ github.repository }} (title and number only). Do not substitute `gh`, REST calls, or guessed PR data.
2. **Web Fetch Testing**: Run `mcpscripts fetch-homepage` through your shell capability. This explicit MCP tool fetches https://github.com and returns `containsGitHub: true`; it also records a host-side receipt. Goose has no native gh-aw `web-fetch` tool. Do not substitute curl or a browser.
3. **File Writing Testing**: Create a test file `/tmp/gh-aw/agent/smoke-test-goose-${{ github.run_id }}.txt` with content "Smoke test passed for Goose at $(date)" (create the directory if it doesn't exist)
4. **Bash Tool Testing**: Execute bash commands to verify file creation was successful (use `cat` to read the file back)
5. **Build gh-aw**: Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build` to verify the agent can successfully build the gh-aw project. If the command fails, mark this test as ❌ and report the failure.
6. **Runtime Configuration Testing**: Run `goose --version` and verify it equals `GH_AW_ENGINE_VERSION`. With Node.js, verify `GOOSE_PROVIDER` is `openai`, `GOOSE_MODEL` is nonempty, `OPENAI_HOST` and `OPENAI_BASE_PATH` are set, and `GH_AW_MAX_TURNS` is `30`. Parse the file at `GOOSE_ADDITIONAL_CONFIG_FILES` and verify the `github` extension uses `streamable_http`, its URI is not `localhost` or `127.0.0.1`, and it has an Authorization header. Report only PASS/FAIL; never print headers, keys, configuration, or the prompt.

Write `/tmp/gh-aw/agent/smoke-test-goose-${{ github.run_id }}.json` with exactly
`pullRequests` (the two `{number, title}` objects), `webFetch`, `fileWrite`,
`bash`, `build`, and `runtime` (each a boolean). Set booleans to true only after
the corresponding check succeeds. The host post-step independently checks this
evidence and fails the job for missing evidence or any failed check.

## Output

**ALWAYS create an issue** with a summary of the smoke test run:
- Title: "Smoke Test: Goose - ${{ github.run_id }}"
- Body should include:
  - Test results (✅ or ❌ for each test)
  - Overall status: PASS or FAIL
  - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
  - Timestamp

  Use `safeoutputs create-issue` through your shell capability to submit the issue.
  Use `safeoutputs add-comment` and `safeoutputs add-labels` for the conditional
  PR outputs below; these are mounted MCP CLIs, not native Goose tool names.

**Only if this workflow was triggered by a pull_request event**: Use the `add_comment` tool to add a **very brief** comment (max 5-10 lines) to the triggering pull request (omit the `item_number` parameter to auto-target the triggering PR) with:
- ✅ or ❌ for each test result
- Overall status: PASS or FAIL

If all tests pass and this workflow was triggered by a pull_request event, use the `add_labels` safe-output tool to add the label `smoke-goose` to the pull request (omit the `item_number` parameter to auto-target the triggering PR).