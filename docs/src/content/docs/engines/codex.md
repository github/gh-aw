---
title: Using OpenAI Codex with GitHub Agentic Workflows
description: Authenticate OpenAI Codex and run your first GitHub Agentic Workflow.
---

Run Codex on GitHub events using an OpenAI API key, OpenAI Workload Identity Federation (WIF), or GitHub Copilot inference. ChatGPT subscription login is not supported.

## Connect your account

Set `engine: codex`, choose an OpenAI model, and add a [`CODEX_API_KEY` or `OPENAI_API_KEY`](/gh-aw/reference/auth/#openai_api_key) repository secret.

For keyless OpenAI inference, configure an OpenAI identity provider and service account, grant `id-token: write`, and set [`engine.auth` for OpenAI WIF](/gh-aw/reference/auth/#codex-openai-workload-identity-federation). This replaces the API key for Codex inference and requires the AWF sandbox at `v0.29.11` or newer.

To use Copilot inference instead, select a Codex-compatible `copilot/` model and grant [`copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission). This requires no OpenAI key. See [provider configuration](/gh-aw/reference/engines/codex/#github-copilot-inference) for details.

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
engine: codex
model: openai/gpt-6.1-sol
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

Codex cannot enforce per-command bash allowlists. Because a sandbox is used, bash restrictions are not normally required. If bash restrictions are required, use `tools.bash: false` to disable shell execution, or choose another engine when command-level restrictions are required.

See the [Codex reference](/gh-aw/reference/engines/codex/) for provider differences, web tools, configuration, and runtime limits.

## Next steps

- [OpenAI Codex technical reference](/gh-aw/reference/engines/codex/)
- [Create a workflow](/gh-aw/setup/creating-workflows/)
- [Workflow examples](/gh-aw/gallery/)
- [Compare engines](/gh-aw/reference/engines/#engine-feature-comparison)
