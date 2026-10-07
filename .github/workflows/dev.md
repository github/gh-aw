---
private: true
emoji: "💻"
on:
  workflow_dispatch:
  label_command:
    name: dev
    strategy: decentralized
  schedule:
    - cron: 'daily around 9:00'  # ~9 AM UTC
name: Dev
description: Print an original haiku using Copilot
intent: Produce an original haiku about code, automation, or workflows without creating repository content.
timeout-minutes: 5
strict: false
engine: copilot
permissions:
  contents: read
  copilot-requests: write
concurrency:
  job-discriminator: ${{ github.run_id }}

safe-outputs:
  noop:
    max: 1
    report-as-issue: false

imports:
  - shared/otlp.md
tools:
  github: false

evals:
  - id: haiku_generated
    question: Did the agent produce an original three-line haiku about code, automation, or workflows with a 5-7-5 syllable pattern?
  - id: haiku_printed
    question: Did the agent record the haiku with noop and print the same three lines as its final response without a title or commentary?
---

# Copilot Haiku

Compose one original haiku about code, automation, or workflows.
Use exactly three lines with a 5-7-5 syllable pattern.

Call `noop` once with the haiku as its message, then print the same haiku as your
final response. Include only the three lines, without a title or commentary.

Do not inspect the repository, run shell commands, or create issues, comments,
pull requests, or files.