---
name: Smoke Temporary ID
description: Test temporary ID functionality for issue chaining and cross-references
on: 
  workflow_dispatch:
  #schedule: every 24h
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "eyes"
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: true
network:
  allowed:
    - defaults
    - node
safe-outputs:
  create-issue:
    expires: 2h
    title-prefix: "[smoke-temporary-id] "
    max: 5
    group: true
  link-sub-issue:
    max: 3
  add-comment:
    max: 2
    hide-older-comments: true
  messages:
    append-only-comments: true
    footer: "> 🧪 *Temporary ID smoke test by [{workflow_name}]({run_url})*"
    run-started: "🧪 [{workflow_name}]({run_url}) is now testing temporary ID functionality..."
    run-success: "✅ [{workflow_name}]({run_url}) completed successfully. Temporary ID validation passed."
    run-failure: "❌ [{workflow_name}]({run_url}) encountered failures. Check the logs for details."
timeout-minutes: 10
---

# Smoke Test: Temporary ID Functionality

This workflow validates that temporary IDs work correctly for:
1. Creating parent-child issue hierarchies
2. Cross-referencing issues in bodies
3. Different temporary ID formats (3-8 alphanumeric characters)

**IMPORTANT**: Use the exact temporary ID format `aw_` followed by 3-8 alphanumeric characters (A-Za-z0-9).

## Test 1: Create Parent Issue with Temporary ID

Create a parent tracking issue with a temporary ID. Use a 6-character alphanumeric ID.

```json
{
  "type": "create_issue",
  "temporary_id": "aw_test01",
  "title": "Test Parent: Temporary ID Validation",
  "body": "This is a parent issue created to test temporary ID functionality.\n\nSub-issues:\n- #aw_test02\n- #aw_test03\n\nAll references should be replaced with actual issue numbers."
}
```

## Test 2: Create Sub-Issues with Cross-References

Create two sub-issues that reference each other and the parent using temporary IDs.

### Sub-Issue 1

```json
{
  "type": "create_issue",
  "temporary_id": "aw_test02",
  "parent": "aw_test01",
  "title": "Sub-Issue 1: Test Temporary ID References",
  "body": "This is sub-issue 1.\n\nParent: #aw_test01\nRelated: #aw_test03\n\nAll temporary IDs should be resolved to actual issue numbers."
}
```

### Sub-Issue 2

```json
{
  "type": "create_issue",
  "temporary_id": "aw_test03",
  "parent": "aw_test01",
  "title": "Sub-Issue 2: Test Different ID Length",
  "body": "This is sub-issue 2 with an 8-character temporary ID.\n\nParent: #aw_test01\nRelated: #aw_test02\n\nTesting that longer temporary IDs (8 chars) work correctly."
}
```

## Test 3: Verify Link Structure

After the issues are created, verify they are properly linked by adding a comment to the parent issue summarizing the test results.

```json
{
  "type": "add_comment",
  "issue_number": "aw_test01",
  "body": "## Test Results\n\n✅ Parent issue created with temporary ID `aw_test01`\n✅ Sub-issue 1 created with temporary ID `aw_test02` and linked to parent\n✅ Sub-issue 2 created with temporary ID `aw_test03` and linked to parent\n✅ Cross-references resolved correctly\n\n**Validation**: Check that:\n1. All temporary ID references (#aw_*) in issue bodies are replaced with actual issue numbers (#123)\n2. Sub-issues show parent relationship in GitHub UI\n3. Parent issue shows sub-issues in task list\n\nTemporary ID format validated: `aw_[A-Za-z0-9]{3,8}`"
}
```

## Expected Outcome

1. Parent issue #aw_test01 created and assigned actual issue number (e.g., #1234)
2. Sub-issues #aw_test02 and #aw_test03 created with actual issue numbers
3. All references like `#aw_test01` replaced with actual numbers like `#1234`
4. Sub-issues properly linked to parent with `parent` field
5. Comment added to parent verifying the test results

**Success Criteria**: All 3 issues created, all temporary ID references resolved, parent-child relationships established.
