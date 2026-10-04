---
description: Fixture workflow demonstrating a workflow_call input used to set engine.version via a runtime expression.
on:
  workflow_call:
    inputs:
      engine-version:
        type: string
engine:
  id: copilot
  version: ${{ inputs.engine-version }}
---
Fix the bug
