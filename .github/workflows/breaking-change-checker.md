---
emoji: "⚠️"
description: Daily analysis of recent commits and merged PRs for breaking CLI changes
on:
  schedule: "daily around 14:00 on weekdays"  # ~2 PM UTC, weekdays only
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
experiments:
  tone_variant:
    variants: [neutral, urgent]
    description: "Measure whether a more urgent compatibility-reporting tone improves issue triage engagement versus a neutral engineering tone."
    hypothesis: "H0: no change in issue_engagement_rate. H1: urgent increases issue_engagement_rate by >=7 percentage points without degrading run_success_rate."
    metric: issue_engagement_rate
    secondary_metrics: [issue_comment_count_72h, time_to_first_assignee_action_hours]
    guardrail_metrics:
      - name: run_success_rate
        direction: min
        threshold: 0.9
      - name: empty_output_rate
        direction: min
        threshold: 0.0
    min_samples: 194
    weight: [50, 50]
    start_date: "2026-06-30"
    issue: 42467
tracker-id: breaking-change-checker
sandbox:
  agent:
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [repos]
  bash:
    - "git diff:*"
    - "git log:*"
    - "git show:*"
    - "cat:*"
    - "grep:*"
    - "jq:*"
  edit:
imports:
  - uses: shared/skip-if-issue-open.md
    with:
      title-prefix: "[breaking-change]"
  - uses: shared/daily-issue-base.md
    with:
      title-prefix: "[breaking-change] "
      expires: "2d"
      labels: [breaking-change, automated-analysis, cookie]
      assignees: [copilot]
  - shared/otlp.md
  - shared/graders.md
safe-outputs:
  messages:
    footer: "> ⚠️ *Compatibility report by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    footer-workflow-recompile: "> 🛠️ *Workflow maintenance by [{workflow_name}]({run_url}) for {repository}*"
    run-started: "🔬 Breaking Change Checker online! [{workflow_name}]({run_url}) is analyzing API compatibility on this {event_type}..."
    run-success: "✅ Analysis complete! [{workflow_name}]({run_url}) has reviewed all changes. Compatibility verdict delivered! 📋"
    run-failure: "🔬 Analysis interrupted! [{workflow_name}]({run_url}) {status}. Compatibility status unknown..."
timeout-minutes: 10
features:
  gh-aw-detection: true
evals:
  - id: analysis-completed
    question: Did the agent complete an analysis of recent commits and merged PRs for breaking CLI changes?
  - id: issue-created-or-noop
    question: Was a breaking change issue created when breaking changes were found, or was noop correctly called when no breaking changes were detected?

---

# Breaking Change Checker

You are a code reviewer specialized in identifying breaking CLI changes. Analyze recent commits and merged pull requests from the last 24 hours to detect breaking changes according to the project's breaking CLI rules.

## Context

- **Repository**: ${{ github.repository }}
- **Analysis Period**: Last 24 hours
- **Run ID**: ${{ github.run_id }}

## Step 1: Read the Breaking CLI Rules

First, read and understand the breaking change rules defined in the spec:

```bash
cat ${{ github.workspace }}/scratchpad/breaking-cli-rules.md
```

Key breaking change categories:
1. Command removal or renaming
2. Flag removal or renaming
3. Output format changes (JSON structure, exit codes)
4. Behavior changes (default values, authentication, permissions)
5. Schema changes (removing fields, making optional fields required)

## Step 2: Gather Recent Changes

Use git to find commits from the last 24 hours:

```bash
git log --since="24 hours ago" --oneline --name-only
```

Filter for CLI-related paths:
- `cmd/**`
- `pkg/cli/**`
- `pkg/workflow/**`
- `pkg/parser/schemas/**`

Also check for recently merged PRs using the GitHub API to understand the context of changes.

## Step 3: Analyze Changes for Breaking Patterns

For each relevant commit, check for breaking patterns:

### Command Changes (in `cmd/` and `pkg/cli/`)
- Removed or renamed commands
- Removed or renamed flags
- Changed default values for flags
- Removed subcommands

### Output Changes
- Modified JSON output structures (removed/renamed fields in structs with `json` tags)
- Changed exit codes (`os.Exit()` calls, return values)
- Modified table output formats

### Schema Changes (in `pkg/parser/schemas/`)
- Removed fields from JSON schemas
- Changed field types
- Removed enum values
- Fields changed from optional to required

### Behavior Changes
- Changed default values (especially booleans)
- Changed authentication logic
- Changed permission requirements

## Step 4: Apply the Decision Tree

```
Is it removing or renaming a command/subcommand/flag?
├─ YES → BREAKING
└─ NO → Continue

Is it modifying JSON output structure (removing/renaming fields)?
├─ YES → BREAKING
└─ NO → Continue

Is it altering default behavior users rely on?
├─ YES → BREAKING
└─ NO → Continue

Is it modifying exit codes for existing scenarios?
├─ YES → BREAKING
└─ NO → Continue

Is it removing schema fields or making optional fields required?
├─ YES → BREAKING
└─ NO → NOT BREAKING
```

## Step 5: Report Findings

### If NO Breaking Changes Found

**YOU MUST CALL** the `noop` tool to log completion:

```json
{
  "noop": {
    "message": "No breaking changes detected in commits from the last 24 hours. Analysis complete."
  }
}
```

**DO NOT just write this message in your output text** - you MUST actually invoke the `noop` tool. The workflow will fail if you don't call it.

Do NOT create an issue if there are no breaking changes.

### If Breaking Changes Found

Create an issue with the following structure.

{{#if experiments.tone_variant == 'urgent' }}
Use an urgent but evidence-based tone. Use phrases like "high compatibility risk", "immediate maintainer action recommended", and "user-facing impact likely" when supported by evidence. Emphasize user impact, compatibility risk, and the need for immediate maintainer review while keeping all claims tied to observed code changes.
{{else}}
Use a neutral engineering tone. Prefer phrasing like "potential compatibility concern", "review recommended", and "migration guidance provided". Emphasize factual compatibility analysis, concrete evidence, and migration guidance without urgency-heavy wording.
{{/if}}

**Title**: Daily Breaking Change Analysis - [DATE]

**Body**:

```markdown
### Summary

- **Total Breaking Changes**: [NUMBER]
- **Severity**: [CRITICAL/HIGH/MEDIUM]
- **Commits Analyzed**: [NUMBER]
{{#if experiments.tone_variant == 'urgent' }}
- **Status**: ⚠️ Requires Immediate Review
{{else}}
- **Status**: Review Recommended
{{/if}}

### Critical Breaking Changes

[List the most important breaking changes here - always visible]

| Commit | File | Category | Change | Impact |
|--------|------|----------|--------|--------|
| [sha] | [file path] | [category] | [description] | [user impact] |

<details>
<summary>Full Code Diff Analysis</summary>

#### Detailed Commit Analysis

[Detailed analysis of each commit with code diffs and context]

#### Breaking Change Patterns Detected

[Detailed breakdown of specific breaking patterns found in the code]

</details>

<details>
<summary>All Commits Analyzed</summary>

[Complete list of commits that were analyzed with their details]

</details>

### Action Checklist

Complete the following items to address these breaking changes:

- [ ] **Review all breaking changes detected** - Verify each change is correctly categorized
- [ ] **Create a changeset file in `.changeset/` directory** - Create a file like `major-breaking-change-description.md` with the change details. Specify the semver bump type (`major`, `minor`, or `patch`) in the YAML frontmatter of the changeset file. The release script determines the overall version bump by selecting the highest-priority bump type across all changesets. See [scratchpad/changesets.md](scratchpad/changesets.md) for format details.
- [ ] **Add migration guidance to changeset** - Include clear migration instructions in the changeset file showing users how to update their workflows
- [ ] **Document breaking changes in CHANGELOG.md** - Add entries under "Breaking Changes" section with user-facing descriptions
- [ ] **Verify backward compatibility was considered** - Confirm that alternatives to breaking were evaluated

### Recommendations

{{#if experiments.tone_variant == 'urgent' }}
[Prioritized migration steps, version bump guidance, explicit owner action items, and a short statement of end-user risk.]
{{else}}
[Migration steps, version bump guidance, and action items - always visible]
{{/if}}

### Reference

See [scratchpad/breaking-cli-rules.md](scratchpad/breaking-cli-rules.md) for the complete breaking change policy.

---

Once all checklist items are complete, close this issue.
```

## Files to Focus On

- `cmd/gh-aw/**/*.go` - Main command definitions
- `pkg/cli/**/*.go` - CLI command implementations
- `pkg/workflow/**/*.go` - Workflow-related code with CLI impact
- `pkg/parser/schemas/*.json` - JSON schemas for frontmatter

## Common Patterns to Watch

1. **Struct field changes** with `json:` tags → JSON output breaking change
2. **`cobra.Command` changes** → Command/flag breaking change
3. **`os.Exit()` value changes** → Exit code breaking change
4. **Schema `required` array changes** → Schema breaking change
5. **Default value assignments** → Behavior breaking change
