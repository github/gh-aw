---
title: Ephemerals
description: Features for automatically expiring workflow resources and reducing noise in your repositories
sidebar:
  order: 9
---

GitHub Agentic Workflows includes several features designed to automatically expire resources and reduce noise in your repositories. These "ephemeral" features help keep your repository clean by automatically cleaning up temporary issues, discussions, pull requests, and workflow runs after they've served their purpose.

## Why Use Ephemerals?

**Cost Control**: Scheduled workflows can accumulate costs over time. Setting expiration dates ensures they stop automatically after a deadline.

**Reduce Clutter**: AI-generated issues and discussions can accumulate quickly. Auto-expiration removes obsolete content, keeping your repository focused on active work.

**Status Updates**: When workflows post status updates repeatedly, hiding older comments prevents timeline clutter while preserving the latest information.

**Clean Automation**: Prevent automated content from overwhelming your main repository by using separate repositories and controlling cross-references.

## Expiration Features

### Workflow Stop-After

Automatically disable workflow triggering after a deadline to control costs and prevent indefinite execution.

```yaml wrap
on: weekly on monday
  stop-after: "+25h"  # 25 hours from compilation time
```

**Accepted formats**:
- **Absolute dates**: `YYYY-MM-DD`, `MM/DD/YYYY`, `DD/MM/YYYY`, `January 2 2006`, `1st June 2025`, ISO 8601
- **Relative deltas**: `+7d`, `+25h`, `+1d12h30m` (calculated from compilation time)

The minimum granularity is hours - minute-only units (e.g., `+30m`) are not allowed. Recompiling the workflow resets the stop time.

**Key behaviors**:
- The workflow is disabled at the specified deadline, preventing new runs
- Existing runs continue to completion
- The stop time is preserved during recompilation unless `--refresh-stop-time` flag is used
- Use `gh aw compile --refresh-stop-time` to regenerate the stop time based on current time

**Use cases**:
- Trial workflows that should run for a limited period
- Experimental features with automatic sunset dates
- Orchestrated initiatives with defined end dates
- Cost-controlled scheduled workflows

See [Triggers Reference](/gh-aw/reference/triggers/#stop-after-configuration-stop-after) for complete documentation.

### Safe Output Expiration

Auto-close issues, discussions, and pull requests after a specified time period. This generates a maintenance workflow that runs automatically at appropriate intervals.

#### Issue Expiration

```yaml wrap
safe-outputs:
  create-issue:
    expires: 7  # Auto-close after 7 days
    labels: [automation, agentic]
```

#### Discussion Expiration

```yaml wrap
safe-outputs:
  create-discussion:
    expires: 3  # Auto-close after 3 days as "OUTDATED"
    category: "general"
```

#### Pull Request Expiration

```yaml wrap
safe-outputs:
  create-pull-request:
    expires: 14  # Auto-close after 14 days (same-repo only)
    draft: true
```

**Supported formats**:
- **Integer**: Number of days (e.g., `7` = 7 days)
- **Relative time**: `2h`, `7d`, `2w`, `1m`, `1y`

Hours less than 24 are treated as 1 day minimum for expiration calculation.

**Maintenance workflow frequency**: The generated `agentics-maintenance.yml` workflow runs at the minimum required frequency based on the shortest expiration time across all workflows:

| Shortest Expiration | Maintenance Frequency |
|---------------------|----------------------|
| 1 day or less | Every 2 hours |
| 2 days | Every 6 hours |
| 3-4 days | Every 12 hours |
| 5+ days | Daily |

**Expiration markers**: The system adds a visible checkbox line with an XML comment to the body of created items:
```markdown
- [x] expires <!-- gh-aw-expires: 2026-01-14T15:30:00.000Z --> on Jan 14, 2026, 3:30 PM UTC
```

The maintenance workflow searches for items with this expiration format (checked checkbox with the XML comment) and automatically closes them with appropriate comments and resolution reasons. Users can uncheck the checkbox to prevent automatic expiration.

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) for complete documentation.

### Close Older Discussions

Automatically close older discussions with the same workflow-id marker when creating new ones. This keeps your discussions focused on the latest information.

```yaml wrap
safe-outputs:
  create-discussion:
    close-older-discussions: true  # Close previous reports
```

**How it works**:
- When a new discussion is created successfully, the system searches for older discussions
- Matches are identified by the workflow-id marker embedded in the discussion body
- Up to 10 older discussions are closed as "OUTDATED"
- Each closed discussion receives a comment linking to the new discussion
- Only runs if the new discussion creation succeeds

**Requirements**:
- GH_AW_WORKFLOW_ID environment variable must be set
- Requires appropriate permissions on the target repository

**Use cases**:
- Weekly status reports where only the latest matters
- Recurring analysis workflows that supersede previous results
- Scheduled summaries that replace older versions

## Noise Reduction Features

### Hide Older Comments

Minimize previous comments from the same workflow before posting new ones. Useful for status update workflows where only the latest information matters.

```yaml wrap
safe-outputs:
  add-comment:
    hide-older-comments: true
    allowed-reasons: [outdated]  # Optional: restrict hiding reasons
```

**How it works**:
- Before posting a new comment, searches for previous comments from the same workflow
- Identifies comments by `GITHUB_WORKFLOW` name
- Hides (minimizes) matching comments in the GitHub UI
- Posts the new comment
- Only hides comments; does not delete them

**Allowed reasons**:
- `spam` - Mark as spam
- `abuse` - Mark as abusive
- `off_topic` - Mark as off-topic
- `outdated` - Mark as outdated (default)
- `resolved` - Mark as resolved

Configure `allowed-reasons` to restrict which reasons can be used. If omitted, only `outdated` is allowed by default.

**Use cases**:
- Workflows posting status updates repeatedly
- Build status notifications where only latest result matters
- Health check workflows reporting periodic results
- Progress tracking workflows with frequent updates

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#hide-older-comments) for complete documentation.

### SideRepoOps Pattern

Run agentic workflows from a separate "side" repository that targets your main codebase. This isolates AI-generated issues, comments, and workflow runs from your main repository, keeping automation infrastructure separate from production code.

See [SideRepoOps](/gh-aw/patterns/siderepoops/) for complete setup and usage documentation.

### Allowed GitHub References

Control which GitHub repository references (`#123`, `owner/repo#456`) are allowed in workflow output. When configured, references to unlisted repositories are escaped with backticks to prevent GitHub from creating timeline items.

```yaml wrap
safe-outputs:
  allowed-github-references: []  # Escape all references
  create-issue:
    target-repo: "my-org/main-repo"
```

**Configuration options**:

- `[]` - Escape all references (prevents all timeline items)
- `["repo"]` - Allow only the target repository's references
- `["repo", "owner/other-repo"]` - Allow specific repositories
- Not specified (default) - All references allowed

**How it works**: References like `#123` become `` `#123` `` and `other/repo#456` becomes `` `other/repo#456` ``, preventing timeline clutter while preserving the information.

**Use cases**:

- **SideRepoOps**: Prevent automation from cluttering main repository timeline
- **Cross-repository operations**: Avoid unwanted cross-references
- **Clean automation**: Maintain readable issue/PR timelines
- **Reporting workflows**: Share information without creating link noise

**Example with SideRepoOps**:

```yaml wrap
safe-outputs:
  github-token: ${{ secrets.MAIN_REPO_PAT }}
  allowed-github-references: []  # Escape all references
  create-issue:
    target-repo: "my-org/main-repo"
    title-prefix: "[automation] "
```

This prevents the side repository's automation from creating "mentioned in..." entries in your main repository's issues and pull requests.

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#security-and-sanitization) for complete documentation.

### Use Discussions Instead of Issues

For ephemeral content, use GitHub discussions instead of issues. Discussions are better suited for temporary content, questions, and updates that don't require long-term tracking.

```yaml wrap
safe-outputs:
  create-discussion:
    category: "general"
    expires: 7  # Auto-close after 7 days
    close-older-discussions: true
```

**Why discussions for ephemeral content?**

| Feature | Issues | Discussions |
|---------|--------|-------------|
| **Purpose** | Long-term tracking | Conversations & updates |
| **Searchability** | High priority in search | Lower search weight |
| **Project boards** | Native integration | Limited integration |
| **Auto-close** | Supported with maintenance workflow | Supported with maintenance workflow |
| **Timeline noise** | Can clutter project tracking | Separate from development work |

**Use cases for ephemeral discussions**:

- Weekly status reports
- Periodic analysis results
- Temporary announcements
- Q&A that expires
- Time-bound experiments
- Community updates

**Combining features**:

```yaml wrap
safe-outputs:
  create-discussion:
    category: "Status Updates"
    expires: 14  # Close after 2 weeks
    close-older-discussions: true  # Replace previous reports
```

This configuration ensures:

1. Only the latest weekly status discussion is open
2. Previous reports are closed when new ones are created
3. All discussions auto-close after 14 days
4. The "Status Updates" category stays clean and focused

## Related Documentation

- [Triggers Reference](/gh-aw/reference/triggers/) - Complete trigger configuration including `stop-after`
- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) - All safe output types and expiration options
- [SideRepoOps](/gh-aw/patterns/siderepoops/) - Complete setup for side repository operations
- [Security Best Practices](/gh-aw/reference/tokens/) - Authentication and security considerations
- [Orchestration](/gh-aw/patterns/orchestration/) - Orchestrating multi-workflow initiatives
