---
name: custom-safe-output-max-length-test
description: Custom safe-output string input with an explicit length limit
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
timeout-minutes: 10
safe-outputs:
  jobs:
    publish-report:
      description: Publish a report with a larger payload
      inputs:
        payload:
          description: The complete report content
          required: true
          type: string
          maxLength: 40000
      steps:
        - run: echo "Report received"
---

# Mission

Publish the report content.
