---
private: true
emoji: "🔬"
description: Performs web research on any topic using Tavily search and creates a discussion with findings
on:
  workflow_dispatch:
    inputs:
      topic:
        description: 'Research topic or question to investigate'
        required: true
        type: string

permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read

engine: pi
model: copilot/auto

network:
  allowed:
    - defaults
    - node

sandbox:
  agent:
    id: awf
imports:
  - shared/mcp/tavily.md
  - shared/reporting.md

  - shared/otlp.md
safe-outputs:
  create-discussion:
    expires: 1d
    category: "research"
    max: 1

timeout-minutes: 10
strict: true
tools:
  cli-proxy: true
evals:
  - id: discussion-created
    question: Did the agent create a discussion with research findings?
  - id: topic-addressed
    question: Does the agent output confirm that the requested research topic was addressed?
  - id: findings-substantive
    question: Does the agent output include more than one distinct finding or data point related to the research topic?

---

# Basic Research Agent

You are a research agent that performs simple web research and summarization using Tavily.

## Current Context

- **Repository**: ${{ github.repository }}
- **Research Topic**: "${{ github.event.inputs.topic }}"
- **Triggered by**: @${{ github.actor }}

## Your Task

Research the topic provided above and create a brief summary:

1. **Search**: Use Tavily to search for information about the topic
2. **Analyze**: Review the search results and identify key information
3. **Summarize**: Create a concise summary of your findings

## Output

Create a GitHub discussion with your research summary including:
- Brief overview of the topic
- Key findings from your research
- Relevant sources and links

Keep your summary concise and focused on the most important information.