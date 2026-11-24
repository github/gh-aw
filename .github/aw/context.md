---
description: GitHub context expression variables and Handlebars-style template conditionals ({{#if}}) for agentic workflows.
---

## GitHub Context Expression Interpolation

**For security reasons, only the expressions below are allowed.** All others fail compilation with an error listing them.

### Allowed Context Variables

Use as `${{ <name> }}`, e.g. `${{ github.event.issue.number }}`.

- **Workflow/run**: `github.actor` (initiating user), `github.event_name`, `github.job`, `github.repository` ("owner/name"), `github.repository_owner`, `github.run_id`, `github.run_number`, `github.server_url` (e.g. <https://github.com>), `github.workflow`, `github.workspace` (default runner working directory)
- **Push/commits**: `github.event.after` (SHA after push), `github.event.before` (SHA before push), `github.event.head_commit.id`
- **Issues**: `github.event.issue.number`, `github.event.issue.state` (open/closed), `github.event.issue.title`
- **Pull requests**: `github.event.pull_request.number`, `github.event.pull_request.state` (open/closed), `github.event.pull_request.title`, `github.event.pull_request.head.sha`, `github.event.pull_request.base.sha`
- **Discussions**: `github.event.discussion.number`, `github.event.discussion.title`, `github.event.discussion.category.name`
- **Comments/reviews**: `github.event.comment.id`, `github.event.review.id`, `github.event.review_comment.id`
- **Releases**: `github.event.release.id`, `github.event.release.name`, `github.event.release.tag_name`, `github.event.release.assets[0].id` (first asset)
- **Deployments**: `github.event.deployment.id`, `github.event.deployment.environment`, `github.event.deployment_status.id`
- **Checks**: `github.event.check_run.id`, `github.event.check_suite.id`
- **Workflow jobs/runs**: `github.event.workflow_job.id`, `github.event.workflow_job.run_id`, `github.event.workflow_run.id`, `github.event.workflow_run.number`, `github.event.workflow_run.run_number`, `github.event.workflow_run.conclusion`, `github.event.workflow_run.status`, `github.event.workflow_run.event` (triggering event), `github.event.workflow_run.html_url`, `github.event.workflow_run.head_sha`
- **Other IDs**: `github.event.installation.id` (GitHub App installation), `github.event.label.id`, `github.event.milestone.id`, `github.event.milestone.number`, `github.event.organization.id`, `github.event.page.id` (GitHub Pages page), `github.event.project.id`, `github.event.project_card.id`, `github.event.project_column.id`, `github.event.repository.id`, `github.event.repository.default_branch`, `github.event.sender.id` (triggering user)

#### Special Pattern Expressions

- **`${{ needs.* }}`** - Any outputs from previous jobs (e.g., `${{ needs.pre_activation.outputs.activated }}`, or `${{ needs.activation.outputs.label_command }}` for the triggering label when using a `label_command` trigger). The activation job cannot reference its own outputs—only jobs after activation can.
- **`${{ steps.* }}`** - Any outputs from previous steps (e.g., `${{ steps.my-step.outputs.result }}`)
- **`${{ github.event.inputs.* }}`** - Any workflow inputs when triggered by workflow_dispatch (e.g., `${{ github.event.inputs.environment }}`)

`aw_context` is reserved and injected by the compiler into supported dispatch and reusable-workflow triggers; do not declare it as a user input. Workflows with `tools.work-queue` receive a compiler-managed `work_queue_assignment` dispatch input: `{"version":4,"dispatch_id":"d_…"}`. The worker reads its Claims from the queue snapshot; do not declare or construct this input.

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

Conditional blocks resolve **at runtime, before the agent sees the prompt**; the agent sees only resolved text.

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

- **Malformed conditions fail compilation** — comparisons need an operator (`==`, `!=`, `===`, `!==`); invalid active conditions in the main workflow, imports, or local runtime imports block lock-file generation.
- **Code regions** — only syntactically invalid implicit conditions inside inline code or fenced blocks stay literal; valid conditions and explicit GitHub Actions expressions there are still bound and validated.
- **No nested conditionals** — inner tags become literal text.
- **Tags are stripped before the agent runs** — never visible in the final prompt.
