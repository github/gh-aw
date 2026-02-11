---
name: Test Rate Limiting with Ignored Roles
engine: copilot
on:
  workflow_dispatch:
  issue_comment:
    types: [created]
rate-limit:
  max: 3
  window: 30
  ignored-roles:
    - admin
    - maintain
  events: [workflow_dispatch, issue_comment]
---

Test workflow to demonstrate rate limiting with ignored roles.

This workflow:
- Limits non-admin/non-maintainer users to 3 runs within a 30-minute window
- Exempts users with "admin" or "maintain" roles from rate limiting
- Applies to workflow_dispatch and issue_comment events

## Testing

### For Admin/Maintainer Users:
1. Trigger the workflow multiple times in quick succession (>3 times)
2. All runs should succeed without rate limiting

### For Other Users (write, triage, read):
1. Trigger the workflow 4 times in quick succession
2. The 4th run should be automatically cancelled with a rate limit warning
3. Wait 30 minutes for the window to reset
4. Trigger again to confirm the limit resets

## Expected Behavior

**Admin/Maintain users:**
```
🔍 Checking rate limit for user 'admin-user' on workflow 'test-rate-limit-ignored-roles.lock.yml'
   Configuration: max=3 runs per 30 minutes
   Current event: workflow_dispatch
   Ignored roles: admin, maintain
   User 'admin-user' has permission level: admin
✅ User 'admin-user' has ignored role 'admin'; skipping rate limit check
```

**Other users (after 3 runs):**
```
⚠️ Rate limit exceeded for user 'contributor' on workflow 'test-rate-limit-ignored-roles.lock.yml'
   User has triggered 3 runs in the last 30 minutes (max: 3)
   Cancelling current workflow run...
```

Hello! I'm testing the rate limiting feature with role-based exemptions.
