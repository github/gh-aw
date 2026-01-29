# Campaign Workers

Campaign workers are first-class workflows designed to be orchestrated by campaign orchestrators. This document describes the worker pattern, input contract, idempotency requirements, and the bootstrap + worker metadata system.

## Overview

Campaign workers follow these principles:

1. **Dispatch-only**: Workers are triggered via `workflow_dispatch` by orchestrators
2. **Standardized contract**: All workers accept `campaign_id` and `payload` inputs
3. **Idempotent**: Workers use deterministic keys to avoid duplicate work
4. **Orchestration-agnostic**: Workers don't encode orchestration policy
5. **Discoverable**: Workers produce outputs with guaranteed labeling contracts

## Bootstrap + Planning Model

When a campaign starts with zero discovered work items (discovery = 0), the orchestrator needs a way to create initial work. The bootstrap configuration provides three strategies:

### 1. Seeder Worker Mode

Dispatch a specialized worker to discover and create initial work items:

```yaml
bootstrap:
  mode: seeder-worker
  seeder-worker:
    workflow-id: security-scanner
    payload:
      scan-type: full
      max-findings: 100
    max-items: 50
```

**Flow**:
1. Orchestrator detects discovery = 0
2. Orchestrator dispatches the seeder worker with configured payload
3. Seeder worker scans for work and creates issues/PRs with tracker labels
4. Next orchestrator run discovers the seeder's outputs

### 2. Project Todos Mode

Read work items from the Project board's "Todo" column:

```yaml
bootstrap:
  mode: project-todos
  project-todos:
    status-field: Status
    todo-value: Backlog
    max-items: 10
    require-fields:
      - Priority
      - Assignee
```

**Flow**:
1. Orchestrator detects discovery = 0
2. Orchestrator queries Project board for items with Status = "Backlog"
3. Orchestrator uses worker metadata to select appropriate worker for each item
4. Orchestrator dispatches workers with payloads built from Project field values

### 3. Manual Mode

Wait for manual work item creation:

```yaml
bootstrap:
  mode: manual
```

**Flow**:
1. Orchestrator detects discovery = 0
2. Orchestrator reports waiting for manual work item creation
3. Users manually create issues/PRs with proper tracker labels
4. Next orchestrator run discovers the manual items

## Worker Metadata

Worker metadata enables deterministic worker selection and ensures worker outputs are discoverable. Define worker metadata in your campaign spec:

```yaml
workers:
  - id: security-fixer
    name: Security Fix Worker
    description: Fixes security vulnerabilities
    capabilities:
      - fix-security-alerts
      - create-pull-requests
    payload-schema:
      repository:
        type: string
        description: Target repository in owner/repo format
        required: true
        example: owner/repo
      alert_id:
        type: string
        description: Security alert identifier
        required: true
        example: alert-123
      severity:
        type: string
        description: Alert severity level
        required: false
        example: high
    output-labeling:
      labels:
        - security
        - automated
      key-in-title: true
      key-format: "campaign-{campaign_id}-{repository}-{alert_id}"
      metadata-fields:
        - Campaign Id
        - Worker Workflow
        - Alert ID
        - Severity
    idempotency-strategy: pr-title-based
    priority: 10
```

### Worker Metadata Fields

- **id**: Workflow identifier (basename without .md)
- **name**: Human-readable worker name
- **description**: What the worker does
- **capabilities**: List of work types this worker can handle
- **payload-schema**: Expected payload structure with types and descriptions
- **output-labeling**: Guaranteed labeling contract for worker outputs
- **idempotency-strategy**: How the worker ensures idempotent execution
- **priority**: Worker selection priority (higher = preferred)

### Deterministic Worker Selection

When the orchestrator needs to dispatch a worker (e.g., during bootstrap from Project todos):

1. **Match capabilities**: Find workers whose capabilities match the work item type
2. **Validate payload**: Check if worker's payload schema can be satisfied from available data
3. **Select by priority**: If multiple workers match, select the one with highest priority
4. **Build payload**: Construct payload according to worker's payload schema
5. **Dispatch**: Call worker with campaign_id and constructed payload

### Output Labeling Contract

The `output-labeling` section guarantees how worker outputs are labeled and formatted:

- **labels**: Labels the worker applies to created items (in addition to the campaign's tracker-label)
- **key-in-title**: Whether worker includes a deterministic key in item titles
- **key-format**: Format of the key when `key-in-title` is true
- **metadata-fields**: Project fields the worker populates

Workers automatically apply the campaign's tracker label (defined at the campaign level) to all created items, ensuring:
- **Discoverable**: Can be found via tracker label searches
- **Attributable**: Can be traced back to the campaign and worker
- **Idempotent**: Can be checked for duplicates via deterministic keys

## Why Dispatch-Only?

Making workers dispatch-only (no schedule/push/pull_request triggers) provides several benefits:

- **Unambiguous ownership**: Workers are clearly orchestrated, not autonomous
- **Prevents duplicate execution**: Avoids conflicts between original triggers and orchestrator
- **Explicit orchestration**: Orchestrator controls when and how workers run
- **Clear responsibility**: Sequential vs parallel execution is orchestrator's concern

## Input Contract

All campaign workers MUST accept these inputs:

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

### Campaign ID

The `campaign_id` identifies the campaign orchestrating this worker. Use it to:

- Label created items: `z_campaign_${campaign_id}`
- Generate deterministic keys: `campaign-${campaign_id}-${work_item_id}`
- Track work in repo-memory: `memory/campaigns/${campaign_id}/`

### Payload

The `payload` is a JSON string containing work-specific data. Parse it to extract:

- `repository`: Target repository (owner/repo format)
- `work_item_id`: Unique identifier for this work item
- `target_ref`: Target branch/ref (e.g., "main")
- Additional context specific to the worker

Example payload:
```json
{
  "repository": "owner/repo",
  "work_item_id": "alert-123",
  "target_ref": "main",
  "alert_type": "sql-injection",
  "severity": "high",
  "file_path": "src/database/query.go",
  "line_number": 42
}
```

## Idempotency Requirements

Workers MUST implement idempotency to prevent duplicate work over repeated orchestrator runs.

### Deterministic Work Item Keys

Compute a stable key for each work item:

```
campaign-{campaign_id}-{repository}-{work_item_id}
```

Example: `campaign-security-q1-2025-myorg-myrepo-alert-123`

Use this key in:
- Branch names: `fix/campaign-security-q1-2025-myorg-myrepo-alert-123`
- PR titles: `[campaign-security-q1-2025-myorg-myrepo-alert-123] Fix SQL injection`
- Issue titles: `[alert-123] High severity: SQL injection vulnerability`

### Check Before Create

Before creating any GitHub resource:

1. **Search for existing items** with the deterministic key
2. **Filter by campaign tracker label**: `z_campaign_${campaign_id}`
3. **If found**: Skip or update existing item
4. **If not found**: Proceed with creation

Example:
```javascript
const workKey = `campaign-${campaignId}-${repository}-${workItemId}`;
const searchQuery = `repo:${repository} is:pr is:open "${workKey}" in:title`;

const existingPRs = await github.search.issuesAndPullRequests({
  q: searchQuery
});

if (existingPRs.total_count > 0) {
  console.log(`PR already exists: ${existingPRs.items[0].html_url}`);
  return; // Skip creation
}
```

### Label All Created Items

Apply the campaign tracker label to all created items:

- Label format: `z_campaign_${campaign_id}`
- Prevents interference from other workflows
- Enables discovery by orchestrator

## Worker Template

```yaml
---
name: My Campaign Worker
description: Worker workflow for campaign orchestration

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

tracker-id: my-campaign-worker

tools:
  github:
    toolsets: [default]

safe-outputs:
  create-pull-request:
    max: 1
  add-comment:
    max: 2
---

# My Campaign Worker

You are a campaign worker that processes work items.

## Step 1: Parse Input

Parse the workflow_dispatch inputs:

```javascript
const campaignId = context.payload.inputs.campaign_id;
const payload = JSON.parse(context.payload.inputs.payload);
```

Extract work item details from payload:
- `repository`: Target repository
- `work_item_id`: Unique identifier
- Additional context fields

## Step 2: Check for Existing Work

Generate deterministic key:
```javascript
const workKey = `campaign-${campaignId}-${payload.repository}-${payload.work_item_id}`;
```

Search for existing PR/issue with this key in title.

If found:
- Log that work already exists
- Optionally add a comment with status update
- Exit successfully

## Step 3: Perform Work

If no existing work found:
1. Create branch with deterministic name
2. Make required changes
3. Create PR with deterministic title
4. Apply labels: `z_campaign_${campaignId}`, [additional labels]

## Step 4: Report Status

Report completion:
- Link to created/updated PR or issue
- Whether work was skipped or completed
- Any errors or blockers encountered
```

## Idempotency Patterns

### Pattern 1: Branch-based Idempotency

```yaml
# In worker prompt
Branch naming pattern: `fix/campaign-${campaignId}-${repo}-${workItemId}`

Before creating:
1. Check if branch exists in target repo
2. If exists: Checkout and update
3. If not: Create new branch
```

### Pattern 2: PR Title-based Idempotency

```yaml
# In worker prompt
PR title pattern: `[${workKey}] ${description}`

Before creating PR:
1. Search for PRs with `${workKey}` in title
2. Filter by `z_campaign_${campaignId}` label
3. If found: Update with comment or skip
4. If not: Create new PR
```

### Pattern 3: Cursor-based Tracking

```yaml
# In worker prompt
Track processed items in repo-memory:
- Path: `memory/campaigns/${campaignId}/processed-${workerId}.json`
- Format: `{"processed": ["item-1", "item-2"]}`

Before processing:
1. Load processed items from repo-memory
2. Check if current work_item_id is in list
3. If in list: Skip
4. If not: Process, add to list, save
```

### Pattern 4: Issue Title-based Idempotency

```yaml
# In worker prompt
Issue title pattern: `[${workItemId}] ${description}`

Before creating issue:
1. Search for issues with `[${workItemId}]` in title
2. Filter by `z_campaign_${campaignId}` label
3. If found: Update existing issue
4. If not: Create new issue
```

## Example: Security Fix Worker

```yaml
---
name: Security Fix Worker
on:
  workflow_dispatch:
    inputs:
      campaign_id:
        description: 'Campaign identifier'
        required: true
        type: string
      payload:
        description: 'JSON with alert details'
        required: true
        type: string

tracker-id: security-fix-worker

tools:
  github:
    toolsets: [default, code_security]
  bash: ["*"]
  edit: true

safe-outputs:
  create-pull-request:
    max: 1
---

# Security Fix Worker

Process a code scanning alert and create a fix PR.

## Parse Input

```javascript
const campaignId = context.payload.inputs.campaign_id;
const payload = JSON.parse(context.payload.inputs.payload);
// payload: { repository, work_item_id: "alert-123", alert_type, file_path, ... }
```

## Idempotency Check

```javascript
const workKey = `campaign-${campaignId}-alert-${payload.work_item_id}`;
const branchName = `fix/${workKey}`;
const prTitle = `[${workKey}] Fix: ${payload.alert_type} in ${payload.file_path}`;

// Search for existing PR
const existingPRs = await github.search.issuesAndPullRequests({
  q: `repo:${payload.repository} is:pr is:open "${workKey}" in:title`
});

if (existingPRs.total_count > 0) {
  console.log(`PR already exists: ${existingPRs.items[0].html_url}`);
  // Optionally add comment with update
  await github.issues.createComment({
    owner: payload.repository.split('/')[0],
    repo: payload.repository.split('/')[1],
    issue_number: existingPRs.items[0].number,
    body: `Still being tracked by campaign ${campaignId}`
  });
  return;
}
```

## Create Fix

```bash
# Clone repo and create branch
git clone https://github.com/${payload.repository}.git
cd $(basename ${payload.repository})
git checkout -b ${branchName}

# Make security fix
# ... fix code ...

# Commit and push
git add .
git commit -m "Fix ${payload.alert_type} in ${payload.file_path}"
git push origin ${branchName}
```

## Create PR

```javascript
const pr = await github.pulls.create({
  owner: payload.repository.split('/')[0],
  repo: payload.repository.split('/')[1],
  title: prTitle,
  body: `Fixes security alert ${payload.work_item_id}\n\n**Campaign**: ${campaignId}\n**Alert Type**: ${payload.alert_type}`,
  head: branchName,
  base: payload.target_ref || 'main'
});

// Apply labels
await github.issues.addLabels({
  owner: payload.repository.split('/')[0],
  repo: payload.repository.split('/')[1],
  issue_number: pr.number,
  labels: [`z_campaign_${campaignId}`, 'security', 'automated']
});

console.log(`Created PR: ${pr.html_url}`);
```

## Report Status

Output:
- PR URL
- Alert ID processed
- Fix applied
- Labels added
```

## Best Practices

### 1. Single Responsibility

Each worker should have one clear purpose:
- ✅ "Create security fix PRs"
- ✅ "Update dependency versions"
- ❌ "Scan and fix and test and deploy"

### 2. Deterministic Behavior

Workers should produce the same output for the same input:
- Use deterministic keys based on input data
- Don't rely on timestamps or random values
- Make work idempotent via existence checks

### 3. Explicit Errors

Report errors clearly:
- Log what failed and why
- Include relevant context (repo, work item ID)
- Don't fail silently

### 4. Minimal Permissions

Request only needed permissions:
- Use specific GitHub toolsets
- Limit safe-output maxima (start with 1-3)
- Don't request wildcard permissions

### 5. Clear Completion Status

Always report what happened:
- "Created PR: [url]"
- "Skipped: PR already exists"
- "Failed: Missing required data"

## Testing Workers

Before using a worker in a campaign:

1. **Test manually** with workflow_dispatch:
   ```bash
   gh workflow run my-worker.yml \
     -f campaign_id=test-campaign \
     -f payload='{"repository":"owner/repo","work_item_id":"test-1"}'
   ```

2. **Verify idempotency** by running twice:
   - First run should create resources
   - Second run should skip/update without errors

3. **Check labels** on created items:
- Verify `z_campaign_test-campaign` label is applied
   - Confirm tracker-id is in description (if applicable)

4. **Test error cases**:
   - Invalid repository
   - Missing payload fields
   - Duplicate work items

## Migration from Fusion Approach

If you have workflows that used the old fusion approach:

### Before (Fusion):
```yaml
on:
  schedule: daily
  push:
  workflow_dispatch:  # Added by fusion
```

### After (Dispatch-Only):
```yaml
on:
  workflow_dispatch:
    inputs:
      campaign_id:
        description: 'Campaign identifier'
        required: true
        type: string
      payload:
        description: 'JSON payload'
        required: true
        type: string
```

### Migration Steps:

1. **Remove autonomous triggers**: Delete schedule/push/pull_request
2. **Add input contract**: Add campaign_id and payload inputs
3. **Update prompt**: Parse inputs at the start
4. **Add idempotency**: Implement deterministic key checking
5. **Apply campaign label**: Label all created items
6. **Test**: Verify with manual dispatch

## See Also

- [Campaign Files Architecture](../scratchpad/campaigns-files.md)
- [Campaign Examples](./src/content/docs/examples/campaigns.md)
- [Safe Outputs Documentation](./src/content/docs/reference/safe-outputs.md)
