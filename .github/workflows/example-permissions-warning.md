---
description: Example workflow demonstrating proper permission provisioning and security best practices
timeout-minutes: 5
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    toolsets: [repos, issues, pull_requests]
    read-only: false
strict: false
features:
  dangerous-permissions-write: true
imports:
  - shared/mood.md
---

# Example: Properly Provisioned Permissions

This workflow demonstrates properly configured permissions for GitHub toolsets.

The workflow uses three GitHub toolsets with appropriate write permissions:
- The `repos` toolset requires `contents: write` for repository operations
- The `issues` toolset requires `issues: write` for issue management
- The `pull_requests` toolset requires `pull-requests: write` for PR operations

All required permissions are properly declared in the frontmatter, so this workflow
compiles without warnings and can execute successfully when dispatched.
