---
description: GitHub context expression variables and Handlebars-style template conditionals ({{#if}}) for agentic workflows.
---

## GitHub Context Expression Interpolation

**For security reasons, only specific expressions are allowed.**

### Allowed Context Variables

- **`${{ github.event.after }}`** - SHA of the most recent commit after the push
- **`${{ github.event.before }}`** - SHA of the most recent commit before the push
- **`${{ github.event.check_run.id }}`** - ID of the check run
- **`${{ github.event.check_suite.id }}`** - ID of the check suite
- **`${{ github.event.comment.id }}`** - ID of the comment
- **`${{ github.event.deployment.id }}`** - ID of the deployment
- **`${{ github.event.deployment_status.id }}`** - ID of the deployment status
- **`${{ github.event.head_commit.id }}`** - ID of the head commit
- **`${{ github.event.installation.id }}`** - ID of the GitHub App installation
- **`${{ github.event.issue.number }}`** - Issue number
- **`${{ github.event.issue.state }}`** - State of the issue (open/closed)
- **`${{ github.event.issue.title }}`** - Title of the issue
- **`${{ github.event.label.id }}`** - ID of the label
- **`${{ github.event.milestone.id }}`** - ID of the milestone
- **`${{ github.event.milestone.number }}`** - Number of the milestone
- **`${{ github.event.organization.id }}`** - ID of the organization
- **`${{ github.event.page.id }}`** - ID of the GitHub Pages page
- **`${{ github.event.project.id }}`** - ID of the project
- **`${{ github.event.project_card.id }}`** - ID of the project card
- **`${{ github.event.project_column.id }}`** - ID of the project column
- **`${{ github.event.pull_request.number }}`** - Pull request number
- **`${{ github.event.pull_request.state }}`** - State of the pull request (open/closed)
- **`${{ github.event.pull_request.title }}`** - Title of the pull request
- **`${{ github.event.pull_request.head.sha }}`** - SHA of the PR head commit
- **`${{ github.event.pull_request.base.sha }}`** - SHA of the PR base commit
- **`${{ github.event.discussion.number }}`** - Discussion number
- **`${{ github.event.discussion.title }}`** - Title of the discussion
- **`${{ github.event.discussion.category.name }}`** - Category name of the discussion
- **`${{ github.event.release.assets[0].id }}`** - ID of the first release asset
- **`${{ github.event.release.id }}`** - ID of the release
- **`${{ github.event.release.name }}`** - Name of the release
- **`${{ github.event.release.tag_name }}`** - Tag name of the release
- **`${{ github.event.repository.id }}`** - ID of the repository
- **`${{ github.event.repository.default_branch }}`** - Default branch of the repository
- **`${{ github.event.review.id }}`** - ID of the review
- **`${{ github.event.review_comment.id }}`** - ID of the review comment
- **`${{ github.event.sender.id }}`** - ID of the user who triggered the event
- **`${{ github.event.deployment.environment }}`** - Deployment environment name
- **`${{ github.event.workflow_job.id }}`** - ID of the workflow job
- **`${{ github.event.workflow_job.run_id }}`** - Run ID of the workflow job
- **`${{ github.event.workflow_run.id }}`** - ID of the workflow run
- **`${{ github.event.workflow_run.number }}`** - Number of the workflow run
- **`${{ github.event.workflow_run.conclusion }}`** - Conclusion of the workflow run
- **`${{ github.event.workflow_run.status }}`** - Status of the workflow run
- **`${{ github.event.workflow_run.event }}`** - Event that triggered the workflow run
- **`${{ github.event.workflow_run.html_url }}`** - HTML URL of the workflow run
- **`${{ github.event.workflow_run.head_sha }}`** - Head SHA of the workflow run
- **`${{ github.event.workflow_run.run_number }}`** - Run number of the workflow run
- **`${{ github.actor }}`** - Username of the person who initiated the workflow
- **`${{ github.event_name }}`** - Name of the event that triggered the workflow
- **`${{ github.job }}`** - Job ID of the current workflow run
- **`${{ github.repository }}`** - Repository name in "owner/name" format
- **`${{ github.repository_owner }}`** - Owner of the repository (organization or user)
- **`${{ github.run_id }}`** - Unique ID of the workflow run
- **`${{ github.run_number }}`** - Number of the workflow run
- **`${{ github.server_url }}`** - Base URL of the server, e.g. <https://github.com>
- **`${{ github.workflow }}`** - Name of the workflow
- **`${{ github.workspace }}`** - The default working directory on the runner for steps

#### Special Pattern Expressions

- **`${{ needs.* }}`** - Any outputs from previous jobs (e.g., `${{ needs.pre_activation.outputs.activated }}`, or `${{ needs.activation.outputs.label_command }}` for the triggering label when using a `label_command` trigger). The activation job cannot reference its own outputs—only jobs after activation can.
- **`${{ steps.* }}`** - Any outputs from previous steps (e.g., `${{ steps.my-step.outputs.result }}`)
- **`${{ github.event.inputs.* }}`** - Any workflow inputs when triggered by workflow_dispatch (e.g., `${{ github.event.inputs.environment }}`)

`aw_context` is reserved and injected by the compiler into supported dispatch and reusable-workflow triggers; do not declare it as a user input. Workflows with `tools.work-queue` also receive a compiler-managed `work_queue_claim` dispatch input.

All other expressions are disallowed.

### Sanitized Context Text (`steps.sanitized.outputs.text`)

**RECOMMENDED**: Prefer `${{ steps.sanitized.outputs.text }}` over individual `github.event` fields for issue/PR content.

Auto-populated per triggering event:

- **Issues / Pull Requests**: `title + "\n\n" + body`
- **Issue Comments / PR Review Comments**: `comment.body`
- **PR Reviews**: `review.body`
- **Other events**: Empty string

**Security Benefits:**

- **@mention neutralization**: converts `@user` to `` `@user` ``
- **Bot trigger protection**: converts `fixes #123` to `` `fixes #123` ``
- **XML tag safety**: converts XML tags to parentheses to prevent injection
- **URI filtering**: only allows HTTPS URIs from trusted domains; others become "(redacted)"
- **Content limits**: truncates to 0.5MB / 65k lines max
- **Control character removal**: strips ANSI escape sequences and non-printable characters

### Security Validation

Expression safety is validated at compile time. Unauthorized expressions cause compilation to fail with an error listing them.

### Example Usage

```markdown
# Valid — prefer sanitized text
Analyze issue #${{ github.event.issue.number }} in repository ${{ github.repository }}.
The issue content is: "${{ steps.sanitized.outputs.text }}"

# Valid — individual fields (less secure)
Created by ${{ github.actor }} with title: "${{ github.event.issue.title }}"
Deploy to environment: "${{ github.event.inputs.environment }}"

# Invalid (compile errors)
# ${{ secrets.GITHUB_TOKEN }}
# ${{ env.MY_VAR }}
# ${{ toJson(github.workflow) }}
```

## Prompt Template Conditionals (`{{#if}}`)

Conditional blocks resolved **at runtime, before the agent sees the prompt** — the agent only sees the final resolved text.

### Syntax

```
{{#if <condition>}}
...true branch content...
{{#else}}
...false branch content (optional)...
{{#endif}}
```

- **`{{#if <condition>}}`** — content included only when truthy
- **`{{#else}}`** — optional false-branch separator
- **`{{#endif}}`** — closes block (**preferred** closing tag)
- **`{{/if}}`** — alternate closing tag (also supported)

Block form (tag on its own line) is recommended for readability.

### Supported Conditions

| Form | Example | Truthy when |
|---|---|---|
| Bare value | `{{#if experiments.flag }}` | value is non-empty and not `"false"` |
| Equality | `{{#if experiments.style == "concise" }}` | value equals the quoted string |
| Inequality | `{{#if experiments.style != "verbose" }}` | value does not equal the quoted string |
| Strict equality | `{{#if experiments.style === "concise" }}` | value strictly equals the quoted string |
| Strict inequality | `{{#if experiments.style !== "verbose" }}` | value strictly differs from the quoted string |

### Example: Conditional Without Else

```markdown
{{#if experiments.skill_hint == "enabled" }}
Check `skills/` and `.github/skills/` for relevant `SKILL.md` files and apply
their guidance.
{{#endif}}
```

### Example: Conditional With Else

```markdown
{{#if experiments.output_style == "concise" }}
Write a maximum of 5 bullet points. Each bullet is one sentence.
{{#else}}
Write a structured report with sections for new features, bug fixes, and refactors.
Include a one-paragraph executive summary at the top.
{{#endif}}
```

### Integration with Experiments

When `experiments:` is set in frontmatter, the selected variant is substituted into `{{#if experiments.<name> == "..." }}` conditions before rendering. See [A/B Testing Experiments](../aw/experiments.md).

### Notes

- **Fenced code blocks are preserved** — `{{#if}}` tags inside `` ``` `` blocks appear verbatim.
- **No nested conditionals** — inner tags become literal text.
- **Tags are stripped before the agent runs** — never visible in the final prompt.
