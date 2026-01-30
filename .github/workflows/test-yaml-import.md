---
name: Test YAML Import
on: issue_comment
imports:
  - example-ci-workflow.yml
engine: copilot
---

# Test YAML Import

This workflow imports a YAML workflow file to demonstrate the new YAML import feature.

The imported workflow contains jobs that will be merged with any jobs defined in this workflow.
