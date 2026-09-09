---
private: true
emoji: "⚠️"
description: Example workflow demonstrating proper permission provisioning and security best practices
timeout-minutes: 5
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read


imports:
  - shared/otlp.md
tools:
  cli-proxy: true
  github:
    toolsets: [repos, issues, pull_requests]
engine:
  id: codex
  model-provider: openai
model: openai/gpt-5.3-codex
strict: false

sandbox:
  agent:
    runtime: cloud-hypervisor
---

# Example: Properly Provisioned Permissions

This workflow demonstrates properly configured permissions for GitHub toolsets.

The GitHub MCP server always operates in read-only mode. The workflow uses three
GitHub toolsets with read permissions:
- The `repos` toolset uses `contents: read` for repository operations
- The `issues` toolset uses `issues: read` for issue management
- The `pull_requests` toolset uses `pull-requests: read` for PR operations

All required permissions are properly declared in the frontmatter, so this workflow
compiles without warnings and can execute successfully when dispatched.