---
private: true
name: UK AI Operational Resilience
description: Applies UK public-sector AI open-code and vulnerability-risk guidance using recent-change governance, risk scoring, and remediation orchestration
on:
  schedule:
    - cron: daily on weekdays
  workflow_dispatch:
    inputs:
      lookback_days:
        description: "How many days of recent changes to analyze (1-30)"
        required: false
        default: "7"
      force_full_scan:
        description: "Ignore recent-change focus and run wider analysis"
        required: false
        default: false
        type: boolean
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read
  security-events: read
tracker-id: uk-ai-operational-resilience
engine:
  id: copilot
  copilot-sdk: true
strict: true
timeout-minutes: 30
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default, actions, repos, code_security, issues]
  bash:
    - "git *"
    - "cat *"
    - "jq *"
    - "ls *"
    - "find *"
    - "wc *"
    - "grep *"
  edit:
safe-outputs:
  create-issue:
    expires: 2d
    max: 2
    labels: [security, high-priority, ai-generated, cookie, "backlog:security"]
    title-prefix: "[uk-ai-resilience] "
pre-agent-steps:
  - name: Pre-compute recent changes governance context
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      LOOKBACK_DAYS: ${{ github.event.inputs.lookback_days || '7' }}
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent

      REPO="${GITHUB_REPOSITORY}"
      DAYS="${LOOKBACK_DAYS}"
      if ! [[ "$DAYS" =~ ^[0-9]+$ ]]; then DAYS=7; fi
      if [ "$DAYS" -lt 1 ]; then DAYS=1; fi
      if [ "$DAYS" -gt 30 ]; then DAYS=30; fi

      SINCE="$(date -u -d "-${DAYS} days" +%Y-%m-%dT%H:%M:%SZ)"
      echo "$SINCE" >/tmp/gh-aw/agent/since.txt

      gh api --paginate "repos/${REPO}/commits?since=${SINCE}&per_page=100" | jq -s 'add // []' > /tmp/gh-aw/agent/recent-commits.json
      gh api --paginate "repos/${REPO}/issues?state=open&labels=security&per_page=100" | jq -s 'add // []' > /tmp/gh-aw/agent/open-security-issues.json
      gh api --paginate "repos/${REPO}/code-scanning/alerts?state=open&per_page=100" | jq -s 'add // []' > /tmp/gh-aw/agent/open-code-scanning-alerts.json || echo "[]" >/tmp/gh-aw/agent/open-code-scanning-alerts.json
      gh api --paginate "repos/${REPO}/secret-scanning/alerts?state=open&per_page=100" | jq -s 'add // []' > /tmp/gh-aw/agent/open-secret-scanning-alerts.json || echo "[]" >/tmp/gh-aw/agent/open-secret-scanning-alerts.json

      jq -r '
        [.[].commit.message]
        | map(select(type=="string"))
        | map(
            if test("security|vuln|cve|patch|auth|secret|token|permissions|hardening"; "i")
            then .
            else empty
            end
          )' /tmp/gh-aw/agent/recent-commits.json > /tmp/gh-aw/agent/security-signal-commits.json

      {
        echo "## UK AI Governance Pre-compute"
        echo "- Repository: ${REPO}"
        echo "- Lookback days: ${DAYS}"
        echo "- Since: ${SINCE}"
        echo "- Commits in window: $(jq 'length' /tmp/gh-aw/agent/recent-commits.json)"
        echo "- Security-signal commits: $(jq 'length' /tmp/gh-aw/agent/security-signal-commits.json)"
        echo "- Open security issues: $(jq 'length' /tmp/gh-aw/agent/open-security-issues.json)"
        echo "- Open code scanning alerts: $(jq 'length' /tmp/gh-aw/agent/open-code-scanning-alerts.json)"
        echo "- Open secret scanning alerts: $(jq 'length' /tmp/gh-aw/agent/open-secret-scanning-alerts.json)"
      } > /tmp/gh-aw/agent/uk-ai-governance-context.md
imports:
  - shared/mcp-pagination.md
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[uk ai resilience] "
      expires: 3d
  - shared/otlp.md
  - shared/reporting.md
---
{{#runtime-import? .github/shared-instructions.md}}

# UK AI Open Code Risk & Resilience Governance

Apply the UK public-sector guidance:
https://www.gov.uk/guidance/ai-open-code-and-vulnerability-risk-in-the-public-sector

Use a **recent-changes focus strategy** to prioritize repository areas that changed within the selected lookback window, then evaluate operational resilience instead of relying on concealment.

## Inputs (pre-computed)

Read these files first:

- `/tmp/gh-aw/agent/uk-ai-governance-context.md`
- `/tmp/gh-aw/agent/recent-commits.json`
- `/tmp/gh-aw/agent/security-signal-commits.json`
- `/tmp/gh-aw/agent/open-security-issues.json`
- `/tmp/gh-aw/agent/open-code-scanning-alerts.json`
- `/tmp/gh-aw/agent/open-secret-scanning-alerts.json`

If all of these are empty/zero and this is not a forced full scan, call `noop` with a brief summary.

## Strategy

1. **Recent-changes first**
   - Focus on components, files, workflows, and dependencies touched in the lookback window.
   - Expand scope only if risk signals indicate systemic gaps.
2. **Resilience over secrecy**
   - Evaluate recoverability and remediation velocity.
   - Do not recommend repository hiding as a default control.
3. **Operational governance loop**
   - Classification → control verification → AI-aware risk scoring → decision → remediation tasks.

## Required Workflow

### Phase 1 — Build a lightweight asset graph

From recent commits and open alerts, identify:

- repository segments and changed surfaces
- likely services/runtime areas
- dependencies implicated by alerts or updates
- ownership signals (maintainers/reviewers/escalation clues)

Use the `asset-tier-classifier` sub-agent for concise structured output.

### Phase 2 — Verify control domains

Verify and summarize evidence for:

- ownership controls
- SDLC controls
- dependency controls
- secret exposure controls
- runtime observability controls
- recovery controls (patch/rollback readiness)

Use the `control-verifier` sub-agent.

### Phase 3 — AI-aware risk scoring

Use the `ai-risk-scorer` sub-agent to score each high-priority changed area with these dimensions:

- exposure amplification
- patchability
- detectability
- operational fragility
- ownership confidence

Assign each area to one tier:

- Tier A — Open Safe
- Tier B — Open With Conditions
- Tier C — Restricted Pending Review
- Tier D — Decommission Candidate

### Phase 4 — Decisioning and remediation

For each Tier B/C/D area:

- define explicit remediation actions
- assign SLA urgency (critical/high/medium/low)
- define human-review triggers where confidence is low or risk is severe
- define exception governance fields where applicable:
  - threat hypothesis
  - exploit acceleration claim
  - operational weakness
  - expiry date
  - mitigation plan

### Phase 5 — Continuous reassessment outputs

Create one discussion report with:

1. Executive summary
2. Asset graph summary (recent-change scoped)
3. Tier classification table
4. Control verification gaps
5. Risk-scoring table and rationale
6. Remediation queue with SLAs
7. Exception register (if any)
8. Operational metrics baseline:
   - MTTR proxy
   - ownership coverage
   - unsupported dependency ratio
   - exception aging
   - exposure without recovery capability

Follow the `reporting` skill: use `###` (h3) or lower for section headers, and wrap long tables (asset graph, tier classification, risk-scoring) in `<details><summary><b>...</b></summary>...</details>` blocks so the executive summary stays scannable.

### Phase 6 — Create issues for high-priority items

Create at most **2** GitHub issues per run for the highest-priority Tier B/C/D findings:

- Prioritize Tier C/D findings first, then highest `remediation_priority` (`critical`, then `high`).
- Before creating an issue, check for duplicate open issues and skip duplicates.
- Each issue body must include:
  - tier and risk-scoring breakdown
  - remediation action and SLA urgency
  - discussion report link
- Do not exceed 2 issues in total for the run.

## Sub-agent orchestration rules

- Prefer dispatching all three sub-agents in one parallel tool-use block before synthesis. If your engine/runtime cannot do true parallel dispatch, run them sequentially in this order: `asset-tier-classifier` → `control-verifier` → `ai-risk-scorer`.
- Treat sub-agent outputs as structured evidence and cite them in final conclusions.
- If a sub-agent fails once, retry once; if it fails again, proceed with partial confidence and record that limitation.

## Guardrails

- Keep permissions read-only and use safe outputs only.
- Prefer concrete, verifiable findings over speculative claims.
- Never recommend permanent hidden-repo exceptions; all exceptions must be temporary and auditable.
- Prioritize actions that improve remediation velocity and operational resilience.
## agent: `asset-tier-classifier`
---
description: Builds recent-change-scoped asset graph candidates and proposes initial A/B/C/D tier hypotheses
model: small
---
You are a governance classification specialist.

Given recent commits and open security signals, produce compact JSON with:
- `assets`: array of changed areas with `name`, `surface`, `owner_signal`, `dependency_signal`
- `initial_tier`: one of A/B/C/D
- `confidence`: low|medium|high
- `notes`: short rationale

Output contract:
- Return a single JSON object with keys exactly: `assets`, `summary`, `errors`.
- `summary` must include `total_assets` and `high_concern_assets`.
- `errors` must be an array (empty when none).

Focus on changed surfaces first; avoid broad full-repo expansion unless evidence requires it.

## agent: `control-verifier`
---
description: Verifies ownership, SDLC, dependency, secret, runtime, and recovery controls for changed areas
model: small
---
You are a control verification specialist.

For each changed area, output JSON with:
- `ownership_controls`
- `sdlc_controls`
- `dependency_controls`
- `secret_controls`
- `runtime_controls`
- `recovery_controls`

Each control section must include:
- `status`: pass|partial|fail
- `evidence`: concise bullets
- `gap`: single most important missing control

Output contract:
- Return a single JSON object with keys exactly: `areas`, `summary`, `errors`.
- `areas` must be an array where each item has `asset_name` and the six control sections above.
- `summary` must include `pass_count`, `partial_count`, and `fail_count`.
- `errors` must be an array (empty when none).

## agent: `ai-risk-scorer`
---
description: Produces AI-aware risk scoring and action tiering for recently changed areas
model: small
---
You are an AI-era operational risk scorer.

For each candidate area, provide JSON fields:
- `exposure_amplification`: 1-5
- `patchability`: 1-5
- `detectability`: 1-5
- `operational_fragility`: 1-5
- `ownership_confidence`: 1-5
- `tier`: A|B|C|D
- `decision`: maintain-open|open-with-conditions|restrict-pending-review|decommission-candidate
- `remediation_priority`: critical|high|medium|low
- `reason`

Scoring philosophy: higher exposure+fragility and lower patchability+detectability+ownership implies higher operational risk.

Output contract:
- Return a single JSON object with keys exactly: `scores`, `summary`, `errors`.
- `scores` must be an array of scored areas using the fields above.
- `summary` must include `tier_counts` and `highest_priority_assets`.
- `errors` must be an array (empty when none).
