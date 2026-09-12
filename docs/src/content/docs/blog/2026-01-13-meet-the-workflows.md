---
title: "Meet the Workflows: Issue Triage"
description: "A curated tour of triage and summarization workflows in the factory"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T01:00:00
sidebar:
  label: "Issue Triage"
prev:
  link: /gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/
  label: Welcome to Peli's Agent Factory
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-continuous-simplicity/
  label: "Continuous Simplicity"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

Welcome back to [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)!

We're the GitHub Next team. Over the past months, we've built and operated a collection of automated agentic workflows. These aren't just demos - these are real agents doing actual work in our [`github/gh-aw`](https://github.com/github/gh-aw) repository and others.

Think of this as your guided tour through our agent factory. We're showcasing the workflows that caught our attention. Every workflow links to its source markdown file, so you can peek under the hood and see exactly how it works.

## Starting Simple: Automated Issue Triage

To start the tour, let's begin with one of the simpler workflows that **handles incoming activity** - issue triage.

Issue triage represents a "hello world" of automated agentic workflows: practical, immediately useful, relatively simple, and impactful. It's used as the starter example in other agentic automation technologies like [Claude Code in GitHub Actions](https://code.claude.com/docs/en/github-actions).

On a regular schedule, the triage agent looks for unlabeled issues, analyzes their content, does research in the codebase and other issues, responds with a comment, and applies appropriate labels based on predefined categories. This helps maintainers quickly understand the nature of incoming issues without manual review.

Let's take a look at the full **[Issue Triage Agent](https://github.com/github/gh-aw/blob/v0.45.5/.github/workflows/issue-triage-agent.md?plain=1)**:

````markdown
---
timeout-minutes: 5
strict: true

on:
  schedule: "daily around 14:00 on weekdays"
  workflow_dispatch:

permissions:
  issues: read

tools:
  github:
    # This workflow processes issues from a public repository, so we restrict
    # the content the agent can see to trusted authors.
    min-integrity: approved
    toolsets: [issues, labels]

safe-outputs:
  add-labels:
    allowed: [bug, feature, enhancement, documentation, question, help-wanted, good-first-issue]
  add-comment: {}

imports:
  - shared/mood.md
  - shared/reporting.md
---

# Issue Triage Agent

List open issues in ${{ github.repository }} that have no labels. For each
unlabeled issue, analyze the title and body, then add one of the allowed
labels: `bug`, `feature`, `enhancement`, `documentation`, `question`,
`help-wanted`, `good-first-issue`, or `community`.

Skip issues that:
- Already have any of these labels
- Have been assigned to any user (especially non-bot users)

After adding the label to an issue, mention the issue author in a comment
using this format (follow shared/reporting.md guidelines):

**Comment Template**:
```markdown
### 🏷️ Issue Triaged

Hi @{author}! I've categorized this issue as **{label_name}** based on the
following analysis:

**Reasoning**: {brief_explanation_of_why_this_label}

<details>
<summary><b>View Triage Details</b></summary>

#### Analysis
- **Keywords detected**: {list_of_keywords_that_matched}
- **Issue type indicators**: {what_made_this_fit_the_category}
- **Confidence**: {High/Medium/Low}

#### Recommended Next Steps
- {context_specific_suggestion_1}
- {context_specific_suggestion_2}

</details>
```

**Key formatting requirements**:
- Use h3 (###) for the main heading
- Keep reasoning visible for quick understanding
- Wrap detailed analysis in `<details>` tags
- Keep total comment concise (collapsed details prevent noise)

## Labels

- `bug`: Indicates a problem or error in the code that needs fixing.
- `feature`: Represents a new feature request or enhancement to existing functionality.
- `enhancement`: Suggests improvements to existing features or code.
- `documentation`: Pertains to issues related to documentation, such as missing or unclear docs.
- `question`: Used for issues that are asking for clarification.
- `help-wanted`: Indicates that the issue is a good candidate for external contributions.
- `good-first-issue`: Marks issues that are suitable for newcomers to the project.
- `community`: Indicates that the issue is related to community engagement.
````

Note how concise the instructions are - it's like reading a to-do list for the agent. The workflow runs on a [fuzzy schedule](/gh-aw/reference/schedule-syntax/) each weekday, and can also be launched manually with `workflow_dispatch`. It checks for unlabeled issues, analyzes their content, and applies appropriate labels based on content analysis. It even leaves a friendly comment explaining the label choice.

In the frontmatter, we define [permissions](/gh-aw/reference/frontmatter/#permissions-permissions), [tools](/gh-aw/reference/tools/), and [safe outputs](/gh-aw/reference/safe-outputs/). This ensures the agent only has access to what it needs and can't perform any unsafe actions. `strict: true` turns compiler warnings into errors so misconfigurations are caught at compile time, and [`imports`](/gh-aw/reference/imports/) pull in shared instructions reused across our workflows. The natural language instructions in the body guide the agent's behavior in a clear, human-readable way.

Issue triage workflows in public repositories may need to process issues from all contributors. As shown above, `min-integrity: approved` restricts agent visibility to owners, members, and collaborators. If you are a maintainer in a public repository and need your triage agent to see and label issues from users without push access, set `min-integrity: none` in your GitHub tools configuration. See [Integrity Filtering](/gh-aw/reference/integrity/) for security considerations and best practices.

We've deliberately kept this workflow ultra-simple. In practice, in your own repo, **customization** is key. Triage differs in every repository. Tailoring workflows to your specific context will make them more effective. Generic agents are okay, but customized ones are often a better fit.

## Using These Workflows

You can add this workflow to your own repository and remix it as follows:

**Issue Triage Agent:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.45.5/.github/workflows/issue-triage-agent.md
```

Then edit and remix the workflow specification to meet your needs, regenerate the lock file using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

You can also [create your own workflows](/gh-aw/setup/creating-workflows/).

## Next Up: Code Quality & Refactoring Workflows

Now that we've explored how triage workflows help us stay on top of incoming activity, let's turn to something far more radical and powerful: agents that continuously improve code.

Continue reading: [Continuous Simplicity →](/gh-aw/blog/2026-01-13-meet-the-workflows-continuous-simplicity/)

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

---

*This is part 1 of a 19-part series exploring the workflows in Peli's Agent Factory.*
