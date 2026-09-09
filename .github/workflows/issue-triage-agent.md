---
emoji: "🔧"
timeout-minutes: 5
engine:
  id: codex
  model-provider: github
model: copilot/gpt-5.3-codex
strict: true
on:
  schedule: "daily around 14:00 on weekdays"  # ~2 PM UTC, weekdays only
  workflow_dispatch:
permissions:
  issues: read


  copilot-requests: write
tools:
  cli-proxy: true
  github:
    min-integrity: approved
    toolsets: [issues, labels]
safe-outputs:
  data:
    issue_number: integer
    label: string
    confidence: string
    reasoning: string
  add-labels:
    allowed: [bug, feature, enhancement, documentation, question, help-wanted, good-first-issue]
  add-comment: {}
imports:
  - shared/github-guard-policy.md
  - shared/reporting.md

  - shared/otlp.md
evals:
  - id: labels_applied_or_noop
    question: Did the agent apply at least one label to an unlabeled issue, or correctly skip when no unlabeled issues were found?
  - id: allowed_labels_only
    question: Does the agent output show that only labels from the allowed set were applied to issues?
sandbox:
  agent:
    runtime: cloud-hypervisor
---

# Issue Triage Agent

List open issues in ${{ github.repository }} that have no labels. For each unlabeled issue, analyze the title and body, then add one of the allowed labels: `bug`, `feature`, `enhancement`, `documentation`, `question`, `help-wanted`, or `good-first-issue`, `community`.

Skip issues that:
- Already have any of these labels
- Have been assigned to any user (especially non-bot users)

After adding the label to an issue, mention the issue author in a comment using this format (follow shared/reporting.md guidelines):

**Comment Template**:
```markdown
### 🏷️ Issue Triaged

Hi @{author}! I've categorized this issue as **{label_name}** based on the following analysis:

**Reasoning**: {brief_explanation_of_why_this_label}

<details>
<summary>View Triage Details</summary>

#### Analysis
- **Keywords detected**: {list_of_keywords_that_matched}
- **Issue type indicators**: {what_made_this_fit_the_category}
- **Confidence**: {High/Medium/Low}

#### Recommended Next Steps
- {context_specific_suggestion_1}
- {context_specific_suggestion_2}

</details>

**References**: [Triage run §{run_id}](https://github.com/github/gh-aw/actions/runs/{run_id})
```

**Key formatting requirements**:
- Use h3 (###) for the main heading
- Keep reasoning visible for quick understanding
- Wrap detailed analysis in `<details>` tags
- Include workflow run reference
- Keep total comment concise (collapsed details prevent noise)

## Batch Comment Optimization

For efficiency, if multiple issues are triaged in a single run:
1. Add individual labels to each issue
2. Add a brief comment to each issue (using the template above)
3. Optionally: Create a discussion summarizing all triage actions for that run

This provides both per-issue context and batch visibility.

## Labels

- `bug`: Indicates a problem or error in the code that needs fixing.
- `feature`: Represents a new feature request or enhancement to existing functionality.
- `enhancement`: Suggests improvements to existing features or code.
- `documentation`: Pertains to issues related to documentation, such as missing or unclear docs.
- `question`: Used for issues that are asking for clarification or have questions about the project.
- `help-wanted`: Indicates that the issue is a good candidate for external contributions and help
- `good-first-issue`: Marks issues that are suitable for newcomers to the project, often with simpler scope.
- `community`: Indicates that the issue is related to community engagement, such as events, discussions, or contributions that don't fit into the other categories. From authors who are not contributors to the codebase but are engaging with the project in other ways.