---
title: Using Claude Code with GitHub Agentic Workflows
description: Authenticate Claude Code and run your first GitHub Agentic Workflow.
---

Run Claude Code on GitHub events using an Anthropic API key or GitHub Copilot inference. A Claude subscription OAuth login is not supported.

## Connect your account

Set `engine: claude` and add an [`ANTHROPIC_API_KEY`](/gh-aw/reference/auth/#anthropic_api_key) repository secret. For keyless authentication, use [Anthropic WIF](/gh-aw/reference/auth/#anthropic-workload-identity-federation-wif).

To use Google Cloud billing and GCP WIF instead, configure [Claude on Vertex AI](/gh-aw/reference/auth/#claude-on-vertex-ai). This requires Model Garden access and the AWF Vertex adapter; no Anthropic key is needed.

To bill inference through Copilot instead, add `model: copilot/auto` and `permissions: { copilot-requests: write }`. No Anthropic key is needed.

## Run a daily report

After [setting up gh-aw](/gh-aw/setup/quick-start/), save this workflow:

```aw wrap title=".github/workflows/daily-status.md"
---
on:
  workflow_dispatch:
  schedule: daily
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  github:
    toolsets: [issues, pull_requests]
safe-outputs:
  create-issue:
    title-prefix: "[status] "
---

# Daily repository status

Summarize open issues and recent pull request activity in one concise issue.
```

Compile with `gh aw compile daily-status`, then commit and push both the Markdown and generated lock file. Run it from the repository's Actions tab or wait for its schedule.

## Capabilities and limitations

Claude supports native web search and command-level shell restrictions. Dynamic workflows, permission modes, bare mode, and provider-specific configuration are covered in the [Claude reference](/gh-aw/reference/engines/claude/).

## Next steps

- [Claude Code technical reference](/gh-aw/reference/engines/claude/)
- [Create a workflow](/gh-aw/setup/creating-workflows/)
- [Workflow examples](/gh-aw/gallery/)
- [Compare engines](/gh-aw/reference/engines/#engine-feature-comparison)
