---
name: Test YAML Import
on: issue_comment
imports:
  - license-check.yml
engine: copilot
---

# Test YAML Import

This workflow imports the existing License Check workflow (license-check.yml) to demonstrate the YAML import feature.

The imported workflow contains a job (license-check) that will be merged with any jobs defined in this workflow.
