---
emoji: "🪟"
description: Windows runner integration test that writes a haiku issue using the Docker agent runtime
on:
  slash_command:
    strategy: centralized
    name: windows
  workflow_dispatch:
    inputs:
      topic:
        description: "Topic for the haiku"
        required: false
        default: "Windows runners"
  workflow_call:
    inputs:
      topic:
        description: "Topic for the haiku"
        required: false
        type: string
        default: "Windows runners"

permissions:
  contents: read

  copilot-requests: write
concurrency:
  job-discriminator: ${{ github.run_id }}

runs-on: windows-latest

model: copilot/gpt-6-luna
engine:
  id: codex
  model-provider: github

network: {}

tools:
  bash:
    - "*"

evals:
  - id: topic_haiku
    question: Does the requested issue contain a three-line 5-7-5 haiku about the supplied topic, defaulting to Windows runners?
  - id: windows_runner_note
    question: Does the requested issue include a one-line note confirming the Windows runner context?
  - id: single_issue_scope
    question: Does the agent request exactly one issue without performing unrelated actions?
safe-outputs:
  create-issue:
    title-prefix: "[windows] "
    labels: [automation, ai-generated]
    max: 1
  report-missing-tool:

timeout-minutes: 10
strict: true
---

# Windows Runner Integration Test

You are running on a **Windows** GitHub Actions runner with the Docker agent runtime. This workflow exists to
validate that agentic workflows execute correctly on Windows.

## Context

- **Repository**: ${{ github.repository }}
- **Topic**: ${{ inputs.topic }}

## Your Task

1. Write a short haiku (three lines, 5-7-5 syllables) about the topic above.
2. Create a single issue containing:
   - The haiku.
   - A one-line note confirming the workflow ran on a Windows runner.

Keep the output minimal. Do not perform any other actions.
