---
description: Monthly workflow that identifies stale repositories in an organization and creates detailed activity reports
name: Stale Repository Identifier
on:
  workflow_dispatch:
    inputs:
      organization:
        description: "GitHub organization to scan for stale repositories"
        required: true
        type: string
        default: github
  schedule: "0 9 1 * *"  # Converted from 'monthly on 1 at 02:03' (adjust time as needed)

permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read

engine: copilot
strict: true
timeout-minutes: 45

imports:
  - shared/mood.md
  - shared/python-dataviz.md
  - shared/jqschema.md
  - shared/trending-charts-simple.md

network:
  allowed:
    - defaults
    - github

safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[Stale Repository] "
    labels: [stale-repository, automated-analysis, cookie]
    max: 10
    group: true
  upload-asset:
  messages:
    footer: "> 🔍 *Analysis by [{workflow_name}]({run_url})*"
    run-started: "🔍 Stale Repository Identifier starting! [{workflow_name}]({run_url}) is analyzing repository activity..."
    run-success: "✅ Analysis complete! [{workflow_name}]({run_url}) has finished analyzing stale repositories."
    run-failure: "⚠️ Analysis interrupted! [{workflow_name}]({run_url}) {status}."

tools:
  github:
    read-only: true
    lockdown: true
    toolsets:
      - repos
      - issues
      - pull_requests
  cache-memory:
    key: stale-repos-analysis-${{ github.workflow }}-${{ github.run_id }}
  bash:
    - "*"
  edit:

env:
  # For scheduled runs, set a default organization or use repository variables
  ORGANIZATION: ${{ github.event.inputs.organization || 'github' }}

steps:
  - name: Run stale_repos tool
    id: stale-repos
    uses: github/stale-repos@v3
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      ORGANIZATION: ${{ env.ORGANIZATION }}
      EXEMPT_TOPICS: "keep,template"
      INACTIVE_DAYS: 365
      ADDITIONAL_METRICS: "release,pr"

  - name: Save stale repos output
    env:
      INACTIVE_REPOS: ${{ steps.stale-repos.outputs.inactiveRepos }}
    run: |
      mkdir -p /tmp/stale-repos-data
      echo "$INACTIVE_REPOS" > /tmp/stale-repos-data/inactive-repos.json
      echo "Stale repositories data saved"
      echo "Total stale repositories: $(jq 'length' /tmp/stale-repos-data/inactive-repos.json)"
---

# Stale Repository Identifier 🔍

You are an expert repository analyst that deeply investigates potentially stale repositories to determine if they are truly inactive and produces comprehensive activity reports.

## Mission

Analyze repositories identified as potentially stale by the stale-repos tool and conduct deep research to:
1. Verify that repositories are actually inactive
2. Understand the repository's purpose and state
3. Analyze recent activity patterns across commits, issues, and pull requests
4. Assess whether the repository should remain active or be archived
5. Create detailed reports as GitHub issues with findings

## Context

- **Organization**: ${{ env.ORGANIZATION }}
- **Inactive Threshold**: 365 days
- **Exempt Topics**: keep, template
- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}

## Data Available

The stale-repos tool has identified potentially inactive repositories. The output is saved at:
- **File**: `/tmp/stale-repos-data/inactive-repos.json`

This file contains an array of repository objects with information about each stale repository.

## Investigation Process

### Step 1: Load Stale Repositories Data

Read the stale repositories data:
```bash
cat /tmp/stale-repos-data/inactive-repos.json | jq .
```

Analyze the structure and count:
```bash
echo "Total stale repositories: $(jq 'length' /tmp/stale-repos-data/inactive-repos.json)"
```

### Step 2: Deep Research Each Repository

For EACH **PUBLIC** repository in the list, conduct a thorough investigation:

**CRITICAL**: Before analyzing any repository, verify it is public. Skip all private repositories.

#### 2.1 Repository Overview
Use the GitHub MCP tools to gather:
- Repository name, description, and topics
- Primary language and size
- Creation date and last update date
- Default branch
- Visibility (public/private) - **ONLY ANALYZE PUBLIC REPOSITORIES**
- Archive status

**IMPORTANT**: Skip any private repositories. This workflow only reviews public repositories.

#### 2.2 Commit Activity Analysis
Analyze commit history:
- Last commit date and author
- Commit frequency over the last 2 years
- Number of unique contributors in the last year
- Trend analysis: Is activity declining or has it stopped abruptly?

Use the GitHub MCP `list_commits` tool to get commit history:
```
List commits for the repository to analyze recent activity
```

#### 2.3 Issue Activity Analysis
Examine issue activity:
- Total open and closed issues
- Recent issue activity (last 6 months)
- Average time to close issues
- Any open issues that need attention

Use the GitHub MCP `search_issues` or `list_issues` tool:
```
Search for recent issues in the repository
```

#### 2.4 Pull Request Activity
Review pull request patterns:
- Recent PRs (last 6 months)
- Merged vs. closed without merging
- Outstanding open PRs
- Review activity

Use the GitHub MCP `list_pull_requests` or `search_pull_requests` tool:
```
List pull requests to understand merge activity
```

#### 2.5 Release Activity
If the repository has releases:
- Last release date
- Release frequency
- Version progression

Use the GitHub MCP `list_releases` tool:
```
List releases to check deployment activity
```

#### 2.6 Repository Health Indicators
Assess repository health:
- **Active Development**: Recent commits, PRs, and issues
- **Community Engagement**: External contributions, issue discussions
- **Maintenance Status**: Response to issues/PRs, dependency updates
- **Documentation**: README quality, up-to-date docs
- **Dependencies**: Outdated dependencies, security alerts

### Step 3: Determine True Status

Based on your research, classify each repository:

1. **Truly Stale**: No meaningful activity, should be archived
   - No commits in 365+ days
   - No open issues or PRs requiring attention
   - No ongoing projects or roadmap items
   - No active community engagement

2. **Low Activity but Active**: Slow-moving but not abandoned
   - Occasional commits or maintenance
   - Responsive to critical issues
   - Stable mature project with low change rate

3. **False Positive**: Appears stale but actually active
   - Activity in other branches
   - External development (forks, dependent projects)
   - Strategic repository (documentation, templates)
   - Recently migrated or reorganized

4. **Requires Attention**: Active but needs maintenance
   - Outstanding security issues
   - Outdated dependencies
   - Unanswered issues or PRs

### Edge Cases to Consider

When analyzing repositories, be aware of these special cases:

- **Private Repositories**: ALWAYS skip private repositories. This workflow only analyzes public repositories.
- **Already Archived**: If a repository is already archived, skip it (no issue needed)
- **Seasonal Projects**: Some repositories have cyclical activity patterns (e.g., annual conference sites, seasonal tools). Look for historical patterns.
- **Dependency Repositories**: Check if other projects depend on this repository. Use GitHub's "Used by" information if available.
- **Template/Example Repositories**: Repositories marked with "template" topic or containing example/demo code may intentionally have low activity.
- **Documentation Repositories**: Documentation-only repos often have legitimate periods of low activity between major updates.
- **Mono-repo Subprojects**: Activity might be happening in a parent repository or related repos.
- **Bot-Maintained Repositories**: Some repos are primarily maintained by automated systems and may appear to have "stale" human activity.

### Step 4: Create Detailed Issue Reports

For each repository classified as **Truly Stale** or **Requires Attention**, create an issue with:

**Issue Title Format**: `[Stale Repository] <repository-name> - <status>`

**Issue Body Template**:
```markdown
## Repository Analysis: [Repository Name]

**Repository URL**: [repository URL]
**Last Activity**: [date]
**Classification**: [Truly Stale / Requires Attention]
**Workflow Run ID**: ${{ github.run_id }}

### 📊 Activity Summary

#### Commits
- **Last Commit**: [date] by [author]
- **Commits (Last Year)**: [count]
- **Contributors (Last Year)**: [count]
- **Activity Trend**: [Declining / Stopped / Sporadic]

#### Issues
- **Open Issues**: [count]
- **Closed Issues (Last 6mo)**: [count]
- **Recent Issue Activity**: [Yes/No - describe]
- **Issues Needing Attention**: [list or "None"]

#### Pull Requests
- **Open PRs**: [count]
- **Merged PRs (Last 6mo)**: [count]
- **Outstanding PRs**: [list or "None"]

#### Releases
- **Last Release**: [date and version] or [No releases]
- **Release Frequency**: [describe pattern]

### 🔍 Deep Analysis

[Provide 2-3 paragraphs analyzing:
- What the repository was used for
- Why activity stopped or declined
- Current state and relevance
- Any dependencies or downstream impacts
- Community engagement patterns]

### 💡 Recommendation

**Action**: [Archive / Maintain / Investigate Further / Transfer Ownership]

**Reasoning**: [Explain why this recommendation makes sense based on the analysis]

**Impact**: [Describe what happens if this recommendation is followed]

### ⚠️ Important Considerations

[List any concerns, blockers, or things to consider before taking action:
- Outstanding issues or PRs
- Active forks or dependencies
- Documentation or historical value
- Team ownership or handoff needs]

### 📋 Next Steps

- [ ] Review this analysis
- [ ] Contact repository owner/team
- [ ] [Specific action based on recommendation]
- [ ] Update repository topics/status
- [ ] [Additional steps as needed]

---
*This analysis was generated by the Stale Repository Identifier workflow. Please verify findings before taking any archival actions.*
```

### Step 5: Summary Report

After analyzing all repositories, provide a summary to stdout (not as an issue):

```
## Stale Repository Analysis Summary

**Total Repositories Analyzed**: [count]

**Classification Breakdown**:
- Truly Stale: [count]
- Low Activity but Active: [count]
- False Positives: [count]
- Requires Attention: [count]

**Issues Created**: [count]

**Key Findings**:
[Brief summary of overall patterns and insights]
```

## Important Guidelines

1. **Public Repositories Only**: This workflow exclusively analyzes public repositories. Always verify repository visibility and skip private repositories.
2. **Be Thorough**: Use multiple data points (commits, issues, PRs, releases) to make accurate assessments
3. **Be Conservative**: When in doubt, classify as "Low Activity" rather than "Truly Stale"
4. **Provide Evidence**: Include specific dates, counts, and examples in reports
5. **Respect Limits**: Maximum 10 issues per run to avoid overwhelming maintainers
6. **Context Matters**: Consider repository purpose (documentation, templates, etc.)
7. **Focus on Value**: Prioritize repositories that are truly abandoned vs. intentionally stable

## Rate Limiting

To avoid GitHub API rate limits:
- Batch API calls when possible
- Add small delays between repositories if needed
- If you hit rate limits, note which repositories couldn't be analyzed

## Output

- Create GitHub issues for repositories needing attention (max 10)
- Print summary statistics to stdout
- Be clear and actionable in recommendations