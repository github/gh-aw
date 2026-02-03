---
title: Markdown
description: Learn agentic workflow markdown content
sidebar:
  order: 300
---

The markdown body is the most important part of your agentic workflow, containing natural language instructions for the AI agent. The markdown follows the frontmatter and is loaded at runtime, allowing you to edit instructions directly on GitHub.com without recompilation. For example:

```aw wrap
---
...frontmatter...
---

# Issue Triage

Read the issue #${{ github.event.issue.number }}. Add a comment to the issue listing useful resources and links.
```

## Writing Effective Instructions

Write instructions as if explaining the task to a new team member. Be specific, provide context about your project and constraints, and structure instructions with headings to guide the agent's workflow.

```aw wrap
# Good: Specific and actionable
Analyze issue #${{ github.event.issue.number }} and add appropriate labels from the repository's label list. Focus on categorizing the issue type (bug, feature, documentation) and priority level (high, medium, low).

# Project Context
This repository follows semantic versioning and GitHub Flow. When reviewing pull requests, ensure all tests pass, documentation is updated for API changes, and breaking changes are clearly marked.

# Weekly Research Report

## Research Areas
Focus on competitor analysis, emerging AI development trends, and community feedback for ${{ github.repository }}.

## Output Format
Create a structured report with executive summary, key findings by area, and recommended actions.
```

### Best Practices

Use action-oriented language with clear verbs (analyze, create, update, triage) and specify expected outcomes. Help agents make consistent decisions by providing criteria and examples:

```aw wrap
# Issue Labeling Criteria
Apply labels: `bug` (incorrect behavior with repro steps), `enhancement` (new features), `question` (help requests), `documentation` (docs/examples). Priority: `high-priority` (security/critical bugs), `medium-priority` (features/non-critical bugs), `low-priority` (nice-to-have improvements).
```

#### Reference Context Securely
Use `needs.activation.outputs.text` instead of raw context fields like `github.event.issue.body`. Sanitized context automatically removes @mentions, bot triggers, XML tags, malicious URIs, and control characters while preventing prompt injection attacks and DoS through size limits.

```aw wrap
# RECOMMENDED: Use sanitized context text
Analyze issue #${{ github.event.issue.number }} in repository ${{ github.repository }}.
The content: "${{ needs.activation.outputs.text }}"

# DISCOURAGED: Raw context (security risks)
The issue body is: "${{ github.event.issue.body }}"
```

#### Handle Edge Cases
Anticipate unusual situations and error conditions. If a workflow fails, document the failure in an issue with error messages and context, tag it with 'workflow-failure', and exit gracefully without partial changes.

### Content Organization

Use numbered lists for multi-step processes, conditional statements for decision-making, and templates for consistent output:

```aw wrap
# Code Review Process
1. Check CI checks are passing and PR has appropriate title/description
2. Scan for code quality issues and verify error handling/logging
3. Create constructive comments and summarize assessment

# Issue Triage Logic
If error messages/stack traces: label 'bug', check for similar issues, request info if needed
If feature request: label 'enhancement', assess scope and complexity
Otherwise: label 'question'/'discussion', provide resources

# Status Report Template
## Summary: [week's activities]
## Key Metrics: PRs merged, issues resolved, new contributors
## Highlights: [achievements, decisions]
## Next Week: [planned priorities]
```

### Common Pitfalls

Avoid over-complexity (keep instructions focused), assuming knowledge (explain project conventions), inconsistent formatting, missing error handling, and vague success criteria. Before deploying, read instructions aloud to check clarity, review examples for accuracy, and consider edge cases.

## Templating

Agentic markdown supports GitHub Actions expression substitutions and conditional templating for content. See [Templating and Substitutions](/gh-aw/reference/templating/) for details.

## Editing and Iteration

> [!TIP]
> The markdown body is loaded at runtime from the original `.md` file. You can edit instructions directly on GitHub.com and changes take effect immediately without recompiling the workflow.

This design enables rapid iteration on AI instructions while maintaining strict compilation requirements for security-sensitive frontmatter configuration. See [Editing Workflows](/gh-aw/guides/editing-workflows/) for complete guidance on when recompilation is needed versus when you can edit directly.

## Related Documentation

- [Editing Workflows](/gh-aw/guides/editing-workflows/) - When to recompile vs edit directly
- [Workflow Structure](/gh-aw/reference/workflow-structure/) - Overall workflow file organization
- [Frontmatter](/gh-aw/reference/frontmatter/) - YAML configuration options
- [Security Guide](/gh-aw/introduction/architecture/) - Comprehensive security guidance
