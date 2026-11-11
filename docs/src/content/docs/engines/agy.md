---
title: Using Agy with GitHub Agentic Workflows
description: Connect a Gemini API key to the Antigravity CLI engine.
---

Agy runs Google's Antigravity CLI in GitHub Actions with gh-aw's sandbox and safe outputs.

> [!WARNING]
> Agy is Linux x64 only and supports Gemini API keys—not Google WIF, ADC, or Vertex AI. It cannot disable native shell, editing, or web tools. Keep [Gemini CLI](/gh-aw/engines/gemini/) if your workflow needs those restrictions or WIF.

## Connect your account

Add [`GEMINI_API_KEY`](/gh-aw/reference/auth/#gemini_api_key) as a repository secret. Select Agy in your workflow:

```yaml
engine: agy
```

The integration configures the native Gemini provider automatically. Use native Agy model names, not Copilot or `google/` prefixes.

## Create a workflow

Follow [Create a workflow](/gh-aw/setup/creating-workflows/), select `agy`, and start with a bounded task. Compile with `gh aw compile WORKFLOW`, then commit and push the source and generated lock file. No shared-engine import is required.

Review the [Agy reference](/gh-aw/reference/engines/agy/) before enabling an existing workflow: Agy is not feature-equivalent to Gemini.

## Evaluating a Gemini API-key workflow

An API-key workflow may be suitable if it needs none of Gemini's unsupported restrictions or customizations. Changing the engine is explicit; there is no automatic migration. Use the [migration checklist](/gh-aw/reference/engines/agy/#evaluating-a-gemini-api-key-workflow) and validate the new configuration before relying on it.

## Next steps

- [Agy technical reference](/gh-aw/reference/engines/agy/)
- [Compare engines](/gh-aw/reference/engines/#engine-feature-comparison)
- [Authentication](/gh-aw/reference/auth/)
- [Workflow examples](/gh-aw/gallery/)
