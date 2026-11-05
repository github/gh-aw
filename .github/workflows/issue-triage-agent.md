---
emoji: "🔧"
timeout-minutes: 5
engine:
  id: codex
  model-provider: github
model: copilot/gpt-6-sol
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
    target: "*"
    max: 5
    pull-requests: false
    allowed: [bug, feature, enhancement, documentation, question, help-wanted, good-first-issue]
    item-schema:
      type: object
      required: [name, rationale, confidence]
      additionalProperties: false
      properties:
        name: { type: string }
        rationale: { type: string }
        confidence: { type: string, enum: [HIGH, MEDIUM, LOW] }
  add-comment:
    target: "*"
    max: 5
    pull-requests: false
imports:
  - shared/github-guard-policy.md
  - shared/reporting.md

  - shared/otlp.md
evals:
  - id: labels_applied_or_noop
    question: Did the agent apply at least one label to an unlabeled issue, or correctly skip when no unlabeled issues were found?
  - id: allowed_labels_only
    question: Does the agent output show that only labels from the allowed set were applied to issues?
---

# Issue Triage Agent

List open issues in ${{ github.repository }} that have no labels. Triage at most 5 eligible issues per run. For each unlabeled issue, analyze the title and body, then add one of the allowed labels: `bug`, `feature`, `enhancement`, `documentation`, `question`, `help-wanted`, or `good-first-issue`.

Skip issues that:
- Already have any of these labels
- Have been assigned to any user (especially non-bot users)

If an issue's title or body is unavailable or withheld by the integrity policy, leave it untouched and report the missing data separately. Do not retry through alternate tools to bypass that policy.

This workflow runs on a schedule or manual dispatch, not an issue event. For both `add_labels` and `add_comment`, explicitly pass the selected issue's numeric `item_number`. Only target eligible issues in ${{ github.repository }}; do not supply a different repository or a `target` tool argument.

After submitting the label action for an issue, submit a comment mentioning the issue author using this format (follow shared/reporting.md guidelines). Safe-output acceptance queues an action; it does not confirm that GitHub has applied it.

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
1. Select at most 5 eligible issues
2. Submit one label action for each selected issue with its explicit `item_number`
3. Submit one brief comment for each selected issue with the same `item_number` (using the template above)

This provides both per-issue context and batch visibility.

## Labels

- `bug`: Indicates a problem or error in the code that needs fixing.
- `feature`: Represents a new feature request or enhancement to existing functionality.
- `enhancement`: Suggests improvements to existing features or code.
- `documentation`: Pertains to issues related to documentation, such as missing or unclear docs.
- `question`: Used for issues that are asking for clarification or have questions about the project.
- `help-wanted`: Indicates that the issue is a good candidate for external contributions and help
- `good-first-issue`: Marks issues that are suitable for newcomers to the project, often with simpler scope.