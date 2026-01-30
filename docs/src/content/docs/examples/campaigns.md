---
title: Campaign Examples
description: Example campaign workflows with worker patterns and idempotency
sidebar:
  badge: { text: 'Examples', variant: 'note' }
---

Example campaigns demonstrating worker coordination, standardized contracts, and idempotent execution patterns.

## Security Audit Campaign

**Security Audit 2026** demonstrates:

- Worker discovery via tracker labels
- Dispatch-only worker design
- Standardized input contracts (`campaign_id`, `payload`)
- Idempotent execution with deterministic keys
- KPI tracking for vulnerability metrics

The campaign coordinates three workers (scanner, fixer, reviewer) that create security-related issues and pull requests. Workers use deterministic branch names and titles to prevent duplicates.

### Key patterns

**Orchestrator responsibilities (dispatch-only):**
- Runs discovery precomputation (writes a manifest for the agent)
- Dispatches workers on schedule
- Coordinates work by dispatching allowlisted workflows

**Worker responsibilities:**
- Accepts `workflow_dispatch` only
- Uses standardized inputs
- Generates deterministic keys
- Checks for existing work
- Labels outputs with the campaign tracker label (defaults to `z_campaign_<id>`)

## Security Scanner Worker

[**Security Scanner**](/gh-aw/examples/campaigns/security-scanner/) shows worker implementation:

```yaml
on:
  workflow_dispatch:
    inputs:
      campaign_id:
        description: 'Campaign identifier'
        required: true
        type: string
      payload:
        description: 'JSON payload with work item details'
        required: true
        type: string
```

The worker:
1. Receives dispatch from orchestrator
2. Scans for vulnerabilities
3. Generates deterministic key: `campaign-{id}-{repo}-{vuln_id}`
4. Checks for existing PR with that key
5. Creates PR only if none exists
6. Labels PR with `z_campaign_{id}`

## Worker design patterns

### Standardized contract

All campaign workers accept the same inputs:

```yaml
inputs:
  campaign_id:
    description: 'Campaign identifier'
    required: true
    type: string
  payload:
    description: 'JSON payload with work details'
    required: true
    type: string
```

The payload contains work item details in JSON format.

### Idempotency

Workers prevent duplicate work using deterministic keys:

```
campaign-{campaign_id}-{repository}-{work_item_id}
```

Used in:
- Branch names: `campaign-security-audit-myorg-myrepo-vuln-123`
- PR titles: `[campaign-security-audit] Fix vulnerability 123`
- Issue titles: `[campaign-security-audit] Security finding 123`

Before creating items, workers search for existing items with the same key.

### Dispatch-only triggers

Workers in the campaign's `workflows` list must use only `workflow_dispatch`:

```yaml
# Correct
on:
  workflow_dispatch:
    inputs: ...

# Incorrect - campaign-controlled workers should not have other triggers
on:
  schedule: daily
  workflow_dispatch:
    inputs: ...
```

Workflows with schedules or event triggers should run independently and let the campaign discover their outputs.

## File organization

```
.github/workflows/
├── security-audit.md                    # Campaign orchestrator workflow
├── security-audit.lock.yml              # Compiled orchestrator
├── security-scanner.md                  # Worker workflow
├── security-fixer.md                    # Worker workflow
└── security-reviewer.md                 # Worker workflow
```

Workers are regular workflows, not in campaign-specific folders. The dispatch-only trigger indicates campaign ownership.

## Campaign lifecycle integration

### Startup

1. Orchestrator dispatches workers
2. Workers create issues/PRs with campaign labels
3. Next run discovers these items
4. Items added to project board

### Ongoing execution

1. Orchestrator dispatches workers (new work)
2. Discovers outputs from previous runs
3. Updates project board incrementally
4. Reports progress against KPIs

### Completion

1. All work items processed
2. Final status update with metrics
3. Campaign state set to `completed`
4. Orchestrator disabled

## Idempotency example

```yaml
# Worker checks for existing PR before creating
- name: Check for existing PR
  id: check
  run: |
    KEY="campaign-${{ inputs.campaign_id }}-${{ github.repository }}-vuln-123"
    EXISTING=$(gh pr list --search "$KEY in:title" --json number --jq '.[0].number')
    echo "existing=$EXISTING" >> $GITHUB_OUTPUT

- name: Create PR
  if: steps.check.outputs.existing == ''
  uses: ./actions/safe-output
  with:
    type: create_pull_request
    title: "[$KEY] Fix vulnerability 123"
    body: "Automated security fix"
    labels: "z_campaign_${{ inputs.campaign_id }}"
```

## Independent workflows

Workflows not in the campaign's `workflows` list can run independently with their own triggers:

```yaml
# Independent worker - keeps its schedule
on:
  schedule:
    - cron: '0 2 * * *'
  workflow_dispatch:

# Creates items with campaign label for discovery
labels: ["z_campaign_security-audit", "security"]

The recommended campaign tracking label format is `z_campaign_<id>`. If you need compatibility with older tooling that filters `campaign:*` labels, you can optionally apply both labels.
```

The campaign discovers these via tracker labels without controlling execution.

## Further reading

- [Campaign guides](/gh-aw/guides/campaigns/) - Setup and configuration
- [Safe outputs](/gh-aw/reference/safe-outputs/) - dispatch_workflow configuration
