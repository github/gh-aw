---
name: Parallel fixture
on: workflow_dispatch
strict: false
engine:
  id: copilot
  model: ${{ experiments.model }}
model-routing: false
network:
  allowed: []
tools:
  github: false
experiments:
  storage: cache
  parallel: true
  model: [gpt-4.1, gpt-5]
  prompt: [concise, detailed]
safe-outputs:
  add-comment:
    max: 1
---
{{#if experiments.prompt == 'concise'}}
Be concise.
{{else}}
Explain your reasoning.
{{/if}}
Summarize the repository.
