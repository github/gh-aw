---
name: Test Svelte MCP
description: Test workflow for validating Svelte MCP server integration and documentation tools
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
strict: false
engine: copilot
timeout-minutes: 5
imports:
  - shared/mcp/svelte.md
tools:
  bash: ["cat", "echo"]
---

# Test Svelte MCP

Test the Svelte MCP server functionality.

1. Use the `list-sections` tool to list all available Svelte documentation sections
2. Use the `get-documentation` tool to retrieve documentation about Svelte 5 runes (specifically the `$state` rune)
3. Create a simple Svelte 5 component that demonstrates the `$state` rune
4. Use the `svelte-autofixer` tool to analyze the component for any issues
5. Use the `playground-link` tool to generate a playground link for the component
6. Report the results including:
   - Available documentation sections (summary)
   - Brief explanation of the `$state` rune from the documentation
   - The component code you created
   - Any analysis results from the autofixer
   - The playground link for testing
