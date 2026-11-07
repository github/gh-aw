---
private: true
emoji: "🧑‍🤝‍🧑"
name: Squad Plan
description: Uses Squad to plan an issue from the /squad-plan slash command and create Copilot-ready sub-issues
on:
  slash_command:
    strategy: centralized
    name: squad-plan
    events: [issue_comment]
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
network:
  allowed:
    - defaults
imports:
  - shared/squad.md
  - shared/reporting.md
tools:
  github:
    mode: gh-proxy
    toolsets: [default]
safe-outputs:
  create-issue:
    title-prefix: "[squad-plan] "
    labels: [cookie]
    max: 8
---

# Squad Plan

Use the Squad (https://github.com/github/squad) team to review the issue
where `/squad-plan` was invoked, produce an implementation plan, and create
small Copilot-ready sub-issues.

## Task

1. Review the triggering issue (#${{ github.event.issue.number }}) and the
   slash-command comment for any additional guidance.
2. Work with the Squad team to produce a concise implementation plan for the
   issue, including scope, sequencing, dependencies, and validation criteria.
3. Create at most 8 small, independently actionable sub-issues from the plan.
   Each created issue is automatically linked as a sub-issue of the triggering
   issue (#${{ github.event.issue.number }}), so do not create a separate
   parent tracking issue.
4. Each sub-issue must be suitable for assignment to GitHub Copilot coding
   agent one by one, carry the configured `cookie` label, and include:
   - a clear objective
   - relevant issue context
   - concrete implementation guidance
   - acceptance criteria
   - any known ordering or dependency notes

## Safe Outputs

- Use `create_issue` for each planned sub-issue.
- Do not create a separate parent issue and do not use `parent` or
  `temporary_id`; sub-issues are linked to the triggering issue automatically.
- If the issue is already fully planned or no useful sub-issues are needed,
  call `noop` with a short explanation.
- If Squad cannot produce a usable plan, call `noop` instead of filing
  incomplete issues.
