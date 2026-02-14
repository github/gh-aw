# Safe Output Staged Mode Implementation - Summary

## Overview
This PR systematically implements staged mode across all safe output JavaScript handlers. 
Staged mode previews mutations without applying them by checking `GH_AW_SAFE_OUTPUTS_STAGED === "true"`.

## Implementation Status: 19/35 Handlers Complete (54%)

### ✅ Completed (19 handlers)

**Newly Implemented (6 handlers):**
1. create_issue.cjs - Preview issue creation
2. add_comment.cjs - Preview comment creation  
3. add_labels.cjs - Preview label additions
4. remove_labels.cjs - Preview label removals
5. create_discussion.cjs - Preview discussion creation
6. update_handler_factory.cjs - Unified factory for update handlers

**Via Factory (3 handlers):**
7. update_issue - Inherits from update_handler_factory
8. update_pull_request - Inherits from update_handler_factory
9. update_discussion - Inherits from update_handler_factory

**Pre-existing (10 handlers):**
10. create_pull_request
11. assign_to_agent
12. autofix_code_scanning_alert
13. create_agent_session
14. close_entity_helpers (helper)
15. noop
16. push_to_pull_request_branch
17. update_release
18. update_runner
19. upload_assets

### 🚧 Remaining (16 handlers)

**Simple Pattern (12 handlers):**
- close_issue
- close_discussion  
- close_pull_request
- mark_pull_request_as_ready_for_review
- add_reviewer
- hide_comment
- assign_milestone
- assign_to_user
- unassign_from_user
- link_sub_issue
- resolve_pr_review_thread

**Project Handlers (3 handlers):**
- create_project
- update_project
- create_project_status_update

**Buffered (2 handlers):**
- create_pr_review_comment
- submit_pr_review

## Implementation Pattern

All handlers follow this consistent pattern:

```javascript
async function main(config = {}) {
  // 1. Extract configuration
  const maxCount = config.max || 10;
  
  // 2. Check staged mode
  const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";
  
  return async function handle(message, resolvedIds) {
    // 3. Before API mutation, check staged mode
    if (isStaged) {
      core.info(`Staged mode: Would perform action...`);
      return {
        success: true,
        staged: true,
        previewInfo: { /* details */ },
      };
    }
    
    // 4. Actual API call
    await github.rest.*(...);
  };
}
```

## Key Files Modified

1. **update_handler_factory.cjs** - Factory function used by update_issue, update_pull_request, update_discussion
2. **create_issue.cjs** - Issue creation handler
3. **add_comment.cjs** - Comment creation handler
4. **add_labels.cjs** - Label addition handler
5. **remove_labels.cjs** - Label removal handler
6. **create_discussion.cjs** - Discussion creation handler

## Benefits

1. **Consistent Preview**: All handlers use same staged mode check
2. **No Mutations**: When staged=true, no API calls are made
3. **Preview Info**: Returns details about what would be done
4. **Easy Testing**: Set GH_AW_SAFE_OUTPUTS_STAGED=true to test
5. **Factory Pattern**: Update handlers get staged mode automatically

## Next Steps for Completion

The remaining 16 handlers follow the same simple 2-step pattern:

**Step 1:** Add `const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";`

**Step 2:** Add check before API call:
```javascript
if (isStaged) {
  return { success: true, staged: true, previewInfo: {...} };
}
```

Estimated time to complete: 30-60 minutes for all remaining handlers.
