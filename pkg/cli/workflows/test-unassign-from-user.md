---
engine: copilot
name: test-unassign-from-user
on:
  workflow_dispatch:
safe-outputs:
  unassign-from-user:
    max: 5
    allowed: ["testuser1", "testuser2"]
    target: "*"
---

# Test unassign-from-user Safe Output

This is a test workflow to validate the `unassign-from-user` safe output handler.

## Test Cases

1. **Basic unassignment**: Remove a single assignee from an issue
2. **Multiple assignees**: Remove multiple assignees at once
3. **Allowed list**: Verify that only allowed usernames can be unassigned
4. **Max limit**: Verify that the max configuration is respected
5. **Target repository**: Verify cross-repository unassignment support

## Instructions

When this workflow runs, test the following scenarios:

- Use `unassign_from_user` tool to remove assignees from test issues
- Verify the configuration allows up to 5 unassignment operations
- Ensure only "testuser1" and "testuser2" can be unassigned
- Test that the tool works with both `assignee` (singular) and `assignees` (plural) fields

## Expected Behavior

The workflow should:
- Successfully unassign users from issues when they are in the allowed list
- Reject unassignment attempts for users not in the allowed list
- Respect the max count limit of 5 operations
- Support cross-repository unassignment when target-repo is configured
