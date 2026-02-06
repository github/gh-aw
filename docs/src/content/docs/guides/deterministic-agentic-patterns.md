---
title: Deterministic & Agentic Patterns
description: Learn how to combine deterministic computation steps with agentic reasoning in GitHub Agentic Workflows for powerful hybrid automation.
sidebar:
  order: 6
---

GitHub Agentic Workflows combine deterministic computation with AI reasoning, enabling data preprocessing, custom trigger filtering, and post-processing patterns.

## When to Use

Combine deterministic steps with AI agents to precompute data, filter triggers, preprocess inputs, post-process outputs, or build multi-stage computation and reasoning pipelines.

## Architecture

Define deterministic jobs in frontmatter alongside agentic execution:

```text
┌────────────────────────┐
│  Deterministic Jobs    │
│  - Data fetching       │
│  - Preprocessing       │
└───────────┬────────────┘
            │ artifacts/outputs
            ▼
┌────────────────────────┐
│   Agent Job (AI)       │
│   - Reasons & decides  │
└───────────┬────────────┘
            │ safe outputs
            ▼
┌────────────────────────┐
│  Safe Output Jobs      │
│  - GitHub API calls    │
└────────────────────────┘
```

## Precomputation Example

```yaml wrap title=".github/workflows/release-highlights.md"
---
on:
  push:
    tags: ['v*.*.*']
engine: copilot
safe-outputs:
  update-release:

steps:
  - run: |
      gh release view "${GITHUB_REF#refs/tags/}" --json name,tagName,body > /tmp/gh-aw/agent/release.json
      gh pr list --state merged --limit 100 --json number,title,labels > /tmp/gh-aw/agent/prs.json
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
---

# Release Highlights Generator

Generate release highlights for `${GITHUB_REF#refs/tags/}`. Analyze PRs in `/tmp/gh-aw/agent/prs.json`, categorize changes, and use update-release to prepend highlights to the release notes.
```

Files in `/tmp/gh-aw/agent/` are automatically uploaded as artifacts and available to the AI agent.

## Multi-Job Pattern

```yaml wrap title=".github/workflows/static-analysis.md"
---
on:
  schedule: daily
engine: claude
safe-outputs:
  create-discussion:

jobs:
  run-analysis:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - run: ./gh-aw compile --zizmor --poutine > /tmp/gh-aw/agent/analysis.txt

steps:
  - uses: actions/download-artifact@v6
    with:
      name: analysis-results
      path: /tmp/gh-aw/
---

# Static Analysis Report

Parse findings in `/tmp/gh-aw/agent/analysis.txt`, cluster by severity, and create a discussion with fix suggestions.
```

Pass data between jobs via artifacts, job outputs, or environment variables.

## Custom Trigger Filtering

```yaml wrap title=".github/workflows/smart-responder.md"
---
on:
  issues:
    types: [opened, edited]
engine: copilot
safe-outputs:
  add-comment:

steps:
  - id: filter
    run: |
      if echo "${{ github.event.issue.body }}" | grep -q "urgent"; then
        echo "priority=high" >> "$GITHUB_OUTPUT"
      else
        exit 1
      fi
---

# Smart Issue Responder

Respond to urgent issue: "${{ github.event.issue.title }}" (Priority: ${{ steps.filter.outputs.priority }})
```

## Post-Processing Pattern

```yaml wrap title=".github/workflows/code-review.md"
---
on:
  pull_request:
    types: [opened]
engine: copilot

safe-outputs:
  jobs:
    format-and-notify:
      description: "Format and post review"
      runs-on: ubuntu-latest
      inputs:
        summary: {required: true, type: string}
      steps:
        - run: |
            echo "## 🤖 AI Code Review\n\n${{ inputs.summary }}" > /tmp/report.md
            gh pr comment ${{ github.event.pull_request.number }} --body-file /tmp/report.md
          env:
            GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
---

# Code Review Agent

Review the pull request and use format-and-notify to post your summary.
```

## Importing Shared Instructions

Define reusable guidance in shared files and import them:

```yaml wrap title=".github/workflows/analysis.md"
---
on:
  schedule: daily
engine: copilot
imports:
  - shared/reporting.md
safe-outputs:
  create-discussion:
---

# Daily Analysis

Follow the report formatting guidelines from shared/reporting.md.
```

## Agent Data Directory

Use `/tmp/gh-aw/agent/` to share data with AI agents. Files here are automatically uploaded as artifacts and accessible to the agent:

```yaml
steps:
  - run: |
      gh api repos/${{ github.repository }}/issues > /tmp/gh-aw/agent/issues.json
      gh api repos/${{ github.repository }}/pulls > /tmp/gh-aw/agent/pulls.json
```

Reference in prompts: "Analyze issues in `/tmp/gh-aw/agent/issues.json` and PRs in `/tmp/gh-aw/agent/pulls.json`."

## Best Practices

**Data sharing**: Store data in `/tmp/gh-aw/agent/` for automatic artifact upload, or pass via environment variables (`echo "KEY=value" >> "$GITHUB_ENV"`).

**Job dependencies**: Use `needs: [job-name]` to define execution order between jobs.

## Related Documentation

- [Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/) - Custom post-processing jobs
- [Frontmatter Reference](/gh-aw/reference/frontmatter/) - Configuration options
- [Compilation Process](/gh-aw/reference/compilation-process/) - How jobs are orchestrated
- [Imports](/gh-aw/reference/imports/) - Sharing configurations across workflows
- [Templating](/gh-aw/reference/templating/) - Using GitHub Actions expressions
