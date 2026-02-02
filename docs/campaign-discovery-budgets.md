# Campaign Discovery Budget Limits Explained

## Overview

Campaign discovery uses **budget limits** to prevent unbounded API usage when searching for worker-created items (issues, PRs, discussions). When these budgets are exhausted before finding any items, the campaign run becomes a **no-op** with zero discovered items.

## The Workflow Run

**Example**: [Security Alert Burndown Run #21255054636](https://github.com/github/gh-aw/actions/runs/21255054636)

This workflow run was a no-op because:
1. **Discovery found 0 items** matching the campaign criteria
2. **Discovery budget limits were reached** before completing the search
3. The campaign orchestrator determined there was **no work to do**

## Discovery Budget Parameters

Campaigns enforce two types of budget limits to pace work and protect GitHub's API:

### 1. Max Discovery Items Per Run

**Default**: 100 items  
**Security Alert Burndown**: 50 items  

This limits the **total number of items** (issues + PRs + discussions) the discovery system will scan in a single run. Once this limit is reached, discovery stops even if more items exist.

**Config**: `governance.max-discovery-items-per-run`

```yaml
governance:
  max-discovery-items-per-run: 50  # Conservative limit
```

### 2. Max Discovery Pages Per Run

**Default**: 10 pages  
**Security Alert Burndown**: 3 pages  

This limits the **number of API pagination pages** fetched during discovery. GitHub's search API returns 100 items per page, so 3 pages = maximum 300 items examined (though only 50 would be retained due to the items budget).

**Config**: `governance.max-discovery-pages-per-run`

```yaml
governance:
  max-discovery-pages-per-run: 3  # Very conservative
```

## Why Security Alert Burndown Had Conservative Budgets

The Security Alert Burndown campaign uses **extremely conservative** discovery budgets:

```yaml
governance:
  max-new-items-per-run: 3
  max-discovery-items-per-run: 50    # Only 50 items scanned
  max-discovery-pages-per-run: 3      # Only 3 API pages
  max-project-updates-per-run: 10
  max-comments-per-run: 3
```

These settings were chosen because:
1. **High-risk campaign** (automated security fixes)
2. **Paced rollout** strategy (handle few items at a time)
3. **API rate limit protection** (minimize GitHub API usage)
4. **Incremental progress** (process work gradually over many runs)

## How Discovery Works

### Discovery Strategy (Multi-layered)

The campaign discovery system (`actions/setup/js/campaign_discovery.cjs`) uses a **three-tier search strategy**:

1. **Primary**: Search by campaign-specific label `z_campaign_<campaign-id>`
2. **Secondary**: Search by generic `agentic-campaign` label (filtered by campaign ID)
3. **Fallback**: Search by tracker-id in issue/PR bodies

### Budget Enforcement

```javascript
// From campaign_discovery.cjs line 383-385
if (totalItemsScanned >= maxDiscoveryItems || totalPagesScanned >= maxDiscoveryPages) {
  core.warning(`Reached discovery budget limits. Stopping discovery.`);
  break;
}
```

When **either budget** is exhausted:
- Discovery stops immediately
- A warning is logged
- The manifest includes partial results
- The cursor is saved for continuation in the next run

## Understanding the No-Op Result

### What Happened in Run #21255054636

1. **Discovery Phase** (Step: "Run campaign discovery precomputation")
   - Searched for items with label `z_campaign_security-alert-burndown`
   - Scanned up to 50 items across up to 3 API pages
   - **Found 0 items** matching the criteria

2. **Agent Phase** (Job: "agent")
   - Received discovery manifest with 0 items
   - Had no work to process
   - Completed successfully without taking action

3. **Conclusion Phase** (Job: "conclusion")
   - Detected no items to process
   - Logged: "No noop items found in agent output"
   - Workflow completed as **successful no-op**

### Why Were No Items Found?

Possible reasons:
1. **No worker workflows have run yet** - Campaign may be newly created
2. **Worker outputs lack required labels** - Items not tagged with `z_campaign_security-alert-burndown`
3. **Items outside campaign scope** - Workers created items in repos not in `scope`
4. **Search query too restrictive** - Label search didn't match existing items
5. **Budget exhausted before reaching relevant items** - Items exist but are on page 4+ (beyond 3-page budget)

## Diagnosing Discovery Issues

### Check Discovery Configuration

```yaml
# From .github/workflows/security-alert-burndown.md
scope:
  - github/gh-aw  # Only searching this repo

workflows:
  - code-scanning-fixer
  - security-fix-pr
```

### Verify Worker Labeling

Worker workflows MUST create issues/PRs with the campaign-specific label:
```
z_campaign_security-alert-burndown
```

Or use tracker-id in the body:
```
gh-aw-tracker-id: code-scanning-fixer
```

### Check Cursor State

The cursor tracks pagination state across runs:
```bash
# Location: repo-memory branch
memory/campaigns/security-alert-burndown/cursor.json
```

## Adjusting Discovery Budgets

### When to Increase Budgets

Increase budgets if:
- Discovery finds 0 items but you know items exist
- Discovery log shows "Reached discovery budget limits"
- Cursor shows pagination stopped early
- Campaign needs faster ramp-up

### Recommended Adjustments

**Conservative** (current):
```yaml
max-discovery-items-per-run: 50
max-discovery-pages-per-run: 3
```

**Moderate**:
```yaml
max-discovery-items-per-run: 100  # Default
max-discovery-pages-per-run: 5
```

**Aggressive**:
```yaml
max-discovery-items-per-run: 200
max-discovery-pages-per-run: 10  # Default
```

### When to Keep Conservative Budgets

Keep conservative budgets for:
- **High-risk campaigns** (security, compliance)
- **Cross-organization campaigns** (many repos)
- **Experimental campaigns** (testing workflows)
- **API rate limit concerns**

## Future Improvements

### Better Logging

The discovery system should log more details when budgets are reached:

```javascript
core.warning(`Reached discovery budget limits. Stopping discovery.`);
core.info(`Total items scanned: ${totalItemsScanned}/${maxDiscoveryItems}`);
core.info(`Total pages scanned: ${totalPagesScanned}/${maxDiscoveryPages}`);
core.info(`Items found: ${allItems.length}`);
```

### Budget Exhaustion Reasons

The conclusion job should distinguish between:
1. **True no-op**: No items exist (successful)
2. **Budget-limited**: Discovery stopped early (may need adjustment)
3. **Configuration error**: Wrong labels or scope

### Discovery Manifest Enhancement

Include budget utilization in manifest:

```json
{
  "discovery": {
    "total_items": 0,
    "items_scanned": 50,
    "pages_scanned": 3,
    "max_items_budget": 50,
    "max_pages_budget": 3,
    "budget_exhausted": true,  // NEW
    "exhausted_reason": "max_pages_reached"  // NEW
  }
}
```

## Summary

The Security Alert Burndown campaign run was a **no-op by design**:
- Discovery exhausted its **conservative 3-page budget** (50 items max)
- Found **0 matching items** within that budget
- Completed successfully without work to do

This is **expected behavior** for campaigns with strict governance policies. The conservative budgets ensure:
- Paced rollout
- API rate limit protection  
- Incremental progress tracking
- Risk mitigation through small batches

To change this behavior, increase `max-discovery-pages-per-run` and/or `max-discovery-items-per-run` in the campaign governance section.
