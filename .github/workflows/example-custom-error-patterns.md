---
on:
  issues:
    types: [opened]
    
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: copilot
  error_patterns:
    - pattern: 'CUSTOM_ERROR:\s+(.+)'
      level_group: 0
      message_group: 1
      description: "Custom project-specific error format"
    - pattern: '\[BUILD_FAILED\]\s+(.+)'
      level_group: 0
      message_group: 1
      description: "Build failure indicator"
imports:
  - shared/mood.md
---

# Example: Custom Error Patterns

This workflow demonstrates how to define custom error patterns on any agentic engine. 
Custom error patterns help detect project-specific error formats in agent logs.

## Features

- Works with any engine (Copilot, Claude, Codex, Custom)
- Can be imported from shared workflows
- Merged with engine's built-in error patterns
- Useful for project-specific error filtering
