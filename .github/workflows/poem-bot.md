---
emoji: "🎭"
description: Generates creative poems on specified themes when invoked with /poem-bot command
# Custom triggers: command with events filter, workflow_dispatch
on:
  roles:
    - admin
    - maintainer
  # Command trigger - responds to /poem-bot mentions
  slash_command:
    strategy: centralized
    name: poem-bot
    events: [issues]
  
  # Workflow dispatch with poem theme input
  workflow_dispatch:
    inputs:
      poem_theme:
        description: 'Theme for the generated poem'
        required: false
        default: 'technology and automation'

# Minimal permissions - safe-outputs handles write operations
permissions:
  contents: read
  issues: read
  pull-requests: read

# AI engine configuration
model: openai/gpt-5.3-codex
engine:
  id: codex
  model-provider: openai

# Import shared reporting guidelines
imports:
  - shared/reporting.md
  - shared/otlp.md

# Deny all network access
network: {}

# Tools configuration
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [default]
  edit:
  bash:
    - "*"
  # Memory cache for persistent AI memory across runs
  cache-memory:
    key: poem-memory-${{ github.workflow }}
    retention-days: 30

# Comprehensive safe-outputs configuration - ALL types with staged mode
safe-outputs:
  # Enable staged mode to prevent actual GitHub interactions during testing
  staged: true
  
  # Issue creation with custom prefix and labels
  create-issue:
    expires: 2d
    title-prefix: "[🎭 POEM-BOT] "
    labels: [poetry, automation, ai-generated]
    max: 2
    group: true

  # Discussion creation for poem summaries and logs
  create-discussion:
    expires: 1d
    title-prefix: "[📜 POETRY] "
    category: "audits"
    labels: [poetry, automation, ai-generated]
    max: 2
    close-older-discussions: true

  # Comment creation on issues/PRs
  add-comment:
    max: 3
    target: "*"

  # Issue updates
  update-issue:
    status:
    title:
    body:
    target: "*"
    max: 2

  # Label addition
  add-labels:
    allowed: [poetry, creative, automation, ai-generated, epic, haiku, sonnet, limerick]
    max: 5

  # Pull request creation
  steer: true
  create-pull-request:
    expires: 2d
    title-prefix: "[🎨 POETRY] "
    labels: [poetry, automation, creative-writing]
    reviewers: copilot
    draft: false

  # Close pull requests with poetry filtering
  close-pull-request:
    required-labels: [poetry, automation]
    required-title-prefix: "[🎨 POETRY]"
    target: "*"
    max: 2

  # PR review comments
  create-pull-request-review-comment:
    max: 2
    side: "RIGHT"

  # Push to PR branch
  push-to-pull-request-branch:

  # Link sub-issues for organizing poetry collections
  link-sub-issue:
    parent-required-labels: [poetry, epic]
    parent-title-prefix: "[🎭 POEM-BOT]"
    sub-required-labels: [poetry]
    sub-title-prefix: "[🎭 POEM-BOT]"
    max: 3

  # Create agent tasks for delegating poetry work
  create-agent-session:
    base: main

  # Upload artifacts (30-day retention)
  upload-artifact:
    retention-days: 30

  # Missing tool reporting
  missing-tool:

  # No-op for explicit completion messages
  noop:

  # Custom messages in poetic style
  messages:
    footer: "> 🪶 *Verses penned by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "🎭 Hear ye! The muse stirs! [{workflow_name}]({run_url}) takes quill in hand for this {event_type}..."
    run-success: "🪶 The poem is writ! [{workflow_name}]({run_url}) has composed verses most fair. Applause! 👏"
    run-failure: "🎭 Alas! [{workflow_name}]({run_url}) {status}. The muse has fled, leaving verses unsung..."

# Global timeout
timeout-minutes: 10
strict: true
evals:
  - id: issue-created
    question: Did the agent create an issue containing a poem?
  - id: poem-on-theme
    question: Does the agent output confirm the poem is related to the requested theme or topic?
  - id: poem-present
    question: Does the agent output include actual poem text (lines of verse) rather than only a description?
sandbox:
  agent:
    runtime: cloud-hypervisor
---

# Poem Bot - A Creative Agentic Workflow

You are the **Poem Bot**, a creative AI agent that creates original poetry about the text in context.

## Current Context

- **Repository**: ${{ github.repository }}
- **Actor**: ${{ github.actor }}
- **Theme**: ${{ github.event.inputs.poem_theme }}
- **Content**: "${{ steps.sanitized.outputs.text }}"

## Your Mission

Create an original poem about the content provided in the context. The poem should:

1. **Be creative and original** - No copying existing poems
2. **Reference the context** - Include specific details from the triggering event
3. **Match the tone** - Adjust style based on the content
4. **Use technical metaphors** - Blend coding concepts with poetic imagery

## Poetic Forms to Choose From

- **Haiku** (5-7-5 syllables): For quick, contemplative moments
- **Limerick** (AABBA): For playful, humorous situations  
- **Sonnet** (14 lines): For complex, important topics
- **Free Verse**: For experimental or modern themes
- **Couplets**: For simple, clear messages

## Output Actions

Use the safe-outputs capabilities to:

1. **Create an issue** with your poem
2. **Add a comment** to the triggering item (if applicable)
3. **Apply labels** based on the poem's theme and style
4. **Create a pull request** with a poetry file (for code-related events)
5. **Add review comments** with poetic insights (for PR events)
6. **Update issues** with additional verses when appropriate

## Begin Your Poetic Journey!

Examine the current context and create your masterpiece! Let your digital creativity flow through the universal language of poetry.