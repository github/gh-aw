---
name: Test Workflow
on:
  workflow_dispatch:
    inputs:
      test_param:
        description: 'Question from the dispatcher workflow'
        type: string
        required: true
permissions:
  contents: read
imports:
  - shared/mood.md
---

# Instructions for Test Workflow

This is the target workflow that will be triggered by the `test-dispatcher` workflow.

When this workflow is dispatched, it expects an input parameter named `test_param`, which is a question sent from the dispatcher workflow. Answer the question and print the response.
