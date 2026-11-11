---
title: Using Gemini CLI with GitHub Agentic Workflows
description: Authenticate Gemini CLI and run your first GitHub Agentic Workflow.
---

Run Gemini CLI workflows with a Gemini API key or Google Workload Identity Federation (WIF).

## Connect your account

> [!NOTE]
> Gemini is deprecated but remains supported. [Agy](/gh-aw/engines/agy/) supports a narrower API-key-only setup; keep Gemini for WIF or command-level shell restrictions.

Set `engine: gemini` and add a [`GEMINI_API_KEY`](/gh-aw/reference/auth/#gemini_api_key) repository secret, or configure [Google WIF](/gh-aw/reference/auth/#google-workload-identity-federation-wif).

The example grants Copilot access for safe-output threat detection, which uses Copilot rather than Gemini. Supply Copilot authentication or [configure a different detection engine](/gh-aw/reference/threat-detection/).

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
  copilot-requests: write
engine: gemini
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

Gemini supports command-level shell restrictions, but not native web search; use an [MCP search server](/gh-aw/reference/web-search/). See the [Gemini reference](/gh-aw/reference/engines/gemini/) for configuration and limitations.

## Next steps

- [Gemini CLI technical reference](/gh-aw/reference/engines/gemini/)
- [Create a workflow](/gh-aw/setup/creating-workflows/)
- [Workflow examples](/gh-aw/gallery/)
- [Compare engines](/gh-aw/reference/engines/#engine-feature-comparison)
