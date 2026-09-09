---
private: true
emoji: "🧪"
name: Smoke Call Workflow
description: Smoke test for the call-workflow safe output - orchestrator that calls a worker via workflow_call at compile-time fan-out
on:
  schedule: every 2 days
  slash_command:
    name: smoke-call-workflow
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["water"]
permissions:
  contents: read
  pull-requests: read
model: gpt-5.3-codex
engine:
  id: codex
strict: true
network:
  allowed:
    - defaults
safe-outputs:
  allowed-domains: [default-safe-outputs]
  call-workflow:
    workflows:
      - smoke-workflow-call
    max: 1
timeout-minutes: 20
imports:
  - shared/otlp.md
tools:
  cli-proxy: true
features:
  gh-aw-detection: false
sandbox:
  agent:
    runtime: cloud-hypervisor
    id: awf
---

# Smoke Test: Call Workflow Orchestrator

This workflow tests the `call-workflow` safe output by acting as an orchestrator that calls the `smoke-workflow-call` reusable worker.

## Task

Call the `smoke-workflow-call` worker workflow using the `call_workflow` MCP tool.
The worker will validate that the repository checkout works correctly in a `workflow_call` context.

## Instructions

1. Use the `smoke_workflow_call` MCP tool to select the `smoke-workflow-call` worker.
2. Pass `"smoke test checkout validation"` as the `task-description` input.

**Important**: You MUST call the `smoke_workflow_call` MCP tool with the `task-description` input. Do not use the `noop` tool.