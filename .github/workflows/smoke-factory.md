---
private: true
description: Smoke test for Factory Droid headless execution and native MCP
name: Smoke Factory
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: factory
model: factory/claude-sonnet-4-6
strict: true
imports:
  - shared/factory.md
  - shared/smoke-test-brevity.md
network:
  allowed:
    - defaults
    - github
    - go
runtimes:
  go:
    version: "1.27.3"
safe-outputs:
  create-issue:
    expires: 2h
    close-older-issues: true
    close-older-key: smoke-factory
    labels: [automation, testing]
timeout-minutes: 15
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke Test: Factory Engine Validation

Report PASS or FAIL for each requirement. Never print API keys, authorization
headers, configuration contents, or the prompt.

1. Use native GitHub MCP tools to fetch the titles and numbers of exactly two
   merged pull requests from ${{ github.repository }}. Do not substitute `gh`,
   curl, or another CLI. Missing or failing native MCP tools mean FAIL.
2. Create `/tmp/gh-aw/agent/smoke-factory-${{ github.run_id }}.txt` with the
   content `Factory smoke test passed`. Use a shell command to read it back and
   verify its contents.
3. Run `GOCACHE=/tmp/gh-aw/agent/go-cache GOMODCACHE=/tmp/gh-aw/agent/go-mod make build`.
   A nonzero exit means FAIL.
4. Verify `droid --version` matches `GH_AW_ENGINE_VERSION`. Use Node.js to
   inspect `GH_AW_MCP_CONFIG` and verify the GitHub MCP server uses `type: "http"`,
   a container-reachable URL, `oauth: false`, and an authorization header.
   Report only PASS or FAIL, not configuration values.

Always use the `create_issue` safe-output tool to publish a concise report titled
"Smoke Test: Factory - ${{ github.run_id }}" with each result, overall PASS or
FAIL, and the run URL:
${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}.
