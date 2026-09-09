---
private: true
emoji: "🧠"
name: DeepSec Security Scan
description: On-demand DeepSec scan that bootstraps a bounded agentic vulnerability review and files one issue for actionable findings
on:
  workflow_dispatch:
    inputs:
      limit:
        description: "Maximum files for DeepSec to investigate"
        required: false
        default: "25"
      thinking-level:
        description: "DeepSec reasoning effort"
        required: false
        default: medium
        type: choice
        options: [low, medium, high, xhigh]
      agent:
        description: "DeepSec backend"
        required: false
        default: codex
        type: choice
        options: [codex, claude, pi]
permissions:
  contents: read
  issues: read
  actions: read
model: claude-sonnet-5
engine:
  id: claude
strict: true
timeout-minutes: 90
network:
  allowed:
    - defaults
    - node
    - ai-gateway.vercel.sh
sandbox:
  agent:
    runtime: cloud-hypervisor
tools:
  bash:
    - "*"
  edit:
safe-outputs:
  create-issue:
    max: 1
    labels: [security, deepsec, cookie]
    close-older-issues: true
    title-prefix: "[deepsec] "
  noop:
steps:
  - name: Checkout repository
    uses: actions/checkout@v7.0.1
    with:
      persist-credentials: false

  - name: Setup Node.js
    uses: actions/setup-node@v7.0.0
    with:
      node-version: "22"

  - name: Prepare DeepSec workspace
    env:
      AI_GATEWAY_API_KEY: ${{ secrets.AI_GATEWAY_API_KEY }}
      EXPR_INPUTS_AGENT: ${{ inputs.agent }}
      EXPR_INPUTS_LIMIT: ${{ inputs.limit }}
      EXPR_INPUTS_THINKING_LEVEL: ${{ inputs.thinking-level }}
    run: |
      set -euo pipefail

      WORK_ROOT="/tmp/gh-aw/deepsec-workspace"
      AGENT_ROOT="/tmp/gh-aw/agent/deepsec"
      REPO_NAME="$(basename "$GITHUB_WORKSPACE")"
      REPO_SNAPSHOT="$WORK_ROOT/$REPO_NAME"
      PROJECT_ID="$REPO_NAME"

      rm -rf "$WORK_ROOT" "$AGENT_ROOT"
      mkdir -p "$REPO_SNAPSHOT" "$AGENT_ROOT"

      git archive --format=tar HEAD | tar -xf - -C "$REPO_SNAPSHOT"

      cd "$REPO_SNAPSHOT"
      npx --yes deepsec@2.2.4 init
      cd .deepsec
      npm install --save-exact deepsec@2.2.4

      if [ -n "${AI_GATEWAY_API_KEY:-}" ]; then
        AI_GATEWAY_CONFIGURED=true
      else
        AI_GATEWAY_CONFIGURED=false
      fi

      {
        printf '%s\n' '# DeepSec workspace'
        printf '\n'
        printf '%s\n' "- Repo snapshot: $REPO_SNAPSHOT"
        printf '%s\n' "- DeepSec workspace: $REPO_SNAPSHOT/.deepsec"
        printf '%s\n' "- Project id: $PROJECT_ID"
        printf '%s\n' "- Info file: $REPO_SNAPSHOT/.deepsec/data/$PROJECT_ID/INFO.md"
        printf '%s\n' "- Setup file: $REPO_SNAPSHOT/.deepsec/data/$PROJECT_ID/SETUP.md"
        printf '%s\n' "- DeepSec skill: $REPO_SNAPSHOT/.deepsec/node_modules/deepsec/SKILL.md"
        printf '%s\n' "- AI Gateway configured: $AI_GATEWAY_CONFIGURED"
        printf '%s\n' "- Requested agent: $EXPR_INPUTS_AGENT"
        printf '%s\n' "- Requested limit: $EXPR_INPUTS_LIMIT"
        printf '%s\n' "- Requested thinking level: $EXPR_INPUTS_THINKING_LEVEL"
        printf '%s\n' '- Findings directory: /tmp/gh-aw/agent/deepsec/findings'
        printf '%s\n' '- Findings JSON: /tmp/gh-aw/agent/deepsec/findings.json'
      } > "$AGENT_ROOT/context.md"
imports:
  - shared/otlp.md
  - shared/reporting.md
evals:
  - id: deepsec_scan_completed
    question: Did the agent complete the prepared DeepSec scan workflow or explicitly noop when credentials or actionable findings were unavailable?
  - id: deepsec_issue_or_noop
    question: Did the run create exactly one issue for actionable DeepSec findings, or noop with a concise explanation?
features:
  gh-aw-detection: true
---

# DeepSec Security Scan

Use the prepared DeepSec workspace to run a bounded vulnerability scan against the repository snapshot.

## Constraints

- Treat this run as cost-sensitive. Respect the requested backend, file limit, and thinking level from `/tmp/gh-aw/agent/deepsec/context.md`.
- Never print, persist, or quote secret values.
- Use `noop` when the AI Gateway credential is unavailable or when DeepSec produces no actionable findings.

## Task

1. Read `/tmp/gh-aw/agent/deepsec/context.md`.
2. In the prepared DeepSec workspace, read:
   - the workspace context file
   - the repository snapshot's `README.md`
   - the repository snapshot's `AGENTS.md`
   - `.deepsec/node_modules/deepsec/SKILL.md`
   - `.deepsec/data/<project-id>/SETUP.md`
3. Replace the placeholder sections in `.deepsec/data/<project-id>/INFO.md` with a short, project-specific summary. Keep it concise and focused on repository-specific security context.
4. Run these commands from the prepared `.deepsec/` workspace, substituting the project id and requested inputs from the context file:
   - `npx deepsec scan --project-id <project-id>`
   - `npx deepsec process --project-id <project-id> --agent <requested-agent> --limit <requested-limit> --thinking-level <requested-thinking-level>`
   - `npx deepsec export --project-id <project-id> --format json --out /tmp/gh-aw/agent/deepsec/findings.json`
   - `npx deepsec export --project-id <project-id> --format md-dir --out /tmp/gh-aw/agent/deepsec/findings`
5. Review the exported findings. Only report actionable, evidence-backed findings. Ignore duplicates, stale findings, and speculative concerns.
6. If there are no actionable findings, call `noop` with a short explanation that includes the requested backend, limit, and thinking level.
7. If there are actionable findings, create exactly one issue with:
   - title: `DeepSec findings in ${{ github.repository }}`
   - body: a concise summary of up to 5 highest-confidence findings, each including severity, affected file or component, why it matters, and concrete remediation guidance
   - a short note that the run was bounded by `agent=${{ inputs.agent }}`, `limit=${{ inputs.limit }}`, and `thinking-level=${{ inputs.thinking-level }}`