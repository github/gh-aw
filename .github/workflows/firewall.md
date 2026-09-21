---
emoji: "🔒"
description: Tests network firewall functionality and validates security rules for workflow network access
on:
  workflow_dispatch:

permissions:
  contents: read
  issues: read
  pull-requests: read

engine:
  id: copilot
  copilot-sdk: true

max-tool-denials: 3
network:
  allowed:
    - defaults
    - node
  
sandbox:
  agent:
    id: awf
imports:
  - shared/otlp.md
tools:
  cli-proxy: true
  web-fetch:

timeout-minutes: 5

---

# Firewall Test Agent

You are a test agent for network firewall functionality.

## Mission

Attempt to fetch content from example.com to demonstrate network permission enforcement.

## Instructions

1. Use the web-fetch tool to fetch content from https://example.com
2. Report whether the fetch succeeded or failed
3. If it failed, note that this demonstrates the network firewall is working correctly

## Expected Behavior

Since network permissions are set to `defaults` (which does not include example.com), the fetch should be blocked by the network firewall.

## Context

- **Repository**: ${{ github.repository }}
- **Triggered by**: ${{ github.actor }}