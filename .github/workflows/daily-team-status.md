---
timeout-minutes: 10
strict: true
on:
  schedule:
  - cron: 0 9 * * 1-5
  stop-after: +1mo
  workflow_dispatch: null
permissions:
  contents: read
  issues: read
  pull-requests: read
tracker-id: daily-team-status
network: defaults
imports:
  - shared/mood.md
  - githubnext/agentics/workflows/shared/reporting.md@d3422bf940923ef1d43db5559652b8e1e71869f3
safe-outputs:
  create-issue:
    expires: 1d
    title-prefix: "[team-status] "
description: |
  This workflow created daily team status reporter creating upbeat activity summaries.
  Gathers recent repository activity (issues, PRs, releases, code changes)
  and generates engaging GitHub issues with productivity insights, community
  highlights, and project recommendations. Uses a positive, encouraging tone with
  moderate emoji usage to boost team morale.
source: githubnext/agentics/workflows/daily-team-status.md@d3422bf940923ef1d43db5559652b8e1e71869f3
tools:
  github: null
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Team Status

Create an upbeat daily status report for the team as a GitHub issue.

## What to include

- Recent repository activity (issues, PRs, releases, code changes)
- Team productivity suggestions and improvement ideas
- Community engagement highlights
- Project investment and feature recommendations

## Style

- Be positive, encouraging, and helpful 🌟
- Use emojis moderately for engagement
- Keep it concise - adjust length based on actual activity

## Report Formatting Guidelines

**CRITICAL**: Follow these formatting guidelines to create well-structured, readable reports:

### 1. Header Levels
**Use h3 (###) or lower for all headers in your report to maintain proper document hierarchy.**

The issue title serves as h1, so all content headers should start at h3:
- Use `###` for main sections (e.g., "### Team Status Overview", "### Key Achievements")
- Use `####` for subsections (e.g., "#### Community Highlights", "#### Productivity Insights")
- Never use `##` (h2) or `#` (h1) in the report body

### 2. Progressive Disclosure
**Wrap long sections in `<details><summary><b>Section Name</b></summary>` tags to improve readability and reduce scrolling.**

Use collapsible sections for:
- Individual contributor details
- Detailed team member activity logs
- Verbose statistics and metrics
- Extended project recommendations

Example:
```markdown
<details>
<summary><b>Detailed Team Activity</b></summary>

[Long team member details...]

</details>
```

### 3. Report Structure Pattern

Your report should follow this structure for optimal readability:

1. **Team Status Overview** (always visible): Brief summary of overall team health and activity
2. **Key Achievements and Blockers** (always visible): Most important highlights and concerns
3. **Individual Contributor Details** (in `<details>` tags): Per-person activity breakdowns
4. **Action Items and Priorities** (always visible): Actionable suggestions and next steps

### Design Principles

Create reports that:
- **Build trust through clarity**: Most important info (status, achievements, blockers) immediately visible
- **Exceed expectations**: Add helpful context, trends, and comparisons to previous periods
- **Create delight**: Use progressive disclosure to reduce overwhelm while keeping details accessible
- **Maintain consistency**: Follow the same patterns as other reporting workflows

## Process

1. Gather recent activity from the repository
2. Create a new GitHub issue with your findings and insights
