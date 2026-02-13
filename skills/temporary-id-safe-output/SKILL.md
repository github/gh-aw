---
name: temporary-id-safe-output
description: Plan for adding temporary ID support to safe output jobs
---


# Adding Temporary ID Support to Safe Output Jobs

This document outlines the implementation plan for adding temporary ID support to safe output jobs. Temporary IDs allow agents to reference newly created issues within the same workflow run before they have actual GitHub issue numbers.

## Problem Statement

When an agent needs to create a parent issue and immediately link sub-issues to it in the same workflow run, the agent doesn't know the actual issue number until the `create_issue` job completes. Temporary IDs bridge this gap by allowing the agent to use placeholder IDs that are resolved to actual issue numbers at execution time.

## Temporary ID Format

Temporary IDs follow the pattern `aw_[A-Za-z0-9]{3,8}` where:
- `aw_` is a fixed prefix identifying agentic workflow temporary IDs
- `XXXXXXXX` is a 3-8 character alphanumeric string (A-Za-z0-9)

Example: `aw_abc`, `aw_abc123`, `aw_Test123`

## Implementation Components

### 1. Shared Module: `temporary_id.cjs`

Location: `pkg/workflow/js/temporary_id.cjs`

This module provides shared utilities for temporary ID handling:

```javascript
// Core functions
generateTemporaryId()           // Generate new temporary ID
isTemporaryId(value)            // Check if value is a temporary ID
normalizeTemporaryId(tempId)    // Normalize to lowercase for map lookups
loadTemporaryIdMap()            // Load map from GH_AW_TEMPORARY_ID_MAP env var
resolveIssueNumber(value, map)  // Resolve value to issue number (supports temp IDs)
replaceTemporaryIdReferences(text, map)  // Replace #aw_XXX references in text
```

### 2. Producer Job: `create_issue`

The `create_issue` job outputs a temporary ID map that other jobs can consume:

**Go changes** (`pkg/workflow/create_issue.go`):
- No changes needed - already outputs `temporary_id_map` 

**JavaScript changes** (`pkg/workflow/js/create_issue.cjs`):
- Generate temporary ID for each created issue
- Build map of `temporary_id -> issue_number`
- Output map via `core.setOutput("temporary_id_map", JSON.stringify(map))`

### 3. Consumer Job: Adding Temporary ID Support

For each safe output job that needs to resolve temporary IDs:

#### Step 1: Update Go Job Builder

In `pkg/workflow/<job_name>.go`:

1. Add `createIssueJobName` parameter to the build function:
```go
func (c *Compiler) build<JobName>Job(data *WorkflowData, mainJobName string, createIssueJobName string) (*Job, error) {
```

2. Add environment variable to pass the temporary ID map:
```go
if createIssueJobName != "" {
    customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_TEMPORARY_ID_MAP: ${{ needs.%s.outputs.temporary_id_map }}\n", createIssueJobName))
}
```

3. Add `create_issue` to the job's `needs` array:
```go
needs := []string{mainJobName}
if createIssueJobName != "" {
    needs = append(needs, createIssueJobName)
}
```

4. Update the `SafeOutputJobConfig` to use the dynamic needs:
```go
return c.buildSafeOutputJob(data, SafeOutputJobConfig{
    // ...
    Needs: needs,
    // ...
})
```

#### Step 2: Update Compiler Jobs

In `pkg/workflow/compiler_jobs.go`:

Pass the `createIssueJobName` when building the job:
```go
job, err := c.build<JobName>Job(data, mainJobName, createIssueJobName)
```

#### Step 3: Update JavaScript Script

In `pkg/workflow/js/<job_name>.cjs`:

1. Import the temporary ID utilities:
```javascript
const { loadTemporaryIdMap, resolveIssueNumber } = require("./temporary_id.cjs");
```

2. Load the temporary ID map at the start of main():
```javascript
const temporaryIdMap = loadTemporaryIdMap();
if (temporaryIdMap.size > 0) {
    core.info(`Loaded temporary ID map with ${temporaryIdMap.size} entries`);
}
```

3. Use `resolveIssueNumber()` to resolve issue numbers:
```javascript
const resolved = resolveIssueNumber(item.issue_number, temporaryIdMap);
if (resolved.errorMessage) {
    core.warning(`Failed to resolve issue: ${resolved.errorMessage}`);
    continue;
}
const issueNumber = resolved.resolved;
if (resolved.wasTemporaryId) {
    core.info(`Resolved temporary ID '${item.issue_number}' to issue #${issueNumber}`);
}
```

#### Step 4: Update Agent Ingestion Validation

In `pkg/workflow/js/collect_ndjson_output.cjs`:

Add validation for fields that accept temporary IDs:
```javascript
function isValidIssueNumberOrTemporaryId(value) {
    if (typeof value === "number" && Number.isInteger(value) && value > 0) {
        return true;
    }
    if (typeof value === "string" && /^aw_[0-9a-f]{12}$/i.test(value)) {
        return true;
    }
    return false;
}
```

Use this validation for fields like `parent_issue_number`, `sub_issue_number`, etc.

### 4. Failure Handling

When temporary ID resolution fails, the job should:
- Log a warning with `core.warning()` instead of failing with `core.setFailed()`
- Continue processing other items
- Include failures in the step summary
- Complete successfully with warnings

This ensures that:
- Partial success is possible (some links may work while others fail)
- The workflow doesn't fail catastrophically due to a single resolution failure
- Users can review warnings in the step summary

## Example Usage

### Workflow Configuration

```yaml
safe-outputs:
  create-issue:
    title-prefix: "[Parent] "
    labels: [tracking]
    max: 3
  link-sub-issue:
    max: 10
```

### Agent Output

```json
{"type": "create_issue", "temporary_id": "aw_abc123", "title": "Parent: Feature X", "body": "..."}
{"type": "link_sub_issue", "parent_issue_number": "aw_abc123", "sub_issue_number": 42}
{"type": "link_sub_issue", "parent_issue_number": "aw_abc123", "sub_issue_number": 43}
```

### Execution Flow

1. `main` job: Agent generates output with temporary ID `aw_abc123`
2. `create_issue` job: Creates issue #100, outputs `{"aw_abc123": 100}`
3. `link_sub_issue` job: 
   - Loads temporary ID map
   - Resolves `aw_abc123` → `100`
   - Links issues #42 and #43 as sub-issues of #100

## Jobs That Support Temporary IDs

| Job | Field(s) | Status |
|-----|----------|--------|
| `link_sub_issue` | `parent_issue_number`, `sub_issue_number` | ✅ Implemented |
| `add_comment` | `issue_number` (via text replacement) | ✅ Implemented |
| `update_issue` | `issue_number` | 🔄 Can be added |
| `close_pull_request` | - | N/A (uses PR numbers) |

## Testing

### Unit Tests

Add tests in `pkg/workflow/js/temporary_id.test.cjs` for:
- `isTemporaryId()` with valid and invalid inputs
- `resolveIssueNumber()` with temporary IDs and regular numbers
- `loadTemporaryIdMap()` with various JSON inputs

### Integration Tests

Add tests in `pkg/workflow/<job_name>_dependencies_test.go` to verify:
- Job includes `create_issue` in needs when configured
- `GH_AW_TEMPORARY_ID_MAP` env var is set correctly
- Job works without `create_issue` dependency

## Security Considerations

1. Temporary IDs are only valid within a single workflow run
2. The map is passed via environment variables (not exposed externally)
3. Agents cannot forge temporary IDs to reference issues from other workflows
4. Resolution failures are logged but don't expose the temporary ID map contents

## Checklist for Adding Support to a New Job

- [ ] Update Go job builder to accept `createIssueJobName` parameter
- [ ] Add `GH_AW_TEMPORARY_ID_MAP` environment variable
- [ ] Update needs array to include `create_issue` conditionally
- [ ] Update compiler_jobs.go to pass `createIssueJobName`
- [ ] Import temporary ID utilities in JavaScript script
- [ ] Use `resolveIssueNumber()` for issue number fields
- [ ] Update validation in `collect_ndjson_output.cjs` if needed
- [ ] Add unit tests for the resolution logic
- [ ] Add integration tests for job dependencies
- [ ] Update documentation
